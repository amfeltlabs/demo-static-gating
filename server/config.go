package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config mirrors demo.toml.
type Config struct {
	Auth struct {
		Endpoint    string   `toml:"endpoint"`
		Login       string   `toml:"login"`
		RequireRole string   `toml:"require-role"`
		Passthrough []string `toml:"passthrough"`
	} `toml:"auth"`

	Grants []struct {
		Subject string   `toml:"subject"`
		Roles   []string `toml:"roles"`
	} `toml:"grant"`
}

// LoadConfig reads the file at path. A non-empty endpoint overrides the one in
// the file. dev relaxes the endpoint requirement, because dev mode never calls
// Hanko.
func LoadConfig(path, endpoint string, dev bool) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, err
	}
	if endpoint != "" {
		c.Auth.Endpoint = endpoint
	}
	return &c, c.validate(dev)
}

func (c *Config) validate(dev bool) error {
	var errs []error
	if c.Auth.Endpoint == "" && !dev {
		errs = append(errs, errors.New("auth.endpoint is empty: set HANKO_API_URL or edit the config"))
	}
	if !strings.HasPrefix(c.Auth.Login, "/") {
		errs = append(errs, fmt.Errorf("auth.login must be a path starting with /, got %q", c.Auth.Login))
	}
	// A valid session alone is not enough when anyone can sign up. Refuse to
	// start without a role rather than quietly let every account in.
	if c.Auth.RequireRole == "" {
		errs = append(errs, errors.New("auth.require-role is empty: a session alone would admit anyone who can sign up"))
	}
	for i, g := range c.Grants {
		if g.Subject == "" {
			errs = append(errs, fmt.Errorf("grant %d has no subject", i+1))
		}
	}
	return errors.Join(errs...)
}

// Roles returns the grants as a lookup table from Hanko user ID to roles.
func (c *Config) Roles() map[string][]string {
	m := make(map[string][]string, len(c.Grants))
	for _, g := range c.Grants {
		m[g.Subject] = append(m[g.Subject], g.Roles...)
	}
	return m
}
