package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// A content-addressed file can be cached for a year and never
	// revalidated: its name changes whenever its contents do. "immutable"
	// stops a reload from revalidating anyway (RFC 8246).
	immutableCache = "public, max-age=31536000, immutable"

	// Everything else revalidates on every request, which the ETag keeps
	// cheap. "private" keeps shared caches, such as a CDN in front of the
	// server, from storing a gated page and handing it to someone else.
	revalidateCache = "private, no-cache"
)

// fingerprinted matches the names Hugo's resources.Fingerprint produces, such
// as main.min.<sha256>.css. It matches the hash rather than a folder, because
// /css/ and /js/ also hold files that keep their name when their contents
// change, and pinning one of those for a year would strand a stale copy in
// every browser.
var fingerprinted = regexp.MustCompile(`\.[0-9a-f]{64}\.(?:css|js|mjs)$`)

// Static serves files from the embedded Hugo output.
//
// http.FileServer would get most of the way, but embedded files have no
// modification time, so it has nothing to answer a conditional request with.
// Static adds a content-hash ETag, per-file Cache-Control, pretty URLs without
// redirects, and a proper 404 page.
type Static struct {
	site  fs.FS
	etags sync.Map // file name -> quoted ETag, never evicted
}

func NewStatic(site fs.FS) *Static {
	return &Static{site: site}
}

func (s *Static) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.AbortWithStatus(http.StatusMethodNotAllowed)
			return
		}

		p := cleanPath(c.Request.URL.Path)
		// Refuse any segment starting with a dot or an underscore, such as
		// /.env or /img/.DS_Store, even though the embed pattern should have
		// kept them out. A refusal looks exactly like a missing file.
		if isHidden(p) {
			s.notFound(c)
			return
		}

		name, ok := s.resolve(p)
		if !ok {
			s.notFound(c)
			return
		}

		f, err := s.site.Open(name)
		if err != nil {
			s.notFound(c)
			return
		}
		defer f.Close()
		rs, ok := f.(io.ReadSeeker)
		if !ok {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		etag, err := s.etag(name, rs)
		if err != nil {
			log.Printf("etag %s: %v", name, err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		c.Header("ETag", etag)
		if fingerprinted.MatchString(name) {
			c.Header("Cache-Control", immutableCache)
		} else {
			c.Header("Cache-Control", revalidateCache)
		}

		// ServeContent compares If-None-Match with the ETag header set above
		// and answers 304 with no body when they match. It also handles HEAD,
		// ranges and the Content-Type. The zero time means no Last-Modified.
		http.ServeContent(c.Writer, c.Request, name, time.Time{}, rs)
		c.Abort()
	}
}

// resolve maps a URL path to a file in the site. Hugo writes /docs/ as
// docs/index.html, so a directory resolves to its index.html.
func (s *Static) resolve(p string) (string, bool) {
	name := strings.Trim(p, "/")
	if name == "" {
		name = "."
	}
	info, err := fs.Stat(s.site, name)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		name = strings.TrimPrefix(name+"/index.html", "./")
		if _, err := fs.Stat(s.site, name); err != nil {
			return "", false
		}
	}
	return name, true
}

// etag returns the file's quoted SHA-256, hashing it only the first time.
// Embedded files cannot change until the binary restarts, so the cache never
// needs to forget anything. The quotes are part of the value: net/http only
// matches If-None-Match against a quoted entity tag.
func (s *Static) etag(name string, rs io.ReadSeeker) (string, error) {
	if v, ok := s.etags.Load(name); ok {
		return v.(string), nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, rs); err != nil {
		return "", err
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	etag := `"` + hex.EncodeToString(h.Sum(nil)) + `"`
	s.etags.Store(name, etag)
	return etag, nil
}

// notFound serves Hugo's 404.html with a 404 status.
func (s *Static) notFound(c *gin.Context) {
	c.Header("Cache-Control", revalidateCache)
	body, err := fs.ReadFile(s.site, "404.html")
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Data(http.StatusNotFound, "text/html; charset=utf-8", body)
	c.Abort()
}

// isHidden reports whether any segment of p starts with a dot or an
// underscore.
func isHidden(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg != "" && (seg[0] == '.' || seg[0] == '_') {
			return true
		}
	}
	return false
}
