package server_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/mcphttp"
	"github.com/liliang-cn/noted/internal/portal"
	"google.golang.org/grpc/codes"
)

// oauthEnv is a server with the portal and MCP sign-in, plus an HTTP client
// that does not follow redirects, so each hop can be checked.
type oauthEnv struct {
	*env
	t    *testing.T
	base string
	acct pb.AccountServiceClient
	c    *http.Client
}

func startOAuth(t *testing.T) *oauthEnv {
	t.Helper()
	e, acct := startAccounts(t, nil, false)
	a, err := auth.New(t.Context(), e.st, false)
	if err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(mcphttp.New(mcphttp.Options{Auth: a, Conn: e.conn(t), Location: time.UTC, Version: "test", Account: acct}))
	t.Cleanup(hs.Close)
	return &oauthEnv{env: e, t: t, base: hs.URL, acct: pb.NewAccountServiceClient(e.conn(t)), c: &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (o *oauthEnv) do(req *http.Request) (*http.Response, string) {
	o.t.Helper()
	resp, err := o.c.Do(req)
	if err != nil {
		o.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (o *oauthEnv) get(path string, hdr map[string]string) (*http.Response, string) {
	o.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, o.base+path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return o.do(req)
}

func (o *oauthEnv) postForm(path string, form url.Values, hdr map[string]string) (*http.Response, string) {
	o.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, o.base+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	return o.do(req)
}

func jsonOf(t *testing.T, body string) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("not JSON: %v: %s", err, body)
	}
	return out
}

func (o *oauthEnv) register(redirects ...string) (int, map[string]any) {
	o.t.Helper()
	body, _ := json.Marshal(map[string]any{
		"client_name": "Claude Code", "redirect_uris": redirects,
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": "none",
	})
	req, _ := http.NewRequest(http.MethodPost, o.base+"/oauth/register", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, b := o.do(req)
	return resp.StatusCode, jsonOf(o.t, b)
}

type pkce struct{ verifier, challenge string }

func newPKCE(seed string) pkce {
	v := strings.Repeat(seed+"~", 64)[:64]
	sum := sha256.Sum256([]byte(v))
	return pkce{v, base64.RawURLEncoding.EncodeToString(sum[:])}
}

// signIn is one browser sign-in up to the pairing page: it returns the
// page's poll secret and the pairing code it shows.
type signIn struct {
	clientID, redirect, state string
	pk                        pkce
	poll, code, page          string
}

var codeRe = regexp.MustCompile(`class="pair-code"[^>]*>([A-Z0-9]{4}-[A-Z0-9]{4})<`)

func (o *oauthEnv) authorizeURL(clientID, redirect, state string, pk pkce, extra url.Values) string {
	q := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {state},
		"code_challenge": {pk.challenge}, "code_challenge_method": {"S256"}, "resource": {o.base + "/mcp"},
	}
	for k, v := range extra {
		q[k] = v
	}
	return "/oauth/authorize?" + q.Encode()
}

func (o *oauthEnv) start(clientID, redirect, state string, hdr map[string]string) signIn {
	o.t.Helper()
	s := signIn{clientID: clientID, redirect: redirect, state: state, pk: newPKCE(state + "-verifier-")}
	resp, body := o.get(o.authorizeURL(clientID, redirect, state, s.pk, nil), nil)
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/oauth/pair?p=") {
		o.t.Fatalf("authorize: %d %q %s", resp.StatusCode, loc, body)
	}
	s.poll = strings.TrimPrefix(loc, "/oauth/pair?p=")
	resp, s.page = o.get(loc, hdr)
	m := codeRe.FindStringSubmatch(s.page)
	if resp.StatusCode != 200 || m == nil {
		o.t.Fatalf("pairing page: %d %s", resp.StatusCode, s.page)
	}
	s.code = m[1]
	return s
}

func (o *oauthEnv) status(s signIn) map[string]any {
	o.t.Helper()
	resp, body := o.get("/oauth/pair/status?p="+url.QueryEscape(s.poll), nil)
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" {
		o.t.Fatalf("status: %d %v %s", resp.StatusCode, resp.Header, body)
	}
	return jsonOf(o.t, body)
}

// callback parses the redirect a status poll (or form) sent the browser to.
func callback(t *testing.T, to, wantPrefix string) url.Values {
	t.Helper()
	if !strings.HasPrefix(to, wantPrefix) {
		t.Fatalf("redirect %q does not go to %q", to, wantPrefix)
	}
	u, err := url.Parse(to)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func (o *oauthEnv) exchange(s signIn, code, verifier, redirect string) (int, map[string]any, *http.Response) {
	o.t.Helper()
	resp, body := o.postForm("/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {s.clientID},
		"redirect_uri": {redirect}, "code_verifier": {verifier}, "resource": {o.base + "/mcp"},
	}, nil)
	return resp.StatusCode, jsonOf(o.t, body), resp
}

func TestOAuthMetadataAndChallenge(t *testing.T) {
	o := startOAuth(t)

	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		resp, body := o.get(p, nil)
		m := jsonOf(t, body)
		if resp.StatusCode != 200 || m["resource"] != o.base+"/mcp" || m["authorization_servers"].([]any)[0] != o.base ||
			m["bearer_methods_supported"].([]any)[0] != "header" {
			t.Fatalf("%s: %d %s", p, resp.StatusCode, body)
		}
	}
	resp, body := o.get("/.well-known/oauth-authorization-server", nil)
	m := jsonOf(t, body)
	if resp.StatusCode != 200 || m["issuer"] != o.base || m["authorization_endpoint"] != o.base+"/oauth/authorize" ||
		m["token_endpoint"] != o.base+"/oauth/token" || m["registration_endpoint"] != o.base+"/oauth/register" ||
		m["code_challenge_methods_supported"].([]any)[0] != "S256" || m["token_endpoint_auth_methods_supported"].([]any)[0] != "none" ||
		m["response_types_supported"].([]any)[0] != "code" || m["grant_types_supported"].([]any)[0] != "authorization_code" {
		t.Fatalf("AS metadata: %d %s", resp.StatusCode, body)
	}
	// Behind a TLS proxy the URLs are https.
	_, body = o.get("/.well-known/oauth-authorization-server", map[string]string{"X-Forwarded-Proto": "https"})
	if !strings.Contains(body, `"issuer":"https://`) {
		t.Fatalf("issuer behind TLS: %s", body)
	}

	want := `Bearer resource_metadata="` + o.base + `/.well-known/oauth-protected-resource"`
	for name, h := range map[string]string{"none": "", "bad": "Bearer noted_bogus"} {
		r := post(t, o.base+"/mcp", h, "", initBody)
		if r.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(r.Header.Get("WWW-Authenticate"), want) {
			t.Errorf("%s: %d %q", name, r.StatusCode, r.Header.Get("WWW-Authenticate"))
		}
	}
	// A valid token is unaffected.
	if r := post(t, o.base+"/mcp", "Bearer "+o.alice, "", initBody); r.StatusCode != http.StatusOK {
		t.Fatalf("initialize with a token: %d", r.StatusCode)
	}
}

func TestOAuthRegister(t *testing.T) {
	o := startOAuth(t)

	code, m := o.register("http://localhost:33418/callback", "https://app.example.com/cb")
	if code != http.StatusCreated || m["client_id"] == "" || m["client_name"] != "Claude Code" ||
		m["token_endpoint_auth_method"] != "none" || len(m["redirect_uris"].([]any)) != 2 || m["client_secret"] != nil {
		t.Fatalf("register: %d %v", code, m)
	}
	for _, bad := range []string{"http://example.com/cb", "http://192.168.1.2/cb", "noted://cb", "https://app.example.com/cb#x", "/cb"} {
		if code, m := o.register(bad); code != http.StatusBadRequest || m["error"] != "invalid_redirect_uri" {
			t.Errorf("%s: %d %v", bad, code, m)
		}
	}
	if code, m := o.register(); code != http.StatusBadRequest || m["error"] != "invalid_redirect_uri" {
		t.Errorf("no redirect URIs: %d %v", code, m)
	}
	for _, ok := range []string{"http://127.0.0.1:1/cb", "http://[::1]:5/cb"} {
		if code, m := o.register(ok); code != http.StatusCreated {
			t.Errorf("%s: %d %v", ok, code, m)
		}
	}
}

func TestOAuthPairedFromTheApp(t *testing.T) {
	o := startOAuth(t)
	_, reg := o.register("http://localhost:33418/callback")
	clientID := reg["client_id"].(string)

	// Claude Code picks a free port each time; a loopback redirect matches on any.
	redirect := "http://localhost:51234/callback"
	s := o.start(clientID, redirect, "st-123", nil)
	for _, want := range []string{"连接 Claude Code 到 noted", "<svg", "打开 noted App", "设置 → 接入 AI 助手", "扫码配对（或输入配对码）", "拒绝", "后过期"} {
		if !strings.Contains(s.page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(s.page, "身份允许") {
		t.Error("no portal session, yet the page offers to approve in the browser")
	}
	if st := o.status(s); st["status"] != "pending" {
		t.Fatalf("status before approval: %v", st)
	}

	// The App reads the code (any case, with or without the dash) and approves.
	typed := strings.ToLower(strings.ReplaceAll(s.code, "-", " "))
	p, err := o.acct.GetPairing(as(o.alice), &pb.GetPairingRequest{Code: typed})
	if err != nil || p.Code != strings.ReplaceAll(s.code, "-", "") || p.ClientName != "Claude Code" || p.Approved ||
		p.ExpireTime.AsTime().Sub(p.CreateTime.AsTime()) != 10*time.Minute {
		t.Fatalf("GetPairing: %+v %v", p, err)
	}
	if p, err = o.acct.ApprovePairing(as(o.alice), &pb.ApprovePairingRequest{Code: s.code}); err != nil || !p.Approved {
		t.Fatalf("ApprovePairing: %+v %v", p, err)
	}
	// Approving mints nothing yet.
	before, _ := o.acct.ListTokens(as(o.alice), &pb.ListTokensRequest{})
	// Another user cannot see or take over alice's pairing.
	if _, err := o.acct.GetPairing(as(o.bob), &pb.GetPairingRequest{Code: s.code}); code(err) != codes.NotFound {
		t.Fatalf("bob sees alice's pairing: %v", err)
	}

	st := o.status(s)
	if st["status"] != "approved" {
		t.Fatalf("status after approval: %v", st)
	}
	q := callback(t, st["redirect"].(string), redirect+"?")
	if q.Get("state") != "st-123" || q.Get("code") == "" {
		t.Fatalf("callback: %v", q)
	}
	// The pairing is used up: no second code.
	if st := o.status(s); st["status"] != "done" || st["redirect"] != nil {
		t.Fatalf("status after the code went out: %v", st)
	}
	if _, err := o.acct.GetPairing(as(o.alice), &pb.GetPairingRequest{Code: s.code}); code(err) != codes.NotFound {
		t.Fatalf("a used pairing is still found: %v", err)
	}

	sc, tok, resp := o.exchange(s, q.Get("code"), s.pk.verifier, redirect)
	if sc != 200 || tok["token_type"] != "Bearer" || !strings.HasPrefix(tok["access_token"].(string), "noted_") ||
		resp.Header.Get("Cache-Control") != "no-store" || tok["refresh_token"] != nil {
		t.Fatalf("token: %d %v %v", sc, tok, resp.Header)
	}
	access := tok["access_token"].(string)

	// The token is a normal noted token of alice, labelled with the client's name.
	after, err := o.acct.ListTokens(as(o.alice), &pb.ListTokensRequest{})
	if err != nil || len(after.Tokens) != len(before.Tokens)+1 || after.Tokens[0].Label != "Claude Code" {
		t.Fatalf("ListTokens: %+v %v", after, err)
	}
	cs, err := httpSession(t, o.base, access)
	if err != nil {
		t.Fatalf("MCP with the issued token: %v", err)
	}
	if cs.InitializeResult().ServerInfo.Name != "noted" {
		t.Fatalf("server info: %+v", cs.InitializeResult().ServerInfo)
	}
	if a, err := o.acct.GetAccount(as(access), &pb.GetAccountRequest{}); err != nil || a.UserName != "alice" {
		t.Fatalf("token's user: %+v %v", a, err)
	}

	// The code works once.
	if sc, m, _ := o.exchange(s, q.Get("code"), s.pk.verifier, redirect); sc != 400 || m["error"] != "invalid_grant" {
		t.Fatalf("reused code: %d %v", sc, m)
	}
}

// approve runs a sign-in to the point where the client holds an auth code.
func (o *oauthEnv) approved(clientID, redirect, state, label string) (signIn, string) {
	o.t.Helper()
	s := o.start(clientID, redirect, state, nil)
	if _, err := o.acct.ApprovePairing(as(o.alice), &pb.ApprovePairingRequest{Code: s.code, Label: label}); err != nil {
		o.t.Fatal(err)
	}
	st := o.status(s)
	q := callback(o.t, st["redirect"].(string), redirect+"?")
	return s, q.Get("code")
}

func TestOAuthTokenRefusals(t *testing.T) {
	o := startOAuth(t)
	_, reg := o.register("http://127.0.0.1:40000/cb")
	clientID := reg["client_id"].(string)
	redirect := "http://127.0.0.1:40000/cb"

	// A wrong verifier fails, and burns the code.
	s, c := o.approved(clientID, redirect, "a", "")
	if sc, m, _ := o.exchange(s, c, newPKCE("someone-else").verifier, redirect); sc != 400 || m["error"] != "invalid_grant" {
		t.Fatalf("bad verifier: %d %v", sc, m)
	}
	if sc, m, _ := o.exchange(s, c, s.pk.verifier, redirect); sc != 400 || m["error"] != "invalid_grant" {
		t.Fatalf("code after a failed attempt: %d %v", sc, m)
	}

	// The redirect URI must be the one the code was issued for.
	s, c = o.approved(clientID, redirect, "b", "")
	if sc, m, _ := o.exchange(s, c, s.pk.verifier, "http://127.0.0.1:40001/cb"); sc != 400 || m["error"] != "invalid_grant" {
		t.Fatalf("wrong redirect_uri: %d %v", sc, m)
	}

	// Another client cannot redeem it.
	s, c = o.approved(clientID, redirect, "c", "mine")
	s.clientID = "not-the-client"
	if sc, m, _ := o.exchange(s, c, s.pk.verifier, redirect); sc != 400 || m["error"] != "invalid_grant" {
		t.Fatalf("other client: %d %v", sc, m)
	}

	if resp, body := o.postForm("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}}, nil); resp.StatusCode != 400 ||
		jsonOf(t, body)["error"] != "unsupported_grant_type" {
		t.Fatalf("refresh grant: %d %s", resp.StatusCode, body)
	}
	if resp, body := o.postForm("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "code": {"nope"}, "client_id": {clientID},
		"redirect_uri": {redirect}, "code_verifier": {s.pk.verifier}}, nil); resp.StatusCode != 400 || jsonOf(t, body)["error"] != "invalid_grant" {
		t.Fatalf("unknown code: %d %s", resp.StatusCode, body)
	}

	// No tokens came out of any of it.
	ts, _ := o.acct.ListTokens(as(o.alice), &pb.ListTokensRequest{})
	for _, tk := range ts.Tokens {
		if tk.Label != "test" {
			t.Errorf("unexpected token %+v", tk)
		}
	}
}

func TestOAuthAuthorizeRefusals(t *testing.T) {
	o := startOAuth(t)
	_, reg := o.register("http://localhost:33418/callback")
	clientID := reg["client_id"].(string)
	redirect := "http://localhost:33418/callback"
	pk := newPKCE("x")

	// Unknown client or redirect: an error page, never a redirect.
	for name, u := range map[string]string{
		"unknown client":   o.authorizeURL("nope", redirect, "s", pk, nil),
		"foreign redirect": o.authorizeURL(clientID, "https://evil.example.com/cb", "s", pk, nil),
		"other path":       o.authorizeURL(clientID, "http://localhost:33418/other", "s", pk, nil),
		"no redirect":      o.authorizeURL(clientID, "", "s", pk, nil),
	} {
		resp, body := o.get(u, nil)
		if resp.StatusCode != 400 || resp.Header.Get("Location") != "" || !strings.Contains(body, "pair-msg") {
			t.Errorf("%s: %d %q", name, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
	// The rest go back to the client with an error.
	for name, c := range map[string]struct {
		extra url.Values
		err   string
	}{
		"no PKCE":          {url.Values{"code_challenge": {""}}, "invalid_request"},
		"plain PKCE":       {url.Values{"code_challenge_method": {"plain"}}, "invalid_request"},
		"token flow":       {url.Values{"response_type": {"token"}}, "unsupported_response_type"},
		"foreign resource": {url.Values{"resource": {"https://elsewhere.example.com/mcp"}}, "invalid_target"},
	} {
		resp, _ := o.get(o.authorizeURL(clientID, redirect, "s1", pk, c.extra), nil)
		q := callback(t, resp.Header.Get("Location"), redirect+"?")
		if resp.StatusCode != http.StatusSeeOther || q.Get("error") != c.err || q.Get("state") != "s1" {
			t.Errorf("%s: %d %v", name, resp.StatusCode, q)
		}
	}
}

func TestOAuthPairingEnds(t *testing.T) {
	o := startOAuth(t)
	_, reg := o.register("http://localhost:33418/callback")
	clientID := reg["client_id"].(string)
	redirect := "http://localhost:33418/callback"

	if _, err := o.acct.GetPairing(as(o.alice), &pb.GetPairingRequest{Code: "ZZZZ-ZZZZ"}); code(err) != codes.NotFound {
		t.Fatalf("unknown code: %v", err)
	}

	// Denied from the App: the client is told, and the code is gone.
	s := o.start(clientID, redirect, "d", nil)
	if _, err := o.acct.DenyPairing(as(o.alice), &pb.DenyPairingRequest{Code: s.code}); err != nil {
		t.Fatal(err)
	}
	st := o.status(s)
	q := callback(t, st["redirect"].(string), redirect+"?")
	if st["status"] != "denied" || q.Get("error") != "access_denied" || q.Get("state") != "d" || q.Get("code") != "" {
		t.Fatalf("denied: %v", st)
	}
	if _, err := o.acct.ApprovePairing(as(o.alice), &pb.ApprovePairingRequest{Code: s.code}); code(err) != codes.NotFound {
		t.Fatalf("approve after deny: %v", err)
	}

	// Expired: the App cannot find it, and the page tells the client.
	s = o.start(clientID, redirect, "e", nil)
	db, err := sql.Open("sqlite", "file:"+o.db+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE pairings SET expires_ms = ? WHERE code = ?`, time.Now().Add(-time.Second).UnixMilli(), strings.ReplaceAll(s.code, "-", "")); err != nil {
		t.Fatal(err)
	}
	if _, err := o.acct.GetPairing(as(o.alice), &pb.GetPairingRequest{Code: s.code}); code(err) != codes.NotFound {
		t.Fatalf("expired code: %v", err)
	}
	st = o.status(s)
	q = callback(t, st["redirect"].(string), redirect+"?")
	if st["status"] != "expired" || q.Get("error") != "access_denied" || !strings.Contains(st["message"].(string), "过期") {
		t.Fatalf("expired: %v", st)
	}
	if _, body := o.get("/oauth/pair?p="+s.poll, nil); !strings.Contains(body, "配对码已过期") || codeRe.MatchString(body) {
		t.Fatalf("expired page still shows the code: %s", body)
	}
}

func TestOAuthPairingGuessesAreLimited(t *testing.T) {
	o := startOAuth(t)
	for i := range 5 {
		if _, err := o.acct.GetPairing(as(o.bob), &pb.GetPairingRequest{Code: "ZZZZZZZ" + string(rune('A'+i))}); code(err) != codes.NotFound {
			t.Fatalf("guess %d: %v", i, err)
		}
	}
	if _, err := o.acct.GetPairing(as(o.bob), &pb.GetPairingRequest{Code: "ZZZZZZZZ"}); code(err) != codes.ResourceExhausted {
		t.Fatalf("sixth guess: %v", err)
	}
	if _, err := o.acct.ApprovePairing(as(o.bob), &pb.ApprovePairingRequest{Code: "ZZZZZZZZ"}); code(err) != codes.ResourceExhausted {
		t.Fatalf("approve while locked: %v", err)
	}
}

func TestOAuthApproveInTheSignedInBrowser(t *testing.T) {
	o := startOAuth(t)
	_, reg := o.register("http://localhost:33418/callback")
	clientID := reg["client_id"].(string)
	redirect := "http://localhost:33418/callback"
	session := map[string]string{"Cookie": portal.CookieName + "=" + o.alice}

	s := o.start(clientID, redirect, "b1", session)
	if !strings.Contains(s.page, "以 alice 身份允许") {
		t.Fatalf("page lacks the approve button: %s", s.page)
	}
	form := url.Values{"p": {s.poll}}
	// Cross-site posts are refused.
	for name, h := range map[string]string{"Origin": "https://evil.example.com", "Sec-Fetch-Site": "cross-site"} {
		hdr := map[string]string{"Cookie": session["Cookie"], name: h}
		if resp, _ := o.postForm("/oauth/pair/approve", form, hdr); resp.StatusCode != http.StatusForbidden {
			t.Errorf("approve with %s %s: %d", name, h, resp.StatusCode)
		}
	}
	if resp, _ := o.postForm("/oauth/pair/approve", form, map[string]string{"Cookie": session["Cookie"]}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("approve with no origin: %d", resp.StatusCode)
	}
	resp, _ := o.postForm("/oauth/pair/approve", form, map[string]string{"Cookie": session["Cookie"], "Origin": o.base})
	q := callback(t, resp.Header.Get("Location"), redirect+"?")
	if resp.StatusCode != http.StatusSeeOther || q.Get("code") == "" || q.Get("state") != "b1" {
		t.Fatalf("approve: %d %v", resp.StatusCode, q)
	}
	if sc, tok, _ := o.exchange(s, q.Get("code"), s.pk.verifier, redirect); sc != 200 || tok["access_token"] == nil {
		t.Fatalf("token: %d %v", sc, tok)
	}

	// Without a session the button is not offered, and deny goes back to the client.
	s = o.start(clientID, redirect, "b2", nil)
	resp, _ = o.postForm("/oauth/pair/deny", url.Values{"p": {s.poll}}, map[string]string{"Origin": o.base})
	q = callback(t, resp.Header.Get("Location"), redirect+"?")
	if resp.StatusCode != http.StatusSeeOther || q.Get("error") != "access_denied" || q.Get("state") != "b2" {
		t.Fatalf("deny: %d %v", resp.StatusCode, q)
	}
	if _, err := o.acct.GetPairing(as(o.alice), &pb.GetPairingRequest{Code: s.code}); code(err) != codes.NotFound {
		t.Fatalf("denied pairing still found: %v", err)
	}
}
