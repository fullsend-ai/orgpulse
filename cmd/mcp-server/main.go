package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/fullsend-ai/orgpulse/internal/mcpserver"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	baseURL := os.Getenv("DEVLAKE_DASHBOARD_URL")
	if baseURL == "" {
		fmt.Fprintln(os.Stderr, "DEVLAKE_DASHBOARD_URL is required")
		os.Exit(1)
	}
	apiKey := os.Getenv("DEVLAKE_DASHBOARD_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "DEVLAKE_DASHBOARD_API_KEY is required")
		os.Exit(1)
	}

	srv := mcpserver.New(baseURL, apiKey, os.Stdin, os.Stdout)
	if err := srv.Run(); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}
