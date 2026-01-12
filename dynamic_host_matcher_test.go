package dynamichost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func TestModule_UnmarshalCaddyfile(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantSrc string
		wantInt time.Duration
	}{
		{
			name: "valid config",
			input: `dynamic_host {
				source https://api.example.com/hosts
				interval 30s
			}`,
			wantSrc: "https://api.example.com/hosts",
			wantInt: 30 * time.Second,
		},
		{
			name: "missing source",
			input: `dynamic_host {
				interval 30s
			}`,
			wantErr: true,
		},
		{
			name: "invalid interval",
			input: `dynamic_host {
				source https://api.example.com/hosts
				interval invalid
			}`,
			wantErr: true,
		},
		{
			name: "interval too short",
			input: `dynamic_host {
				source https://api.example.com/hosts
				interval 500ms
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := caddyfile.NewTestDispenser(tt.input)
			m := &Module{}

			err := m.UnmarshalCaddyfile(d)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalCaddyfile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if m.Source != tt.wantSrc {
					t.Errorf("Source = %v, want %v", m.Source, tt.wantSrc)
				}
				if time.Duration(m.Interval) != tt.wantInt {
					t.Errorf("Interval = %v, want %v", time.Duration(m.Interval), tt.wantInt)
				}
			}
		})
	}
}

func TestModule_RefreshHosts(t *testing.T) {
	tests := []struct {
		name        string
		setupServer func() *httptest.Server
		wantErr     bool
	}{
		{
			name: "valid response",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"hosts": ["example.com", "test.com"]}`))
				}))
			},
			wantErr: false,
		},
		{
			name: "404 error",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.NotFound(w, r)
				}))
			},
			wantErr: true,
		},
		{
			name: "invalid JSON",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte("invalid json"))
				}))
			},
			wantErr: true,
		},
		{
			name: "empty host list",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"hosts": []}`))
				}))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setupServer()
			defer server.Close()

			m := newTestModule(t, server.URL)
			err := m.refreshHosts(context.Background())

			if (err != nil) != tt.wantErr {
				t.Errorf("refreshHosts() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// newTestModule creates a properly initialized module for testing
func newTestModule(t *testing.T, sourceURL string) *Module {
	t.Helper()

	m := &Module{
		Source: sourceURL,
	}

	u, err := url.Parse(sourceURL)
	if err != nil {
		t.Fatalf("Failed to parse test URL: %v", err)
	}

	m.u = u
	m.client = &http.Client{Timeout: 5 * time.Second}
	m.logger = zap.NewNop()
	m.mu = sync.RWMutex{}
	m.matcher = caddyhttp.MatchHost{}
	m.ctx = caddy.Context{}

	return m
}
