package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/database"
)

func main() {
	host := envOr("DASHBOARD_DB_HOST", "localhost")
	port, _ := strconv.Atoi(envOr("DASHBOARD_DB_PORT", "3306"))
	user := requireEnv("DASHBOARD_DB_USER")
	pass := requireEnv("DASHBOARD_DB_PASSWORD")
	name := requireEnv("DASHBOARD_DB_NAME")
	tlsCABundlePath := envOr("DASHBOARD_DB_TLS_CA_BUNDLE_PATH", "")

	store, err := database.NewMySQLStore(host, port, user, pass, name, tlsCABundlePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database connection failed: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := store.ClearConfig(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to clear config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("configuration cleared (API keys preserved)")
}

func requireEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fmt.Fprintf(os.Stderr, "required environment variable %s is not set\n", k)
		os.Exit(1)
	}
	return v
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
