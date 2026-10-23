// Command server serves a Hugo site from inside its own binary, and only to
// readers who hold a valid Hanko session and a required role.
//
// This is a proof of concept. It has no rate limiting, no security headers, no
// server timeouts, no trusted-proxy configuration and no graceful shutdown. See
// the README before you put anything like it on the internet.
package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net"
	"os"

	"github.com/gin-gonic/gin"
)

// A bare `dist`, not `all:dist`. The plain pattern skips files whose names
// start with a dot or an underscore, so a stray `.env` or `.DS_Store` in the
// build folder never makes it into the binary. The static handler refuses such
// paths as well; this keeps them out of the release in the first place.
//
//go:embed dist
var content embed.FS

func main() {
	configPath := flag.String("config", "demo.toml", "path to the configuration file")
	addr := flag.String("addr", "localhost:8080", "address to listen on")
	devSubject := flag.String("dev-subject", "", "skip Hanko and treat every request as this user ID (local only)")
	flag.Parse()

	cfg, err := LoadConfig(*configPath, os.Getenv("HANKO_API_URL"), *devSubject != "")
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	site, err := fs.Sub(content, "dist")
	if err != nil {
		log.Fatalf("embedded site: %v", err)
	}
	if _, err := fs.Stat(site, "index.html"); err != nil {
		log.Fatal("the embedded site has no index.html: build the Hugo site before the server (make build)")
	}

	gate := &Gate{
		Validator:   HankoValidator{Endpoint: cfg.Auth.Endpoint},
		Grants:      cfg.Roles(),
		RequireRole: cfg.Auth.RequireRole,
		Passthrough: cfg.Auth.Passthrough,
		LoginPath:   cfg.Auth.Login,
	}

	if *devSubject != "" {
		// A stand-in for a signed-in user, so the demo runs without a Hanko
		// project. Refuse it on anything but loopback: a server that believes
		// every visitor is signed in must never be reachable from outside.
		if !isLoopback(*addr) {
			log.Fatalf("--dev-subject only works on a loopback address, not %q", *addr)
		}
		log.Printf("WARNING: dev mode, every request is treated as user %s", *devSubject)
		gate.DevSubject = *devSubject
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// Gin trusts every proxy's X-Forwarded-For by default. Nothing here uses
	// the client IP, but trusting nobody is the honest default.
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Fatal(err)
	}

	// There are no routes. Every request falls through to NoRoute, which runs
	// the gate first and the file server second.
	r.NoRoute(gate.Handler(), NewStatic(site).Handler())

	log.Printf("listening on http://%s", *addr)
	log.Fatal(r.Run(*addr))
}

// isLoopback reports whether addr (host:port) binds to a loopback interface
// only. An empty host binds to every interface, so it is not loopback.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
