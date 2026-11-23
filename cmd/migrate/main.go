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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if len(os.Args) > 1 && os.Args[1] == "decrypt-secrets" {
		decryptSecrets(ctx, host, port, user, pass, name, tlsCABundlePath)
		return
	}

	if err := database.Migrate(ctx, host, port, user, pass, name, tlsCABundlePath); err != nil {
		fmt.Fprintf(os.Stderr, "migration failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("migrations applied successfully")
}

// decryptSecrets writes encrypted credentials back as plaintext, needed
// before rolling back to a version without CONFIG_ENCRYPTION_KEY support.
func decryptSecrets(ctx context.Context, host string, port int, user, pass, name, tlsCABundlePath string) {
	store, err := database.NewMySQLStore(host, port, user, pass, name, tlsCABundlePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()
	enabled, err := store.EnableSecretEncryptionFromEnv()
	if err != nil || !enabled {
		fmt.Fprintf(os.Stderr, "decrypt-secrets needs a valid CONFIG_ENCRYPTION_KEY: %v\n", err)
		os.Exit(1)
	}
	n, err := store.DecryptAllSecrets(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "decrypt failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("decrypted %d credentials\n", n)
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
