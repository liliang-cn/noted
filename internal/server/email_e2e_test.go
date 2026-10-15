package server_test

import (
	"context"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/apple"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/reminder"
	"github.com/liliang-cn/noted/internal/server"
	"github.com/liliang-cn/noted/internal/service"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

// startAccounts serves alice and bob plus email sign-up (when open) and, with
// f, Sign in with Apple that creates users. It returns the account service it
// shares with the web portal.
func startAccounts(t *testing.T, f *fakeApple, signup bool) (*env, *service.Account) {
	t.Helper()
	e := start(t, nil, false)
	a, err := auth.New(context.Background(), e.st, false)
	if err != nil {
		t.Fatal(err)
	}
	d := server.Deps{Store: e.st, Auth: a, Hub: reminder.NewHub(), Location: time.UTC, EmailSignup: signup}
	if f != nil {
		d.Apple = &apple.Verifier{BundleID: testBundle, KeysURL: f.srv.URL}
		d.AppleSignup = true
	}
	d.Account = server.NewAccount(d)
	d.Account.PasswordCost = bcrypt.MinCost // production uses 12; keep the race build fast
	g := server.New(d)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go g.Serve(lis)
	t.Cleanup(g.Stop)
	e.addr = lis.Addr().String()
	return e, d.Account
}

func from(ip string) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	_ = cancel
	return metadata.AppendToOutgoingContext(ctx, "x-forwarded-for", ip)
}

func TestEmailSignUpAndSignIn(t *testing.T) {
	e, _ := startAccounts(t, nil, true)
	acct := pb.NewAccountServiceClient(e.conn(t))
	notes := pb.NewNoteServiceClient(e.conn(t))
	ctx := context.Background()

	up, err := acct.SignUp(ctx, &pb.SignUpRequest{Email: "  Carol@Example.COM ", Password: "correct horse", DeviceLabel: "iPhone"})
	if err != nil || up.Token == "" || up.UserName != "carol" {
		t.Fatalf("sign up: %+v %v", up, err)
	}
	if a, err := acct.GetAccount(as(up.Token), &pb.GetAccountRequest{}); err != nil || a.Email != "carol@example.com" {
		t.Fatalf("account email: %+v %v", a, err)
	}
	n, err := notes.CreateNote(as(up.Token), &pb.CreateNoteRequest{Note: &pb.Note{Title: "carol's"}})
	if err != nil {
		t.Fatal(err)
	}

	// Same email in another case is a duplicate.
	if _, err := acct.SignUp(ctx, &pb.SignUpRequest{Email: "CAROL@example.com", Password: "another one"}); code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate email: %v", err)
	}
	// A clashing user name gets a suffix.
	other, err := acct.SignUp(ctx, &pb.SignUpRequest{Email: "alice@elsewhere.org", Password: "12345678"})
	if err != nil || other.UserName == "alice" || !strings.HasPrefix(other.UserName, "alice-") {
		t.Fatalf("name clash: %+v %v", other, err)
	}
	// Rules.
	for name, req := range map[string]*pb.SignUpRequest{
		"short password": {Email: "d@example.com", Password: "1234567"},
		"long password":  {Email: "d@example.com", Password: strings.Repeat("x", 129)},
		"bad email":      {Email: "not-an-email", Password: "12345678"},
		"no domain dot":  {Email: "d@localhost", Password: "12345678"},
		"long label":     {Email: "d@example.com", Password: "12345678", DeviceLabel: strings.Repeat("x", 61)},
	} {
		if _, err := acct.SignUp(ctx, req); code(err) != codes.InvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}

	// Sign in, case-insensitively, reaches the same data.
	in, err := acct.SignIn(ctx, &pb.SignInRequest{Email: "carol@EXAMPLE.com", Password: "correct horse", DeviceLabel: "Mac"})
	if err != nil || in.UserName != "carol" {
		t.Fatalf("sign in: %+v %v", in, err)
	}
	if _, err := notes.GetNote(as(in.Token), &pb.GetNoteRequest{Id: n.Id}); err != nil {
		t.Fatalf("signed-in token sees carol's note: %v", err)
	}
	// Wrong password and unknown email look the same.
	_, e1 := acct.SignIn(ctx, &pb.SignInRequest{Email: "carol@example.com", Password: "wrong horse"})
	_, e2 := acct.SignIn(ctx, &pb.SignInRequest{Email: "nobody@example.com", Password: "wrong horse"})
	if code(e1) != codes.Unauthenticated || code(e2) != codes.Unauthenticated || e1.Error() != e2.Error() {
		t.Fatalf("wrong login errors differ: %v / %v", e1, e2)
	}
	// A password of up to 128 characters works in full (not cut at bcrypt's 72 bytes).
	long := strings.Repeat("密", 128)
	if _, err := acct.SignUp(ctx, &pb.SignUpRequest{Email: "long@example.com", Password: long}); err != nil {
		t.Fatal(err)
	}
	if _, err := acct.SignIn(ctx, &pb.SignInRequest{Email: "long@example.com", Password: long[:len(long)-3] + "x"}); code(err) != codes.Unauthenticated {
		t.Fatalf("a different last character must not sign in: %v", err)
	}
}

func TestEmailSignUpClosedByDefault(t *testing.T) {
	e, _ := startAccounts(t, nil, false)
	acct := pb.NewAccountServiceClient(e.conn(t))
	if _, err := acct.SignUp(context.Background(), &pb.SignUpRequest{Email: "x@example.com", Password: "12345678"}); code(err) != codes.FailedPrecondition {
		t.Fatalf("closed sign-up: %v", err)
	}
	// An existing user can still add an email and sign in with it.
	if _, err := acct.SetEmailPassword(as(e.alice), &pb.SetEmailPasswordRequest{Email: "alice@example.com", Password: "alice-pass"}); err != nil {
		t.Fatal(err)
	}
	if in, err := acct.SignIn(context.Background(), &pb.SignInRequest{Email: "alice@example.com", Password: "alice-pass"}); err != nil || in.UserName != "alice" {
		t.Fatalf("sign in: %+v %v", in, err)
	}
}

func TestSignInLockout(t *testing.T) {
	e, _ := startAccounts(t, nil, true)
	acct := pb.NewAccountServiceClient(e.conn(t))
	if _, err := acct.SignUp(context.Background(), &pb.SignUpRequest{Email: "dave@example.com", Password: "dave-pass"}); err != nil {
		t.Fatal(err)
	}

	// Per email: five misses (from different addresses) lock the email.
	for i := range 5 {
		ip := "198.51.100." + string(rune('1'+i))
		if _, err := acct.SignIn(from(ip), &pb.SignInRequest{Email: "dave@example.com", Password: "nope-nope"}); code(err) != codes.Unauthenticated {
			t.Fatalf("miss %d: %v", i, err)
		}
	}
	if _, err := acct.SignIn(from("198.51.100.9"), &pb.SignInRequest{Email: "dave@example.com", Password: "dave-pass"}); code(err) != codes.ResourceExhausted {
		t.Fatalf("locked email with the right password: %v", err)
	}

	// Per address: five misses on other emails from one address lock that address.
	if _, err := acct.SignUp(context.Background(), &pb.SignUpRequest{Email: "erin@example.com", Password: "erin-pass"}); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		email := "ghost" + string(rune('a'+i)) + "@example.com"
		if _, err := acct.SignIn(from("203.0.113.7"), &pb.SignInRequest{Email: email, Password: "whatever1"}); code(err) != codes.Unauthenticated {
			t.Fatalf("miss %d: %v", i, err)
		}
	}
	if _, err := acct.SignIn(from("203.0.113.7"), &pb.SignInRequest{Email: "erin@example.com", Password: "erin-pass"}); code(err) != codes.ResourceExhausted {
		t.Fatalf("locked address: %v", err)
	}
	if _, err := acct.SignIn(from("203.0.113.8"), &pb.SignInRequest{Email: "erin@example.com", Password: "erin-pass"}); err != nil {
		t.Fatalf("another address is not locked: %v", err)
	}
}

func TestSetEmailPasswordOnAnAppleAccount(t *testing.T) {
	f := newFakeApple(t)
	e, _ := startAccounts(t, f, true)
	acct := pb.NewAccountServiceClient(e.conn(t))
	soon := time.Now().Add(10 * time.Minute)

	ap, err := acct.SignInWithApple(context.Background(), &pb.SignInWithAppleRequest{IdentityToken: f.token(t, "001.zzz", testBundle, "n1", soon), Nonce: "n1"})
	if err != nil {
		t.Fatal(err)
	}
	cal := pb.NewCalendarServiceClient(e.conn(t))
	if _, err := cal.CreateTask(as(ap.Token), &pb.CreateTaskRequest{Task: &pb.Task{Title: "from the iPhone"}}); err != nil {
		t.Fatal(err)
	}

	// No password yet: none needed to set one; the email is required.
	if _, err := acct.SetEmailPassword(as(ap.Token), &pb.SetEmailPasswordRequest{Password: "apple-pass"}); code(err) != codes.InvalidArgument {
		t.Fatalf("no email: %v", err)
	}
	a, err := acct.SetEmailPassword(as(ap.Token), &pb.SetEmailPasswordRequest{Email: "Frank@Example.com", Password: "apple-pass"})
	if err != nil || a.Email != "frank@example.com" || !a.AppleLinked {
		t.Fatalf("set: %+v %v", a, err)
	}
	in, err := acct.SignIn(context.Background(), &pb.SignInRequest{Email: "frank@example.com", Password: "apple-pass"})
	if err != nil || in.UserName != ap.UserName {
		t.Fatalf("sign in: %+v %v", in, err)
	}
	tl, err := cal.ListTasks(as(in.Token), &pb.ListTasksRequest{})
	if err != nil || len(tl.Tasks) != 1 || tl.Tasks[0].Title != "from the iPhone" {
		t.Fatalf("email sign-in reaches the Apple account's data: %v %+v", err, tl)
	}

	// Changing it now needs the current password.
	if _, err := acct.SetEmailPassword(as(in.Token), &pb.SetEmailPasswordRequest{Password: "newer-pass"}); code(err) != codes.PermissionDenied {
		t.Fatalf("no current password: %v", err)
	}
	if _, err := acct.SetEmailPassword(as(in.Token), &pb.SetEmailPasswordRequest{Password: "newer-pass", CurrentPassword: "wrong-pass"}); code(err) != codes.PermissionDenied {
		t.Fatalf("wrong current password: %v", err)
	}
	if _, err := acct.SetEmailPassword(as(in.Token), &pb.SetEmailPasswordRequest{Password: "newer-pass", CurrentPassword: "apple-pass"}); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if _, err := acct.SignIn(context.Background(), &pb.SignInRequest{Email: "frank@example.com", Password: "apple-pass"}); code(err) != codes.Unauthenticated {
		t.Fatalf("old password still works: %v", err)
	}
	// Another account cannot take the email.
	if _, err := acct.SetEmailPassword(as(e.bob), &pb.SetEmailPasswordRequest{Email: "FRANK@example.com", Password: "bobs-pass"}); code(err) != codes.AlreadyExists {
		t.Fatalf("taking frank's email: %v", err)
	}
}

func pbAccount(t *testing.T, e *env) pb.AccountServiceClient {
	return pb.NewAccountServiceClient(e.conn(t))
}

func setEmailReq(email, pw string) *pb.SetEmailPasswordRequest {
	return &pb.SetEmailPasswordRequest{Email: email, Password: pw}
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}
