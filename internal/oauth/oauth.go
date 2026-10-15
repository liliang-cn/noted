// Package oauth lets an MCP client sign in without a pasted token, the way
// the MCP authorization spec (2025-06-18) describes: protected-resource
// metadata (RFC 9728), authorization-server metadata (RFC 8414), dynamic
// client registration (RFC 7591), and the authorization-code grant with PKCE
// S256 (OAuth 2.1).
//
// The authorization step is a pairing: the browser page shows a code and a QR
// code, and the person approves it in the signed-in App (AccountService.
// ApprovePairing) or, when this browser is signed in to the web portal, right
// on the page. The access token the client gets is an ordinary noted token of
// that user, labelled with the client's name, so it is listed and revoked
// with the others. There are no refresh tokens; a revoked token makes the
// client sign in again.
package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/portal"
	"github.com/liliang-cn/noted/internal/service"
	"github.com/liliang-cn/noted/internal/store"
)

// Endpoint paths. The metadata paths are fixed by the RFCs.
const (
	ResourceMetadataPath = "/.well-known/oauth-protected-resource"
	ServerMetadataPath   = "/.well-known/oauth-authorization-server"
	AuthorizePath        = "/oauth/authorize"
	TokenPath            = "/oauth/token"
	RegisterPath         = "/oauth/register"
	pairPath             = "/oauth/pair"
)

type Options struct {
	Auth    *auth.Authenticator
	Account *service.Account
	// ResourcePath is the protected MCP endpoint, e.g. "/mcp".
	ResourcePath string
	// PublicURL is the origin clients reach this server at; empty: each
	// request's own (portal.BaseURL).
	PublicURL string
}

type server struct {
	auth     *auth.Authenticator
	acct     *service.Account
	st       *store.Store
	resource string
	public   string
	assets   map[string]*asset
}

// New returns the handler for /.well-known/ and /oauth/.
func New(o Options) http.Handler {
	s := &server{auth: o.Auth, acct: o.Account, st: o.Account.Store, resource: o.ResourcePath, public: o.PublicURL, assets: loadAssets()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+ResourceMetadataPath, s.resourceMetadata)
	mux.HandleFunc("GET "+ResourceMetadataPath+o.ResourcePath, s.resourceMetadata)
	mux.HandleFunc("GET "+ServerMetadataPath, s.serverMetadata)
	mux.HandleFunc("GET /.well-known/openid-configuration", s.serverMetadata)
	mux.HandleFunc("POST "+RegisterPath, s.register)
	mux.HandleFunc("POST "+TokenPath, s.token)
	mux.HandleFunc("GET "+AuthorizePath, s.authorize)
	mux.HandleFunc("GET "+pairPath, s.pairPage)
	mux.HandleFunc("GET "+pairPath+"/status", s.pairStatus)
	mux.HandleFunc("POST "+pairPath+"/approve", s.pairApprove)
	mux.HandleFunc("POST "+pairPath+"/deny", s.pairDeny)
	mux.HandleFunc("GET /oauth/{asset}", s.serveAsset)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// Metadata, registration and the token endpoint are called by
		// clients, browser-based ones included; none of them reads a cookie.
		if strings.HasPrefix(r.URL.Path, "/.well-known/") || r.URL.Path == RegisterPath || r.URL.Path == TokenPath {
			h.Set("Access-Control-Allow-Origin", "*")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Protocol-Version")
				h.Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		}
		mux.ServeHTTP(w, r)
	})
}

// ResourceMetadataURL is where a 401 from the MCP endpoint points clients
// (the resource_metadata of its WWW-Authenticate header).
func ResourceMetadataURL(base string) string { return base + ResourceMetadataPath }

func (s *server) base(r *http.Request) string { return portal.BaseURL(r, s.public) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// oauthErr writes an RFC 6749 error response.
func oauthErr(w http.ResponseWriter, code int, kind, desc string) {
	writeJSON(w, code, map[string]string{"error": kind, "error_description": desc})
}

// ---- metadata ----

func (s *server) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.base(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 base + s.resource,
		"authorization_servers":    []string{base},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "noted",
	})
}

func (s *server) serverMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.base(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + AuthorizePath,
		"token_endpoint":                        base + TokenPath,
		"registration_endpoint":                 base + RegisterPath,
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
	})
}

// ---- dynamic client registration ----

const (
	maxClientName   = 60
	maxRedirectURIs = 10
	defaultClient   = "MCP 客户端"
)

// checkRedirectURI accepts https URLs and, for native apps (RFC 8252), plain
// http on a loopback host.
func checkRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return errors.New("redirect URI " + raw + " is not an absolute URL")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return errors.New("redirect URI " + raw + " must not have a fragment")
	}
	switch {
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && isLoopback(u.Hostname()):
		return nil
	}
	return errors.New("redirect URI " + raw + " must be https, or http on localhost, 127.0.0.1 or [::1]")
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *server) register(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var md struct {
		RedirectURIs  []string `json:"redirect_uris"`
		ClientName    string   `json:"client_name"`
		GrantTypes    []string `json:"grant_types"`
		ResponseTypes []string `json:"response_types"`
	}
	if err := json.NewDecoder(r.Body).Decode(&md); err != nil {
		oauthErr(w, http.StatusBadRequest, "invalid_client_metadata", "the body must be a JSON object of client metadata")
		return
	}
	if len(md.RedirectURIs) == 0 || len(md.RedirectURIs) > maxRedirectURIs {
		oauthErr(w, http.StatusBadRequest, "invalid_redirect_uri", "give 1 to 10 redirect_uris")
		return
	}
	for _, u := range md.RedirectURIs {
		if err := checkRedirectURI(u); err != nil {
			oauthErr(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
	}
	if len(md.GrantTypes) > 0 && !contains(md.GrantTypes, "authorization_code") {
		oauthErr(w, http.StatusBadRequest, "invalid_client_metadata", "only the authorization_code grant is supported")
		return
	}
	if len(md.ResponseTypes) > 0 && !contains(md.ResponseTypes, "code") {
		oauthErr(w, http.StatusBadRequest, "invalid_client_metadata", "only the code response type is supported")
		return
	}
	name := strings.TrimSpace(md.ClientName)
	if name == "" {
		name = defaultClient
	}
	if utf8.RuneCountInString(name) > maxClientName {
		name = string([]rune(name)[:maxClientName])
	}
	c, err := s.st.CreateOAuthClient(r.Context(), name, md.RedirectURIs)
	if err != nil {
		slog.Error("oauth: register client", "err", err)
		oauthErr(w, http.StatusInternalServerError, "server_error", "could not register the client")
		return
	}
	// Whatever was asked for, the client is public and uses only the code grant.
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  c.ID,
		"client_id_issued_at":        c.Created.Unix(),
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// ---- token endpoint ----

// pkceS256 reports whether verifier hashes to challenge (RFC 7636).
func pkceS256(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, c := range verifier {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", c)) {
			return false
		}
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

// sameResource compares two resource URIs, ignoring the case of scheme and
// host and a trailing slash.
func sameResource(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) && strings.EqualFold(ua.Host, ub.Host) &&
		strings.TrimRight(ua.Path, "/") == strings.TrimRight(ub.Path, "/") && ua.RawQuery == ub.RawQuery
}

// resourceOK accepts the MCP endpoint, or this server as a whole.
func (s *server) resourceOK(r *http.Request, resource string) bool {
	base := s.base(r)
	return sameResource(resource, base+s.resource) || sameResource(resource, base)
}

func (s *server) token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if err := r.ParseForm(); err != nil {
		oauthErr(w, http.StatusBadRequest, "invalid_request", "the body must be application/x-www-form-urlencoded")
		return
	}
	f := r.PostForm
	if gt := f.Get("grant_type"); gt != "authorization_code" {
		oauthErr(w, http.StatusBadRequest, "unsupported_grant_type", "only grant_type=authorization_code is supported")
		return
	}
	code, clientID := f.Get("code"), f.Get("client_id")
	if code == "" || clientID == "" || f.Get("code_verifier") == "" {
		oauthErr(w, http.StatusBadRequest, "invalid_request", "code, client_id and code_verifier are required")
		return
	}
	// Redeeming burns the code, so a failed attempt below cannot be retried
	// with another verifier.
	ac, err := s.st.RedeemAuthCode(r.Context(), code)
	if errors.Is(err, store.ErrNotFound) {
		oauthErr(w, http.StatusBadRequest, "invalid_grant", "the authorization code is invalid, expired or already used")
		return
	}
	if err != nil {
		slog.Error("oauth: redeem code", "err", err)
		oauthErr(w, http.StatusInternalServerError, "server_error", "could not redeem the code")
		return
	}
	switch {
	case ac.ClientID != clientID:
		oauthErr(w, http.StatusBadRequest, "invalid_grant", "the code was issued to another client")
		return
	case f.Get("redirect_uri") != ac.RedirectURI:
		oauthErr(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the authorization request")
		return
	case !pkceS256(f.Get("code_verifier"), ac.CodeChallenge):
		oauthErr(w, http.StatusBadRequest, "invalid_grant", "code_verifier does not match the code challenge")
		return
	}
	if res := f.Get("resource"); res != "" && !(ac.Resource == "" && s.resourceOK(r, res) || ac.Resource != "" && sameResource(res, ac.Resource)) {
		oauthErr(w, http.StatusBadRequest, "invalid_target", "resource does not match the authorization request")
		return
	}
	tok, t, err := s.st.NewToken(r.Context(), ac.UserID, ac.Label)
	if err != nil {
		slog.Error("oauth: mint token", "err", err)
		oauthErr(w, http.StatusInternalServerError, "server_error", "could not issue a token")
		return
	}
	slog.Info("oauth: token issued", "client", ac.ClientID, "token_id", t.ID, "label", t.Label)
	writeJSON(w, http.StatusOK, map[string]string{"access_token": tok, "token_type": "Bearer"})
}

// redirectWith appends params to a redirect URI's query.
func redirectWith(uri string, params url.Values) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	q := u.Query()
	for k, v := range params {
		if len(v) > 0 && v[0] != "" {
			q[k] = v
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func clientDenied(p store.Pairing, desc string) string {
	return redirectWith(p.RedirectURI, url.Values{"error": {"access_denied"}, "error_description": {desc}, "state": {p.State}})
}
