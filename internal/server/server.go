package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/authz"
	"github.com/fullsend-ai/orgpulse/internal/chart"
	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/internal/devlake"
	"github.com/fullsend-ai/orgpulse/internal/logging"
	"github.com/fullsend-ai/orgpulse/internal/report"
	"github.com/fullsend-ai/orgpulse/schemas"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type FrontendConfig struct {
	AIAnalyticsURL       string          `json:"ai_analytics_url"`
	TestMode             bool            `json:"test_mode"`
	TestEmail            string          `json:"test_email"`
	TestIsAdmin          bool            `json:"test_is_admin,omitempty"`
	DevAdminOverride     bool            `json:"-"`
	DevLakeEnabled       bool            `json:"devlake_enabled"`
	DevLakeSkipTokenTest bool            `json:"devlake_skip_token_test,omitempty"`
	WorkflowBaseURL      string          `json:"workflow_base_url"`
	PublicAPIURL         string          `json:"public_api_url,omitempty"`
	AdminUsers           []string        `json:"-"`
	TestGroups           []string        `json:"-"`
	OperatorGroups       []string        `json:"-"`
	AuthEmailDomains     []string        `json:"-"`
	Membership           authz.Directory `json:"-"`
	// AllowPrivateEndpoints lets user-supplied endpoints resolve to private
	// addresses (DEVLAKE_ALLOW_PRIVATE_ENDPOINTS).
	AllowPrivateEndpoints bool `json:"-"`
}

func (fc FrontendConfig) AuthConfig() AuthConfig {
	return AuthConfig{
		AdminUsers:       fc.AdminUsers,
		DevAdminOverride: fc.DevAdminOverride,
		TestMode:         fc.TestMode,
		TestEmail:        fc.TestEmail,
		TestIsAdmin:      fc.TestIsAdmin,
		TestGroups:       fc.TestGroups,
		OperatorGroups:   fc.OperatorGroups,
		EmailDomains:     fc.AuthEmailDomains,
	}
}

type Server struct {
	srv         *http.Server
	healthCheck *HealthChecker
}

func New(port int, enablePprof bool, staticFS fs.FS, store database.Store, mysqlStore database.ConfigStore, reportSvc *report.Service, frontendCfg FrontendConfig, workflowExec *WorkflowExecutor, aiCfg AIAssistConfig, devlakeSvc *devlake.Service, devlakeDB *sql.DB, workflowExecURL string) *Server {
	if devlakeSvc != nil && devlakeSvc.Enabled() {
		frontendCfg.DevLakeEnabled = true
	}
	authCfg := frontendCfg.AuthConfig()
	az := newAuthorizer(mysqlStore, authCfg, frontendCfg.Membership)
	mux := http.NewServeMux()

	healthCheck := NewHealthChecker(mysqlStore, devlakeDB, workflowExecURL)
	mux.HandleFunc("GET /health/live", healthCheck.LiveHandler)
	mux.HandleFunc("GET /health/ready", healthCheck.ReadyHandler)

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	mux.Handle("GET /metrics", metricsHandler())
	mux.Handle("POST /chart", requireAuth(mysqlStore, authCfg, az)(http.HandlerFunc(chartHandler)))
	mux.Handle("POST /sendreport", requireAdmin(mysqlStore, authCfg, az)(sendReportHandler(reportSvc)))
	mux.HandleFunc("GET /api/config", frontendConfigHandler(frontendCfg, mysqlStore, authCfg))
	mux.HandleFunc("GET /api/v1/config-keys", configKeysHandler)
	mux.HandleFunc("GET /api/v1/dashboard-template-catalog", dashboardTemplateCatalogHandler)
	mux.HandleFunc("GET /api/v1/alert-test-response", alertTestResponseHandler)
	mux.Handle("GET /", spaHandler("/", staticFS))

	if store != nil {
		auth := requireAuth(mysqlStore, authCfg, az)
		mux.Handle("GET /api/orgs", auth(assembledHandler(store.GetOrgs, filterOrgs)))
		mux.Handle("GET /api/products", auth(assembledHandler(store.GetProducts, filterProducts)))
		mux.Handle("GET /api/teams", auth(assembledHandler(store.GetTeams, filterTeams)))
	}

	if mysqlStore != nil {
		registerAPI(mux, mysqlStore, authCfg, workflowExec, devlakeSvc, devlakeDB, az, apiOptions{
			outbound: outboundConfig{
				AllowPrivate:  frontendCfg.AllowPrivateEndpoints,
				TLSSkipVerify: devlakeSvc != nil && devlakeSvc.TLSSkipVerify(),
			},
			trustedReportURLs: trustedReportURLs(frontendCfg, workflowExecURL),
		})
		if aiCfg.Enabled() {
			mux.Handle("POST /api/v1/ai/generate", requireAdmin(mysqlStore, authCfg, az)(aiAssistHandler(aiCfg)))
		}
		mux.HandleFunc("POST /api/v1/ai/analytics", aiAnalyticsHandler())
	}

	if enablePprof {
		mux.HandleFunc("GET /debug/pprof/", pprof.Index)
		mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	}

	if workflowExecURL != "" {
		proxy := workflowProductionProxyHandler(workflowExecURL, func(r *http.Request, body []byte) error {
			if r.Method == http.MethodOptions {
				return nil
			}
			ident, err := authenticateRequest(r, mysqlStore, authCfg)
			if err != nil {
				return err
			}
			return authorizeMetricRaw(r.Context(), az, ident, body, r.URL.Query())
		})
		mux.HandleFunc("GET /workflows/production/{name}", proxy)
		mux.HandleFunc("POST /workflows/production/{name}", proxy)
	}

	return &Server{
		srv: &http.Server{
			Addr:              fmt.Sprintf(":%d", port),
			Handler:           middlewareChain(mux),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       2 * time.Minute,
			WriteTimeout:      10 * time.Minute,
			IdleTimeout:       2 * time.Minute,
		},
		healthCheck: healthCheck,
	}
}

func (s *Server) Handler() http.Handler {
	return s.srv.Handler
}

// middlewareChain composes every cross-cutting concern applied to the mux.
// Order matters: CORS and security headers must survive an early rejection by
// the body cap or the CSRF guard, and logging wraps the request ID so it can
// report it.
func middlewareChain(mux http.Handler) http.Handler {
	return corsMiddleware(securityHeaders(http.MaxBytesHandler(withLogging(withRequestID(crossOriginProtection(mux))), maxRequestBody)))
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	}
}

// maxRequestBody caps every request body. Bulk imports are the largest
// legitimate payloads and stay well below it.
const maxRequestBody = 32 << 20

// maxUpstreamBody caps how much of an upstream response is read.
const maxUpstreamBody = 32 << 20

// crossOriginProtection rejects cross-site browser requests with unsafe
// methods, which is what CSRF against the proxy session cookie needs.
// Requests carrying an API key are exempt: a browser cannot attach that
// header cross-origin with credentials, and API clients are not browsers.
func crossOriginProtection(next http.Handler) http.Handler {
	protected := http.NewCrossOriginProtection().Handler(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if extractAPIKey(r) != "" {
			next.ServeHTTP(w, r)
			return
		}
		protected.ServeHTTP(w, r)
	})
}

// contentSecurityPolicy only allows scripts bundled with the app. Diagram
// endpoints and images may live on other hosts, so connect-src and img-src
// stay open; inline styles are used by PatternFly and Chart.js.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: https: http:; font-src 'self' data:; connect-src *; " +
	"object-src 'none'; base-uri 'self'; frame-ancestors 'self'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-Request-ID")
		w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func metricsHandler() http.Handler {
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

func chartHandler(w http.ResponseWriter, r *http.Request) {
	var req chart.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}
	buf, err := chart.Render(req)
	if err != nil {
		writePublicError(w, r, http.StatusBadRequest, "render error", err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	if req.Subtype != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", req.Subtype+".png"))
	}
	if _, err := w.Write(buf); err != nil {
		logging.FromContext(r.Context()).Error("write error", "err", err, "path", r.URL.Path)
		return
	}
}

func sendReportHandler(svc *report.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			http.Error(w, "report service not configured", http.StatusServiceUnavailable)
			return
		}
		var req struct {
			SubscriptionID int64 `json:"subscription_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
			return
		}
		if req.SubscriptionID == 0 {
			http.Error(w, "subscription_id is required", http.StatusBadRequest)
			return
		}

		err := svc.SendReport(r.Context(), req.SubscriptionID)
		if err != nil {
			switch {
			case errors.Is(err, report.ErrNotFound):
				http.Error(w, "subscription not found", http.StatusNotFound)
			default:
				writePublicError(w, r, http.StatusInternalServerError, "report error", err)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
		if err != nil {
			writePublicError(w, r, http.StatusInternalServerError, "response encode error", err)
			return
		}
	}
}

func configKeysHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(schemas.ConfigKeysJSON)); err != nil {
		logging.FromContext(r.Context()).Error("write error", "err", err, "path", r.URL.Path)
		return
	}
}

func dashboardTemplateCatalogHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(schemas.DashboardTemplatesJSON)); err != nil {
		logging.FromContext(r.Context()).Error("write error", "err", err, "path", r.URL.Path)
		return
	}
}

func alertTestResponseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(schemas.AlertTestResponseJSON)); err != nil {
		logging.FromContext(r.Context()).Error("write error", "err", err, "path", r.URL.Path)
		return
	}
}

func frontendConfigHandler(cfg FrontendConfig, store database.ConfigStore, authCfg AuthConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type response struct {
			FrontendConfig
			UserEmail string   `json:"user_email"`
			UserName  string   `json:"user_name"`
			IsAdmin   bool     `json:"is_admin"`
			Roles     []string `json:"roles"`
		}
		resp := response{FrontendConfig: cfg}
		id, _ := authenticateRequest(r, store, authCfg)
		resp.UserName = id.Username
		resp.UserEmail = id.Email
		resp.IsAdmin = id.IsAdmin
		resp.Roles = id.Roles
		if len(resp.Roles) == 0 {
			if id.Known() {
				resp.Roles = []string{"contributor"}
			} else {
				resp.Roles = []string{"anonymous"}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(resp)
		if err != nil {
			writePublicError(w, r, http.StatusInternalServerError, "response encode error", err)
			return
		}
	}
}

func spaHandler(prefix string, fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.StripPrefix(prefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "" {
			path = "."
		}
		_, err := fs.Stat(fsys, path)
		if err != nil {
			if strings.HasPrefix(r.URL.Path, "api/") || strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	}))
}

func assembledHandler[T any](load func(context.Context) ([]T, error), filter func(authz.Grant, []T) []T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := load(r.Context())
		if err != nil {
			logging.FromContext(r.Context()).Error("assembled config: database error", "path", r.URL.Path, "err", err)
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		if grant, ok := grantFromContext(r.Context()); ok {
			items = filter(grant, items)
		}
		if items == nil {
			items = []T{}
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(items)
		if err != nil {
			writePublicError(w, r, http.StatusInternalServerError, "response encode error", err)
			return
		}
	}
}

// writePublicError logs the cause and sends only the public message so
// driver, renderer and SMTP details never reach the client.
func writePublicError(w http.ResponseWriter, r *http.Request, status int, msg string, err error) {
	reqID := logging.RequestIDFromContext(r.Context())
	logging.FromContext(r.Context()).Error(msg, "err", err, "path", r.URL.Path, "request_id", reqID)
	http.Error(w, msg, status)
}
