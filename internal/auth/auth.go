// Package auth turns a bearer token into a user on every gRPC call (and, via
// Verify, on every remote MCP request).
package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/liliang-cn/noted/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type ctxKey struct{}
type tokenKey struct{}

// WithUser returns ctx carrying u. Tests and the AI tools use it to act as a user.
func WithUser(ctx context.Context, u store.User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// User returns the authenticated user. Handlers behind the interceptor can rely on ok.
func User(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(ctxKey{}).(store.User)
	return u, ok
}

// TokenID returns the id of the token that authenticated this call; empty
// when auth is disabled.
func TokenID(ctx context.Context) string {
	id, _ := ctx.Value(tokenKey{}).(string)
	return id
}

// DefaultUser is the identity used when auth is disabled.
const DefaultUser = "default"

// Public methods need no token.
var public = map[string]bool{
	"/noted.v1.AccountService/SignInWithApple":                       true,
	"/noted.v1.AccountService/SignUp":                                true,
	"/noted.v1.AccountService/SignIn":                                true,
	"/grpc.health.v1.Health/Check":                                   true,
	"/grpc.health.v1.Health/Watch":                                   true,
	"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo":      true,
	"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo": true,
}

type Authenticator struct {
	store    *store.Store
	disabled bool
	fixed    store.User
}

// New builds an Authenticator. With disabled set, every caller becomes the
// "default" user and no token is checked.
func New(ctx context.Context, s *store.Store, disabled bool) (*Authenticator, error) {
	a := &Authenticator{store: s, disabled: disabled}
	if disabled {
		u, err := s.EnsureUser(ctx, DefaultUser)
		if err != nil {
			return nil, err
		}
		a.fixed = u
	}
	return a, nil
}

func (a *Authenticator) authenticate(ctx context.Context, method string) (context.Context, error) {
	if public[method] {
		return ctx, nil
	}
	if a.disabled {
		return WithUser(ctx, a.fixed), nil
	}
	md, _ := metadata.FromIncomingContext(ctx)
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization metadata; send \"authorization: Bearer <token>\"")
	}
	tok, ok := strings.CutPrefix(vals[0], "Bearer ")
	if !ok || strings.TrimSpace(tok) == "" {
		return nil, status.Error(codes.Unauthenticated, "authorization must be \"Bearer <token>\"")
	}
	u, id, err := a.Verify(ctx, tok)
	if errors.Is(err, ErrInvalidToken) {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "authentication failed")
	}
	return WithToken(ctx, u, id), nil
}

// WithToken returns ctx carrying u, authenticated by the token with that id.
// The web portal uses it to call the services in-process as its session.
func WithToken(ctx context.Context, u store.User, id string) context.Context {
	return context.WithValue(WithUser(ctx, u), tokenKey{}, id)
}

// ErrInvalidToken is returned by Verify for a token nobody holds.
var ErrInvalidToken = errors.New("invalid token")

// Disabled reports whether auth is off (every caller is DefaultUser).
func (a *Authenticator) Disabled() bool { return a.disabled }

// Verify resolves a bearer token (without the "Bearer " prefix) to its user
// and the token's id, exactly as the gRPC interceptor does.
func (a *Authenticator) Verify(ctx context.Context, tok string) (store.User, string, error) {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return store.User{}, "", ErrInvalidToken
	}
	u, id, err := a.store.Authenticate(ctx, tok)
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, "", ErrInvalidToken
	}
	return u, id, err
}

func (a *Authenticator) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		ctx, err := a.authenticate(ctx, info.FullMethod)
		if err != nil {
			return nil, err
		}
		return h(ctx, req)
	}
}

type wrapped struct {
	grpc.ServerStream
	ctx context.Context
}

func (w wrapped) Context() context.Context { return w.ctx }

func (a *Authenticator) Stream() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		ctx, err := a.authenticate(ss.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		return h(srv, wrapped{ss, ctx})
	}
}
