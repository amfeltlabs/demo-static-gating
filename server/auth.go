package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// sessionCookie is where Hanko Elements keeps the session token.
	sessionCookie = "hanko"

	// returnCookie remembers the page a reader asked for while they sign in.
	// site/assets/js/critical/return-path.js reads it on /post-login/.
	returnCookie       = "demo_return_to"
	returnCookieMaxAge = 600 // seconds
)

// forbiddenBody is what a signed-in reader without the role sees. It is
// written inline rather than served from the site: a page in the site sits
// behind the same gate, so the denial would redirect to itself. It names no
// role, because a stranger has no use for the shape of your access model.
var forbiddenBody = []byte(`<!doctype html><html lang="en"><head><meta charset="utf-8">` +
	`<title>Not authorized</title></head><body><h1>Not authorized</h1>` +
	`<p>You are signed in, but your account does not have access to this site.</p>` +
	`<p><a href="/login/">Switch account</a></p></body></html>`)

// SessionValidator decides whether a session token is valid, and whose it is.
type SessionValidator interface {
	Validate(ctx context.Context, token string) (subject string, valid bool, err error)
}

// HankoValidator asks the Hanko API about a session token.
//
// Every gated request makes one call, which is fine for a demo and a reason to
// add a short-lived cache before real traffic arrives.
type HankoValidator struct {
	Endpoint string
}

func (v HankoValidator) Validate(ctx context.Context, token string) (string, bool, error) {
	body, err := json.Marshal(map[string]string{"session_token": token})
	if err != nil {
		return "", false, err
	}
	u := strings.TrimSuffix(v.Endpoint, "/") + "/sessions/validate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("hanko answered %s", res.Status)
	}

	var out struct {
		IsValid bool `json:"is_valid"`
		Claims  struct {
			Subject string `json:"subject"`
		} `json:"claims"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", false, err
	}
	return out.Claims.Subject, out.IsValid && out.Claims.Subject != "", nil
}

// Gate lets a request through only when it is on the passthrough list, or when
// it carries a valid session whose user holds RequireRole.
type Gate struct {
	Validator   SessionValidator
	Grants      map[string][]string // Hanko user ID -> roles
	RequireRole string
	Passthrough []string
	LoginPath   string

	// DevSubject, when set, replaces the session check: every request is
	// treated as this user. main only allows it on a loopback address.
	DevSubject string
}

func (g *Gate) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Match on the cleaned path, the same one the file server will open.
		// Matching the raw path would let /css/../docs/ through as "/css/".
		p := cleanPath(c.Request.URL.Path)
		if g.isPassthrough(p) {
			c.Next()
			return
		}

		subject, err := g.subject(c)
		if err != nil {
			log.Printf("session check failed: %v", err)
			c.AbortWithStatus(http.StatusBadGateway)
			return
		}
		if subject == "" {
			g.toLogin(c)
			return
		}

		// Authorization comes after authentication, so the user ID it keys on
		// is one Hanko vouched for. No role earns a 403, never a redirect: the
		// login page would sign the reader straight back in and loop.
		if !slices.Contains(g.Grants[subject], g.RequireRole) {
			c.Header("Cache-Control", "private, no-store")
			c.Data(http.StatusForbidden, "text/html; charset=utf-8", forbiddenBody)
			c.Abort()
			return
		}

		c.Next()
	}
}

// subject returns the signed-in user's ID, or "" when there is no valid
// session.
func (g *Gate) subject(c *gin.Context) (string, error) {
	if g.DevSubject != "" {
		return g.DevSubject, nil
	}
	token, err := c.Cookie(sessionCookie)
	if err != nil || token == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	subject, valid, err := g.Validator.Validate(ctx, token)
	if err != nil || !valid {
		return "", err
	}
	return subject, nil
}

// isPassthrough reports whether p is public. An entry may hold one "*",
// which matches any run of characters, slashes included: "/css/*" covers
// the folder, and "/js/*.mjs" every .mjs file below /js/. An entry without
// "*" must match exactly. Everything a passthrough entry matches is readable
// without signing in, so keep the list to what the login page itself loads.
func (g *Gate) isPassthrough(p string) bool {
	for _, entry := range g.Passthrough {
		prefix, suffix, wildcard := strings.Cut(entry, "*")
		if !wildcard {
			if p == entry {
				return true
			}
			continue
		}
		if len(p) >= len(prefix)+len(suffix) &&
			strings.HasPrefix(p, prefix) && strings.HasSuffix(p, suffix) {
			return true
		}
	}
	return false
}

// toLogin redirects to the login page. For a page load it first saves the
// requested path, so /post-login/ can send the reader back to it.
func (g *Gate) toLogin(c *gin.Context) {
	if c.Request.Method == http.MethodGet && strings.Contains(c.GetHeader("Accept"), "text/html") {
		target := c.Request.URL.RequestURI()
		if len(target) <= 2048 {
			http.SetCookie(c.Writer, &http.Cookie{
				Name:     returnCookie,
				Value:    url.PathEscape(target), // decodeURIComponent reads this back
				Path:     "/",
				MaxAge:   returnCookieMaxAge,
				SameSite: http.SameSiteLaxMode,
				Secure:   c.Request.TLS != nil,
				HttpOnly: false, // return-path.js has to read it
			})
		}
	}
	c.Header("Cache-Control", "private, no-store")
	c.Redirect(http.StatusFound, g.LoginPath)
	c.Abort()
}

// cleanPath resolves "." and ".." and duplicate slashes, and keeps a trailing
// slash, so "/docs/" and "/docs" stay distinguishable.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	cleaned := path.Clean("/" + p)
	if strings.HasSuffix(p, "/") && cleaned != "/" {
		cleaned += "/"
	}
	return cleaned
}
