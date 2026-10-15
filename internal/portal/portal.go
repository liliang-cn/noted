// Package portal is the small web page where people sign up or sign in with an
// email and password and mint the tokens their AI assistants connect with.
//
// The JSON API under /api/ calls the same AccountService implementation as
// gRPC, in process, so every rule (labels, password length, lockout, who may
// see which token) lives in one place. A browser session is an ordinary noted
// token labelled "网页", kept in an httpOnly cookie; logging out revokes it.
package portal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// CookieName holds the session token.
const CookieName = "noted_session"

// SessionLabel names the token behind a browser session.
const SessionLabel = "网页"

// CSRFHeader must be sent (with value "1") on every non-GET /api/ call. A
// cross-site form cannot set it, and a cross-site script cannot send it
// without a CORS preflight this server never grants.
const CSRFHeader = "X-Noted"

type Options struct {
	Auth    *auth.Authenticator
	Account *service.Account
	// PublicURL is the origin clients reach this server at; empty: each
	// request's own (see BaseURL).
	PublicURL string
}

type portal struct {
	auth   *auth.Authenticator
	acct   *service.Account
	public string
}

// New returns the handler for "/" (the page and its assets) and "/api/".
func New(o Options) http.Handler {
	p := &portal{auth: o.Auth, acct: o.Account, public: o.PublicURL}
	site := mustLoadSite()
	site.public = o.PublicURL

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/signup", p.signup)
	mux.HandleFunc("POST /api/login", p.login)
	mux.HandleFunc("POST /api/logout", p.logout)
	mux.HandleFunc("GET /api/me", p.session(p.me))
	mux.HandleFunc("GET /api/tokens", p.session(p.listTokens))
	mux.HandleFunc("POST /api/tokens", p.session(p.createToken))
	mux.HandleFunc("DELETE /api/tokens/{id}", p.session(p.revokeToken))
	mux.HandleFunc("POST /api/password", p.session(p.password))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeErr(w, http.StatusNotFound, "no such API")
	})
	mux.Handle("/", site)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
			if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(CSRFHeader) != "1" {
				writeErr(w, http.StatusForbidden, "missing "+CSRFHeader+" header")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		}
		mux.ServeHTTP(w, r)
	})
}

// ---- helpers ----

func clientCtx(r *http.Request) context.Context {
	return service.WithClientIP(r.Context(), service.TrustedClientIP(r.RemoteAddr, r.Header.Values("X-Forwarded-For")))
}

func secure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// BaseURL is the origin clients reach this server at, without a trailing
// slash: public when configured, else the request's scheme (X-Forwarded-Proto
// counts) and host (X-Forwarded-Host counts when a proxy on a private network
// sent it).
func BaseURL(r *http.Request, public string) string {
	if public != "" {
		return strings.TrimRight(public, "/")
	}
	scheme := "http"
	if secure(r) {
		scheme = "https"
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" && fromProxy(r.RemoteAddr) {
		host = strings.TrimSpace(strings.Split(fh, ",")[0])
	}
	return scheme + "://" + host
}

func fromProxy(remote string) bool {
	h, _, err := net.SplitHostPort(remote)
	if err != nil {
		h = remote
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func setSession(w http.ResponseWriter, r *http.Request, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: tok, Path: "/", HttpOnly: true, Secure: secure(r),
		SameSite: http.SameSiteStrictMode, MaxAge: int((180 * 24 * time.Hour).Seconds()),
	})
}

func clearSession(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: secure(r),
		SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
}

type authed func(w http.ResponseWriter, r *http.Request, ctx context.Context)

// session resolves the cookie into a signed-in context, exactly as a gRPC
// call with that token would be.
func (p *portal) session(h authed) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil || c.Value == "" {
			writeErr(w, http.StatusUnauthorized, "not signed in")
			return
		}
		u, id, err := p.auth.Verify(r.Context(), c.Value)
		if errors.Is(err, auth.ErrInvalidToken) {
			clearSession(w, r)
			writeErr(w, http.StatusUnauthorized, "not signed in")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "authentication failed")
			return
		}
		h(w, r, auth.WithToken(clientCtx(r), u, id))
	}
}

func readJSON(r *http.Request, v any) error {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}

var pj = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}

func writeProto(w http.ResponseWriter, m proto.Message) {
	b, err := pj.Marshal(m)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

var httpCode = map[codes.Code]int{
	codes.InvalidArgument:    http.StatusBadRequest,
	codes.Unauthenticated:    http.StatusUnauthorized,
	codes.PermissionDenied:   http.StatusForbidden,
	codes.FailedPrecondition: http.StatusForbidden,
	codes.NotFound:           http.StatusNotFound,
	codes.AlreadyExists:      http.StatusConflict,
	codes.ResourceExhausted:  http.StatusTooManyRequests,
}

// writeStatus turns a service error into an HTTP error with its message.
func writeStatus(w http.ResponseWriter, err error) {
	st := status.Convert(err)
	code, ok := httpCode[st.Code()]
	if !ok {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeErr(w, code, st.Message())
}

// ---- handlers ----

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (p *portal) signup(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := readJSON(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, "bad JSON")
		return
	}
	res, err := p.acct.SignUp(clientCtx(r), &pb.SignUpRequest{Email: c.Email, Password: c.Password, DeviceLabel: SessionLabel})
	if err != nil {
		writeStatus(w, err)
		return
	}
	setSession(w, r, res.Token)
	writeJSON(w, http.StatusOK, map[string]string{"user_name": res.UserName})
}

func (p *portal) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := readJSON(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, "bad JSON")
		return
	}
	res, err := p.acct.SignIn(clientCtx(r), &pb.SignInRequest{Email: c.Email, Password: c.Password, DeviceLabel: SessionLabel})
	if err != nil {
		writeStatus(w, err)
		return
	}
	setSession(w, r, res.Token)
	writeJSON(w, http.StatusOK, map[string]string{"user_name": res.UserName})
}

func (p *portal) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		if u, id, err := p.auth.Verify(r.Context(), c.Value); err == nil {
			_, _ = p.acct.RevokeToken(auth.WithToken(r.Context(), u, id), &pb.RevokeTokenRequest{Id: id})
		}
	}
	clearSession(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (p *portal) me(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	a, err := p.acct.GetAccount(ctx, &pb.GetAccountRequest{})
	if err != nil {
		writeStatus(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_name": a.UserName, "email": a.Email,
		"mcp_url": BaseURL(r, p.public) + "/mcp",
	})
}

func (p *portal) listTokens(w http.ResponseWriter, _ *http.Request, ctx context.Context) {
	res, err := p.acct.ListTokens(ctx, &pb.ListTokensRequest{})
	if err != nil {
		writeStatus(w, err)
		return
	}
	writeProto(w, res)
}

func (p *portal) createToken(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	var in struct {
		Label string `json:"label"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad JSON")
		return
	}
	res, err := p.acct.CreateToken(ctx, &pb.CreateTokenRequest{Label: in.Label})
	if err != nil {
		writeStatus(w, err)
		return
	}
	writeProto(w, res)
}

func (p *portal) revokeToken(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	id := r.PathValue("id")
	if _, err := p.acct.RevokeToken(ctx, &pb.RevokeTokenRequest{Id: id}); err != nil {
		writeStatus(w, err)
		return
	}
	if id == auth.TokenID(ctx) {
		clearSession(w, r) // revoked the browser's own session
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (p *portal) password(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	var in struct {
		Email           string `json:"email"`
		Password        string `json:"password"`
		CurrentPassword string `json:"current_password"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad JSON")
		return
	}
	a, err := p.acct.SetEmailPassword(ctx, &pb.SetEmailPasswordRequest{Email: in.Email, Password: in.Password, CurrentPassword: in.CurrentPassword})
	if err != nil {
		writeStatus(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"user_name": a.UserName, "email": a.Email})
}
