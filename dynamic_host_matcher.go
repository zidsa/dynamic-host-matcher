// Package dynamichost provides a Caddy HTTP matcher that dynamically loads
// host lists from HTTP endpoints. This is particularly useful for SaaS
// applications where the list of valid hosts changes frequently.
//
// Example usage in Caddyfile:
//
//	@dynamic_hosts {
//		dynamic_host {
//			source https://api.example.com/hosts
//			interval 30s
//		}
//	}
//
// The source endpoint should return JSON in the format:
//
//	{"hosts": ["example.com", "app1.example.com", "*.wildcard.com"]}
package dynamichost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(new(Module))
}

// Module implements a Caddy HTTP request matcher that dynamically loads
// host lists from HTTP endpoints.
type Module struct {
	// Source is the HTTP/HTTPS URL endpoint that provides the JSON host list.
	Source string `json:"source,omitempty"`

	// Interval specifies how often to refresh the host list from the source.
	// Default: 30s if not specified.
	Interval caddy.Duration `json:"interval,omitempty"`

	ctx     caddy.Context
	u       *url.URL
	client  *http.Client
	mu      sync.RWMutex
	matcher caddyhttp.MatchHost
	logger  *zap.Logger
}

func (m *Module) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.matchers.dynamic_host",
		New: func() caddy.Module { return new(Module) },
	}
}

func (m *Module) Provision(ctx caddy.Context) error {
	if m.Source == "" {
		return fmt.Errorf("dynamic_host matcher: source URL is required")
	}

	u, err := url.Parse(m.Source)
	if err != nil {
		return fmt.Errorf("dynamic_host matcher: invalid source URL '%s': %w", m.Source, err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("dynamic_host matcher: source URL must use http or https scheme, got '%s'", u.Scheme)
	}

	m.u = u
	m.client = &http.Client{Timeout: 5 * time.Second}
	m.ctx = ctx
	m.matcher = caddyhttp.MatchHost{}
	m.logger = ctx.Logger()

	if m.Interval == 0 {
		m.Interval = caddy.Duration(30 * time.Second)
	}

	m.logger.Info("initializing dynamic host matcher",
		zap.String("source", m.Source),
		zap.Duration("interval", time.Duration(m.Interval)))

	// Initial fetch
	if err := m.refreshHosts(ctx); err != nil {
		m.logger.Warn("failed to fetch initial hosts", zap.Error(err))
	}

	go m.refreshLoop()
	return nil
}

func (m *Module) Match(req *http.Request) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.matcher.Match(req)
}

func (m *Module) refreshLoop() {
	ticker := time.NewTicker(time.Duration(m.Interval))
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.refreshHosts(m.ctx); err != nil {
				m.logger.Error("failed to refresh hosts", zap.Error(err))
			}
		case <-m.ctx.Done():
			return
		}
	}
}

// refreshHosts fetches and updates the host list from the source endpoint.
func (m *Module) refreshHosts(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Caddy-Dynamic-Host-Matcher/1.0")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("endpoint returned status %d", resp.StatusCode)
	}

	var data struct {
		Hosts []string `json:"hosts"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("failed to decode JSON: %w", err)
	}

	if len(data.Hosts) == 0 {
		return fmt.Errorf("empty host list returned")
	}

	// Validate hosts
	for _, host := range data.Hosts {
		if strings.TrimSpace(host) == "" {
			return fmt.Errorf("invalid empty host in list")
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	oldCount := len(m.matcher)
	m.matcher = data.Hosts

	if err := m.matcher.Provision(m.ctx); err != nil {
		return fmt.Errorf("failed to provision matcher: %w", err)
	}

	m.logger.Info("updated host list",
		zap.Int("count", len(data.Hosts)),
		zap.Int("previous", oldCount))

	return nil
}

func (m *Module) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next() // consume directive name

	for d.NextBlock(0) {
		switch d.Val() {
		case "source":
			if !d.NextArg() {
				return d.ArgErr()
			}
			m.Source = d.Val()

			if _, err := url.Parse(m.Source); err != nil {
				return d.Errf("invalid source URL: %v", err)
			}

		case "interval":
			if !d.NextArg() {
				return d.ArgErr()
			}

			interval, err := caddy.ParseDuration(d.Val())
			if err != nil {
				return d.Errf("invalid interval: %v", err)
			}

			if interval < time.Second {
				return d.Errf("interval too short, minimum is 1s")
			}
			if interval > 24*time.Hour {
				return d.Errf("interval too long, maximum is 24h")
			}

			m.Interval = caddy.Duration(interval)

		default:
			return d.Errf("unrecognized parameter '%s'", d.Val())
		}
	}

	if m.Source == "" {
		return d.Err("source parameter is required")
	}

	return nil
}

var (
	_ caddy.Module             = (*Module)(nil)
	_ caddy.Provisioner        = (*Module)(nil)
	_ caddyhttp.RequestMatcher = (*Module)(nil)
	_ caddyfile.Unmarshaler    = (*Module)(nil)
)
