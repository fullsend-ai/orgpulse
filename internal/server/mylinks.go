package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/fullsend-ai/orgpulse/internal/database"
)

type QuickLink struct {
	Source string `json:"source"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Icon   string `json:"icon,omitempty"`
}

func myLinksHandler(store database.ConfigStore, outbound outboundConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ident, ok := identityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		conns := personalConnections(r.Context(), store, ident.Email)
		if len(conns) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"links": []QuickLink{}})
			return
		}

		var mu sync.Mutex
		var all []QuickLink
		var wg sync.WaitGroup
		client := extHTTPClient(outbound)

		for _, c := range conns {
			wg.Add(1)
			go func(c sourceConn) {
				defer wg.Done()
				var links []QuickLink
				switch c.plugin {
				case "github":
					links = fetchGitHubLinks(r.Context(), client, c)
				case "gitlab":
					links = fetchGitLabLinks(r.Context(), client, c)
				case "jira":
					links = fetchJiraLinks(r.Context(), client, c)
				}
				mu.Lock()
				all = append(all, links...)
				mu.Unlock()
			}(c)
		}
		wg.Wait()

		if all == nil {
			all = []QuickLink{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"links": all})
	}
}

// --------------- GitHub ---------------

func fetchGitHubLinks(ctx context.Context, client *http.Client, conn sourceConn) []QuickLink {
	base := strings.TrimRight(conn.endpoint, "/")
	var mu sync.Mutex
	var all []QuickLink
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		links, err := fetchGitHubOrgs(ctx, client, conn, base)
		if err != nil {
			slog.Warn("my-links: github orgs failed", "error", err)
			return
		}
		mu.Lock()
		all = append(all, links...)
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		links, err := fetchGitHubStarred(ctx, client, conn, base)
		if err != nil {
			slog.Warn("my-links: github starred failed", "error", err)
			return
		}
		mu.Lock()
		all = append(all, links...)
		mu.Unlock()
	}()
	wg.Wait()
	return all
}

func fetchGitHubOrgs(ctx context.Context, client *http.Client, conn sourceConn, base string) ([]QuickLink, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/user/orgs?per_page=10", nil)
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
		return nil, fmt.Errorf("GitHub orgs returned %d", resp.StatusCode)
	}

	var raw []struct {
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	htmlBase := strings.Replace(base, "api.github.com", "github.com", 1)
	htmlBase = strings.TrimSuffix(htmlBase, "/api/v3")

	out := make([]QuickLink, 0, len(raw))
	for _, o := range raw {
		out = append(out, QuickLink{
			Source: "github",
			Kind:   "org",
			Name:   o.Login,
			URL:    htmlBase + "/" + o.Login,
			Icon:   o.AvatarURL,
		})
	}
	return out, nil
}

func fetchGitHubStarred(ctx context.Context, client *http.Client, conn sourceConn, base string) ([]QuickLink, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/user/starred?per_page=10&sort=updated&direction=desc", nil)
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
		return nil, fmt.Errorf("GitHub starred returned %d", resp.StatusCode)
	}

	var raw []struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]QuickLink, 0, len(raw))
	for _, r := range raw {
		out = append(out, QuickLink{
			Source: "github",
			Kind:   "starred",
			Name:   r.FullName,
			URL:    r.HTMLURL,
		})
	}
	return out, nil
}

// --------------- GitLab ---------------

func fetchGitLabLinks(ctx context.Context, client *http.Client, conn sourceConn) []QuickLink {
	base := strings.TrimRight(conn.endpoint, "/")
	var mu sync.Mutex
	var all []QuickLink
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		links, err := fetchGitLabGroups(ctx, client, conn, base)
		if err != nil {
			slog.Warn("my-links: gitlab groups failed", "error", err)
			return
		}
		mu.Lock()
		all = append(all, links...)
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		links, err := fetchGitLabStarred(ctx, client, conn, base)
		if err != nil {
			slog.Warn("my-links: gitlab starred failed", "error", err)
			return
		}
		mu.Lock()
		all = append(all, links...)
		mu.Unlock()
	}()
	wg.Wait()
	return all
}

func fetchGitLabGroups(ctx context.Context, client *http.Client, conn sourceConn, base string) ([]QuickLink, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/groups?min_access_level=10&per_page=10&order_by=name&sort=asc", nil)
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
		return nil, fmt.Errorf("GitLab groups returned %d", resp.StatusCode)
	}

	var raw []struct {
		Name      string `json:"name"`
		WebURL    string `json:"web_url"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]QuickLink, 0, len(raw))
	for _, g := range raw {
		out = append(out, QuickLink{
			Source: "gitlab",
			Kind:   "group",
			Name:   g.Name,
			URL:    g.WebURL,
			Icon:   g.AvatarURL,
		})
	}
	return out, nil
}

func fetchGitLabStarred(ctx context.Context, client *http.Client, conn sourceConn, base string) ([]QuickLink, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/projects?starred=true&per_page=10&order_by=last_activity_at&sort=desc", nil)
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
		return nil, fmt.Errorf("GitLab starred returned %d", resp.StatusCode)
	}

	var raw []struct {
		PathWithNamespace string `json:"path_with_namespace"`
		WebURL            string `json:"web_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]QuickLink, 0, len(raw))
	for _, p := range raw {
		out = append(out, QuickLink{
			Source: "gitlab",
			Kind:   "starred",
			Name:   p.PathWithNamespace,
			URL:    p.WebURL,
		})
	}
	return out, nil
}

// --------------- Jira ---------------

func fetchJiraLinks(ctx context.Context, client *http.Client, conn sourceConn) []QuickLink {
	base := jiraBaseURL(conn.endpoint)
	var mu sync.Mutex
	var all []QuickLink
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		links, err := fetchJiraRecentProjects(ctx, client, conn, base)
		if err != nil {
			slog.Warn("my-links: jira projects failed", "error", err)
			return
		}
		mu.Lock()
		all = append(all, links...)
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		links, err := fetchJiraFavouriteFilters(ctx, client, conn, base)
		if err != nil {
			slog.Warn("my-links: jira filters failed", "error", err)
			return
		}
		mu.Lock()
		all = append(all, links...)
		mu.Unlock()
	}()
	wg.Wait()
	return all
}

func fetchJiraRecentProjects(ctx context.Context, client *http.Client, conn sourceConn, base string) ([]QuickLink, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/rest/api/3/project/recent?maxResults=10", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(conn.username, conn.token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira recent projects returned %d", resp.StatusCode)
	}

	var raw []struct {
		Key        string            `json:"key"`
		Name       string            `json:"name"`
		AvatarURLs map[string]string `json:"avatarUrls"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]QuickLink, 0, len(raw))
	for _, p := range raw {
		icon := ""
		if p.AvatarURLs != nil {
			icon = p.AvatarURLs["16x16"]
		}
		out = append(out, QuickLink{
			Source: "jira",
			Kind:   "project",
			Name:   p.Key + " — " + p.Name,
			URL:    base + "/browse/" + p.Key,
			Icon:   icon,
		})
	}
	return out, nil
}

func fetchJiraFavouriteFilters(ctx context.Context, client *http.Client, conn sourceConn, base string) ([]QuickLink, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/rest/api/3/filter/favourite?maxResults=10", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(conn.username, conn.token)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira favourite filters returned %d", resp.StatusCode)
	}

	var raw []struct {
		Name    string `json:"name"`
		ViewURL string `json:"viewUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]QuickLink, 0, len(raw))
	for _, f := range raw {
		out = append(out, QuickLink{
			Source: "jira",
			Kind:   "filter",
			Name:   f.Name,
			URL:    f.ViewURL,
		})
	}
	return out, nil
}
