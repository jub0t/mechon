// Package config reads the panel's settings from the environment. There is no config file.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Panel struct {
	Listen      string   // MECHON_LISTEN, default ":8080"
	DatabaseURL string   // MECHON_DATABASE_URL, required
	PublicURL   *url.URL // MECHON_PUBLIC_URL, required: where browsers reach the panel
	TrustProxy  bool     // MECHON_TRUST_PROXY: take the client IP from X-Forwarded-For
}

func LoadPanel() (Panel, error) {
	c := Panel{
		Listen:      env("MECHON_LISTEN", ":8080"),
		DatabaseURL: os.Getenv("MECHON_DATABASE_URL"),
		TrustProxy:  env("MECHON_TRUST_PROXY", "false") == "true",
	}
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("MECHON_DATABASE_URL is required"))
	}
	raw := os.Getenv("MECHON_PUBLIC_URL")
	if raw == "" {
		errs = append(errs, errors.New("MECHON_PUBLIC_URL is required (e.g. https://panel.example.com)"))
	} else if u, err := url.Parse(strings.TrimRight(raw, "/")); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("MECHON_PUBLIC_URL %q is not an http(s) URL", raw))
	} else {
		c.PublicURL = u
	}
	return c, errors.Join(errs...)
}

// Origin is the scheme://host[:port] browsers send in the Origin header.
func (c Panel) Origin() string { return c.PublicURL.Scheme + "://" + c.PublicURL.Host }

// SecureCookies is on whenever the panel is served over HTTPS.
func (c Panel) SecureCookies() bool { return c.PublicURL.Scheme == "https" }

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
