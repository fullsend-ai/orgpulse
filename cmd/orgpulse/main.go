package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/authz"
	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/internal/devlake"
	"github.com/fullsend-ai/orgpulse/internal/logging"
	"github.com/fullsend-ai/orgpulse/internal/netguard"
	"github.com/fullsend-ai/orgpulse/internal/report"
	"github.com/fullsend-ai/orgpulse/internal/server"
	"github.com/fullsend-ai/orgpulse/web"
)

func sanitizeLog(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", ""), "\r", "")
}

func main() {
	cfg := mustLoadBaseConfig()

	// Setup logging early
	logger := logging.Setup(cfg.LogLevel, cfg.LogFormat, os.Stderr)

	httpClient := &http.Client{Timeout: 10 * time.Second}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var store database.Store
	var configStore database.ConfigStore
	var mysqlStore *database.MySQLStore
	if cfg.DBUser != "" && cfg.DBPassword != "" && cfg.DBName != "" {
		warnPlaintextDB(logger, "DASHBOARD_DB", cfg.DBHost, cfg.DBTLSCABundlePath)
		migrateCtx, migrateCancel := context.WithTimeout(ctx, 60*time.Second)
		if err := database.Migrate(migrateCtx, cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBTLSCABundlePath); err != nil {
			migrateCancel()
			logger.Error("failed to apply database migrations", "err", err)
			os.Exit(1)
		}
		migrateCancel()
		logger.Info("database migrations applied successfully")

		var err error
		mysqlStore, err = database.NewMySQLStore(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBTLSCABundlePath)
		if err != nil {
			logger.Error("failed to connect to database", "err", err)
			os.Exit(1)
		}
		defer mysqlStore.Close()
		store = mysqlStore
		configStore = mysqlStore

		encrypted, err := mysqlStore.EnableSecretEncryptionFromEnv()
		if err != nil {
			logger.Error("invalid CONFIG_ENCRYPTION_KEY", "err", err)
			os.Exit(1)
		}
		if err := mysqlStore.CheckSecretEncryption(ctx); err != nil {
			logger.Error("stored credentials cannot be read", "err", err)
			os.Exit(1)
		}
		if encrypted {
			n, err := mysqlStore.EncryptExistingSecrets(ctx)
			if err != nil {
				logger.Error("failed to encrypt existing credentials; they stay readable in plaintext", "err", err)
			} else if n > 0 {
				logger.Info("encrypted existing credentials", "count", n)
			}
		} else {
			logger.Warn("CONFIG_ENCRYPTION_KEY is not set: integration and personal tokens are stored in plaintext")
		}

		if cfg.AdminAPIKey != "" {
			if err := mysqlStore.EnsureAdminKey(ctx, cfg.AdminAPIKey); err != nil {
				logger.Error("failed to bootstrap admin API key", "err", err)
				os.Exit(1)
			}
		}
	} else {
		logger.Info("dashboard database not configured, /api/* endpoints disabled")
	}

	logger.Info("starting orgpulse",
		"port", cfg.Port,
		"pprof", cfg.EnablePprof,
		"database", cfg.DBName != "",
	) // #nosec G706

	staticFS := web.StaticFS

	authTestMode := mustBool("AUTH_TEST_MODE", false)
	authTestEmail := envString(os.Getenv, "AUTH_TEST_EMAIL", "testuser@example.com")
	authTestIsAdmin := mustBool("AUTH_TEST_IS_ADMIN", false)
	devAdminOverride := mustBool("DEV_ADMIN_OVERRIDE", false)
	adminUsers := server.ParseAdminUsers(os.Getenv("ADMIN_USERS"))
	operatorGroups := server.ParseAdminUsers(os.Getenv("OPERATOR_ROVER_GROUPS"))
	testGroups := server.ParseAdminUsers(os.Getenv("AUTH_TEST_GROUPS"))
	authEmailDomains := server.ParseAdminUsers(os.Getenv("AUTH_EMAIL_DOMAINS"))
	allowPrivateEndpoints := mustBool("DEVLAKE_ALLOW_PRIVATE_ENDPOINTS", false)
	warnInsecureAuth(logger, devAdminOverride, authTestMode, adminUsers, authEmailDomains, os.Getenv("ROVER_GROUPS_CSV") != "")
	var membership authz.Directory
	if path := strings.TrimSpace(os.Getenv("ROVER_GROUPS_CSV")); path != "" {
		membership = authz.OpenCSV(path)
	}

	workflowBaseURL := strings.TrimRight(envString(os.Getenv, "WORKFLOW_BASE_URL", fmt.Sprintf("http://localhost:%d", cfg.Port)), "/")

	frontendCfg := server.FrontendConfig{
		AIAnalyticsURL:        os.Getenv("AI_ANALYTICS_URL"),
		TestMode:              authTestMode,
		TestEmail:             authTestEmail,
		TestIsAdmin:           authTestIsAdmin,
		DevAdminOverride:      devAdminOverride,
		AdminUsers:            adminUsers,
		TestGroups:            testGroups,
		OperatorGroups:        operatorGroups,
		Membership:            membership,
		AuthEmailDomains:      authEmailDomains,
		AllowPrivateEndpoints: allowPrivateEndpoints,
		WorkflowBaseURL:       workflowBaseURL,
		PublicAPIURL:          strings.TrimRight(os.Getenv("PUBLIC_API_URL"), "/"),
		DevLakeSkipTokenTest:  mustBool("DEVLAKE_SKIP_TOKEN_TEST", false),
	}

	aiAnalyticsURL := frontendCfg.AIAnalyticsURL
	aiSummaryURL := os.Getenv("AI_SUMMARY_URL")
	smtpHost := os.Getenv("SMTP_HOST")
	smtpPort := mustInt("SMTP_PORT", 25)
	smtpFrom := os.Getenv("SMTP_FROM")

	var reportSvc *report.Service
	if aiAnalyticsURL != "" && aiSummaryURL != "" && smtpHost != "" && smtpFrom != "" && mysqlStore != nil {
		// Report and alert URLs may target cluster-internal workflow services,
		// so private addresses stay reachable; metadata and link-local do not.
		reportClient := netguard.Client(10*time.Second, true, false)
		reportSvc = report.NewService(mysqlStore, reportClient, report.Config{
			AIAnalyticsURL: aiAnalyticsURL,
			AISummaryURL:   aiSummaryURL,
			SMTPHost:       smtpHost,
			SMTPPort:       smtpPort,
			SMTPFrom:       smtpFrom,
		})
	} else {
		logger.Info("report service not configured (need AI_ANALYTICS_URL, AI_SUMMARY_URL, SMTP_HOST, SMTP_FROM, and database)")
	}

	var workflowExec *server.WorkflowExecutor
	var devlakeDB *sql.DB
	if mysqlStore != nil {
		dlDBHost := envString(os.Getenv, "DEVLAKE_DB_HOST", "localhost")
		dlDBPort := mustInt("DEVLAKE_DB_PORT", 3306)
		dlDBUser := os.Getenv("DEVLAKE_DB_USER")
		dlDBPassword := os.Getenv("DEVLAKE_DB_PASSWORD")
		dlDBName := os.Getenv("DEVLAKE_DB_NAME")
		dlDBTLSCABundlePath := os.Getenv("DEVLAKE_DB_TLS_CA_BUNDLE_PATH")

		if dlDBUser != "" && dlDBPassword != "" && dlDBName != "" {
			warnPlaintextDB(logger, "DEVLAKE_DB", dlDBHost, dlDBTLSCABundlePath)
			var err error
			devlakeDB, err = database.OpenDB(dlDBHost, dlDBPort, dlDBUser, dlDBPassword, dlDBName, dlDBTLSCABundlePath)
			if err != nil {
				logger.Error("failed to open DevLake database", "err", err)
				os.Exit(1)
			}
			defer devlakeDB.Close()
			workflowExec = server.NewWorkflowExecutor(devlakeDB)
			logger.Info("DevLake database configured", "host", dlDBHost, "database", dlDBName) // #nosec G706
		} else {
			logger.Info("DevLake database not configured (DEVLAKE_DB_USER, DEVLAKE_DB_PASSWORD, DEVLAKE_DB_NAME)")
		}
	}

	if reportSvc != nil {
		scheduler := report.NewScheduler(reportSvc, report.SchedulerConfig{
			SubscriptionInterval: envDuration("SUBSCRIPTION_CHECK_INTERVAL_SECONDS", 3600),
			AlertInterval:        envDuration("ALERT_CHECK_INTERVAL_SECONDS", 3600),
			SubscriptionSpread:   envDuration("SUBSCRIPTION_SPREAD_SECONDS", 14400),
			AlertSpread:          envDuration("ALERT_SPREAD_SECONDS", 7200),
		})
		go scheduler.Run(ctx)
	}

	aiCfg := server.AIAssistConfig{
		APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
		Model:   envString(os.Getenv, "ANTHROPIC_MODEL", "claude-sonnet-4-20250514"),
		BaseURL: envString(os.Getenv, "ANTHROPIC_API_URL", "https://api.anthropic.com"),
	}
	if aiCfg.Enabled() {
		logger.Info("AI code assist enabled", "model", aiCfg.Model)
	}

	var devlakeSvc *devlake.Service
	devlakeCfg := devlake.Config{
		APIURL:                os.Getenv("DEVLAKE_API_URL"),
		APIKey:                os.Getenv("DEVLAKE_API_KEY"),
		CronConfig:            envString(os.Getenv, "DEVLAKE_CRON_CONFIG", "0 0 * * *"),
		AllowPrivateEndpoints: allowPrivateEndpoints,
		TLSSkipVerify:         mustBool("TLS_SKIP_VERIFY", false),
	}
	if devlakeCfg.TLSSkipVerify {
		logger.Warn("TLS_SKIP_VERIFY is enabled: certificates of DevLake source endpoints and personal endpoints are not verified")
	}
	if devlakeCfg.APIURL != "" && configStore != nil {
		devlakeSvc = devlake.NewService(devlakeCfg, httpClient, configStore)
		logger.Info("DevLake integration enabled", "url", sanitizeLog(devlakeCfg.APIURL)) // #nosec G706
	} else {
		logger.Info("DevLake integration not configured (need DEVLAKE_API_URL and database)")
	}

	workflowExecURL := envString(os.Getenv, "WORKFLOW_EXEC_URL", "http://localhost:7070")

	srv := server.New(cfg.Port, cfg.EnablePprof, staticFS, store, configStore, reportSvc, frontendCfg, workflowExec, aiCfg, devlakeSvc, devlakeDB, workflowExecURL)
	if err := srv.Run(ctx); err != nil {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}

// warnPlaintextDB warns when a remote MySQL connection is not encrypted.
func warnPlaintextDB(logger *slog.Logger, prefix, host, caBundle string) {
	if caBundle != "" {
		return
	}
	if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return
	}
	logger.Warn(prefix+"_TLS_CA_BUNDLE_PATH is not set: the connection to "+prefix+"_HOST is not encrypted", "host", sanitizeLog(host)) // #nosec G706
}

// warnInsecureAuth logs every setting that weakens authentication so a
// development configuration that reaches production is visible in the logs.
func warnInsecureAuth(logger *slog.Logger, devAdminOverride, testMode bool, adminUsers, emailDomains []string, rover bool) {
	if devAdminOverride {
		logger.Warn("DEV_ADMIN_OVERRIDE is enabled: every request, including unauthenticated ones, is treated as an administrator. Never enable this outside local development")
	}
	if testMode {
		logger.Warn("AUTH_TEST_MODE is enabled: every request is treated as the AUTH_TEST_EMAIL user. Never enable this outside local development")
	}
	if slices.Contains(adminUsers, "*") {
		logger.Warn("ADMIN_USERS contains '*': every authenticated user is an administrator")
	}
	if (len(adminUsers) > 0 || rover) && len(emailDomains) == 0 {
		logger.Warn("AUTH_EMAIL_DOMAINS is not set: a bare name in ADMIN_USERS or a Rover export matches that name at any email domain. Set it to your SSO email domain(s)")
	}
}

// baseConfig holds the settings that must be known before logging, the
// database and the HTTP server are wired up.
type baseConfig struct {
	Port              int
	EnablePprof       bool
	LogLevel          string
	LogFormat         string
	DBHost            string
	DBPort            int
	DBUser            string
	DBPassword        string
	DBName            string
	DBTLSCABundlePath string
	AdminAPIKey       string
}

// mustLoadBaseConfig exits with status 1 when a base setting is invalid.
func mustLoadBaseConfig() baseConfig {
	cfg, err := loadBaseConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return cfg
}

// loadBaseConfig reads the base settings through getenv so defaults,
// overrides and conversion errors are testable without touching the process
// environment.
func loadBaseConfig(getenv func(string) string) (baseConfig, error) {
	var cfg baseConfig

	cfg.LogLevel = envString(getenv, "LOG_LEVEL", "info")
	cfg.LogFormat = envString(getenv, "LOG_FORMAT", "text")

	var err error
	if cfg.Port, err = envInt(getenv, "PORT", 9090); err != nil {
		return baseConfig{}, err
	}
	if cfg.EnablePprof, err = envBool(getenv, "ENABLE_PPROF", false); err != nil {
		return baseConfig{}, err
	}

	cfg.DBHost = envString(getenv, "DASHBOARD_DB_HOST", "localhost")
	if cfg.DBPort, err = envInt(getenv, "DASHBOARD_DB_PORT", 3306); err != nil {
		return baseConfig{}, err
	}
	cfg.DBUser = getenv("DASHBOARD_DB_USER")
	cfg.DBPassword = getenv("DASHBOARD_DB_PASSWORD")
	cfg.DBName = getenv("DASHBOARD_DB_NAME")
	cfg.DBTLSCABundlePath = getenv("DASHBOARD_DB_TLS_CA_BUNDLE_PATH")
	cfg.AdminAPIKey = getenv("DASHBOARD_ADMIN_API_KEY")

	return cfg, nil
}

func envDuration(key string, defaultSec int) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return time.Duration(defaultSec) * time.Second
	}
	sec, err := strconv.Atoi(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid value for %s: %s\n", key, v)
		os.Exit(1)
	}
	return time.Duration(sec) * time.Second
}

func envInt(getenv func(string) string, key string, defaultVal int) (int, error) {
	v := getenv(key)
	if v == "" {
		return defaultVal, nil
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid value for %s: %s", key, v)
	}
	return i, nil
}

func envString(getenv func(string) string, key, defaultVal string) string {
	v := getenv(key)
	if v == "" {
		return defaultVal
	}
	return v
}

func envBool(getenv func(string) string, key string, defaultVal bool) (bool, error) { //nolint:unparam
	v := getenv(key)
	if v == "" {
		return defaultVal, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid value for %s: %s", key, v)
	}
	return b, nil
}

// mustInt and mustBool keep the fail-fast behaviour of the settings that are
// read outside loadBaseConfig: an invalid value is fatal before startup.
func mustInt(key string, defaultVal int) int {
	v, err := envInt(os.Getenv, key, defaultVal)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return v
}

func mustBool(key string, defaultVal bool) bool { //nolint:unparam
	v, err := envBool(os.Getenv, key, defaultVal)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return v
}
