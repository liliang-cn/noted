// Package mcphttp serves noted's MCP tools over Streamable HTTP, so a remote
// client (Claude Code, Codex, ...) connects with just a URL and a bearer token.
//
// Each MCP session gets its own server built by mcpserver.New, whose gRPC
// client talks to the local noted listener with the caller's token. The tools,
// their validation and per-user isolation are therefore exactly those of
// `noted mcp` over stdio.
package mcphttp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/mcpserver"
	"github.com/liliang-cn/noted/internal/oauth"
	"github.com/liliang-cn/noted/internal/portal"
	"github.com/liliang-cn/noted/internal/service"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Path is where the MCP endpoint is served.
const Path = "/mcp"

type Options struct {
	Auth     *auth.Authenticator
	Conn     *grpc.ClientConn // to this server's own gRPC listener
	Location *time.Location   // reads zone-less times
	Version  string
	Logger   *slog.Logger // nil: slog.Default()
	// Account, when set, also serves the web portal (sign up / sign in, token
	// management) at / and /api/, and MCP sign-in without a pasted token
	// (OAuth, paired from the App) at /.well-known/ and /oauth/.
	Account *service.Account
	// PublicURL is the origin clients reach this server at, e.g.
	// "https://noted.example.com"; empty: each request's own.
	PublicURL string
}

type tokenKey struct{}

// New returns the HTTP handler: the MCP endpoint at /mcp, GET /healthz and,
// with Options.Account, the web portal.
func New(o Options) http.Handler {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	getServer := func(r *http.Request) *mcp.Server {
		tok, _ := r.Context().Value(tokenKey{}).(string)
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		srv, err := mcpserver.New(ctx, bearerConn{o.Conn, tok}, o.Location, o.Version)
		if err != nil {
			o.Logger.Error("mcp: cannot start session", "err", err)
			return nil
		}
		return srv
	}
	h := mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{
		// An idle client reconnects with a fresh session.
		SessionTimeout: 2 * time.Hour,
		// Every request carries a bearer token a browser page cannot know, so
		// DNS rebinding gains nothing; the check would only break a reverse
		// proxy on the same host that keeps the public Host header.
		DisableLocalhostProtection: !o.Auth.Disabled(),
	})

	// With sign-in available, a 401 points the client at it (RFC 9728).
	var metadataURL func(*http.Request) string
	if o.Account != nil {
		metadataURL = func(r *http.Request) string { return oauth.ResourceMetadataURL(portal.BaseURL(r, o.PublicURL)) }
	}

	mux := http.NewServeMux()
	mux.Handle(Path, requireToken(o.Auth, metadataURL, h))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	if o.Account != nil {
		mux.Handle("/", portal.New(portal.Options{Auth: o.Auth, Account: o.Account, PublicURL: o.PublicURL}))
		oh := oauth.New(oauth.Options{Auth: o.Auth, Account: o.Account, ResourcePath: Path, PublicURL: o.PublicURL})
		mux.Handle("/.well-known/", oh)
		mux.Handle("/oauth/", oh)
	}
	return mux
}

// requireToken checks "Authorization: Bearer <token>" on every request, as the
// gRPC interceptor does, so a revoked token stops working mid-session. It also
// binds each MCP session to the token that opened it.
func requireToken(a *auth.Authenticator, metadataURL func(*http.Request) string, next http.Handler) http.Handler {
	// The SDK's middleware is what records the session owner; it runs after our
	// own check and just takes over the verified identity.
	bind := mcpauth.RequireBearerToken(func(ctx context.Context, _ string, _ *http.Request) (*mcpauth.TokenInfo, error) {
		id, _ := ctx.Value(tokenIDKey{}).(string)
		return &mcpauth.TokenInfo{UserID: id}, nil
	}, &mcpauth.RequireBearerTokenOptions{AllowMissingExpiration: true})(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Disabled() {
			next.ServeHTTP(w, r)
			return
		}
		challenge := `Bearer realm="noted"`
		if metadataURL != nil {
			challenge = `Bearer resource_metadata="` + metadataURL(r) + `"`
		}
		tok, ok := bearer(r.Header.Get("Authorization"))
		if !ok {
			unauthorized(w, challenge, "missing bearer token: send \"Authorization: Bearer <token>\"")
			return
		}
		_, id, err := a.Verify(r.Context(), tok)
		if errors.Is(err, auth.ErrInvalidToken) {
			unauthorized(w, challenge+`, error="invalid_token"`, "invalid token")
			return
		}
		if err != nil {
			http.Error(w, "authentication failed", http.StatusInternalServerError)
			return
		}
		ctx := context.WithValue(r.Context(), tokenKey{}, tok)
		ctx = context.WithValue(ctx, tokenIDKey{}, id)
		bind.ServeHTTP(w, r.WithContext(ctx))
	})
}

type tokenIDKey struct{}

func bearer(h string) (string, bool) {
	scheme, tok, ok := strings.Cut(strings.TrimSpace(h), " ")
	if !ok || !strings.EqualFold(scheme, "bearer") {
		return "", false
	}
	tok = strings.TrimSpace(tok)
	return tok, tok != ""
}

func unauthorized(w http.ResponseWriter, challenge, msg string) {
	w.Header().Set("WWW-Authenticate", challenge)
	http.Error(w, msg, http.StatusUnauthorized)
}

// bearerConn sends every call on the shared connection with one user's token.
type bearerConn struct {
	cc  *grpc.ClientConn
	tok string
}

func (b bearerConn) ctx(ctx context.Context) context.Context {
	if b.tok == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+b.tok)
}

func (b bearerConn) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	return b.cc.Invoke(b.ctx(ctx), method, args, reply, opts...)
}

func (b bearerConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return b.cc.NewStream(b.ctx(ctx), desc, method, opts...)
}
