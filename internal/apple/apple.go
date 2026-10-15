// Package apple verifies Sign in with Apple identity tokens.
package apple

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	Issuer  = "https://appleid.apple.com"
	KeysURL = "https://appleid.apple.com/auth/keys"
)

// Verifier checks RS256 identity tokens issued to one app (the bundle id).
type Verifier struct {
	BundleID string
	KeysURL  string // default KeysURL; tests point it at a fake
	Client   *http.Client
	Now      func() time.Time

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

type Identity struct {
	Sub   string
	Email string
}

type claims struct {
	Iss   string `json:"iss"`
	Aud   string `json:"aud"`
	Sub   string `json:"sub"`
	Exp   int64  `json:"exp"`
	Nonce string `json:"nonce"`
	Email string `json:"email"`
}

func (v *Verifier) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

// Verify returns the Apple ID in token. nonce is the raw string whose SHA-256
// hex the app gave Apple; a token minted for another request is refused.
func (v *Verifier) Verify(ctx context.Context, token, nonce string) (Identity, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Identity{}, errors.New("not a JWT")
	}
	var hdr struct{ Alg, Kid string }
	if err := decode(parts[0], &hdr); err != nil {
		return Identity{}, fmt.Errorf("header: %w", err)
	}
	if hdr.Alg != "RS256" {
		return Identity{}, errors.New("unsupported signing algorithm")
	}
	key, err := v.key(ctx, hdr.Kid)
	if err != nil {
		return Identity{}, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Identity{}, errors.New("bad signature encoding")
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
		return Identity{}, errors.New("signature does not match")
	}
	var c claims
	if err := decode(parts[1], &c); err != nil {
		return Identity{}, fmt.Errorf("claims: %w", err)
	}
	switch {
	case c.Iss != Issuer:
		return Identity{}, errors.New("wrong issuer")
	case c.Aud != v.BundleID:
		return Identity{}, errors.New("token was issued to another app")
	case c.Exp <= v.now().Unix():
		return Identity{}, errors.New("token expired")
	case c.Sub == "":
		return Identity{}, errors.New("token has no subject")
	}
	want := sha256.Sum256([]byte(nonce))
	if nonce == "" || c.Nonce != hex.EncodeToString(want[:]) {
		return Identity{}, errors.New("nonce does not match")
	}
	return Identity{Sub: c.Sub, Email: c.Email}, nil
}

func decode(seg string, into any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, into)
}

// key returns the public key for kid, refetching Apple's set when the id is
// unknown (they rotate) or the cache is old.
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if k, ok := v.keys[kid]; ok && v.now().Sub(v.fetched) < time.Hour {
		return k, nil
	}
	if v.keys != nil && v.now().Sub(v.fetched) < time.Minute {
		return nil, errors.New("unknown signing key")
	}
	url := v.KeysURL
	if url == "" {
		url = KeysURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c := v.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching Apple keys: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching Apple keys: %s", resp.Status)
	}
	var set struct {
		Keys []struct{ Kid, Kty, N, E string } `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return nil, err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	v.keys, v.fetched = keys, v.now()
	if k, ok := keys[kid]; ok {
		return k, nil
	}
	return nil, errors.New("unknown signing key")
}
