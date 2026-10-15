package oauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	pb "github.com/liliang-cn/noted/gen/noted/v1"
	"github.com/liliang-cn/noted/internal/auth"
	"github.com/liliang-cn/noted/internal/portal"
	"github.com/liliang-cn/noted/internal/service"
	"github.com/liliang-cn/noted/internal/store"
	"rsc.io/qr"
)

//go:embed static
var static embed.FS

var pageTmpl = template.Must(template.ParseFS(static, "static/pair.html"))

type asset struct {
	body  []byte
	ctype string
	ver   string
}

func loadAssets() map[string]*asset {
	out := map[string]*asset{}
	for name, ct := range map[string]string{"pair.css": "text/css; charset=utf-8", "pair.js": "text/javascript; charset=utf-8"} {
		b, err := static.ReadFile("static/" + name)
		if err != nil {
			panic(err)
		}
		sum := sha256.Sum256(b)
		out[name] = &asset{body: b, ctype: ct, ver: hex.EncodeToString(sum[:])[:12]}
	}
	return out
}

func (s *server) assetURL(name string) string { return "/oauth/" + name + "?v=" + s.assets[name].ver }

func (s *server) serveAsset(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assets[path.Base(r.PathValue("asset"))]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", a.ctype)
	if r.URL.Query().Get("v") == a.ver {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(a.body))
}

// ---- authorize ----

// matchRedirect finds raw among the client's redirect URIs. A loopback
// redirect matches on any port (RFC 8252 §7.3): native clients such as Claude
// Code listen on whatever port is free.
func matchRedirect(c store.OAuthClient, raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	for _, reg := range c.RedirectURIs {
		if raw == reg {
			return true
		}
		ru, err := url.Parse(reg)
		if err == nil && u.Scheme == "http" && ru.Scheme == "http" && isLoopback(u.Hostname()) &&
			u.Hostname() == ru.Hostname() && u.Path == ru.Path && u.RawQuery == ru.RawQuery && u.User == nil {
			return true
		}
	}
	return false
}

// validChallenge checks the shape of an S256 code challenge: 43 base64url
// characters.
func validChallenge(c string) bool {
	if len(c) != 43 {
		return false
	}
	for _, r := range c {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (s *server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, err := s.st.OAuthClient(r.Context(), q.Get("client_id"))
	if errors.Is(err, store.ErrNotFound) || q.Get("client_id") == "" {
		s.errorPage(w, r, http.StatusBadRequest, "这个客户端没有在 noted 登记过。回到客户端重新连接。")
		return
	}
	if err != nil {
		slog.Error("oauth: authorize", "err", err)
		s.errorPage(w, r, http.StatusInternalServerError, "服务器出错了，请稍后再试。")
		return
	}
	redirect := q.Get("redirect_uri")
	if redirect == "" || !matchRedirect(c, redirect) {
		s.errorPage(w, r, http.StatusBadRequest, "回调地址和客户端登记的不一致，无法继续。")
		return
	}
	// From here on errors go back to the client, which shows them.
	state := q.Get("state")
	fail := func(kind, desc string) {
		http.Redirect(w, r, redirectWith(redirect, url.Values{"error": {kind}, "error_description": {desc}, "state": {state}}), http.StatusSeeOther)
	}
	resource := q.Get("resource")
	switch {
	case q.Get("response_type") != "code":
		fail("unsupported_response_type", "only response_type=code is supported")
	case q.Get("code_challenge_method") != "S256" || !validChallenge(q.Get("code_challenge")):
		fail("invalid_request", "PKCE with code_challenge_method=S256 is required")
	case len(state) > 1024:
		fail("invalid_request", "state is too long")
	case resource != "" && !s.resourceOK(r, resource):
		fail("invalid_target", "unknown resource "+resource)
	default:
		_, poll, err := s.st.CreatePairing(r.Context(), store.Pairing{
			ClientID: c.ID, ClientName: c.Name, RedirectURI: redirect, CodeChallenge: q.Get("code_challenge"), State: state, Resource: resource,
		})
		if err != nil {
			slog.Error("oauth: create pairing", "err", err)
			s.errorPage(w, r, http.StatusInternalServerError, "服务器出错了，请稍后再试。")
			return
		}
		// The page lives at its own URL so a reload shows the same code.
		http.Redirect(w, r, pairPath+"?p="+poll, http.StatusSeeOther)
	}
}

// ---- the pairing page ----

type pageData struct {
	AppCSS, PairCSS, PairJS string
	Title                   string
	ClientName              string
	Error                   string // the request cannot go on; nothing else is shown
	Message                 string // the pairing has ended
	Pending                 bool
	Code                    string // K7QM-29XD
	QR                      template.HTML
	Poll                    string
	Left                    int // seconds until it expires
	LeftText                string
	UserName                string // this browser's portal session, if any
}

func (s *server) render(w http.ResponseWriter, code int, d pageData) {
	d.AppCSS, d.PairCSS, d.PairJS = portal.StylesheetURL(), s.assetURL("pair.css"), s.assetURL("pair.js")
	if d.Title == "" {
		d.Title = "连接到 noted"
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Frame-Options", "DENY")
	// No form-action: the approve and deny forms answer with a redirect to
	// the client's own (loopback) address.
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'")
	var buf bytes.Buffer
	if err := pageTmpl.Execute(&buf, d); err != nil {
		slog.Error("oauth: render", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(code)
	_, _ = w.Write(buf.Bytes())
}

func (s *server) errorPage(w http.ResponseWriter, _ *http.Request, code int, msg string) {
	s.render(w, code, pageData{Error: msg})
}

// DisplayCode groups a pairing code for reading: K7QM29XD -> K7QM-29XD.
func DisplayCode(code string) string {
	if len(code) != 8 {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// PairURL is what the QR code holds: the App opens it and offers to approve.
func PairURL(code, base string) string {
	return "noted://pair?" + url.Values{"code": {code}, "server": {base}}.Encode()
}

// endedMessage says why a pairing can no longer be approved.
func endedMessage(p store.Pairing, now time.Time) string {
	switch {
	case p.Issued:
		return "已连接，可以关闭这个页面。"
	case p.Denied:
		return "已拒绝。"
	case !now.Before(p.Expires):
		return fmt.Sprintf("配对码已过期。回到 %s 重新连接。", p.ClientName)
	}
	return ""
}

// sessionUser is the user signed in to the web portal in this browser.
func (s *server) sessionUser(r *http.Request) (store.User, string, bool) {
	c, err := r.Cookie(portal.CookieName)
	if err != nil || c.Value == "" {
		return store.User{}, "", false
	}
	u, id, err := s.auth.Verify(r.Context(), c.Value)
	return u, id, err == nil
}

func (s *server) pairPage(w http.ResponseWriter, r *http.Request) {
	poll := r.URL.Query().Get("p")
	p, err := s.st.PairingByPoll(r.Context(), poll)
	if errors.Is(err, store.ErrNotFound) || poll == "" {
		s.errorPage(w, r, http.StatusNotFound, "这个配对已经结束或过期了。回到客户端重新连接。")
		return
	}
	if err != nil {
		slog.Error("oauth: pairing page", "err", err)
		s.errorPage(w, r, http.StatusInternalServerError, "服务器出错了，请稍后再试。")
		return
	}
	now := time.Now()
	d := pageData{Title: "连接 " + p.ClientName + " 到 noted", ClientName: p.ClientName, Poll: poll}
	if !p.Live(now) {
		d.Message = endedMessage(p, now)
		s.render(w, http.StatusOK, d)
		return
	}
	svg, err := qrSVG(PairURL(p.Code, s.base(r)))
	if err != nil {
		slog.Error("oauth: qr", "err", err)
	}
	left := int(p.Expires.Sub(now).Seconds())
	d.Pending, d.Code, d.QR, d.Left, d.LeftText = true, DisplayCode(p.Code), svg, left, fmt.Sprintf("%d:%02d", left/60, left%60)
	if u, _, ok := s.sessionUser(r); ok && (p.UserID == "" || p.UserID == u.ID) {
		d.UserName = u.Name
	}
	s.render(w, http.StatusOK, d)
}

// pairStatus is what the page polls. Once the pairing is approved, the first
// poll turns it into an authorization code and the page goes to the client.
func (s *server) pairStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	poll := r.URL.Query().Get("p")
	p, err := s.st.PairingByPoll(r.Context(), poll)
	if errors.Is(err, store.ErrNotFound) || poll == "" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "gone", "message": "这个配对已经结束或过期了。回到客户端重新连接。"})
		return
	}
	if err != nil {
		slog.Error("oauth: pairing status", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "error"})
		return
	}
	now := time.Now()
	switch {
	case p.Issued:
		writeJSON(w, http.StatusOK, map[string]string{"status": "done", "message": endedMessage(p, now)})
	case p.Denied:
		writeJSON(w, http.StatusOK, map[string]string{"status": "denied", "message": endedMessage(p, now), "redirect": clientDenied(p, "the pairing was denied")})
	case !now.Before(p.Expires):
		writeJSON(w, http.StatusOK, map[string]string{"status": "expired", "message": endedMessage(p, now), "redirect": clientDenied(p, "the pairing code expired")})
	case p.UserID != "":
		to, err := s.issue(r.Context(), poll)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "pending"}) // ended in between; the next poll says how
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "approved", "message": "已允许，正在回到 " + p.ClientName + "…", "redirect": to})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": "pending"})
	}
}

// issue mints the authorization code of an approved pairing and returns the
// client redirect that carries it.
func (s *server) issue(ctx context.Context, poll string) (string, error) {
	p, code, err := s.st.IssueAuthCode(ctx, poll)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("oauth: issue code", "err", err)
		}
		return "", err
	}
	return redirectWith(p.RedirectURI, url.Values{"code": {code}, "state": {p.State}}), nil
}

// sameOrigin admits a form post only from this site's own page: the Origin
// header names this host, or, without one, Sec-Fetch-Site says same-origin.
func (s *server) sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		ou, err := url.Parse(o)
		bu, err2 := url.Parse(s.base(r))
		return err == nil && err2 == nil && strings.EqualFold(ou.Host, bu.Host)
	}
	return r.Header.Get("Sec-Fetch-Site") == "same-origin"
}

func (s *server) backToPage(w http.ResponseWriter, r *http.Request, poll string) {
	http.Redirect(w, r, pairPath+"?p="+url.QueryEscape(poll), http.StatusSeeOther)
}

// pairApprove approves from the page itself, as the browser's portal session.
func (s *server) pairApprove(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		s.errorPage(w, r, http.StatusForbidden, "请在 noted 的配对页面上操作。")
		return
	}
	poll := r.PostFormValue("p")
	p, err := s.st.PairingByPoll(r.Context(), poll)
	if err != nil || poll == "" {
		s.errorPage(w, r, http.StatusNotFound, "这个配对已经结束或过期了。回到客户端重新连接。")
		return
	}
	u, id, ok := s.sessionUser(r)
	if !ok {
		s.backToPage(w, r, poll)
		return
	}
	ctx := auth.WithToken(service.WithClientIP(r.Context(), service.TrustedClientIP(r.RemoteAddr, r.Header.Values("X-Forwarded-For"))), u, id)
	if _, err := s.acct.ApprovePairing(ctx, &pb.ApprovePairingRequest{Code: p.Code}); err != nil {
		s.backToPage(w, r, poll)
		return
	}
	to, err := s.issue(r.Context(), poll)
	if err != nil {
		s.backToPage(w, r, poll)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func (s *server) pairDeny(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		s.errorPage(w, r, http.StatusForbidden, "请在 noted 的配对页面上操作。")
		return
	}
	poll := r.PostFormValue("p")
	if poll == "" {
		s.errorPage(w, r, http.StatusNotFound, "这个配对已经结束或过期了。回到客户端重新连接。")
		return
	}
	p, err := s.st.DenyPairingByPoll(r.Context(), poll)
	if err != nil {
		s.backToPage(w, r, poll)
		return
	}
	http.Redirect(w, r, clientDenied(p, "the pairing was denied"), http.StatusSeeOther)
}

// qrSVG draws text as a QR code, one path of dark modules with the standard
// four-module quiet zone.
func qrSVG(text string) (template.HTML, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 4
	n := c.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img" aria-label="配对二维码"><rect width="%d" height="%d" fill="#fff"/><path fill="#0B0B0F" d="`, n, n, n, n)
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; {
			if !c.Black(x, y) {
				x++
				continue
			}
			run := 1
			for c.Black(x+run, y) {
				run++
			}
			fmt.Fprintf(&b, "M%d %dh%dv1h-%dz", x+quiet, y+quiet, run, run)
			x += run
		}
	}
	b.WriteString(`"/></svg>`)
	return template.HTML(b.String()), nil //nolint:gosec // built from numbers only
}
