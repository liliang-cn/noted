package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"net/mail"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/store"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const (
	bcryptCost     = 12
	minPassword    = 8
	maxPassword    = 128
	maxEmail       = 254
	lockAfter      = 5
	lockWindow     = 10 * time.Minute
	lockFor        = 10 * time.Minute
	wrongLogin     = "wrong email or password"
	tooManyLogins  = "too many failed sign-ins; try again in 10 minutes"
	defaultEmailUA = "邮箱登录"
)

// NormalizeEmail trims and lowercases an email and checks its syntax.
func NormalizeEmail(raw string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(raw))
	if e == "" {
		return "", errors.New("email is required")
	}
	if len(e) > maxEmail {
		return "", errors.New("email is too long")
	}
	a, err := mail.ParseAddress(e)
	local, domain, ok := strings.Cut(e, "@")
	if err != nil || a.Address != e || !ok || local == "" || !strings.Contains(domain, ".") ||
		strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", errors.New("email is not a valid address")
	}
	return e, nil
}

func checkPasswordRules(p string) error {
	n := utf8.RuneCountInString(p)
	if n < minPassword || n > maxPassword {
		return status.Errorf(codes.InvalidArgument, "password must be %d to %d characters", minPassword, maxPassword)
	}
	return nil
}

// bcrypt reads at most 72 bytes, and a 128-character password can be longer,
// so it hashes a fixed-length digest of the password instead.
func prehash(p string) []byte {
	sum := sha256.Sum256([]byte(p))
	return []byte(base64.StdEncoding.EncodeToString(sum[:]))
}

func hashPassword(p string, cost int) (string, error) {
	h, err := bcrypt.GenerateFromPassword(prehash(p), cost)
	return string(h), err
}

// cost is the bcrypt cost for new hashes: 12 unless a test lowers it.
func (s *Account) cost() int {
	if s.PasswordCost == 0 {
		return bcryptCost
	}
	return s.PasswordCost
}

func passwordMatches(hash, p string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), prehash(p)) == nil
}

var dummyHashes sync.Map // cost -> hash

// dummyHash is checked against for an unknown email, so that a miss costs as
// much time as a wrong password and does not reveal which emails exist.
func (s *Account) dummyHash() string {
	if h, ok := dummyHashes.Load(s.cost()); ok {
		return h.(string)
	}
	h, _ := hashPassword("not a password anyone has", s.cost())
	dummyHashes.Store(s.cost(), h)
	return h
}

// Guard refuses sign-in for a while after repeated failures, per email and
// per client address. State is in memory: a restart forgets it.
type Guard struct {
	mu  sync.Mutex
	m   map[string]*attempts
	now func() time.Time
}

type attempts struct {
	fails []time.Time
	until time.Time
}

func NewGuard() *Guard { return &Guard{m: map[string]*attempts{}, now: time.Now} }

func guardKeys(email, ip string) []string {
	var ks []string
	if email != "" {
		ks = append(ks, "email:"+email)
	}
	if ip != "" {
		ks = append(ks, "ip:"+ip)
	}
	return ks
}

// Locked reports whether any key is locked out.
func (g *Guard) Locked(keys ...string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	for _, k := range keys {
		if a := g.m[k]; a != nil && now.Before(a.until) {
			return true
		}
	}
	return false
}

// Fail records a failure for every key, locking those that reach the limit.
func (g *Guard) Fail(keys ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if len(g.m) > 10000 {
		g.sweep(now)
	}
	for _, k := range keys {
		a := g.m[k]
		if a == nil {
			a = &attempts{}
			g.m[k] = a
		}
		recent := a.fails[:0]
		for _, t := range a.fails {
			if now.Sub(t) < lockWindow {
				recent = append(recent, t)
			}
		}
		a.fails = append(recent, now)
		if len(a.fails) >= lockAfter {
			a.until = now.Add(lockFor)
			a.fails = nil
		}
	}
}

// Reset forgets the failures of a key (after a successful sign-in).
func (g *Guard) Reset(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if a := g.m[key]; a != nil && !g.now().Before(a.until) {
		delete(g.m, key)
	}
}

func (g *Guard) sweep(now time.Time) {
	for k, a := range g.m {
		if !now.Before(a.until) && (len(a.fails) == 0 || now.Sub(a.fails[len(a.fails)-1]) >= lockWindow) {
			delete(g.m, k)
		}
	}
}

type clientIPKey struct{}

// WithClientIP records the caller's address for in-process calls (the web
// portal), where there is no gRPC peer.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPKey{}, ip)
}

var cgnat = func() *net.IPNet { _, n, _ := net.ParseCIDR("100.64.0.0/10"); return n }()

// ClientIP returns the caller's address: the direct peer, or, when that peer
// is a proxy on a private network, the last X-Forwarded-For entry it added.
func ClientIP(ctx context.Context) string {
	if ip, ok := ctx.Value(clientIPKey{}).(string); ok {
		return ip
	}
	var remote string
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		remote = p.Addr.String()
	}
	md, _ := metadata.FromIncomingContext(ctx)
	return TrustedClientIP(remote, md.Get("x-forwarded-for"))
}

// TrustedClientIP resolves the client address from the connection's remote
// address and the X-Forwarded-For values, trusting the latter only from a
// loopback or private-network peer (a reverse proxy).
func TrustedClientIP(remote string, xff []string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate() || cgnat.Contains(ip)) && len(xff) > 0 {
		parts := strings.Split(xff[len(xff)-1], ",")
		if fwd := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); fwd != nil {
			return fwd.String()
		}
	}
	return host
}

func (s *Account) guard() *Guard {
	if s.Guard == nil {
		return noGuard
	}
	return s.Guard
}

var noGuard = NewGuard()

func (s *Account) SignUp(ctx context.Context, req *pb.SignUpRequest) (*pb.SignInResponse, error) {
	if !s.EmailSignup {
		return nil, status.Error(codes.FailedPrecondition, "sign-up is closed")
	}
	email, err := NormalizeEmail(req.GetEmail())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := checkPasswordRules(req.GetPassword()); err != nil {
		return nil, err
	}
	label, err := tokenLabel(req.GetDeviceLabel(), defaultEmailUA)
	if err != nil {
		return nil, err
	}
	hash, err := hashPassword(req.GetPassword(), s.cost())
	if err != nil {
		return nil, toStatus(err)
	}
	u, err := s.Store.CreateEmailUser(ctx, email, hash)
	if errors.Is(err, store.ErrEmailTaken) {
		return nil, status.Error(codes.AlreadyExists, "this email is already registered; sign in instead")
	}
	if err != nil {
		return nil, toStatus(err)
	}
	tok, err := s.Store.CreateToken(ctx, u.ID, label)
	if err != nil {
		return nil, toStatus(err)
	}
	return &pb.SignInResponse{Token: tok, UserName: u.Name}, nil
}

func (s *Account) SignIn(ctx context.Context, req *pb.SignInRequest) (*pb.SignInResponse, error) {
	if strings.TrimSpace(req.GetEmail()) == "" || req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "email and password are required")
	}
	label, err := tokenLabel(req.GetDeviceLabel(), defaultEmailUA)
	if err != nil {
		return nil, err
	}
	email, err := NormalizeEmail(req.GetEmail())
	if err != nil {
		email = strings.ToLower(strings.TrimSpace(req.GetEmail()))
	}
	keys := guardKeys(email, ClientIP(ctx))
	g := s.guard()
	if g.Locked(keys...) {
		return nil, status.Error(codes.ResourceExhausted, tooManyLogins)
	}
	login, lerr := s.Store.EmailLogin(ctx, email)
	if lerr != nil && !errors.Is(lerr, store.ErrNotFound) {
		return nil, toStatus(lerr)
	}
	if lerr != nil {
		passwordMatches(s.dummyHash(), req.GetPassword()) // same cost as a real check
		g.Fail(keys...)
		return nil, status.Error(codes.Unauthenticated, wrongLogin)
	}
	if !passwordMatches(login.PasswordHash, req.GetPassword()) {
		g.Fail(keys...)
		return nil, status.Error(codes.Unauthenticated, wrongLogin)
	}
	g.Reset("email:" + email)
	u, err := s.Store.User(ctx, login.UserID)
	if err != nil {
		return nil, toStatus(err)
	}
	tok, err := s.Store.CreateToken(ctx, u.ID, label)
	if err != nil {
		return nil, toStatus(err)
	}
	return &pb.SignInResponse{Token: tok, UserName: u.Name}, nil
}

func (s *Account) SetEmailPassword(ctx context.Context, req *pb.SetEmailPasswordRequest) (*pb.Account, error) {
	u, ok := auth.User(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	curEmail, curHash, err := s.Store.UserEmail(ctx, u.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, toStatus(err)
	}
	raw := req.GetEmail()
	if strings.TrimSpace(raw) == "" {
		raw = curEmail
	}
	email, err := NormalizeEmail(raw)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := checkPasswordRules(req.GetPassword()); err != nil {
		return nil, err
	}
	if curHash != "" {
		if req.GetCurrentPassword() == "" {
			return nil, status.Error(codes.PermissionDenied, "current password is required")
		}
		keys := guardKeys(curEmail, ClientIP(ctx))
		g := s.guard()
		if g.Locked(keys...) {
			return nil, status.Error(codes.ResourceExhausted, tooManyLogins)
		}
		if !passwordMatches(curHash, req.GetCurrentPassword()) {
			g.Fail(keys...)
			return nil, status.Error(codes.PermissionDenied, "current password is wrong")
		}
	}
	hash, err := hashPassword(req.GetPassword(), s.cost())
	if err != nil {
		return nil, toStatus(err)
	}
	if err := s.Store.SetUserEmail(ctx, u.ID, email, hash); errors.Is(err, store.ErrEmailTaken) {
		return nil, status.Error(codes.AlreadyExists, "another account already uses this email")
	} else if err != nil {
		return nil, toStatus(err)
	}
	return s.account(ctx)
}
