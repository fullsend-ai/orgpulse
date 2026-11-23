package netguard

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		allowPrivate bool
		wantErr      bool
	}{
		{"valid https", "https://api.github.com", false, false},
		{"valid http", "http://jira.example.com", false, false},
		{"loopback IP", "http://127.0.0.1:8080", false, true},
		{"private 10.x", "http://10.0.0.1:8080", false, true},
		{"private 192.168.x", "http://192.168.1.1:8080", false, true},
		{"private 172.16.x", "http://172.16.0.1:8080", false, true},
		{"cgnat", "http://100.100.100.200/latest/meta-data", false, true},
		{"link-local", "http://169.254.169.254/latest/meta-data", false, true},
		{"unspecified v4", "http://0.0.0.0:8080", false, true},
		{"unspecified v6", "http://[::]:8080", false, true},
		{"this network", "http://0.1.2.3", false, true},
		{"mapped link-local", "http://[::ffff:169.254.169.254]/", false, true},
		{"localhost hostname", "http://localhost:8080", false, true},
		{"dot-local hostname", "http://service.local:8080", false, true},
		{"dot-internal hostname", "http://devlake.internal:8080", false, true},
		{"ftp scheme", "ftp://evil.com/file", false, true},
		{"empty host", "http:///path", false, true},
		{"private allowed: 10.x", "http://10.0.0.1:8080", true, false},
		{"private allowed: loopback", "http://127.0.0.1:8080", true, false},
		{"private allowed: cluster name", "http://devlake.svc.cluster.local:8080", true, false},
		{"private allowed: link-local still blocked", "http://169.254.169.254/latest/meta-data", true, true},
		{"private allowed: unspecified still blocked", "http://0.0.0.0:8080", true, true},
		{"private allowed: ftp still blocked", "ftp://evil.com/file", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(tt.url, tt.allowPrivate)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL(%q, %v) error = %v, wantErr %v", tt.url, tt.allowPrivate, err, tt.wantErr)
			}
		})
	}
}

func TestClientBlocksAtDialTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	resp, err := Client(5*time.Second, true, false).Get(srv.URL)
	if err != nil {
		t.Fatalf("allowPrivate client: %v", err)
	}
	resp.Body.Close()

	resp, err = Client(5*time.Second, false, false).Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected loopback dial to be refused when private endpoints are not allowed")
	}
}

func TestClientBlocksRedirectToMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	defer srv.Close()

	resp, err := Client(2*time.Second, true, false).Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected redirect to link-local address to be refused")
	}
}

func TestCheckIP(t *testing.T) {
	if err := CheckIP(net.ParseIP("8.8.8.8"), false); err != nil {
		t.Errorf("public address refused: %v", err)
	}
	if err := CheckIP(net.ParseIP("fd00::1"), false); err == nil {
		t.Error("unique-local address allowed without allowPrivate")
	}
	if err := CheckIP(net.ParseIP("fe80::1"), true); err == nil {
		t.Error("link-local v6 address allowed")
	}
}
