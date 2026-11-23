package app

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Which control planes LendSpare may talk to. A website never names one: the deep link and the
// typed code only select from this list, and a connection's bearer token goes to its own
// origin and nowhere else (docs/PROTOCOL.md, "Words").

// productionOrigins are the providers new connections may be made to.
var productionOrigins = []string{"https://lendspare.com", "https://share.brainybrain.com"}

// legacyOrigins are allowed only for a connection that was migrated from a schema-1 config:
// that server was the default of the previous program, so existing pairings keep working,
// but nothing new can be connected to it.
var legacyOrigins = []string{"https://ai.artur.work"}

// originPolicy adds the development origins given with --dev-origin to the fixed lists.
type originPolicy struct {
	dev []string // normalised
}

func newOriginPolicy(dev []string) (originPolicy, error) {
	p := originPolicy{}
	for _, d := range dev {
		n, err := normalizeOrigin(d)
		if err != nil {
			return p, fmt.Errorf("--dev-origin %q: %w", d, err)
		}
		p.dev = append(p.dev, n)
	}
	return p, nil
}

// normalizeOrigin returns scheme://host[:port] in lower case with a default port removed, or
// an error when the value is anything but a bare origin (no credentials, path, query, fragment).
func normalizeOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", errors.New("not an origin")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", errors.New("an origin must be http or https")
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("an origin has no credentials, path, query or fragment")
	}
	host := strings.ToLower(u.Host)
	switch {
	case u.Scheme == "https" && strings.HasSuffix(host, ":443"):
		host = strings.TrimSuffix(host, ":443")
	case u.Scheme == "http" && strings.HasSuffix(host, ":80"):
		host = strings.TrimSuffix(host, ":80")
	}
	return u.Scheme + "://" + host, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// allowed says whether a connection may be made to, or kept with, this origin. legacy is the
// connection's own flag: only a migrated one may use a legacy origin.
func (p originPolicy) allowed(origin string, legacy bool) error {
	n, err := normalizeOrigin(origin)
	if err != nil {
		return err
	}
	switch {
	case contains(productionOrigins, n), contains(p.dev, n), legacy && contains(legacyOrigins, n):
		return nil
	}
	return fmt.Errorf("%s is not an allowed provider", hostOf(n))
}

var hostParamRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?$`)

// fromParam turns the deep link's `origin=<host[:port]>` into an allowed origin. It takes a
// host and nothing else: a URL, a path or credentials are refused outright.
func (p originPolicy) fromParam(param string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(param))
	if h == "" || !hostParamRe.MatchString(h) {
		return "", errors.New("the link does not name a provider")
	}
	if contains(productionOrigins, "https://"+h) {
		return "https://" + h, nil
	}
	for _, d := range p.dev {
		if hostOf(d) == h {
			return d, nil
		}
	}
	return "", fmt.Errorf("%s is not an allowed provider", h)
}

// byHost finds an allowed origin by its host (the `--provider` flag and the provider list).
func (p originPolicy) byHost(host string) (string, error) {
	return p.fromParam(host)
}

// providers lists what the Connect screen offers: the production providers first.
func (p originPolicy) providers() []string {
	return append(append([]string{}, productionOrigins...), p.dev...)
}
