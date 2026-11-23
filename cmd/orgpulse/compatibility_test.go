package main

import (
	"strings"
	"testing"
)

func TestLoadBaseConfigDefaults(t *testing.T) {
	cfg, err := loadBaseConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Port", cfg.Port, 9090},
		{"EnablePprof", cfg.EnablePprof, false},
		{"LogLevel", cfg.LogLevel, "info"},
		{"LogFormat", cfg.LogFormat, "text"},
		{"DBHost", cfg.DBHost, "localhost"},
		{"DBPort", cfg.DBPort, 3306},
		{"DBUser", cfg.DBUser, ""},
		{"DBPassword", cfg.DBPassword, ""},
		{"DBName", cfg.DBName, ""},
		{"DBTLSCABundlePath", cfg.DBTLSCABundlePath, ""},
		{"AdminAPIKey", cfg.AdminAPIKey, ""},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoadBaseConfigOverrides(t *testing.T) {
	env := map[string]string{
		"PORT":                            "8081",
		"ENABLE_PPROF":                    "true",
		"LOG_LEVEL":                       "debug",
		"LOG_FORMAT":                      "json",
		"DASHBOARD_DB_HOST":               "mysql",
		"DASHBOARD_DB_PORT":               "3307",
		"DASHBOARD_DB_USER":               "u",
		"DASHBOARD_DB_PASSWORD":           "p",
		"DASHBOARD_DB_NAME":               "dashboard",
		"DASHBOARD_DB_TLS_CA_BUNDLE_PATH": "/ca.pem",
		"DASHBOARD_ADMIN_API_KEY":         "k",
	}
	cfg, err := loadBaseConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 8081 || !cfg.EnablePprof || cfg.LogLevel != "debug" || cfg.LogFormat != "json" {
		t.Errorf("unexpected general config: %+v", cfg)
	}
	if cfg.DBHost != "mysql" || cfg.DBPort != 3307 || cfg.DBUser != "u" ||
		cfg.DBPassword != "p" || cfg.DBName != "dashboard" ||
		cfg.DBTLSCABundlePath != "/ca.pem" || cfg.AdminAPIKey != "k" {
		t.Errorf("unexpected database config: %+v", cfg)
	}
}

func TestLoadBaseConfigInvalidValues(t *testing.T) {
	for _, tt := range []struct{ key, value string }{
		{"PORT", "ninety"},
		{"DASHBOARD_DB_PORT", "mysql"},
		{"ENABLE_PPROF", "maybe"},
	} {
		t.Run(tt.key, func(t *testing.T) {
			_, err := loadBaseConfig(func(k string) string {
				if k == tt.key {
					return tt.value
				}
				return ""
			})
			if err == nil || !strings.Contains(err.Error(), tt.key) {
				t.Fatalf("expected error mentioning %q, got %v", tt.key, err)
			}
		})
	}
}

// TestDatabaseFieldsLoadIndependently pins that the three database fields load
// independently; it does not exercise the enabled condition in main.
func TestDatabaseFieldsLoadIndependently(t *testing.T) {
	for _, env := range []map[string]string{
		{"DASHBOARD_DB_USER": "u"},
		{"DASHBOARD_DB_USER": "u", "DASHBOARD_DB_PASSWORD": "p"},
		{"DASHBOARD_DB_PASSWORD": "p", "DASHBOARD_DB_NAME": "d"},
	} {
		cfg, err := loadBaseConfig(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.DBUser != "" && cfg.DBPassword != "" && cfg.DBName != "" {
			t.Errorf("database unexpectedly enabled for %v", env)
		}
	}
}
