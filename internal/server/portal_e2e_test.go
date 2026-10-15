package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/mcphttp"
	"github.com/liliang-cn/noted/internal/portal"
)

type browser struct {
	t    *testing.T
	base string
	c    *http.Client
}

func startPortal(t *testing.T, signup bool) (*env, *browser) {
	t.Helper()
	e, acct := startAccounts(t, nil, signup)
	a, err := auth.New(t.Context(), e.st, false)
	if err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(mcphttp.New(mcphttp.Options{Auth: a, Conn: e.conn(t), Location: time.UTC, Version: "test", Account: acct}))
	t.Cleanup(hs.Close)
	jar, _ := cookiejar.New(nil)
	return e, &browser{t: t, base: hs.URL, c: &http.Client{Jar: jar}}
}

// do sends a request; csrf adds the X-Noted header every page call sends.
func (b *browser) do(method, path, body string, csrf bool) (int, map[string]any, *http.Response) {
	b.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, b.base+path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf {
		req.Header.Set(portal.CSRFHeader, "1")
	}
	resp, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out, resp
}

func (b *browser) cookie() string {
	for _, c := range b.c.Jar.Cookies(mustURL(b.base)) {
		if c.Name == portal.CookieName {
			return c.Value
		}
	}
	return ""
}

func TestPortalPageAndCSRF(t *testing.T) {
	_, b := startPortal(t, true)
	resp, err := http.Get(b.base + "/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(page), "接入 AI 助手") || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatalf("page: %d %.200s", resp.StatusCode, page)
	}
	for _, f := range []string{"/app.js", "/app.css"} {
		if r, err := http.Get(b.base + f); err != nil || r.StatusCode != 200 {
			t.Fatalf("%s: %v %v", f, err, r)
		} else {
			r.Body.Close()
		}
	}

	// Without the custom header every write is refused, before anything happens.
	if code, _, _ := b.do("POST", "/api/signup", `{"email":"pat@example.com","password":"pat-pass-1"}`, false); code != http.StatusForbidden {
		t.Fatalf("signup without X-Noted: %d", code)
	}
	if code, _, _ := b.do("POST", "/api/login", `{"email":"pat@example.com","password":"pat-pass-1"}`, true); code != http.StatusUnauthorized {
		t.Fatalf("the CSRF-refused signup must not have created the account: %d", code)
	}
	if code, _, _ := b.do("GET", "/api/me", "", false); code != http.StatusUnauthorized {
		t.Fatalf("me without a session: %d", code)
	}
}

func get(t *testing.T, url string, hdr map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

// The product page and every asset it links are embedded, typed, versioned
// by content hash and cacheable; nothing is loaded from elsewhere.
func TestPortalSiteAssets(t *testing.T) {
	_, b := startPortal(t, true)

	resp, page := get(t, b.base+"/", nil)
	html := string(page)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/html; charset=utf-8" || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("page: %d %v", resp.StatusCode, resp.Header)
	}
	for _, want := range []string{"<title>Noted</title>", `name="description"`, "接入 AI 助手", "自己部署", "docker compose up -d", "noted user add", "笔记 Markdown、日程 iCalendar、全部数据 JSON、交易记录 CSV", "noted.json", "calendar.ics", "trades.csv",
		"https://github.com/liliang-cn/noted", `og:image" content="` + b.base + "/img/og-today.jpg?v=",
		"claude mcp add --scope user --transport http noted " + b.base + "/mcp", "&lt;令牌&gt;"} {
		if !strings.Contains(html, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(html, "{{ORIGIN}}") || regexp.MustCompile(`(src|href)="(https?:)?//[^"]*\.(js|css|woff2?)`).MatchString(html) {
		t.Error("page has an unreplaced origin or an external script/style/font")
	}
	// Behind TLS the absolute URLs are https.
	if _, p := get(t, b.base+"/", map[string]string{"X-Forwarded-Proto": "https"}); !strings.Contains(string(p), `content="https://`) {
		t.Error("og URLs are not https behind a TLS proxy")
	}

	refs := regexp.MustCompile(`"(?:`+regexp.QuoteMeta(b.base)+`)?(/[a-z0-9/_.-]+)\?v=([0-9a-f]{12})"`).FindAllStringSubmatch(html, -1)
	types := map[string]string{".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".webp": "image/webp", ".jpg": "image/jpeg", ".png": "image/png", ".svg": "image/svg+xml"}
	seen := map[string]bool{}
	imgBytes := 0
	for _, m := range refs {
		p, v := m[1], m[2]
		if seen[p] {
			continue
		}
		seen[p] = true
		r, body := get(t, b.base+p+"?v="+v, nil)
		if r.StatusCode != 200 || r.Header.Get("Content-Type") != types[path.Ext(p)] || len(body) == 0 {
			t.Fatalf("%s: %d %q", p, r.StatusCode, r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" || r.Header.Get("ETag") == "" || r.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s headers: %v", p, r.Header)
		}
		if strings.HasPrefix(p, "/img/") {
			imgBytes += len(body)
		}
		if r2, _ := get(t, b.base+p, map[string]string{"If-None-Match": r.Header.Get("ETag")}); r2.StatusCode != http.StatusNotModified || r2.Header.Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s revalidation: %d %q", p, r2.StatusCode, r2.Header.Get("Cache-Control"))
		}
	}
	for _, want := range []string{"/app.css", "/app.js", "/icon.svg", "/img/og-today.jpg", "/img/apple-touch-icon.png",
		"/img/today.webp", "/img/plan.webp", "/img/cal.webp", "/img/todo.webp", "/img/note.webp", "/img/project.webp", "/img/goal.webp", "/img/invest.webp", "/img/ai.webp"} {
		if !seen[want] {
			t.Errorf("page does not link %s", want)
		}
	}
	if imgBytes > 600<<10 {
		t.Errorf("images weigh %d KB", imgBytes>>10)
	}

	// A stale or missing version is served but must be revalidated.
	if r, _ := get(t, b.base+"/app.css?v=000000000000", nil); r.StatusCode != 200 || r.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("stale version: %d %q", r.StatusCode, r.Header.Get("Cache-Control"))
	}
	for _, p := range []string{"/nope", "/img/", "/static/app.css", "/index.html/x"} {
		if r, _ := get(t, b.base+p, nil); r.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d", p, r.StatusCode)
		}
	}
	req, _ := http.NewRequest("HEAD", b.base+"/", nil)
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != 200 {
		t.Fatalf("HEAD /: %v %v", err, r)
	}
	if r, err := http.Post(b.base+"/", "text/plain", nil); err != nil || r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /: %v %v", err, r)
	}
}

func TestPortalSessionAndTokens(t *testing.T) {
	_, b := startPortal(t, true)

	code, out, resp := b.do("POST", "/api/signup", `{"email":"Pat@Example.com","password":"pat-pass-1"}`, true)
	if code != 200 || out["user_name"] != "pat" {
		t.Fatalf("signup: %d %v", code, out)
	}
	sc := resp.Header.Get("Set-Cookie")
	if !strings.Contains(sc, "HttpOnly") || !strings.Contains(sc, "SameSite=Strict") || strings.Contains(sc, "Secure") {
		t.Fatalf("cookie over plain HTTP: %s", sc)
	}
	if code, out, _ := b.do("GET", "/api/me", "", false); code != 200 || out["email"] != "pat@example.com" || !strings.HasSuffix(out["mcp_url"].(string), "/mcp") {
		t.Fatalf("me: %d %v", code, out)
	}
	if code, _, _ := b.do("POST", "/api/signup", `{"email":"pat@example.com","password":"pat-pass-2"}`, true); code != http.StatusConflict {
		t.Fatalf("duplicate signup: %d", code)
	}

	// Mint a token for an assistant; it works on /mcp.
	code, out, _ = b.do("POST", "/api/tokens", `{"label":"Claude Code"}`, true)
	if code != 200 {
		t.Fatalf("create token: %d %v", code, out)
	}
	tok := out["token"].(string)
	info := out["info"].(map[string]any)
	if info["label"] != "Claude Code" || info["current"] != false {
		t.Fatalf("info: %v", info)
	}
	if r := post(t, b.base+"/mcp", "Bearer "+tok, "", initBody); r.StatusCode != 200 {
		t.Fatalf("minted token on /mcp: %d", r.StatusCode)
	}
	if code, _, _ := b.do("POST", "/api/tokens", `{"label":"`+strings.Repeat("x", 61)+`"}`, true); code != http.StatusBadRequest {
		t.Fatalf("long label: %d", code)
	}

	code, out, _ = b.do("GET", "/api/tokens", "", false)
	list, _ := out["tokens"].([]any)
	if code != 200 || len(list) != 2 {
		t.Fatalf("list: %d %v", code, out)
	}
	var web map[string]any
	for _, x := range list {
		if m := x.(map[string]any); m["current"] == true {
			web = m
		}
	}
	if web == nil || web["label"] != portal.SessionLabel {
		t.Fatalf("the browser's own token: %v", list)
	}

	// Revoke the assistant's token (header required): /mcp now refuses it.
	id := info["id"].(string)
	if code, _, _ := b.do("DELETE", "/api/tokens/"+id, "", false); code != http.StatusForbidden {
		t.Fatalf("revoke without X-Noted: %d", code)
	}
	if code, _, _ := b.do("DELETE", "/api/tokens/"+id, "", true); code != 200 {
		t.Fatalf("revoke: %d", code)
	}
	if r := post(t, b.base+"/mcp", "Bearer "+tok, "", initBody); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token on /mcp: %d", r.StatusCode)
	}
	if code, _, _ := b.do("DELETE", "/api/tokens/"+id, "", true); code != http.StatusNotFound {
		t.Fatalf("second revoke: %d", code)
	}

	// Password change needs the current password.
	if code, _, _ := b.do("POST", "/api/password", `{"current_password":"nope-nope","password":"pat-pass-2"}`, true); code != http.StatusForbidden {
		t.Fatalf("wrong current password: %d", code)
	}
	if code, out, _ := b.do("POST", "/api/password", `{"current_password":"pat-pass-1","password":"pat-pass-2"}`, true); code != 200 {
		t.Fatalf("password: %d %v", code, out)
	}

	// Logging out revokes the session token itself.
	session := b.cookie()
	if session == "" {
		t.Fatal("no session cookie")
	}
	if code, _, _ := b.do("POST", "/api/logout", "", true); code != 200 {
		t.Fatalf("logout: %d", code)
	}
	if b.cookie() != "" {
		t.Fatal("cookie kept after logout")
	}
	if code, _, _ := b.do("GET", "/api/me", "", false); code != http.StatusUnauthorized {
		t.Fatalf("me after logout: %d", code)
	}
	if r := post(t, b.base+"/mcp", "Bearer "+session, "", initBody); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the logged-out session token still works: %d", r.StatusCode)
	}

	// Log in again with the new password; the old one fails.
	if code, _, _ := b.do("POST", "/api/login", `{"email":"pat@example.com","password":"pat-pass-1"}`, true); code != http.StatusUnauthorized {
		t.Fatalf("old password: %d", code)
	}
	if code, _, _ := b.do("POST", "/api/login", `{"email":"PAT@example.com","password":"pat-pass-2"}`, true); code != 200 {
		t.Fatalf("login: %d", code)
	}
	if code, out, _ := b.do("GET", "/api/me", "", false); code != 200 || out["user_name"] != "pat" {
		t.Fatalf("me after login: %d %v", code, out)
	}
}

func TestPortalSignupClosedAndSecureCookie(t *testing.T) {
	e, b := startPortal(t, false)
	if code, out, _ := b.do("POST", "/api/signup", `{"email":"q@example.com","password":"12345678"}`, true); code != http.StatusForbidden || !strings.Contains(out["error"].(string), "sign-up is closed") {
		t.Fatalf("closed signup: %d %v", code, out)
	}
	// Behind a TLS proxy the cookie is Secure.
	if _, err := pbAccount(t, e).SetEmailPassword(as(e.alice), setEmailReq("alice@example.com", "alice-pass")); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", b.base+"/api/login", strings.NewReader(`{"email":"alice@example.com","password":"alice-pass"}`))
	req.Header.Set(portal.CSRFHeader, "1")
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Set-Cookie"), "Secure") {
		t.Fatalf("login behind TLS: %d %s", resp.StatusCode, resp.Header.Get("Set-Cookie"))
	}
}
