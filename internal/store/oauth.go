package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// How long a browser sign-in waits for the App, how long the code it hands the
// client stays redeemable, and how long an ended pairing is kept so its page
// can still say what happened.
const (
	PairingTTL  = 10 * time.Minute
	AuthCodeTTL = 5 * time.Minute
	pairingKeep = time.Hour
	// A client that registered but never started a sign-in is dropped after this.
	unusedClientTTL = 24 * time.Hour
)

// OAuthClient is an MCP client that registered itself (RFC 7591). All are
// public clients: no secret, PKCE instead.
type OAuthClient struct {
	ID           string
	Name         string
	RedirectURIs []string
	Created      time.Time
}

func (s *Store) CreateOAuthClient(ctx context.Context, name string, redirectURIs []string) (OAuthClient, error) {
	uris, err := json.Marshal(redirectURIs)
	if err != nil {
		return OAuthClient{}, err
	}
	now := time.Now()
	_, _ = s.db.ExecContext(ctx, `DELETE FROM oauth_clients WHERE last_used_ms IS NULL AND created_ms < ?`, ms(now.Add(-unusedClientTTL)))
	c := OAuthClient{ID: uuid.NewString(), Name: name, RedirectURIs: redirectURIs, Created: fromMS(ms(now))}
	_, err = s.db.ExecContext(ctx, `INSERT INTO oauth_clients(id, name, redirect_uris, created_ms) VALUES (?, ?, ?, ?)`,
		c.ID, c.Name, string(uris), ms(now))
	return c, err
}

func (s *Store) OAuthClient(ctx context.Context, id string) (OAuthClient, error) {
	var c OAuthClient
	var uris string
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id, name, redirect_uris, created_ms FROM oauth_clients WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &uris, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthClient{}, ErrNotFound
	}
	if err != nil {
		return OAuthClient{}, err
	}
	c.Created = fromMS(created)
	return c, json.Unmarshal([]byte(uris), &c.RedirectURIs)
}

// Pairing is one browser sign-in of an OAuth client, waiting for a signed-in
// App (or browser session) to approve it by its code.
type Pairing struct {
	Code          string // normalised, e.g. "K7QM29XD"
	ClientID      string
	ClientName    string
	RedirectURI   string
	CodeChallenge string // S256
	State         string
	Resource      string
	Created       time.Time
	Expires       time.Time
	UserID        string // who approved it; empty while pending
	Label         string // the label of the token it will get
	Denied        bool
	Issued        bool // its authorization code went to the client; the pairing is used up
}

// Live reports whether the pairing can still be approved or denied.
func (p Pairing) Live(now time.Time) bool { return !p.Denied && !p.Issued && now.Before(p.Expires) }

// The pairing alphabet leaves out 0/O, 1/I/L so a code read off a screen is
// typed back right.
const pairingAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

const pairingCodeLen = 8

func newPairingCode() (string, error) {
	out := make([]byte, 0, pairingCodeLen)
	buf := make([]byte, 16)
	limit := byte(256 - 256%len(pairingAlphabet)) // reject the tail so every letter is equally likely
	for len(out) < pairingCodeLen {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b < limit && len(out) < pairingCodeLen {
				out = append(out, pairingAlphabet[int(b)%len(pairingAlphabet)])
			}
		}
	}
	return string(out), nil
}

// CreatePairing starts a pairing for p (its client, redirect URI, challenge,
// state and resource) and returns it with the secret its browser page polls
// with. Ended pairings and spent codes are cleaned up on the way.
func (s *Store) CreatePairing(ctx context.Context, p Pairing) (Pairing, string, error) {
	now := time.Now()
	_, _ = s.db.ExecContext(ctx, `DELETE FROM pairings WHERE expires_ms < ?`, ms(now.Add(-pairingKeep)))
	_, _ = s.db.ExecContext(ctx, `DELETE FROM oauth_codes WHERE expires_ms < ?`, ms(now))

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Pairing{}, "", err
	}
	poll := hex.EncodeToString(raw)
	p.Created = fromMS(ms(now))
	p.Expires = fromMS(ms(now.Add(PairingTTL)))
	p.UserID, p.Label, p.Denied, p.Issued = "", "", false, false
	for range 5 { // a collision among 31^8 codes is all but impossible; retry anyway
		code, err := newPairingCode()
		if err != nil {
			return Pairing{}, "", err
		}
		res, err := s.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO pairings(code, poll_hash, client_id, redirect_uri, code_challenge, state, resource, created_ms, expires_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			code, hashToken(poll), p.ClientID, p.RedirectURI, p.CodeChallenge, p.State, p.Resource, ms(p.Created), ms(p.Expires))
		if err != nil {
			return Pairing{}, "", err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			p.Code = code
			_, _ = s.db.ExecContext(ctx, `UPDATE oauth_clients SET last_used_ms = ? WHERE id = ?`, ms(now), p.ClientID)
			if p.ClientName == "" {
				_ = s.db.QueryRowContext(ctx, `SELECT name FROM oauth_clients WHERE id = ?`, p.ClientID).Scan(&p.ClientName)
			}
			return p, poll, nil
		}
	}
	return Pairing{}, "", errors.New("could not allocate a pairing code")
}

const pairingCols = `p.code, p.client_id, c.name, p.redirect_uri, p.code_challenge, p.state, p.resource,
	p.created_ms, p.expires_ms, COALESCE(p.user_id, ''), p.label, p.denied, p.issued
	FROM pairings p JOIN oauth_clients c ON c.id = p.client_id`

// liveCond selects pairings that can still be approved or denied.
const liveCond = `denied = 0 AND issued = 0 AND expires_ms > ?`

func (s *Store) pairing(ctx context.Context, where string, args ...any) (Pairing, error) {
	var p Pairing
	var created, expires int64
	err := s.db.QueryRowContext(ctx, `SELECT `+pairingCols+` WHERE `+where, args...).Scan(
		&p.Code, &p.ClientID, &p.ClientName, &p.RedirectURI, &p.CodeChallenge, &p.State, &p.Resource,
		&created, &expires, &p.UserID, &p.Label, &p.Denied, &p.Issued)
	if errors.Is(err, sql.ErrNoRows) {
		return Pairing{}, ErrNotFound
	}
	if err != nil {
		return Pairing{}, err
	}
	p.Created, p.Expires = fromMS(created), fromMS(expires)
	return p, nil
}

// PairingByPoll returns the pairing a browser page polls, in whatever state.
func (s *Store) PairingByPoll(ctx context.Context, poll string) (Pairing, error) {
	return s.pairing(ctx, `p.poll_hash = ?`, hashToken(poll))
}

// LivePairing returns a pairing that the user may still approve or deny: one
// nobody has approved yet, or that this user approved. Anything else, expired,
// denied or used included, is ErrNotFound.
func (s *Store) LivePairing(ctx context.Context, code, userID string) (Pairing, error) {
	return s.pairing(ctx, `p.code = ? AND p.denied = 0 AND p.issued = 0 AND p.expires_ms > ? AND (p.user_id IS NULL OR p.user_id = ?)`,
		code, ms(time.Now()), userID)
}

// ApprovePairing binds a live pairing to the user; its client gets a token of
// that user, labelled label, when it redeems the code. Approving twice is fine.
func (s *Store) ApprovePairing(ctx context.Context, code, userID, label string) (Pairing, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE pairings SET user_id = ?, label = ?
		WHERE code = ? AND `+liveCond+` AND (user_id IS NULL OR user_id = ?)`,
		userID, label, code, ms(time.Now()), userID)
	if err != nil {
		return Pairing{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Pairing{}, ErrNotFound
	}
	return s.LivePairing(ctx, code, userID)
}

// DenyPairing ends a live pairing that is unapproved or approved by userID.
func (s *Store) DenyPairing(ctx context.Context, code, userID string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE pairings SET denied = 1
		WHERE code = ? AND `+liveCond+` AND (user_id IS NULL OR user_id = ?)`, code, ms(time.Now()), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DenyPairingByPoll ends a live pairing from its own browser page.
func (s *Store) DenyPairingByPoll(ctx context.Context, poll string) (Pairing, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE pairings SET denied = 1 WHERE poll_hash = ? AND `+liveCond,
		hashToken(poll), ms(time.Now()))
	if err != nil {
		return Pairing{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Pairing{}, ErrNotFound
	}
	return s.PairingByPoll(ctx, poll)
}

// AuthCode is what an authorization code stands for until it is redeemed.
type AuthCode struct {
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	Resource      string
	UserID        string
	Label         string
	Expires       time.Time
}

// IssueAuthCode turns an approved, live pairing into a single-use
// authorization code, once: the pairing is used up. ErrNotFound when the
// pairing is not approved, or is denied, expired or already used.
func (s *Store) IssueAuthCode(ctx context.Context, poll string) (Pairing, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Pairing{}, "", err
	}
	code := hex.EncodeToString(raw)
	now := time.Now()
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE pairings SET issued = 1 WHERE poll_hash = ? AND user_id IS NOT NULL AND `+liveCond,
			hashToken(poll), ms(now))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO oauth_codes(hash, client_id, redirect_uri, code_challenge, resource, user_id, label, expires_ms)
			SELECT ?, client_id, redirect_uri, code_challenge, resource, user_id, label, ? FROM pairings WHERE poll_hash = ?`,
			hashToken(code), ms(now.Add(AuthCodeTTL)), hashToken(poll))
		return err
	})
	if err != nil {
		return Pairing{}, "", err
	}
	p, err := s.PairingByPoll(ctx, poll)
	return p, code, err
}

// RedeemAuthCode consumes an authorization code: it works once, and only
// before it expires.
func (s *Store) RedeemAuthCode(ctx context.Context, code string) (AuthCode, error) {
	var a AuthCode
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var expires int64
		h := hashToken(code)
		err := tx.QueryRowContext(ctx, `
			SELECT client_id, redirect_uri, code_challenge, resource, user_id, label, expires_ms FROM oauth_codes WHERE hash = ?`, h).
			Scan(&a.ClientID, &a.RedirectURI, &a.CodeChallenge, &a.Resource, &a.UserID, &a.Label, &expires)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_codes WHERE hash = ?`, h); err != nil {
			return err
		}
		a.Expires = fromMS(expires)
		if !time.Now().Before(a.Expires) {
			return ErrNotFound
		}
		return nil
	})
	return a, err
}
