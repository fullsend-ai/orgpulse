package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/internal/netguard"
)

// outboundConfig controls requests to user-supplied personal endpoints.
type outboundConfig struct {
	AllowPrivate  bool
	TLSSkipVerify bool
}

func extHTTPClient(outbound outboundConfig) *http.Client {
	return netguard.Client(30*time.Second, outbound.AllowPrivate, outbound.TLSSkipVerify)
}

type UnifiedIssue struct {
	Source    string   `json:"source"`
	SourceID  string   `json:"source_id"`
	URL       string   `json:"url"`
	Key       string   `json:"key"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	RawStatus string   `json:"raw_status"`
	Priority  string   `json:"priority,omitempty"`
	Type      string   `json:"type"`
	Labels    []string `json:"labels"`
	Created   string   `json:"created"`
	Updated   string   `json:"updated"`
	Repo      string   `json:"repo"`
	TeamID    string   `json:"team_id"`
}

type IssueTransition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type sourceConn struct {
	plugin   string
	endpoint string
	token    string
	username string
	teamID   string
}

// --------------- Handlers ---------------

func myIssuesHandler(store database.ConfigStore, outbound outboundConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ident, ok := identityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		conns := personalConnections(r.Context(), store, ident.Email)
		if len(conns) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"issues": []UnifiedIssue{}, "has_tokens": false})
			return
		}

		issues, errs := fetchAllIssues(r.Context(), conns, outbound)
		if issues == nil {
			issues = []UnifiedIssue{}
		}
		resp := map[string]any{"issues": issues, "has_tokens": true}
		if len(errs) > 0 {
			resp["errors"] = errs
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func myContributionsHandler(store database.ConfigStore, outbound outboundConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ident, ok := identityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		conns := personalConnections(r.Context(), store, ident.Email)
		if len(conns) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"issues": []UnifiedIssue{}, "has_tokens": false})
			return
		}

		issues, errs := fetchAllContributions(r.Context(), conns, outbound)
		if issues == nil {
			issues = []UnifiedIssue{}
		}
		resp := map[string]any{"issues": issues, "has_tokens": true}
		if len(errs) > 0 {
			resp["errors"] = errs
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func myIssueTransitionsHandler(store database.ConfigStore, outbound outboundConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ident, ok := identityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		source := r.URL.Query().Get("source")
		sourceID := r.URL.Query().Get("source_id")
		status := r.URL.Query().Get("status")

		if source == "" || sourceID == "" {
			writeError(w, http.StatusBadRequest, "source and source_id are required")
			return
		}

		var transitions []IssueTransition

		switch source {
		case "github":
			transitions = githubTransitions(status)
		case "gitlab":
			transitions = gitlabTransitions(status)
		case "jira":
			conn, err := personalConnection(r.Context(), store, ident.Email, source)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "no personal Jira token configured")
				return
			}
			var fetchErr error
			transitions, fetchErr = jiraTransitions(r.Context(), conn, sourceID, outbound)
			if fetchErr != nil {
				slog.Warn("my-issues: jira transitions failed", "error", fetchErr)
				writeError(w, http.StatusBadGateway, "failed to fetch Jira transitions")
				return
			}
		default:
			writeError(w, http.StatusBadRequest, "unknown source")
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"transitions": transitions})
	}
}

func myIssueTransitionHandler(store database.ConfigStore, outbound outboundConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ident, ok := identityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		var req struct {
			Source       string `json:"source"`
			SourceID     string `json:"source_id"`
			TransitionID string `json:"transition_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.Source == "" || req.SourceID == "" || req.TransitionID == "" {
			writeError(w, http.StatusBadRequest, "source, source_id, and transition_id are required")
			return
		}

		conn, err := personalConnection(r.Context(), store, ident.Email, req.Source)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "no personal token configured for "+req.Source)
			return
		}

		client := extHTTPClient(outbound)
		switch req.Source {
		case "github":
			err = execGitHubTransition(r.Context(), client, conn, req.SourceID, req.TransitionID)
		case "gitlab":
			err = execGitLabTransition(r.Context(), client, conn, req.SourceID, req.TransitionID)
		case "jira":
			err = execJiraTransition(r.Context(), client, conn, req.SourceID, req.TransitionID)
		default:
			writeError(w, http.StatusBadRequest, "unknown source")
			return
		}

		if err != nil {
			slog.Warn("my-issues: transition failed", "source", req.Source, "error", err)
			writeError(w, http.StatusBadGateway, "transition failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}

// --------------- Connection lookup ---------------

func personalConnections(ctx context.Context, store database.ConfigStore, email string) []sourceConn {
	cfg, err := store.GetEntityConfigKV(ctx, "user", email)
	if err != nil {
		return nil
	}

	var conns []sourceConn
	for _, plugin := range []string{"github", "gitlab", "jira"} {
		tok := cfg["personal_"+plugin+"_token"]
		ep := cfg["personal_"+plugin+"_endpoint"]
		if tok == "" || ep == "" {
			continue
		}
		conns = append(conns, sourceConn{
			plugin:   plugin,
			endpoint: ep,
			token:    tok,
			username: cfg["personal_"+plugin+"_username"],
		})
	}
	return conns
}

func personalConnection(ctx context.Context, store database.ConfigStore, email, plugin string) (sourceConn, error) {
	cfg, err := store.GetEntityConfigKV(ctx, "user", email)
	if err != nil {
		return sourceConn{}, err
	}
	tok := cfg["personal_"+plugin+"_token"]
	ep := cfg["personal_"+plugin+"_endpoint"]
	if tok == "" || ep == "" {
		return sourceConn{}, fmt.Errorf("no personal %s token configured", plugin)
	}
	return sourceConn{
		plugin:   plugin,
		endpoint: ep,
		token:    tok,
		username: cfg["personal_"+plugin+"_username"],
	}, nil
}

// --------------- Parallel fetch ---------------

func fetchAllIssues(ctx context.Context, conns []sourceConn, outbound outboundConfig) ([]UnifiedIssue, []string) {
	var mu sync.Mutex
	var all []UnifiedIssue
	var errs []string
	var wg sync.WaitGroup
	client := extHTTPClient(outbound)

	for _, c := range conns {
		wg.Add(1)
		go func(c sourceConn) {
			defer wg.Done()
			var items []UnifiedIssue
			var err error
			switch c.plugin {
			case "github":
				items, err = fetchGitHubIssues(ctx, client, c)
			case "gitlab":
				items, err = fetchGitLabIssues(ctx, client, c)
			case "jira":
				items, err = fetchJiraIssues(ctx, client, c)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				slog.Warn("my-issues: fetch failed", "plugin", c.plugin, "error", err)
				errs = append(errs, fmt.Sprintf("%s: %s", c.plugin, err.Error()))
				return
			}
			all = append(all, items...)
		}(c)
	}
	wg.Wait()
	return all, errs
}

// --------------- GitHub ---------------

func fetchGitHubIssues(ctx context.Context, client *http.Client, conn sourceConn) ([]UnifiedIssue, error) {
	return fetchGitHubIssuesWithFilter(ctx, client, conn, "assigned", 100)
}

func fetchGitHubIssuesWithFilter(ctx context.Context, client *http.Client, conn sourceConn, filter string, limit int) ([]UnifiedIssue, error) {
	u := fmt.Sprintf("%s/issues?filter=%s&state=open&per_page=%d&sort=updated&direction=desc",
		strings.TrimRight(conn.endpoint, "/"), filter, limit)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+conn.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var raw []struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		State     string `json:"state"`
		HTMLURL   string `json:"html_url"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		Labels    []struct {
			Name string `json:"name"`
		} `json:"labels"`
		PullRequest *struct {
			URL string `json:"url"`
		} `json:"pull_request"`
		Repository struct {
			FullName string `json:"full_name"`
			Archived bool   `json:"archived"`
		} `json:"repository"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]UnifiedIssue, 0, len(raw))
	for _, gi := range raw {
		if gi.Repository.Archived {
			continue
		}
		typ := "issue"
		if gi.PullRequest != nil {
			typ = "pull_request"
		}
		labels := make([]string, len(gi.Labels))
		for i, l := range gi.Labels {
			labels[i] = l.Name
		}
		out = append(out, UnifiedIssue{
			Source:    "github",
			SourceID:  fmt.Sprintf("%s/%d", gi.Repository.FullName, gi.Number),
			URL:       gi.HTMLURL,
			Key:       fmt.Sprintf("%s#%d", gi.Repository.FullName, gi.Number),
			Title:     gi.Title,
			Status:    gi.State,
			RawStatus: gi.State,
			Type:      typ,
			Labels:    labels,
			Created:   gi.CreatedAt,
			Updated:   gi.UpdatedAt,
			Repo:      gi.Repository.FullName,
			TeamID:    conn.teamID,
		})
	}
	return out, nil
}

func githubTransitions(currentStatus string) []IssueTransition {
	if currentStatus == "closed" {
		return []IssueTransition{{ID: "open", Name: "Reopen"}}
	}
	return []IssueTransition{{ID: "closed", Name: "Close"}}
}

func execGitHubTransition(ctx context.Context, client *http.Client, conn sourceConn, sourceID, transitionID string) error {
	parts := strings.SplitN(sourceID, "/", 3)
	if len(parts) != 3 {
		return fmt.Errorf("invalid source_id: %s", sourceID)
	}
	u := fmt.Sprintf("%s/repos/%s/%s/issues/%s",
		strings.TrimRight(conn.endpoint, "/"), parts[0], parts[1], parts[2])

	body := fmt.Sprintf(`{"state":%q}`, transitionID)
	req, err := http.NewRequestWithContext(ctx, "PATCH", u, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "token "+conn.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GitHub PATCH returned %d: %s", resp.StatusCode, errBody)
	}
	return nil
}

// --------------- GitLab ---------------

func fetchGitLabIssues(ctx context.Context, client *http.Client, conn sourceConn) ([]UnifiedIssue, error) {
	return fetchGitLabIssuesWithPaths(ctx, client, conn, []struct{ path, typ string }{
		{"/issues?scope=assigned_to_me&state=opened&per_page=100&order_by=updated_at&sort=desc", "issue"},
		{"/merge_requests?scope=assigned_to_me&state=opened&per_page=100&order_by=updated_at&sort=desc", "merge_request"},
	})
}

func fetchGitLabIssuesWithPaths(ctx context.Context, client *http.Client, conn sourceConn, specs []struct{ path, typ string }) ([]UnifiedIssue, error) {
	base := strings.TrimRight(conn.endpoint, "/")

	type glItem struct {
		IID        int      `json:"iid"`
		Title      string   `json:"title"`
		State      string   `json:"state"`
		WebURL     string   `json:"web_url"`
		Labels     []string `json:"labels"`
		CreatedAt  string   `json:"created_at"`
		UpdatedAt  string   `json:"updated_at"`
		ProjectID  int      `json:"project_id"`
		References struct {
			Full string `json:"full"`
		} `json:"references"`
	}

	fetch := func(path string, itemType string) ([]UnifiedIssue, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", base+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("PRIVATE-TOKEN", conn.token)

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitLab API returned %d for %s", resp.StatusCode, path)
		}

		var items []glItem
		if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
			return nil, err
		}

		out := make([]UnifiedIssue, 0, len(items))
		for _, gi := range items {
			status := gi.State
			if status == "opened" {
				status = "open"
			}

			ref := gi.References.Full
			if ref == "" {
				ref = fmt.Sprintf("#%d", gi.IID)
			}
			repo := ref
			if idx := strings.LastIndex(ref, "#"); idx > 0 {
				repo = ref[:idx]
			} else if idx := strings.LastIndex(ref, "!"); idx > 0 {
				repo = ref[:idx]
			}

			prefix := "issue"
			if itemType == "merge_request" {
				prefix = "mr"
			}

			labels := gi.Labels
			if labels == nil {
				labels = []string{}
			}

			out = append(out, UnifiedIssue{
				Source:    "gitlab",
				SourceID:  fmt.Sprintf("%s/%d/%d", prefix, gi.ProjectID, gi.IID),
				URL:       gi.WebURL,
				Key:       ref,
				Title:     gi.Title,
				Status:    status,
				RawStatus: gi.State,
				Type:      itemType,
				Labels:    labels,
				Created:   gi.CreatedAt,
				Updated:   gi.UpdatedAt,
				Repo:      repo,
				TeamID:    conn.teamID,
			})
		}
		return out, nil
	}

	var mu sync.Mutex
	var all []UnifiedIssue
	var wg sync.WaitGroup
	var firstErr error

	for _, spec := range specs {
		wg.Add(1)
		go func(path, typ string) {
			defer wg.Done()
			items, err := fetch(path, typ)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			all = append(all, items...)
		}(spec.path, spec.typ)
	}
	wg.Wait()

	if len(all) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return all, nil
}

func gitlabTransitions(currentStatus string) []IssueTransition {
	if currentStatus == "open" || currentStatus == "opened" {
		return []IssueTransition{{ID: "close", Name: "Close"}}
	}
	return []IssueTransition{{ID: "reopen", Name: "Reopen"}}
}

func execGitLabTransition(ctx context.Context, client *http.Client, conn sourceConn, sourceID, transitionID string) error {
	parts := strings.SplitN(sourceID, "/", 3)
	if len(parts) != 3 {
		return fmt.Errorf("invalid source_id: %s", sourceID)
	}
	kind, projectID, iid := parts[0], parts[1], parts[2]

	resource := "issues"
	if kind == "mr" {
		resource = "merge_requests"
	}

	u := fmt.Sprintf("%s/projects/%s/%s/%s?state_event=%s",
		strings.TrimRight(conn.endpoint, "/"), projectID, resource, iid, url.QueryEscape(transitionID))

	req, err := http.NewRequestWithContext(ctx, "PUT", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", conn.token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GitLab PUT returned %d: %s", resp.StatusCode, errBody)
	}
	return nil
}

// --------------- Jira ---------------

func jiraBaseURL(endpoint string) string {
	base := strings.TrimRight(endpoint, "/")
	return strings.TrimSuffix(base, "/rest")
}

func fetchJiraIssues(ctx context.Context, client *http.Client, conn sourceConn) ([]UnifiedIssue, error) {
	return fetchJiraIssuesWithJQL(ctx, client, conn,
		"assignee=currentUser() AND statusCategory != Done ORDER BY updated DESC", 100)
}

func fetchJiraIssuesWithJQL(ctx context.Context, client *http.Client, conn sourceConn, jql string, limit int) ([]UnifiedIssue, error) {
	base := jiraBaseURL(conn.endpoint)
	u := base + "/rest/api/3/search/jql"

	body, err := json.Marshal(map[string]any{
		"jql":        jql,
		"maxResults": limit,
		"fields":     []string{"summary", "status", "priority", "issuetype", "created", "updated", "labels", "project"},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", u, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(conn.username, conn.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("jira search returned %d: %s", resp.StatusCode, errBody)
	}

	var result struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
				Status  struct {
					Name string `json:"name"`
				} `json:"status"`
				Priority *struct {
					Name string `json:"name"`
				} `json:"priority"`
				IssueType struct {
					Name string `json:"name"`
				} `json:"issuetype"`
				Created string   `json:"created"`
				Updated string   `json:"updated"`
				Labels  []string `json:"labels"`
				Project struct {
					Key  string `json:"key"`
					Name string `json:"name"`
				} `json:"project"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	out := make([]UnifiedIssue, 0, len(result.Issues))
	for _, ji := range result.Issues {
		priority := ""
		if ji.Fields.Priority != nil {
			priority = ji.Fields.Priority.Name
		}

		raw := ji.Fields.Status.Name
		status := strings.ToLower(raw)
		switch {
		case status == "done" || status == "closed" || status == "resolved":
			status = "closed"
		case status == "to do" || status == "open" || status == "new" || status == "backlog":
			status = "open"
		case strings.Contains(status, "progress") || strings.Contains(status, "review"):
			status = "in_progress"
		}

		labels := ji.Fields.Labels
		if labels == nil {
			labels = []string{}
		}

		out = append(out, UnifiedIssue{
			Source:    "jira",
			SourceID:  ji.Key,
			URL:       base + "/browse/" + ji.Key,
			Key:       ji.Key,
			Title:     ji.Fields.Summary,
			Status:    status,
			RawStatus: raw,
			Priority:  priority,
			Type:      strings.ToLower(ji.Fields.IssueType.Name),
			Labels:    labels,
			Created:   ji.Fields.Created,
			Updated:   ji.Fields.Updated,
			Repo:      ji.Fields.Project.Key,
			TeamID:    conn.teamID,
		})
	}
	return out, nil
}

func jiraTransitions(ctx context.Context, conn sourceConn, issueKey string, outbound outboundConfig) ([]IssueTransition, error) {
	base := jiraBaseURL(conn.endpoint)
	u := base + "/rest/api/3/issue/" + url.PathEscape(issueKey) + "/transitions"

	client := extHTTPClient(outbound)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil) //nolint:gosec // URL built from user-configured endpoint
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(conn.username, conn.token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req) //nolint:gosec // URL built from user-configured endpoint
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira transitions returned %d", resp.StatusCode)
	}

	var result struct {
		Transitions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"transitions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	out := make([]IssueTransition, len(result.Transitions))
	for i, t := range result.Transitions {
		out[i] = IssueTransition{ID: t.ID, Name: t.Name}
	}
	return out, nil
}

func execJiraTransition(ctx context.Context, client *http.Client, conn sourceConn, issueKey, transitionID string) error {
	base := jiraBaseURL(conn.endpoint)
	u := base + "/rest/api/3/issue/" + url.PathEscape(issueKey) + "/transitions"

	payload := fmt.Sprintf(`{"transition":{"id":%q}}`, transitionID)
	req, err := http.NewRequestWithContext(ctx, "POST", u, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.SetBasicAuth(conn.username, conn.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("jira transition returned %d: %s", resp.StatusCode, errBody)
	}
	return nil
}

// --------------- Contributions (mentioned / reviewer / watcher) ---------------

func fetchAllContributions(ctx context.Context, conns []sourceConn, outbound outboundConfig) ([]UnifiedIssue, []string) {
	var mu sync.Mutex
	var all []UnifiedIssue
	var errs []string
	var wg sync.WaitGroup
	client := extHTTPClient(outbound)

	for _, c := range conns {
		wg.Add(1)
		go func(c sourceConn) {
			defer wg.Done()
			var items []UnifiedIssue
			var err error
			switch c.plugin {
			case "github":
				items, err = fetchGitHubIssuesWithFilter(ctx, client, c, "mentioned", 25)
			case "gitlab":
				items, err = fetchGitLabIssuesWithPaths(ctx, client, c, []struct{ path, typ string }{
					{"/merge_requests?scope=all&reviewer_username=@me&state=opened&per_page=25&order_by=updated_at&sort=desc", "merge_request"},
				})
			case "jira":
				items, err = fetchJiraIssuesWithJQL(ctx, client, c,
					"(reporter = currentUser() OR watcher = currentUser()) AND assignee != currentUser() AND statusCategory != Done ORDER BY updated DESC", 25)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				slog.Warn("my-contributions: fetch failed", "plugin", c.plugin, "error", err)
				errs = append(errs, fmt.Sprintf("%s: %s", c.plugin, err.Error()))
				return
			}
			all = append(all, items...)
		}(c)
	}
	wg.Wait()
	return all, errs
}
