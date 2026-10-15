package portal

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed static
var static embed.FS

// originMarker in index.html is replaced, per request, with the scheme and
// host the page was reached on, so Open Graph URLs are absolute.
const originMarker = "{{ORIGIN}}"

type asset struct {
	body  []byte
	ctype string
	etag  string // strong, quoted
	ver   string // short content hash, the ?v= that may be cached forever
}

// site serves the embedded page and its assets. index.html links every asset
// as "/name?v=<hash>"; a request carrying the current hash may be cached for
// a year, anything else is revalidated with the ETag.
type site struct {
	index  []byte
	assets map[string]*asset // by URL path, e.g. "/app.css"
	public string            // Options.PublicURL
}

var contentTypes = map[string]string{
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
	".jpg":  "image/jpeg",
	".png":  "image/png",
}

func mustLoadSite() *site {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		panic(err)
	}
	s := &site{assets: map[string]*asset{}}
	err = fs.WalkDir(sub, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(sub, name)
		if err != nil {
			return err
		}
		if name == "index.html" {
			s.index = b
			return nil
		}
		ct, ok := contentTypes[path.Ext(name)]
		if !ok {
			panic("portal: no content type for " + name)
		}
		sum := sha256.Sum256(b)
		h := hex.EncodeToString(sum[:])
		s.assets["/"+name] = &asset{body: b, ctype: ct, etag: `"` + h[:32] + `"`, ver: h[:12]}
		return nil
	})
	if err != nil || s.index == nil {
		panic("portal: embedded site is incomplete")
	}
	// Longest names first so "/img/a.webp" is not rewritten by a shorter match.
	names := make([]string, 0, len(s.assets))
	for n := range s.assets {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	idx := string(s.index)
	for _, n := range names {
		idx = strings.ReplaceAll(idx, `"`+n+`"`, `"`+n+"?v="+s.assets[n].ver+`"`)
		idx = strings.ReplaceAll(idx, originMarker+n+`"`, originMarker+n+"?v="+s.assets[n].ver+`"`)
	}
	s.index = []byte(idx)
	return s
}

func (s *site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h := w.Header()
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		origin := html.EscapeString(BaseURL(r, s.public))
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(bytes.ReplaceAll(s.index, []byte(originMarker), []byte(origin))))
		return
	}
	a, ok := s.assets[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.Set("Content-Type", a.ctype)
	h.Set("ETag", a.etag)
	if r.URL.Query().Get("v") == a.ver {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(a.body))
}

// StylesheetURL is the cache-busting URL of the page's stylesheet, for the
// other pages on this host that share its look (the MCP sign-in page).
var StylesheetURL = sync.OnceValue(func() string {
	b, err := static.ReadFile("static/app.css")
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return "/app.css?v=" + hex.EncodeToString(sum[:])[:12]
})
