package server_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/apple"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/reminder"
	"github.com/liliang-cn/noted/internal/server"
	"google.golang.org/grpc/codes"
)

const testBundle = "cn.superleo.noted"

// fakeApple signs identity tokens with its own key and serves the matching key set.
type fakeApple struct {
	key *rsa.PrivateKey
	srv *httptest.Server
}

func newFakeApple(t *testing.T) *fakeApple {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeApple{key: k}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes()),
		}}})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeApple) token(t *testing.T, sub, aud, rawNonce string, exp time.Time) string {
	t.Helper()
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	sum := sha256.Sum256([]byte(rawNonce))
	head := enc(map[string]string{"alg": "RS256", "kid": "k1"})
	body := enc(map[string]any{"iss": apple.Issuer, "aud": aud, "sub": sub, "exp": exp.Unix(), "nonce": hex.EncodeToString(sum[:])})
	d := sha256.Sum256([]byte(head + "." + body))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, d[:])
	if err != nil {
		t.Fatal(err)
	}
	return head + "." + body + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func startApple(t *testing.T, f *fakeApple, signup bool) *env {
	t.Helper()
	e := start(t, nil, false)
	a, err := auth.New(context.Background(), e.st, false)
	if err != nil {
		t.Fatal(err)
	}
	g := server.New(server.Deps{
		Store: e.st, Auth: a, Hub: reminder.NewHub(), Location: time.UTC,
		Apple: &apple.Verifier{BundleID: testBundle, KeysURL: f.srv.URL}, AppleSignup: signup,
	})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go g.Serve(lis)
	t.Cleanup(g.Stop)
	e.addr = lis.Addr().String()
	return e
}

func TestSignInWithAppleAfterLinking(t *testing.T) {
	f := newFakeApple(t)
	e := startApple(t, f, false)
	acct := pb.NewAccountServiceClient(e.conn(t))
	soon := time.Now().Add(10 * time.Minute)

	// Before linking, the Apple ID is a stranger and a closed server refuses it.
	_, err := acct.SignInWithApple(context.Background(), &pb.SignInWithAppleRequest{IdentityToken: f.token(t, "001.abc", testBundle, "n1", soon), Nonce: "n1"})
	if code(err) != codes.FailedPrecondition {
		t.Fatalf("unlinked Apple ID: %v", err)
	}

	// A device that already has a token links it.
	got, err := acct.LinkApple(as(e.alice), &pb.LinkAppleRequest{IdentityToken: f.token(t, "001.abc", testBundle, "n2", soon), Nonce: "n2"})
	if err != nil || got.UserName != "alice" || !got.AppleLinked || !got.AppleEnabled {
		t.Fatalf("link: %+v %v", got, err)
	}
	// The same Apple ID cannot also belong to bob.
	if _, err := acct.LinkApple(as(e.bob), &pb.LinkAppleRequest{IdentityToken: f.token(t, "001.abc", testBundle, "n3", soon), Nonce: "n3"}); code(err) != codes.InvalidArgument {
		t.Fatalf("stealing an Apple ID: %v", err)
	}

	// Now a fresh device signs in with no token and gets alice's data.
	resp, err := acct.SignInWithApple(context.Background(), &pb.SignInWithAppleRequest{
		IdentityToken: f.token(t, "001.abc", testBundle, "n4", soon), Nonce: "n4", DeviceLabel: "iPhone"})
	if err != nil || resp.UserName != "alice" || resp.Token == "" {
		t.Fatalf("sign in: %+v %v", resp, err)
	}
	notes := pb.NewNoteServiceClient(e.conn(t))
	mine, err := notes.CreateNote(as(e.alice), &pb.CreateNoteRequest{Note: &pb.Note{Title: "from the old device"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := notes.GetNote(as(resp.Token), &pb.GetNoteRequest{Id: mine.Id}); err != nil {
		t.Fatalf("the minted token should see alice's notes: %v", err)
	}

	// Unlinking closes the door again.
	if got, err := acct.UnlinkApple(as(resp.Token), &pb.UnlinkAppleRequest{}); err != nil || got.AppleLinked {
		t.Fatalf("unlink: %+v %v", got, err)
	}
	if _, err := acct.SignInWithApple(context.Background(), &pb.SignInWithAppleRequest{IdentityToken: f.token(t, "001.abc", testBundle, "n5", soon), Nonce: "n5"}); code(err) != codes.FailedPrecondition {
		t.Fatalf("after unlink: %v", err)
	}
}

func TestSignInWithAppleRefusesBadTokens(t *testing.T) {
	f := newFakeApple(t)
	e := startApple(t, f, true)
	acct := pb.NewAccountServiceClient(e.conn(t))
	soon := time.Now().Add(10 * time.Minute)
	try := func(tok, nonce string) error {
		_, err := acct.SignInWithApple(context.Background(), &pb.SignInWithAppleRequest{IdentityToken: tok, Nonce: nonce})
		return err
	}

	for name, tc := range map[string]struct{ tok, nonce string }{
		"another app":        {f.token(t, "s", "com.other.app", "n", soon), "n"},
		"expired":            {f.token(t, "s", testBundle, "n", time.Now().Add(-time.Minute)), "n"},
		"replayed nonce":     {f.token(t, "s", testBundle, "n", soon), "other"},
		"no nonce":           {f.token(t, "s", testBundle, "", soon), ""},
		"garbage":            {"abc.def.ghi", "n"},
		"tampered signature": {f.token(t, "s", testBundle, "n", soon)[:len(f.token(t, "s", testBundle, "n", soon))-4] + "AAAA", "n"},
	} {
		if err := try(tc.tok, tc.nonce); code(err) != codes.Unauthenticated {
			t.Errorf("%s: want Unauthenticated, got %v", name, err)
		}
	}

	// A token signed by some other key is refused even when every claim is right.
	other := newFakeApple(t)
	if err := try(other.token(t, "s", testBundle, "n", soon), "n"); code(err) != codes.Unauthenticated {
		t.Errorf("foreign key: %v", err)
	}

	// With sign-up on, an unknown Apple ID gets its own user, and signing in again finds it.
	tok := f.token(t, "new-person", testBundle, "n", soon)
	r1, err := acct.SignInWithApple(context.Background(), &pb.SignInWithAppleRequest{IdentityToken: tok, Nonce: "n"})
	if err != nil || r1.UserName == "" {
		t.Fatalf("sign-up: %+v %v", r1, err)
	}
	r2, err := acct.SignInWithApple(context.Background(), &pb.SignInWithAppleRequest{IdentityToken: tok, Nonce: "n"})
	if err != nil || r2.UserName != r1.UserName || r2.Token == r1.Token {
		t.Fatalf("second sign-in: %+v %v", r2, err)
	}
}

func TestSignInWithAppleOffByDefault(t *testing.T) {
	e := start(t, nil, false)
	_, err := pb.NewAccountServiceClient(e.conn(t)).SignInWithApple(context.Background(), &pb.SignInWithAppleRequest{IdentityToken: "a.b.c", Nonce: "n"})
	if code(err) != codes.FailedPrecondition {
		t.Fatalf("%v", err)
	}
}

func TestProIsGrantedByTheServer(t *testing.T) {
	e := start(t, nil, false)
	acct := pb.NewAccountServiceClient(e.conn(t))
	if a, err := acct.GetAccount(as(e.alice), &pb.GetAccountRequest{}); err != nil || a.Pro {
		t.Fatalf("before: %+v %v", a, err)
	}
	if err := e.st.SetUserPro(context.Background(), "alice", true); err != nil {
		t.Fatal(err)
	}
	if a, _ := acct.GetAccount(as(e.alice), &pb.GetAccountRequest{}); !a.Pro {
		t.Fatal("alice should have Pro")
	}
	if a, _ := acct.GetAccount(as(e.bob), &pb.GetAccountRequest{}); a.Pro {
		t.Fatal("bob should not")
	}
	if err := e.st.SetUserPro(context.Background(), "nobody", true); err == nil {
		t.Fatal("granting Pro to a user that does not exist should fail")
	}
}
