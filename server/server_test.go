package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

const hashedCSS = "css/main.min.0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.css"

// fakeValidator accepts the tokens it knows and maps them to user IDs.
type fakeValidator map[string]string

func (f fakeValidator) Validate(_ context.Context, token string) (string, bool, error) {
	subject, ok := f[token]
	return subject, ok, nil
}

func newTestServer() *gin.Engine {
	gin.SetMode(gin.TestMode)
	site := fstest.MapFS{
		"index.html":                  {Data: []byte("home")},
		"404.html":                    {Data: []byte("not found")},
		"docs/index.html":             {Data: []byte("docs")},
		"login/index.html":            {Data: []byte("login")},
		"css/main.css":                {Data: []byte("body{}")},
		hashedCSS:                     {Data: []byte("body{}")},
		"css/.env":                    {Data: []byte("SECRET=1")},
		"img/favicon.png":             {Data: []byte("png")},
		"img/diagram.svg":             {Data: []byte("<svg/>")},
		"js/core.bundle.js":           {Data: []byte("core")},
		"js/chunks/a.mjs":             {Data: []byte("chunk")},
		"js/flexsearch-index.en.json": {Data: []byte(`[{"content":"secret"}]`)},
	}
	gate := &Gate{
		Validator: fakeValidator{
			"staff-token":    "user-staff",
			"stranger-token": "user-stranger",
		},
		Grants:      map[string][]string{"user-staff": {"staff"}},
		RequireRole: "staff",
		Passthrough: []string{"/login/", "/css/*", "/img/favicon*", "/js/*.js", "/js/*.mjs"},
		LoginPath:   "/login/",
	}
	r := gin.New()
	r.NoRoute(gate.Handler(), NewStatic(site).Handler())
	return r
}

func do(r http.Handler, target, token string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGate(t *testing.T) {
	r := newTestServer()
	tests := []struct {
		name, target, token string
		want                int
	}{
		{"no session redirects to login", "/docs/", "", http.StatusFound},
		{"unknown token redirects to login", "/docs/", "forged", http.StatusFound},
		{"valid session without the role is forbidden", "/docs/", "stranger-token", http.StatusForbidden},
		{"valid session with the role is served", "/docs/", "staff-token", http.StatusOK},
		{"login page is public", "/login/", "", http.StatusOK},
		{"assets are public", "/css/main.css", "", http.StatusOK},
		{"traversal out of a public folder is gated", "/css/../docs/", "", http.StatusFound},
		{"login page images are public", "/img/favicon.png", "", http.StatusOK},
		{"content images are gated", "/img/diagram.svg", "", http.StatusFound},
		{"login page scripts are public", "/js/core.bundle.js", "", http.StatusOK},
		{"nested script chunks are public", "/js/chunks/a.mjs", "", http.StatusOK},
		{"the search index is gated", "/js/flexsearch-index.en.json", "", http.StatusFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := do(r, tt.target, tt.token).Code; got != tt.want {
				t.Errorf("GET %s = %d, want %d", tt.target, got, tt.want)
			}
		})
	}
}

func TestLoginRedirectSavesReturnPath(t *testing.T) {
	w := do(newTestServer(), "/docs/?q=1", "", "Accept", "text/html")
	if loc := w.Header().Get("Location"); loc != "/login/" {
		t.Fatalf("Location = %q, want /login/", loc)
	}
	var found bool
	for _, c := range w.Result().Cookies() {
		if c.Name == returnCookie {
			found = true
			if c.Value != "%2Fdocs%2F%3Fq=1" {
				t.Errorf("return cookie = %q", c.Value)
			}
		}
	}
	if !found {
		t.Error("no return cookie on a page load")
	}
}

func TestHiddenFilesAreNotServed(t *testing.T) {
	// /css/* is public, and a .env was embedded anyway; it must still 404.
	if got := do(newTestServer(), "/css/.env", "").Code; got != http.StatusNotFound {
		t.Errorf("GET /css/.env = %d, want 404", got)
	}
}

func TestETagRevalidation(t *testing.T) {
	r := newTestServer()
	first := do(r, "/docs/", "staff-token")
	etag := first.Header().Get("ETag")
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Fatalf("ETag %q is not quoted", etag)
	}
	if cc := first.Header().Get("Cache-Control"); cc != revalidateCache {
		t.Errorf("Cache-Control = %q, want %q", cc, revalidateCache)
	}

	again := do(r, "/docs/", "staff-token", "If-None-Match", etag)
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Errorf("revalidation = %d with %d bytes, want 304 with none", again.Code, again.Body.Len())
	}
}

func TestCacheControl(t *testing.T) {
	r := newTestServer()
	if cc := do(r, "/"+hashedCSS, "").Header().Get("Cache-Control"); cc != immutableCache {
		t.Errorf("fingerprinted: Cache-Control = %q, want %q", cc, immutableCache)
	}
	if cc := do(r, "/css/main.css", "").Header().Get("Cache-Control"); cc != revalidateCache {
		t.Errorf("unfingerprinted: Cache-Control = %q, want %q", cc, revalidateCache)
	}
}

func TestMissingPageIs404(t *testing.T) {
	w := do(newTestServer(), "/nope/", "staff-token")
	if w.Code != http.StatusNotFound || w.Body.String() != "not found" {
		t.Errorf("GET /nope/ = %d %q, want 404 with the 404 page", w.Code, w.Body.String())
	}
}

func TestConfigRefusesMissingRole(t *testing.T) {
	var c Config
	c.Auth.Endpoint = "https://example.hanko.io"
	c.Auth.Login = "/login/"
	if err := c.validate(false); err == nil || !strings.Contains(err.Error(), "require-role") {
		t.Errorf("validate() = %v, want a require-role error", err)
	}
}

func TestIsLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"localhost:8080": true,
		"127.0.0.1:8080": true,
		"[::1]:8080":     true,
		":8080":          false,
		"0.0.0.0:8080":   false,
	} {
		if got := isLoopback(addr); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

// TestShippedPassthrough checks the list in demo.toml itself, so widening an
// entry there (back to "/js/*" or "/img/*", say) fails here.
func TestShippedPassthrough(t *testing.T) {
	cfg, err := LoadConfig("demo.toml", "", true)
	if err != nil {
		t.Fatal(err)
	}
	g := &Gate{Passthrough: cfg.Auth.Passthrough}
	for p, public := range map[string]bool{
		"/login/":                        true,
		"/js/core.bundle.en.0123.js":     true,
		"/js/hanko-elements.min.mjs":     true,
		"/img/favicon_hu_0123.png":       true,
		"/js/flexsearch-index.en.json":   false,
		"/js/core.bundle.en.0123.js.map": false,
		"/img/architecture/diagram.svg":  false,
		"/docs/":                         false,
		"/index.xml":                     false,
	} {
		if got := g.isPassthrough(p); got != public {
			t.Errorf("isPassthrough(%q) = %v, want %v", p, got, public)
		}
	}
}
