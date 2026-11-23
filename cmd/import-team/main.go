package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type previewResponse struct {
	Team struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		ContactEmail string `json:"contact_email,omitempty"`
		Exists       bool   `json:"exists"`
	} `json:"team"`
	Actions []struct {
		Type        string `json:"type"`
		Description string `json:"description"`
	} `json:"actions"`
	Duplicates []struct {
		Owner         string   `json:"owner"`
		Name          string   `json:"name"`
		RepoType      string   `json:"repo_type"`
		ExistingTeams []string `json:"existing_teams"`
	} `json:"duplicates"`
	RequiredConnections []struct {
		Plugin          string `json:"plugin"`
		DefaultEndpoint string `json:"default_endpoint"`
		NeedsUsername   bool   `json:"needs_username"`
	} `json:"required_connections"`
	Warnings []string `json:"warnings"`
	Errors   []string `json:"errors"`
	Valid    bool     `json:"valid"`
}

type executeResponse struct {
	TeamID    string `json:"team_id"`
	TeamName  string `json:"team_name"`
	Timestamp string `json:"timestamp"`
	Actions   []struct {
		Type        string `json:"type"`
		Description string `json:"description"`
		Status      string `json:"status,omitempty"`
	} `json:"actions"`
	Duplicates []struct {
		Owner         string   `json:"owner"`
		Name          string   `json:"name"`
		RepoType      string   `json:"repo_type"`
		ExistingTeams []string `json:"existing_teams"`
	} `json:"duplicates"`
	SyncResult json.RawMessage `json:"sync_result,omitempty"`
	RevertInfo json.RawMessage `json:"revert_info"`
}

type connectionEntry struct {
	Plugin     string `json:"plugin"`
	Token      string `json:"token"`
	Endpoint   string `json:"endpoint"`
	Username   string `json:"username,omitempty"`
	ExpiryDate string `json:"expiry_date"`
}

func main() {
	dashURL := flag.String("url", envOr("DASHBOARD_URL", "http://localhost:9090"), "dashboard base URL")
	dashToken := flag.String("token", os.Getenv("DASHBOARD_ADMIN_TOKEN"), "dashboard admin API key")
	productID := flag.String("product-id", os.Getenv("DASHBOARD_PRODUCT_ID"), "product ID to link the imported team to")
	yes := flag.Bool("yes", false, "skip confirmation prompt")
	reportPath := flag.String("report", "", "path to save the import report JSON (default: auto-generated)")

	ghToken := flag.String("github-token", os.Getenv("GITHUB_TOKEN"), "GitHub personal access token")
	ghEndpoint := flag.String("github-endpoint", "https://api.github.com", "GitHub API endpoint")
	ghExpiry := flag.String("github-expiry", "never", "GitHub token expiry (YYYY-MM-DD or \"never\")")

	glToken := flag.String("gitlab-token", os.Getenv("GITLAB_TOKEN"), "GitLab personal access token")
	glEndpoint := flag.String("gitlab-endpoint", "https://gitlab.com", "GitLab API endpoint")
	glExpiry := flag.String("gitlab-expiry", "never", "GitLab token expiry (YYYY-MM-DD or \"never\")")

	jiraToken := flag.String("jira-token", os.Getenv("JIRA_TOKEN"), "Jira API token")
	jiraEndpoint := flag.String("jira-endpoint", "", "Jira API endpoint (e.g. https://issues.redhat.com)")
	jiraUsername := flag.String("jira-username", os.Getenv("JIRA_USERNAME"), "Jira username / email")
	jiraExpiry := flag.String("jira-expiry", "never", "Jira token expiry (YYYY-MM-DD or \"never\")")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: import-team [options] <team.json>\n\n")
		fmt.Fprintf(os.Stderr, "Import a legacy team JSON file into the dashboard.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nEnvironment variables:\n")
		fmt.Fprintf(os.Stderr, "  DASHBOARD_URL           Dashboard base URL (default: http://localhost:9090)\n")
		fmt.Fprintf(os.Stderr, "  DASHBOARD_ADMIN_TOKEN   Admin API key\n")
		fmt.Fprintf(os.Stderr, "  DASHBOARD_PRODUCT_ID    Product ID to link team to\n")
		fmt.Fprintf(os.Stderr, "  GITHUB_TOKEN            GitHub personal access token\n")
		fmt.Fprintf(os.Stderr, "  GITLAB_TOKEN            GitLab personal access token\n")
		fmt.Fprintf(os.Stderr, "  JIRA_TOKEN              Jira API token\n")
		fmt.Fprintf(os.Stderr, "  JIRA_USERNAME           Jira username / email\n")
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}
	if *dashToken == "" {
		fmt.Fprintln(os.Stderr, "error: dashboard admin token is required (--token or DASHBOARD_ADMIN_TOKEN)")
		os.Exit(1)
	}

	teamFile := flag.Arg(0)
	teamData, err := os.ReadFile(teamFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: reading %s: %v\n", teamFile, err)
		os.Exit(1)
	}

	var raw json.RawMessage
	if err := json.Unmarshal(teamData, &raw); err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid JSON in %s: %v\n", teamFile, err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	baseURL := strings.TrimRight(*dashURL, "/")

	// Step 1: Preview
	fmt.Println("Analyzing team JSON...")
	preview, err := doPreview(client, baseURL, *dashToken, raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: preview failed: %v\n", err)
		os.Exit(1)
	}

	printPreview(preview)

	if !preview.Valid {
		fmt.Fprintln(os.Stderr, "\nImport cannot proceed due to errors above.")
		os.Exit(1)
	}

	// Build connections from flags for required plugins.
	connections := buildConnections(preview, *ghToken, *ghEndpoint, *ghExpiry,
		*glToken, *glEndpoint, *glExpiry, *jiraToken, *jiraEndpoint, *jiraUsername, *jiraExpiry)

	// Validate that all required connections have tokens.
	for _, rc := range preview.RequiredConnections {
		found := false
		for _, c := range connections {
			if c.Plugin == rc.Plugin {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(os.Stderr, "error: %s connection required but no --%s-token provided\n", rc.Plugin, rc.Plugin)
			os.Exit(1)
		}
	}

	// Step 2: Confirm
	if !*yes {
		fmt.Print("\nProceed with import? [y/N] ")
		var answer string
		_, _ = fmt.Scanln(&answer)
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Import cancelled.")
			os.Exit(0)
		}
	}

	// Step 3: Execute
	fmt.Println("\nImporting team...")
	report, err := doExecute(client, baseURL, *dashToken, raw, *productID, connections)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: import failed: %v\n", err)
		os.Exit(1)
	}

	printReport(report)

	// Step 4: Save report
	outPath := *reportPath
	if outPath == "" {
		outPath = fmt.Sprintf("import-report-%s-%s.json",
			report.TeamID, strings.ReplaceAll(report.Timestamp, ":", "-"))
	}
	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to marshal report: %v\n", err)
		return
	}
	outPath = filepath.Clean(outPath)
	if err := os.WriteFile(outPath, reportJSON, 0o600); err != nil { //nolint:gosec // CLI tool: user controls output path
		fmt.Fprintf(os.Stderr, "warning: failed to save report to %s: %v\n", outPath, err)
	} else {
		fmt.Printf("\nReport saved to %s\n", outPath)
	}
}

func doPreview(client *http.Client, baseURL, token string, teamData json.RawMessage) (*previewResponse, error) {
	resp, err := apiPost(client, baseURL+"/api/v1/admin/import-team/preview", token, teamData)
	if err != nil {
		return nil, err
	}
	var preview previewResponse
	if err := json.Unmarshal(resp, &preview); err != nil {
		return nil, fmt.Errorf("decode preview: %w", err)
	}
	return &preview, nil
}

func doExecute(client *http.Client, baseURL, token string, teamData json.RawMessage, productID string, connections []connectionEntry) (*executeResponse, error) {
	body := struct {
		Team        json.RawMessage   `json:"team"`
		ProductID   string            `json:"product_id,omitempty"`
		Connections []connectionEntry `json:"connections"`
	}{teamData, productID, connections}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	resp, err := apiPost(client, baseURL+"/api/v1/admin/import-team/execute", token, bodyBytes)
	if err != nil {
		return nil, err
	}
	var report executeResponse
	if err := json.Unmarshal(resp, &report); err != nil {
		return nil, fmt.Errorf("decode report: %w", err)
	}
	return &report, nil
}

func apiPost(client *http.Client, url, token string, body []byte) ([]byte, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, fmt.Errorf("authentication failed (HTTP %d) — check your --token", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		var apiErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error != "" {
			return nil, fmt.Errorf("server error (HTTP %d): %s", resp.StatusCode, apiErr.Error)
		}
		return nil, fmt.Errorf("server error (HTTP %d): %s", resp.StatusCode, string(data))
	}
	return data, nil
}

func buildConnections(preview *previewResponse,
	ghToken, ghEndpoint, ghExpiry,
	glToken, glEndpoint, glExpiry,
	jiraToken, jiraEndpoint, jiraUsername, jiraExpiry string,
) []connectionEntry {
	var conns []connectionEntry
	for _, rc := range preview.RequiredConnections {
		switch rc.Plugin {
		case "github":
			if ghToken != "" {
				conns = append(conns, connectionEntry{
					Plugin:     "github",
					Token:      ghToken,
					Endpoint:   ghEndpoint,
					ExpiryDate: ghExpiry,
				})
			}
		case "gitlab":
			if glToken != "" {
				conns = append(conns, connectionEntry{
					Plugin:     "gitlab",
					Token:      glToken,
					Endpoint:   glEndpoint,
					ExpiryDate: glExpiry,
				})
			}
		case "jira":
			if jiraToken != "" {
				conns = append(conns, connectionEntry{
					Plugin:     "jira",
					Token:      jiraToken,
					Endpoint:   jiraEndpoint,
					Username:   jiraUsername,
					ExpiryDate: jiraExpiry,
				})
			}
		}
	}
	return conns
}

func printPreview(p *previewResponse) {
	fmt.Printf("\n=== Import Preview ===\n")
	fmt.Printf("Team: %s (%s)\n", p.Team.Name, p.Team.ID)
	if p.Team.ContactEmail != "" {
		fmt.Printf("Email: %s\n", p.Team.ContactEmail)
	}

	if len(p.Errors) > 0 {
		fmt.Printf("\nErrors:\n")
		for _, e := range p.Errors {
			fmt.Printf("  ✗ %s\n", e)
		}
	}

	if len(p.Warnings) > 0 {
		fmt.Printf("\nWarnings:\n")
		for _, w := range p.Warnings {
			fmt.Printf("  ! %s\n", w)
		}
	}

	if len(p.Actions) > 0 {
		fmt.Printf("\nActions (%d):\n", len(p.Actions))
		for _, a := range p.Actions {
			fmt.Printf("  • [%s] %s\n", a.Type, a.Description)
		}
	}

	if len(p.Duplicates) > 0 {
		fmt.Printf("\nDuplicate repositories:\n")
		for _, d := range p.Duplicates {
			fmt.Printf("  • %s/%s (%s) — already in: %s\n",
				d.Owner, d.Name, d.RepoType, strings.Join(d.ExistingTeams, ", "))
		}
	}

	if len(p.RequiredConnections) > 0 {
		fmt.Printf("\nRequired connections:\n")
		for _, rc := range p.RequiredConnections {
			extra := ""
			if rc.DefaultEndpoint != "" {
				extra = fmt.Sprintf(" (endpoint: %s)", rc.DefaultEndpoint)
			}
			if rc.NeedsUsername {
				extra += " (requires username)"
			}
			fmt.Printf("  • %s%s\n", rc.Plugin, extra)
		}
	}
}

func printReport(r *executeResponse) {
	fmt.Printf("\n=== Import Report ===\n")
	fmt.Printf("Team: %s (%s)\n", r.TeamName, r.TeamID)
	fmt.Printf("Timestamp: %s\n", r.Timestamp)

	if len(r.Actions) > 0 {
		fmt.Printf("\nResults (%d):\n", len(r.Actions))
		for _, a := range r.Actions {
			var icon string
			switch a.Status {
			case "success":
				icon = "✓"
			case "error":
				icon = "✗"
			default:
				icon = "•"
			}
			fmt.Printf("  %s [%s] %s\n", icon, a.Type, a.Description)
		}
	}

	if len(r.Duplicates) > 0 {
		fmt.Printf("\nDuplicate repositories:\n")
		for _, d := range r.Duplicates {
			fmt.Printf("  • %s/%s (%s) — also in: %s\n",
				d.Owner, d.Name, d.RepoType, strings.Join(d.ExistingTeams, ", "))
		}
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
