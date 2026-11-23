package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// spaIndexHTML stands in for the embedded PatternFly shell.
const spaIndexHTML = "<html>dashboard-shell</html>"

// pprofIndexMarker appears in the pprof.Index page and nowhere in the SPA shell.
const pprofIndexMarker = "Types of profiles available"

func newContractServer(enablePprof bool) *Server {
	return New(0, enablePprof, fstest.MapFS{}, nil, nil, nil, FrontendConfig{}, nil, AIAssistConfig{}, nil, nil, "")
}

func serve(srv *Server, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func TestHealthRoutes(t *testing.T) {
	srv := newContractServer(false)

	for _, path := range []string{"/health/live", "/health/ready"} {
		w := serve(srv, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("GET %s: got %d, want 200", path, w.Code)
			continue
		}
		var body map[string]string
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Errorf("GET %s: decode body: %v", path, err)
			continue
		}
		if body["status"] != "ok" {
			t.Errorf("GET %s: status = %q, want %q", path, body["status"], "ok")
		}
	}

	for _, path := range []string{"/health/live", "/health/ready"} {
		if w := serve(srv, httptest.NewRequest("POST", path, nil)); w.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: got %d, want 405", path, w.Code)
		}
	}

	// The explicit GET /health route returns 404 in production too, so the
	// SPA catch-all never serves the dashboard shell for this path.
	if w := serve(srv, httptest.NewRequest("GET", "/health", nil)); w.Code != http.StatusNotFound {
		t.Errorf("GET /health: got %d, want 404", w.Code)
	}
}

func newSPAServer(enablePprof bool) *Server {
	return New(0, enablePprof, fstest.MapFS{"index.html": {Data: []byte(spaIndexHTML)}}, nil, nil, nil, FrontendConfig{}, nil, AIAssistConfig{}, nil, nil, "")
}

// TestPPROFDefaultOff pins that pprof handlers are absent unless explicitly
// enabled. They are not: the SPA catch-all ("GET /") claims the path, so the
// response is the dashboard shell rather than the pprof index (a 404 would
// require an explicit route, which production does not register).
func TestPPROFDefaultOff(t *testing.T) {
	w := serve(newSPAServer(false), httptest.NewRequest("GET", "/debug/pprof/", nil))
	if w.Code != http.StatusOK {
		t.Errorf("GET /debug/pprof/: got %d, want 200 from the SPA catch-all", w.Code)
	}
	if body := w.Body.String(); strings.Contains(body, pprofIndexMarker) {
		t.Errorf("GET /debug/pprof/ served the pprof index with pprof disabled: %q", body)
	} else if body != spaIndexHTML {
		t.Errorf("GET /debug/pprof/ body = %q, want the SPA shell %q", body, spaIndexHTML)
	}
}

func TestPPROFEnabled(t *testing.T) {
	w := serve(newSPAServer(true), httptest.NewRequest("GET", "/debug/pprof/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /debug/pprof/: got %d, want 200", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, pprofIndexMarker) {
		t.Errorf("GET /debug/pprof/ did not serve the pprof index: %q", body)
	}
}

func TestAPIAndMetricsRoutes(t *testing.T) {
	srv := newContractServer(false)

	w := serve(srv, httptest.NewRequest("GET", "/api/config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/config: got %d, want 200", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, `"test_mode"`) {
		t.Errorf("GET /api/config: body %q missing \"test_mode\"", body)
	}

	w = serve(srv, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /metrics: got %d, want 200", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "go_") {
		t.Errorf("GET /metrics: body missing a go_ metric")
	}

	for _, path := range []string{"/chart", "/sendreport"} {
		w := serve(srv, httptest.NewRequest("POST", path, strings.NewReader("{}")))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("POST %s anonymous: got %d, want 401", path, w.Code)
		}
	}
}

func TestContractSecurityHeaders(t *testing.T) {
	srv := newContractServer(false)
	w := serve(srv, httptest.NewRequest("GET", "/health/live", nil))

	for k, want := range map[string]string{
		"X-Content-Type-Options":      "nosniff",
		"X-Frame-Options":             "SAMEORIGIN",
		"Referrer-Policy":             "strict-origin-when-cross-origin",
		"Access-Control-Allow-Origin": "*",
	} {
		if got := w.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("Content-Security-Policy = %q, want default-src 'self'", csp)
	}
}

// fillerReader yields n bytes of 'a' without allocating them all.
type fillerReader struct{ remaining int }

func (f *fillerReader) Read(p []byte) (int, error) {
	if f.remaining <= 0 {
		return 0, io.EOF
	}
	if len(p) > f.remaining {
		p = p[:f.remaining]
	}
	for i := range p {
		p[i] = 'a'
	}
	f.remaining -= len(p)
	return len(p), nil
}

func TestRequestBodyCap(t *testing.T) {
	h := middlewareChain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
		}
	}))

	for _, tc := range []struct {
		name string
		size int
		want int
	}{
		{"at cap", maxRequestBody, http.StatusOK},
		{"over cap", maxRequestBody + 1, http.StatusBadRequest},
	} {
		req := httptest.NewRequest("POST", "/anything", &fillerReader{remaining: tc.size})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("%s (%d bytes): got %d, want %d", tc.name, tc.size, w.Code, tc.want)
		}
	}
}

func TestServerTimeouts(t *testing.T) {
	srv := New(9090, false, fstest.MapFS{}, nil, nil, nil, FrontendConfig{}, nil, AIAssistConfig{}, nil, nil, "")

	if got := srv.srv.Addr; got != ":9090" {
		t.Errorf("Addr = %q, want %q", got, ":9090")
	}
	for name, tc := range map[string]struct {
		got  time.Duration
		want time.Duration
	}{
		"ReadHeaderTimeout": {srv.srv.ReadHeaderTimeout, 10 * time.Second},
		"ReadTimeout":       {srv.srv.ReadTimeout, 2 * time.Minute},
		"WriteTimeout":      {srv.srv.WriteTimeout, 10 * time.Minute},
		"IdleTimeout":       {srv.srv.IdleTimeout, 2 * time.Minute},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", name, tc.got, tc.want)
		}
	}
}
