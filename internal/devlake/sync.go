package devlake

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/internal/netguard"
)

type Service struct {
	client *Client
	config Config
	store  database.ConfigStore
}

func NewService(cfg Config, httpClient *http.Client, store database.ConfigStore) *Service {
	return &Service{
		client: NewClient(cfg.APIURL, cfg.APIKey, httpClient),
		config: cfg,
		store:  store,
	}
}

func (s *Service) Enabled() bool {
	return s.config.APIURL != ""
}

func (s *Service) TLSSkipVerify() bool {
	return s.config.TLSSkipVerify
}

func (s *Service) Client() *Client {
	return s.client
}

func (s *Service) HasRunningPipelines(ctx context.Context) (bool, error) {
	return s.client.HasRunningPipelines(ctx)
}

type desiredBlueprint struct {
	name      string
	plugin    string
	connID    int
	ciTool    string
	repoSpecs []repoSpec
}

type repoSpec struct {
	owner   string
	name    string
	boardID string
	scopeID string
	rawData json.RawMessage
}

type scopeDedupKey struct {
	pluginName string
	endpoint   string
	scopeID    string
}

type scopeOwner struct {
	blueprint string
	team      string
}

func (o scopeOwner) describe() string {
	if o.team != "" {
		return fmt.Sprintf("team %s (blueprint %q)", o.team, o.blueprint)
	}
	return fmt.Sprintf("blueprint %q", o.blueprint)
}

func (s *Service) SyncTeam(ctx context.Context, teamID string) (*SyncResult, error) {
	result := &SyncResult{}

	desired, err := s.buildDesiredState(ctx, teamID, result)
	if err != nil {
		return nil, fmt.Errorf("build desired state: %w", err)
	}

	s.resolveScopes(ctx, teamID, desired, result)

	if err := s.dedup(ctx, teamID, desired, result); err != nil {
		return nil, fmt.Errorf("dedup check: %w", err)
	}

	s.registerScopes(ctx, desired, result)
	s.ensureJiraScopeConfigs(ctx, teamID, desired)

	if err := s.reconcile(ctx, teamID, desired, result); err != nil {
		return nil, fmt.Errorf("reconcile: %w", err)
	}

	slog.Info("sync summary", "team", teamID,
		"created", len(result.Created), "updated", len(result.Updated),
		"deleted", len(result.Deleted), "skipped", len(result.Skipped),
		"errors", len(result.Errors), "errorDetails", result.Errors)

	if syncJSON, err := json.Marshal(result); err == nil {
		_ = s.store.SetEntityConfigKey(ctx, "team", teamID, "last_sync_result", string(syncJSON))
		_ = s.store.SetEntityConfigKey(ctx, "team", teamID, "last_sync_time", time.Now().UTC().Format(time.RFC3339))
	}

	return result, nil
}

func (s *Service) TriggerTeam(ctx context.Context, teamID string) (*TriggerResult, error) {
	teamLabel := "team:" + teamID
	blueprints, err := s.client.ListBlueprints(ctx, teamLabel)
	if err != nil {
		return nil, fmt.Errorf("list blueprints: %w", err)
	}

	result := &TriggerResult{}
	for _, bp := range blueprints {
		if err := s.client.TriggerBlueprint(ctx, bp.ID); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("trigger %q: %v", bp.Name, err))
			slog.Warn("blueprint trigger failed", "name", bp.Name, "blueprintID", bp.ID, "err", err)
		} else {
			result.Triggered = append(result.Triggered, bp.Name)
			slog.Info("blueprint triggered", "name", bp.Name, "blueprintID", bp.ID)
		}
	}
	return result, nil
}

func (s *Service) AvailableCodecov(ctx context.Context) (*AvailableCodecov, error) {
	connections, err := s.client.ListConnections(ctx, "codecov")
	if err != nil {
		return nil, err
	}
	blueprints, err := s.client.ListBlueprints(ctx, "")
	if err != nil {
		return nil, err
	}
	available := &AvailableCodecov{Connections: append([]connectionInfo{}, connections...), Blueprints: []AvailableCodecovBlueprint{}}
	for _, bp := range blueprints {
		if hasLabel(bp.Labels, "managed:lfmbot") || hasTeamLabel(bp.Labels) || len(bp.Connections) != 1 {
			continue
		}
		conn := bp.Connections[0]
		if conn.PluginName != "codecov" || len(conn.Scopes) == 0 {
			continue
		}
		item := AvailableCodecovBlueprint{ID: bp.ID, Name: bp.Name, ConnectionID: conn.ConnectionID}
		for _, scope := range conn.Scopes {
			item.Scopes = append(item.Scopes, scope.ScopeID)
		}
		available.Blueprints = append(available.Blueprints, item)
	}
	return available, nil
}

func hasTeamLabel(labels []string) bool {
	for _, label := range labels {
		if strings.HasPrefix(label, "team:") {
			return true
		}
	}
	return false
}

func (s *Service) AttachCodecov(ctx context.Context, teamID string, req AttachCodecovRequest) error {
	if req.ConnectionID <= 0 || req.BlueprintID <= 0 {
		return fmt.Errorf("connection_id and blueprint_id are required")
	}
	configured, err := s.teamConnectionID(ctx, teamID, "codecov")
	if err != nil {
		return err
	}
	if configured != 0 {
		return fmt.Errorf("team already has a Codecov connection")
	}
	connections, err := s.client.ListConnections(ctx, "codecov")
	if err != nil {
		return err
	}
	var selected *connectionInfo
	for i := range connections {
		if connections[i].ID == req.ConnectionID {
			selected = &connections[i]
			break
		}
	}
	if selected == nil {
		return fmt.Errorf("codecov connection %d not found", req.ConnectionID)
	}
	entry := ConnectionEntry{Plugin: "codecov", Endpoint: selected.Endpoint, Organization: selected.Organization, Service: selected.Service}
	if err := validateCodecovEntry(entry); err != nil {
		return err
	}
	blueprints, err := s.client.ListBlueprints(ctx, "")
	if err != nil {
		return err
	}
	selectedBP, err := findAttachableCodecovBlueprint(blueprints, req)
	if err != nil {
		return err
	}
	sources, err := s.store.ListSourceRows(ctx, teamID)
	if err != nil {
		return err
	}
	allowed := make(map[string]bool)
	for _, src := range sources {
		if src.Type != "gitrepos" {
			continue
		}
		for _, repo := range src.Repos {
			if CodecovMatchesRepo(entry.Service, entry.Organization, repo) {
				allowed[strings.ToLower(repo.Owner+"/"+repo.Name)] = true
			}
		}
	}
	for _, scope := range selectedBP.Connections[0].Scopes {
		if !allowed[strings.ToLower(scope.ScopeID)] {
			return fmt.Errorf("codecov scope %q is not a matching team repository", scope.ScopeID)
		}
		if bpName := codecovScopeOwner(blueprints, selectedBP.ID, scope.ScopeID); bpName != "" {
			return fmt.Errorf("codecov scope %q already belongs to blueprint %q", scope.ScopeID, bpName)
		}
	}
	labels := append(append([]string(nil), selectedBP.Labels...), "team:"+teamID)
	if err := s.client.PatchBlueprintLabels(ctx, selectedBP.ID, labels); err != nil {
		return err
	}
	if _, err := s.storeConnectionConfig(ctx, teamID, entry, req.ConnectionID); err != nil {
		if rollbackErr := s.client.PatchBlueprintLabels(ctx, selectedBP.ID, selectedBP.Labels); rollbackErr != nil {
			return fmt.Errorf("store connection: %w; rollback blueprint labels: %w", err, rollbackErr)
		}
		return err
	}
	return nil
}

func findAttachableCodecovBlueprint(blueprints []Blueprint, req AttachCodecovRequest) (*Blueprint, error) {
	for i := range blueprints {
		bp := &blueprints[i]
		if bp.ID != req.BlueprintID {
			continue
		}
		if !hasLabel(bp.Labels, "managed:lfmbot") && !hasTeamLabel(bp.Labels) && len(bp.Connections) == 1 &&
			bp.Connections[0].PluginName == "codecov" && bp.Connections[0].ConnectionID == req.ConnectionID &&
			len(bp.Connections[0].Scopes) > 0 {
			return bp, nil
		}
		break
	}
	return nil, fmt.Errorf("blueprint %d is not an unattached Codecov blueprint on connection %d", req.BlueprintID, req.ConnectionID)
}

func codecovScopeOwner(blueprints []Blueprint, excludedID int64, scopeID string) string {
	for _, bp := range blueprints {
		if bp.ID == excludedID {
			continue
		}
		for _, conn := range bp.Connections {
			if conn.PluginName != "codecov" {
				continue
			}
			for _, scope := range conn.Scopes {
				if strings.EqualFold(scope.ScopeID, scopeID) {
					return bp.Name
				}
			}
		}
	}
	return ""
}

type TestConnectionResult struct {
	ExpiryDate string `json:"expiry_date,omitempty"`
}

func sanitizeEntry(entry *ConnectionEntry) {
	entry.Endpoint = strings.TrimSpace(entry.Endpoint)
	entry.Username = strings.TrimSpace(entry.Username)
	entry.Organization = strings.TrimSpace(entry.Organization)
	entry.Service = strings.TrimSpace(entry.Service)
	entry.CITool = strings.TrimSpace(entry.CITool)
}

func validateCodecovEntry(entry ConnectionEntry) error {
	if entry.Organization == "" {
		return fmt.Errorf("organization is required for codecov connections")
	}
	switch entry.Service {
	case "github", "gitlab", "github_enterprise", "gitlab_enterprise":
		return nil
	default:
		return fmt.Errorf("unsupported codecov service %q: team repositories must use GitHub or GitLab", entry.Service)
	}
}

func (s *Service) TestConnection(ctx context.Context, entry ConnectionEntry) (*TestConnectionResult, error) {
	sanitizeEntry(&entry)
	if entry.Plugin == PluginTestRegistry {
		return s.testTestRegistry(ctx, entry)
	}
	if err := s.checkEndpoint(entry.Endpoint); err != nil {
		return nil, err
	}
	switch entry.Plugin {
	case "github":
		return s.testGitHub(ctx, entry)
	case "gitlab":
		return s.testGitLab(ctx, entry)
	case "jira":
		return s.testJira(ctx, entry)
	case "codecov":
		if entry.Service == "" {
			entry.Service = "github"
		}
		if err := validateCodecovEntry(entry); err != nil {
			return nil, err
		}
		return &TestConnectionResult{}, s.client.TestCodecovConnection(ctx, entry)
	default:
		return nil, fmt.Errorf("unsupported plugin: %s", entry.Plugin)
	}
}

func (s *Service) testGitHub(ctx context.Context, entry ConnectionEntry) (*TestConnectionResult, error) {
	extClient := s.extHTTPClient()
	result := &TestConnectionResult{}

	endpoint := strings.TrimRight(entry.Endpoint, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/user", nil)
	if err != nil {
		return nil, fmt.Errorf("test github connection: %w", err)
	}
	req.Header.Set("Authorization", "token "+entry.Token)

	resp, err := extClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("test github connection: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("invalid credentials (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if expiry := resp.Header.Get("github-authentication-token-expiration"); expiry != "" {
		if m := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})`).FindString(expiry); m != "" {
			result.ExpiryDate = m
		}
	}

	if err := s.client.TestGitHubConnection(ctx, entry); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) testGitLab(ctx context.Context, entry ConnectionEntry) (*TestConnectionResult, error) {
	extClient := s.extHTTPClient()
	result := &TestConnectionResult{}

	endpoint := regexp.MustCompile(`/api/v[0-9]+$`).ReplaceAllString(
		strings.TrimRight(entry.Endpoint, "/"), "")

	selfReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/v4/personal_access_tokens/self", nil)
	if err != nil {
		return nil, fmt.Errorf("test gitlab connection: %w", err)
	}
	selfReq.Header.Set("PRIVATE-TOKEN", entry.Token)

	selfResp, err := extClient.Do(selfReq)
	if err != nil {
		return nil, fmt.Errorf("test gitlab connection: %w", err)
	}
	defer selfResp.Body.Close()

	if selfResp.StatusCode == http.StatusOK {
		var data struct {
			ExpiresAt string   `json:"expires_at"`
			Scopes    []string `json:"scopes"`
		}
		if err := json.NewDecoder(selfResp.Body).Decode(&data); err == nil {
			if data.ExpiresAt != "" {
				result.ExpiryDate = data.ExpiresAt
			}
			if len(data.Scopes) > 0 && !slices.ContainsFunc(data.Scopes, func(s string) bool {
				return s == "api" || s == "read_api"
			}) {
				return nil, fmt.Errorf("test gitlab connection: The provided token is missing the required permission: read_api (or api)")
			}
		}
		return result, nil
	}

	if selfResp.StatusCode == http.StatusNotFound {
		userReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/v4/user", nil)
		if err != nil {
			return nil, fmt.Errorf("test gitlab connection: %w", err)
		}
		userReq.Header.Set("PRIVATE-TOKEN", entry.Token)

		userResp, err := extClient.Do(userReq)
		if err != nil {
			return nil, fmt.Errorf("test gitlab connection: %w", err)
		}
		defer userResp.Body.Close()

		if userResp.StatusCode == http.StatusUnauthorized || userResp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("invalid credentials (HTTP %d)", userResp.StatusCode)
		}
		if userResp.StatusCode >= 400 {
			return nil, fmt.Errorf("HTTP %d", userResp.StatusCode)
		}
		return result, nil
	}

	if selfResp.StatusCode == http.StatusUnauthorized || selfResp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("invalid credentials (HTTP %d)", selfResp.StatusCode)
	}
	return nil, fmt.Errorf("HTTP %d", selfResp.StatusCode)
}

func (s *Service) testJira(ctx context.Context, entry ConnectionEntry) (*TestConnectionResult, error) {
	extClient := s.extHTTPClient()
	result := &TestConnectionResult{}

	endpoint := strings.TrimSuffix(strings.TrimRight(entry.Endpoint, "/"), "/rest")
	url := endpoint + "/rest/api/2/myself"
	slog.Info("testJira: calling Jira API", "url", url, "username", entry.Username)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("test jira connection: %w", err)
	}
	req.SetBasicAuth(entry.Username, entry.Token)

	resp, err := extClient.Do(req)
	if err != nil {
		slog.Warn("testJira: request failed", "url", url, "err", err)
		return nil, fmt.Errorf("test jira connection: %w", err)
	}
	defer resp.Body.Close()

	slog.Info("testJira: response received", "url", url, "status", resp.StatusCode)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("invalid credentials (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return result, nil
}

type ProxyBrowseResult struct {
	Key     string `json:"key,omitempty"`
	Name    string `json:"name"`
	BoardID string `json:"board_id,omitempty"`
}

type LookupResult struct {
	Valid         bool            `json:"valid"`
	CanonicalName string          `json:"canonical_name,omitempty"`
	Archived      bool            `json:"archived,omitempty"`
	Warning       string          `json:"warning,omitempty"`
	Error         string          `json:"error,omitempty"`
	RawData       json.RawMessage `json:"-"`
}

func (s *Service) LookupRepo(ctx context.Context, plugin, endpoint, token, owner, name string, extra map[string]string) *LookupResult {
	lookupCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	switch plugin {
	case "github":
		return s.lookupGitHub(lookupCtx, endpoint, token, owner, name)
	case "gitlab":
		return s.lookupGitLab(lookupCtx, endpoint, token, owner, name)
	case "jira":
		username := extra["username"]
		return s.lookupJira(lookupCtx, endpoint, token, username, owner)
	default:
		return &LookupResult{Error: fmt.Sprintf("lookup not supported for plugin %q", plugin)}
	}
}

func (s *Service) lookupGitHub(ctx context.Context, endpoint, token, owner, name string) *LookupResult {
	endpoint = strings.TrimRight(endpoint, "/")
	reqURL := fmt.Sprintf("%s/repos/%s/%s", endpoint, url.PathEscape(owner), url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}
	req.Header.Set("Authorization", "token "+token)

	resp, err := s.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
		}
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &LookupResult{Error: "Token expired — update in Token Management"}
	}
	if resp.StatusCode == http.StatusNotFound {
		return &LookupResult{Error: "Repository not found"}
	}
	if resp.StatusCode >= 400 {
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}

	var repo struct {
		FullName string `json:"full_name"`
		Archived bool   `json:"archived"`
	}
	if err := json.Unmarshal(body, &repo); err != nil {
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}

	result := &LookupResult{Valid: true, CanonicalName: repo.FullName, Archived: repo.Archived, RawData: body}
	if repo.Archived {
		result.Warning = "This repository is archived — metrics may be limited"
	}
	return result
}

func (s *Service) lookupGitLab(ctx context.Context, endpoint, token, owner, name string) *LookupResult {
	endpoint = regexp.MustCompile(`/api/v[0-9]+$`).ReplaceAllString(
		strings.TrimRight(endpoint, "/"), "")
	fullPath := owner + "/" + name
	reqURL := fmt.Sprintf("%s/api/v4/projects/%s", endpoint, url.PathEscape(fullPath))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}
	req.Header.Set("PRIVATE-TOKEN", token)

	resp, err := s.httpClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
		}
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &LookupResult{Error: "Token expired — update in Token Management"}
	}
	if resp.StatusCode == http.StatusNotFound {
		return &LookupResult{Error: "Repository not found"}
	}
	if resp.StatusCode >= 400 {
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}

	var project struct {
		PathWithNamespace string `json:"path_with_namespace"`
		Archived          bool   `json:"archived"`
	}
	if err := json.Unmarshal(body, &project); err != nil {
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}

	result := &LookupResult{Valid: true, CanonicalName: project.PathWithNamespace, Archived: project.Archived, RawData: body}
	if project.Archived {
		result.Warning = "This repository is archived — metrics may be limited"
	}
	return result
}

func (s *Service) lookupJira(ctx context.Context, endpoint, token, username, projectKey string) *LookupResult {
	slog.Info("lookupJira: called",
		"endpoint", endpoint, "username", username, "projectKey", projectKey)
	endpoint = strings.TrimSuffix(strings.TrimRight(endpoint, "/"), "/rest")
	reqURL := fmt.Sprintf("%s/rest/api/2/project/%s", endpoint, url.PathEscape(projectKey))
	slog.Info("lookupJira: requesting", "url", reqURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		slog.Warn("lookupJira: failed to create request", "err", err)
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}
	req.SetBasicAuth(username, token)

	resp, err := s.httpClient().Do(req)
	if err != nil {
		slog.Warn("lookupJira: request failed", "url", reqURL, "err", err, "ctxErr", ctx.Err())
		if ctx.Err() != nil {
			return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
		}
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}
	defer resp.Body.Close()

	slog.Info("lookupJira: response", "status", resp.StatusCode, "projectKey", projectKey)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &LookupResult{Error: "Token expired — update in Token Management"}
	}
	if resp.StatusCode == http.StatusNotFound {
		slog.Warn("lookupJira: project not found", "projectKey", projectKey)
		return &LookupResult{Error: "Project not found"}
	}
	if resp.StatusCode >= 400 {
		slog.Warn("lookupJira: unexpected status", "status", resp.StatusCode, "projectKey", projectKey)
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}

	var project struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &project); err != nil {
		slog.Warn("lookupJira: failed to parse response", "err", err, "body", string(body[:min(len(body), 200)]))
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}

	slog.Info("lookupJira: project found", "key", project.Key, "name", project.Name)
	return &LookupResult{Valid: true, CanonicalName: project.Key}
}

func (s *Service) LookupRepoForTeam(ctx context.Context, teamID, plugin, owner, name string) *LookupResult {
	slog.Info("LookupRepoForTeam: called",
		"team", teamID, "plugin", plugin, "owner", owner, "name", name)
	cfg, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
	if err != nil {
		slog.Warn("LookupRepoForTeam: config read failed, returning warning",
			"team", teamID, "err", err)
		return &LookupResult{Valid: true, Warning: "Could not verify — will be checked at sync time"}
	}

	tokenKey := "devlake_" + plugin + "_token"
	token := cfg[tokenKey]
	if token == "" {
		if connID, err := strconv.Atoi(cfg["devlake_codecov_connection_id"]); err == nil && connID > 0 &&
			CodecovMatchesRepo(cfg["devlake_codecov_service"], cfg["devlake_codecov_organization"], database.RepoRow{
				Type: plugin, Owner: owner, Name: name,
			}) {
			return &LookupResult{Valid: true, Warning: "Could not pre-validate without a Git provider token; Codecov scope will be checked during sync"}
		}
		slog.Warn("LookupRepoForTeam: no token in config",
			"team", teamID, "plugin", plugin, "tokenKey", tokenKey)
		return &LookupResult{Error: fmt.Sprintf("No %s connection configured — set one up first", plugin)}
	}

	epKey := "devlake_" + plugin + "_endpoint"
	endpoint := cfg[epKey]
	if endpoint == "" {
		switch plugin {
		case "github":
			endpoint = "https://api.github.com"
		case "gitlab":
			endpoint = "https://gitlab.com"
		}
	}
	slog.Info("LookupRepoForTeam: using endpoint",
		"team", teamID, "plugin", plugin, "endpoint", endpoint)

	extra := map[string]string{}
	if username := cfg["devlake_"+plugin+"_username"]; username != "" {
		extra["username"] = username
		slog.Info("LookupRepoForTeam: username from config",
			"team", teamID, "plugin", plugin, "username", username)
	} else if plugin == "jira" {
		slog.Info("LookupRepoForTeam: no jira username in config, trying connection fallback",
			"team", teamID)
		if connIDStr := cfg["devlake_jira_connection_id"]; connIDStr != "" {
			if connID, err := strconv.Atoi(connIDStr); err == nil {
				if conn, err := s.client.GetConnection(ctx, "jira", connID); err == nil {
					if u, ok := conn["username"].(string); ok && u != "" {
						extra["username"] = u
						_ = s.store.SetEntityConfigKey(ctx, "team", teamID, "devlake_jira_username", u)
						slog.Info("LookupRepoForTeam: backfilled jira username from connection",
							"team", teamID, "connID", connID, "username", u)
					} else {
						slog.Warn("LookupRepoForTeam: connection has no username",
							"team", teamID, "connID", connID, "connFields", conn)
					}
				} else {
					slog.Warn("LookupRepoForTeam: failed to get connection",
						"team", teamID, "connID", connIDStr, "err", err)
				}
			}
		} else {
			slog.Warn("LookupRepoForTeam: no jira connection_id in config",
				"team", teamID)
		}
	}

	slog.Info("LookupRepoForTeam: calling LookupRepo",
		"team", teamID, "plugin", plugin, "owner", owner, "name", name, "extra", extra)
	result := s.LookupRepo(ctx, plugin, endpoint, token, owner, name, extra)
	slog.Info("LookupRepoForTeam: result",
		"team", teamID, "plugin", plugin,
		"valid", result.Valid, "error", result.Error, "warning", result.Warning,
		"hasRawData", result.RawData != nil)
	return result
}

func (s *Service) buildScopeFromLookup(plugin string, rawData json.RawMessage) (json.RawMessage, error) {
	var raw map[string]any
	if err := json.Unmarshal(rawData, &raw); err != nil {
		return nil, err
	}

	switch plugin {
	case "github":
		id, _ := raw["id"].(float64)
		scope := map[string]any{
			"githubId":    int64(id),
			"fullName":    raw["full_name"],
			"name":        raw["name"],
			"htmlUrl":     raw["html_url"],
			"cloneUrl":    raw["clone_url"],
			"description": raw["description"],
			"language":    raw["language"],
			"createdDate": raw["created_at"],
			"updatedDate": raw["updated_at"],
		}
		if owner, ok := raw["owner"].(map[string]any); ok {
			scope["owner"] = owner["login"]
		}
		out, err := json.Marshal(scope)
		return out, err

	case "gitlab":
		id, _ := raw["id"].(float64)
		scope := map[string]any{
			"gitlabId":          int64(id),
			"pathWithNamespace": raw["path_with_namespace"],
			"name":              raw["name"],
			"httpUrlToRepo":     raw["http_url_to_repo"],
			"description":       raw["description"],
			"createdDate":       raw["created_at"],
			"updatedDate":       raw["last_activity_at"],
		}
		out, err := json.Marshal(scope)
		return out, err

	default:
		return nil, fmt.Errorf("buildScopeFromLookup not supported for plugin %q", plugin)
	}
}

func (s *Service) httpClient() *http.Client {
	return s.httpClientWithTimeout(10 * time.Second)
}

func (s *Service) extHTTPClient() *http.Client {
	return s.httpClientWithTimeout(30 * time.Second)
}

func (s *Service) httpClientWithTimeout(timeout time.Duration) *http.Client {
	return netguard.Client(timeout, s.config.AllowPrivateEndpoints, s.config.TLSSkipVerify)
}

func (s *Service) checkEndpoint(endpoint string) error {
	return netguard.ValidateURL(endpoint, s.config.AllowPrivateEndpoints)
}

func (s *Service) ProxyLookup(ctx context.Context, entry ConnectionEntry, owner, name string) *LookupResult {
	sanitizeEntry(&entry)
	if err := s.checkEndpoint(entry.Endpoint); err != nil {
		return &LookupResult{Error: err.Error()}
	}
	return s.LookupRepo(ctx, entry.Plugin, entry.Endpoint, entry.Token, owner, name, nil)
}

func (s *Service) ProxyBrowse(ctx context.Context, entry ConnectionEntry, query string) ([]ProxyBrowseResult, error) {
	sanitizeEntry(&entry)
	if entry.Plugin == PluginTestRegistry {
		return s.proxyBrowseTestRegistry(ctx, entry)
	}
	if err := s.checkEndpoint(entry.Endpoint); err != nil {
		return nil, err
	}
	switch entry.Plugin {
	case "github":
		return s.proxyBrowseGitHub(ctx, entry)
	case "gitlab":
		return s.proxyBrowseGitLab(ctx, entry)
	case "jira":
		return s.proxyBrowseJira(ctx, entry, query)
	default:
		return nil, fmt.Errorf("proxy browse not supported for plugin %q", entry.Plugin)
	}
}

func (s *Service) proxyBrowseGitHub(ctx context.Context, entry ConnectionEntry) ([]ProxyBrowseResult, error) {
	endpoint := strings.TrimRight(entry.Endpoint, "/")
	proxyClient := s.extHTTPClient()
	var all []ProxyBrowseResult

	for page := 1; ; page++ {
		reqURL := fmt.Sprintf("%s/user/repos?per_page=100&page=%d&sort=full_name", endpoint, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("proxy browse github: %w", err)
		}
		req.Header.Set("Authorization", "token "+entry.Token)

		resp, err := proxyClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("proxy browse github: %w", err)
		}
		if resp.StatusCode >= 400 {
			resp.Body.Close()
			return nil, fmt.Errorf("proxy browse github: HTTP %d", resp.StatusCode)
		}

		var repos []struct {
			FullName string `json:"full_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("proxy browse github: decode: %w", err)
		}
		resp.Body.Close()

		if len(repos) == 0 {
			break
		}
		for _, r := range repos {
			all = append(all, ProxyBrowseResult{Key: r.FullName, Name: r.FullName})
		}
		if len(repos) < 100 {
			break
		}
	}
	return all, nil
}

func (s *Service) proxyBrowseGitLab(ctx context.Context, entry ConnectionEntry) ([]ProxyBrowseResult, error) {
	endpoint := regexp.MustCompile(`/api/v[0-9]+$`).ReplaceAllString(
		strings.TrimRight(entry.Endpoint, "/"), "")
	proxyClient := s.extHTTPClient()
	var all []ProxyBrowseResult

	for page := 1; ; page++ {
		reqURL := fmt.Sprintf("%s/api/v4/projects?membership=true&per_page=100&page=%d&order_by=name&sort=asc", endpoint, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("proxy browse gitlab: %w", err)
		}
		req.Header.Set("PRIVATE-TOKEN", entry.Token)

		resp, err := proxyClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("proxy browse gitlab: %w", err)
		}
		if resp.StatusCode >= 400 {
			resp.Body.Close()
			return nil, fmt.Errorf("proxy browse gitlab: HTTP %d", resp.StatusCode)
		}

		var projects []struct {
			PathWithNamespace string `json:"path_with_namespace"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("proxy browse gitlab: decode: %w", err)
		}
		resp.Body.Close()

		if len(projects) == 0 {
			break
		}
		for _, p := range projects {
			all = append(all, ProxyBrowseResult{Key: p.PathWithNamespace, Name: p.PathWithNamespace})
		}
		if len(projects) < 100 {
			break
		}
	}
	return all, nil
}

func (s *Service) proxyBrowseJira(ctx context.Context, entry ConnectionEntry, query string) ([]ProxyBrowseResult, error) {
	endpoint := strings.TrimSuffix(strings.TrimRight(entry.Endpoint, "/"), "/rest")
	query = strings.TrimSpace(query)

	proxyClient := s.extHTTPClient()
	var all []ProxyBrowseResult
	startAt := 0
	const maxResults = 50

	for {
		targetURL := fmt.Sprintf("%s/rest/agile/1.0/board?startAt=%d&maxResults=%d",
			endpoint, startAt, maxResults)
		if query != "" {
			targetURL += "&name=" + url.QueryEscape(query)
		}
		slog.Debug("proxy browse jira boards", "url", targetURL, "startAt", startAt)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.SetBasicAuth(entry.Username, entry.Token)

		resp, err := proxyClient.Do(req)
		if err != nil {
			slog.Error("proxy browse jira request failed", "err", err)
			return nil, fmt.Errorf("proxy browse jira: %w", err)
		}

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
			resp.Body.Close()
			slog.Error("proxy browse jira upstream error",
				"status", resp.StatusCode,
				"body", truncate(string(body), 500))
			return nil, fmt.Errorf("proxy browse jira: HTTP %d: %s",
				resp.StatusCode, truncate(string(body), 200))
		}

		var page struct {
			Values []struct {
				ID       int    `json:"id"`
				Name     string `json:"name"`
				Location struct {
					ProjectKey  string `json:"projectKey"`
					ProjectName string `json:"projectName"`
				} `json:"location"`
			} `json:"values"`
			Total      int  `json:"total"`
			MaxResults int  `json:"maxResults"`
			IsLast     bool `json:"isLast"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			resp.Body.Close()
			slog.Error("proxy browse jira decode failed", "err", err)
			return nil, fmt.Errorf("proxy browse jira: decode: %w", err)
		}
		resp.Body.Close()

		for _, b := range page.Values {
			all = append(all, ProxyBrowseResult{
				Key:     b.Location.ProjectKey,
				Name:    b.Name,
				BoardID: strconv.Itoa(b.ID),
			})
		}

		if page.IsLast || len(page.Values) == 0 {
			break
		}
		startAt += len(page.Values)
	}

	return all, nil
}

type JiraIssueType struct {
	Name string `json:"name"`
}

type JiraField struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Custom bool   `json:"custom"`
}

type JiraProjectMeta struct {
	IssueTypes []JiraIssueType `json:"issue_types"`
	Fields     []JiraField     `json:"fields"`
}

type JiraProject struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (s *Service) ProxyJiraProject(ctx context.Context, entry ConnectionEntry, projectKey string) (*JiraProject, error) {
	sanitizeEntry(&entry)
	if err := s.checkEndpoint(entry.Endpoint); err != nil {
		return nil, err
	}
	endpoint := strings.TrimSuffix(strings.TrimRight(entry.Endpoint, "/"), "/rest")
	projectURL := fmt.Sprintf("%s/rest/api/2/project/%s", endpoint, url.PathEscape(projectKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, projectURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build project request: %w", err)
	}
	req.SetBasicAuth(entry.Username, entry.Token)

	resp, err := s.extHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch project %s: %w", projectKey, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
		return nil, fmt.Errorf("fetch project %s: HTTP %d: %s", projectKey, resp.StatusCode, truncate(string(body), 200))
	}

	var project JiraProject
	if err := json.NewDecoder(resp.Body).Decode(&project); err != nil {
		return nil, fmt.Errorf("decode project %s: %w", projectKey, err)
	}
	if project.Key == "" || project.Name == "" {
		return nil, fmt.Errorf("project %s response was incomplete", projectKey)
	}
	return &project, nil
}

func (s *Service) ProxyJiraMeta(ctx context.Context, entry ConnectionEntry, projectKey string) (*JiraProjectMeta, error) {
	sanitizeEntry(&entry)
	if err := s.checkEndpoint(entry.Endpoint); err != nil {
		return nil, err
	}
	endpoint := strings.TrimSuffix(strings.TrimRight(entry.Endpoint, "/"), "/rest")

	proxyClient := s.extHTTPClient()
	meta := &JiraProjectMeta{}

	// Fetch issue types for this project.
	projURL := fmt.Sprintf("%s/rest/api/2/project/%s", endpoint, url.PathEscape(projectKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, projURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build project request: %w", err)
	}
	req.SetBasicAuth(entry.Username, entry.Token)

	resp, err := proxyClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch project %s: %w", projectKey, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
		return nil, fmt.Errorf("fetch project %s: HTTP %d: %s",
			projectKey, resp.StatusCode, truncate(string(body), 200))
	}

	var projData struct {
		IssueTypes []struct {
			Name string `json:"name"`
		} `json:"issueTypes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&projData); err != nil {
		return nil, fmt.Errorf("decode project %s: %w", projectKey, err)
	}
	for _, it := range projData.IssueTypes {
		meta.IssueTypes = append(meta.IssueTypes, JiraIssueType{Name: it.Name})
	}

	// Fetch fields.
	fieldsURL := endpoint + "/rest/api/2/field"
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, fieldsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build fields request: %w", err)
	}
	req2.SetBasicAuth(entry.Username, entry.Token)

	resp2, err := proxyClient.Do(req2)
	if err != nil {
		return nil, fmt.Errorf("fetch fields: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp2.Body, maxResponseBody))
		return nil, fmt.Errorf("fetch fields: HTTP %d: %s",
			resp2.StatusCode, truncate(string(body), 200))
	}

	var rawFields []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Custom bool   `json:"custom"`
		Schema *struct {
			Type string `json:"type"`
		} `json:"schema"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&rawFields); err != nil {
		return nil, fmt.Errorf("decode fields: %w", err)
	}
	for _, f := range rawFields {
		if f.Schema != nil && (f.Schema.Type == "number" || f.Schema.Type == "integer" || f.Custom) {
			meta.Fields = append(meta.Fields, JiraField{ID: f.ID, Name: f.Name, Custom: f.Custom})
		}
	}

	return meta, nil
}

type TeamJiraBoardMetaResult struct {
	ProjectKey string          `json:"project_key"`
	IssueTypes []JiraIssueType `json:"issue_types"`
	Fields     []JiraField     `json:"fields"`
}

func (s *Service) TeamJiraBoardMeta(ctx context.Context, teamID string, boardID string) (*TeamJiraBoardMetaResult, error) {
	cfg, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
	if err != nil {
		return nil, fmt.Errorf("config read failed: %w", err)
	}
	token := cfg["devlake_jira_token"]
	endpoint := cfg["devlake_jira_endpoint"]
	username := cfg["devlake_jira_username"]
	if token == "" || endpoint == "" || username == "" {
		return nil, fmt.Errorf("missing Jira credentials for team %s", teamID)
	}

	entry := ConnectionEntry{
		Plugin:   "jira",
		Endpoint: endpoint,
		Token:    token,
		Username: username,
	}
	return s.ProxyJiraBoardMeta(ctx, entry, boardID)
}

func (s *Service) ProxyJiraBoardMeta(ctx context.Context, entry ConnectionEntry, boardID string) (*TeamJiraBoardMetaResult, error) {
	sanitizeEntry(&entry)
	if err := s.checkEndpoint(entry.Endpoint); err != nil {
		return nil, err
	}

	ep := strings.TrimSuffix(strings.TrimRight(entry.Endpoint, "/"), "/rest")
	boardURL := fmt.Sprintf("%s/rest/agile/1.0/board/%s", ep, url.PathEscape(boardID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, boardURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build board request: %w", err)
	}
	req.SetBasicAuth(entry.Username, entry.Token)

	resp, err := s.extHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch board %s: %w", boardID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
		return nil, fmt.Errorf("fetch board %s: HTTP %d: %s", boardID, resp.StatusCode, truncate(string(body), 200))
	}

	var board struct {
		Location struct {
			ProjectKey string `json:"projectKey"`
		} `json:"location"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&board); err != nil {
		return nil, fmt.Errorf("decode board %s: %w", boardID, err)
	}
	if board.Location.ProjectKey == "" {
		return nil, fmt.Errorf("board %s has no project key", boardID)
	}

	meta, err := s.ProxyJiraMeta(ctx, entry, board.Location.ProjectKey)
	if err != nil {
		return nil, fmt.Errorf("fetch meta for project %s: %w", board.Location.ProjectKey, err)
	}

	return &TeamJiraBoardMetaResult{
		ProjectKey: board.Location.ProjectKey,
		IssueTypes: meta.IssueTypes,
		Fields:     meta.Fields,
	}, nil
}

type JiraScopeConfig struct {
	RequirementTypes []string `json:"requirement_types"`
	BugTypes         []string `json:"bug_types"`
	StoryPointField  string   `json:"story_point_field"`
}

func (s *Service) ApplyJiraScopeConfigs(ctx context.Context, teamID string, configs map[string]JiraScopeConfig) error {
	connID, err := s.teamConnectionID(ctx, teamID, "jira")
	if err != nil {
		return fmt.Errorf("lookup jira connection: %w", err)
	}
	if connID == 0 {
		return fmt.Errorf("no jira connection for team %s", teamID)
	}

	scopes, err := s.client.ListConnectionScopes(ctx, "jira", connID)
	if err != nil {
		slog.Warn("ApplyJiraScopeConfigs: failed to list scopes", "team", teamID, "err", err)
	}

	for boardID, cfg := range configs {
		typeMappings := map[string]map[string]string{}
		for _, t := range cfg.RequirementTypes {
			typeMappings[t] = map[string]string{"standardType": "REQUIREMENT"}
		}
		for _, t := range cfg.BugTypes {
			typeMappings[t] = map[string]string{"standardType": "BUG"}
		}

		body := map[string]any{
			"name":            fmt.Sprintf("%s-conn%d-board-%s", teamID, connID, boardID),
			"entities":        []string{"TICKET"},
			"typeMappings":    typeMappings,
			"storyPointField": cfg.StoryPointField,
		}

		existingConfigID := 0
		for _, scope := range scopes {
			sid := scopeIDFromMap("jira", scope)
			if sid == boardID {
				if cfgID, ok := scope["scopeConfigId"].(float64); ok && cfgID > 0 {
					existingConfigID = int(cfgID)
				}
				break
			}
		}

		if existingConfigID > 0 {
			if err := s.client.UpdateScopeConfig(ctx, "jira", connID, existingConfigID, body); err != nil {
				slog.Error("failed to update scope config",
					"team", teamID, "board", boardID, "configID", existingConfigID, "err", err)
			} else {
				slog.Info("updated scope config",
					"team", teamID, "board", boardID, "configID", existingConfigID)
			}
		} else {
			configName := fmt.Sprintf("%s-conn%d-board-%s", teamID, connID, boardID)
			configID, err := s.findOrCreateScopeConfig(ctx, "jira", connID, configName, body)
			if err != nil {
				slog.Error("failed to get scope config",
					"team", teamID, "board", boardID, "err", err)
				continue
			}
			if err := s.client.UpdateScopeConfig(ctx, "jira", connID, configID, body); err != nil {
				slog.Warn("failed to update found scope config, continuing with attach",
					"team", teamID, "board", boardID, "configID", configID, "err", err)
			}
			if err := s.client.PatchScope(ctx, "jira", connID, boardID, configID); err != nil {
				slog.Error("failed to attach scope config",
					"team", teamID, "board", boardID, "configID", configID, "err", err)
			} else {
				slog.Info("attached scope config",
					"team", teamID, "board", boardID, "configID", configID)
			}
		}
	}
	return nil
}

func (s *Service) CleanupTeam(ctx context.Context, teamID string) (*CleanupResult, error) {
	result := &CleanupResult{}

	// Step 1: delete managed blueprints and detach manually created ones.
	teamLabel := "team:" + teamID
	blueprints, err := s.client.ListBlueprints(ctx, teamLabel)
	if err != nil {
		return nil, fmt.Errorf("list team blueprints: %w", err)
	}

	freedPlugins := make(map[string]bool)
	freedCodecovConnections := make(map[int]bool)
	for _, bp := range blueprints {
		if !hasLabel(bp.Labels, "managed:lfmbot") {
			continue
		}
		for _, conn := range bp.Connections {
			freedPlugins[conn.PluginName] = true
			if conn.PluginName == "codecov" {
				freedCodecovConnections[conn.ConnectionID] = true
			}
		}
	}

	for _, bp := range blueprints {
		if !hasLabel(bp.Labels, "managed:lfmbot") {
			labels := make([]string, 0, len(bp.Labels))
			for _, label := range bp.Labels {
				if label != teamLabel {
					labels = append(labels, label)
				}
			}
			if err := s.client.PatchBlueprintLabels(ctx, bp.ID, labels); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("detach blueprint %q: %v", bp.Name, err))
			}
			continue
		}
		if err := s.client.DeleteBlueprint(ctx, bp.ID); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("delete blueprint %q: %v", bp.Name, err))
			slog.Error("cleanup: failed to delete blueprint",
				"team", teamID, "blueprint", bp.Name, "id", bp.ID, "err", err)
		} else {
			result.DeletedBlueprints = append(result.DeletedBlueprints, bp.Name)
			slog.Info("cleanup: deleted blueprint",
				"team", teamID, "blueprint", bp.Name, "id", bp.ID)
		}
	}

	// Step 2: migrate other teams that share the same plugin connections.
	if len(freedPlugins) > 0 {
		teams, err := s.store.ListTeamRows(ctx)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("list teams for migration: %v", err))
		} else {
			for _, team := range teams {
				if team.ID == teamID {
					continue
				}
				sources, err := s.store.ListSourceRows(ctx, team.ID)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("list sources for %s: %v", team.ID, err))
					continue
				}
				usesFreedPlugin := teamUsesPlugins(sources, freedPlugins)
				if !usesFreedPlugin && len(freedCodecovConnections) > 0 {
					usesFreedPlugin, err = s.teamUsesFreedCodecovConnection(ctx, team.ID, sources, freedCodecovConnections)
					if err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("read Codecov config for %s: %v", team.ID, err))
						continue
					}
				}
				if !usesFreedPlugin {
					continue
				}
				syncResult, err := s.SyncTeam(ctx, team.ID)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("re-sync %s: %v", team.ID, err))
					continue
				}
				for _, name := range syncResult.Created {
					result.Migrated = append(result.Migrated, fmt.Sprintf("%s → %s", name, team.ID))
				}
				for _, name := range syncResult.Updated {
					result.Migrated = append(result.Migrated, fmt.Sprintf("%s → %s (updated)", name, team.ID))
				}
				slog.Info("cleanup: migrated resources to other team",
					"deleted_team", teamID, "synced_team", team.ID,
					"created", syncResult.Created, "updated", syncResult.Updated)
			}
		}
	}

	// Step 3: delete scopes and connections exclusive to this team.
	s.cleanupConnections(ctx, teamID, result)

	return result, nil
}

func (s *Service) teamUsesFreedCodecovConnection(ctx context.Context, teamID string, sources []database.SourceRow, freed map[int]bool) (bool, error) {
	cfg, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
	if err != nil {
		return false, err
	}
	connID, _ := strconv.Atoi(cfg["devlake_codecov_connection_id"])
	if !freed[connID] {
		return false, nil
	}
	for _, src := range sources {
		if src.Type != "gitrepos" {
			continue
		}
		for _, repo := range src.Repos {
			if CodecovMatchesRepo(cfg["devlake_codecov_service"], cfg["devlake_codecov_organization"], repo) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *Service) cleanupConnections(ctx context.Context, teamID string, result *CleanupResult) {
	cfg, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("read team config: %v", err))
		return
	}

	plugins := teamPlugins
	type ownedConn struct {
		plugin string
		connID int
	}
	var owned []ownedConn

	for _, plugin := range plugins {
		connIDStr := cfg["devlake_"+plugin+"_connection_id"]
		if connIDStr == "" {
			continue
		}
		connID, err := strconv.Atoi(connIDStr)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("invalid %s connection ID %q: %v", plugin, connIDStr, err))
			continue
		}
		owned = append(owned, ownedConn{plugin: plugin, connID: connID})
	}

	if len(owned) == 0 {
		return
	}

	// Build a set of connection IDs used by other teams.
	otherConnIDs := make(map[string]bool)
	teams, err := s.store.ListTeamRows(ctx)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("list teams for connection check: %v", err))
		return
	}
	for _, team := range teams {
		if team.ID == teamID {
			continue
		}
		otherCfg, err := s.store.GetEntityConfigKV(ctx, "team", team.ID)
		if err != nil {
			continue
		}
		for _, plugin := range plugins {
			if v := otherCfg["devlake_"+plugin+"_connection_id"]; v != "" {
				otherConnIDs[plugin+":"+v] = true
			}
		}
	}

	// Collect scope IDs still referenced by any remaining blueprint.
	liveScopeIDs := make(map[ScopeKey]bool)
	liveConnIDs := make(map[string]bool)
	manualConnIDs := make(map[string]bool)
	allBPs, err := s.client.ListBlueprints(ctx, "")
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("list all blueprints for scope check: %v", err))
		slog.Error("cleanup: failed to list all blueprints for scope check",
			"team", teamID, "err", err)
		return
	}
	for _, bp := range allBPs {
		for _, conn := range bp.Connections {
			key := conn.PluginName + ":" + strconv.Itoa(conn.ConnectionID)
			liveConnIDs[key] = true
			if !hasLabel(bp.Labels, "managed:lfmbot") {
				manualConnIDs[key] = true
			}
			for _, scope := range conn.Scopes {
				liveScopeIDs[ScopeKey{conn.PluginName, conn.ConnectionID, scope.ScopeID}] = true
			}
		}
	}

	for _, oc := range owned {
		key := oc.plugin + ":" + strconv.Itoa(oc.connID)
		shared := otherConnIDs[key]
		if manualConnIDs[key] {
			continue
		}

		// Delete scopes on this connection that are no longer in any blueprint.
		scopes, err := s.client.ListConnectionScopes(ctx, oc.plugin, oc.connID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("list scopes (%s, conn %d): %v", oc.plugin, oc.connID, err))
			slog.Error("cleanup: failed to list scopes",
				"team", teamID, "plugin", oc.plugin, "connection_id", oc.connID, "err", err)
		} else {
			for _, scope := range scopes {
				scopeID := scopeIDFromMap(oc.plugin, scope)
				scopeName := scopeFullName(oc.plugin, scope)
				if scopeID == "" {
					continue
				}
				sk := ScopeKey{oc.plugin, oc.connID, scopeID}
				if liveScopeIDs[sk] {
					slog.Info("cleanup: scope still in use by another blueprint, keeping",
						"team", teamID, "plugin", oc.plugin, "connection_id", oc.connID,
						"scope_id", scopeID, "scope_name", scopeName)
					continue
				}
				if err := s.client.DeleteScope(ctx, oc.plugin, oc.connID, scopeID); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("delete scope %s (%s): %v", scopeName, oc.plugin, err))
					slog.Error("cleanup: failed to delete scope",
						"team", teamID, "plugin", oc.plugin, "connection_id", oc.connID,
						"scope_id", scopeID, "scope_name", scopeName, "err", err)
				} else {
					label := scopeName
					if label == "" {
						label = scopeID
					}
					result.DeletedScopes = append(result.DeletedScopes, oc.plugin+":"+label)
					slog.Info("cleanup: deleted scope",
						"team", teamID, "plugin", oc.plugin, "connection_id", oc.connID,
						"scope_id", scopeID, "scope_name", scopeName)
				}
			}
		}

		if shared || liveConnIDs[key] {
			slog.Info("cleanup: connection shared with other teams, keeping",
				"team", teamID, "plugin", oc.plugin, "connection_id", oc.connID)
			continue
		}

		// Delete the connection itself (only if exclusive to this team).
		if err := s.client.DeleteConnection(ctx, oc.plugin, oc.connID); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("delete %s connection %d: %v", oc.plugin, oc.connID, err))
			slog.Error("cleanup: failed to delete connection",
				"team", teamID, "plugin", oc.plugin, "connection_id", oc.connID, "err", err)
		} else {
			result.DeletedConnections = append(result.DeletedConnections, fmt.Sprintf("%s (conn %d)", oc.plugin, oc.connID))
			slog.Info("cleanup: deleted connection",
				"team", teamID, "plugin", oc.plugin, "connection_id", oc.connID)
		}
	}
}

func (s *Service) CleanupOrphanedScopes(ctx context.Context, teamID string) ([]string, []string) {
	var deleted []string
	var errs []string

	cfg, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
	if err != nil {
		return nil, []string{fmt.Sprintf("read team config: %v", err)}
	}

	plugins := teamPlugins
	type connRef struct {
		plugin string
		connID int
	}
	var conns []connRef
	for _, plugin := range plugins {
		idStr := cfg["devlake_"+plugin+"_connection_id"]
		if idStr == "" {
			continue
		}
		connID, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}
		conns = append(conns, connRef{plugin, connID})
	}
	if len(conns) == 0 {
		slog.Info("scope-cleanup: no connections found in team config", "team", teamID)
		return nil, nil
	}

	slog.Info("scope-cleanup: starting", "team", teamID, "connections", len(conns))

	liveScopeIDs := make(map[ScopeKey]bool)
	manualConnIDs := make(map[string]bool)
	allBPs, err := s.client.ListBlueprints(ctx, "")
	if err != nil {
		return nil, []string{fmt.Sprintf("list blueprints for scope check: %v", err)}
	}
	for _, bp := range allBPs {
		for _, conn := range bp.Connections {
			if !hasLabel(bp.Labels, "managed:lfmbot") {
				manualConnIDs[conn.PluginName+":"+strconv.Itoa(conn.ConnectionID)] = true
			}
			for _, scope := range conn.Scopes {
				liveScopeIDs[ScopeKey{conn.PluginName, conn.ConnectionID, scope.ScopeID}] = true
			}
		}
	}

	slog.Info("scope-cleanup: built live scope set",
		"team", teamID, "blueprints", len(allBPs), "live_scopes", len(liveScopeIDs))

	for _, c := range conns {
		if manualConnIDs[c.plugin+":"+strconv.Itoa(c.connID)] {
			continue
		}
		scopes, err := s.client.ListConnectionScopes(ctx, c.plugin, c.connID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("list scopes (%s, conn %d): %v", c.plugin, c.connID, err))
			continue
		}
		slog.Info("scope-cleanup: checking connection",
			"team", teamID, "plugin", c.plugin, "connection_id", c.connID,
			"scopes_on_connection", len(scopes))
		for _, scope := range scopes {
			scopeID := scopeIDFromMap(c.plugin, scope)
			scopeName := scopeFullName(c.plugin, scope)
			if scopeID == "" {
				slog.Warn("scope-cleanup: could not extract scope ID",
					"team", teamID, "plugin", c.plugin, "scope", scope)
				continue
			}
			sk := ScopeKey{c.plugin, c.connID, scopeID}
			if liveScopeIDs[sk] {
				slog.Debug("scope-cleanup: scope still live",
					"team", teamID, "plugin", c.plugin,
					"scope_id", scopeID, "scope_name", scopeName)
				continue
			}
			slog.Info("scope-cleanup: deleting orphaned scope",
				"team", teamID, "plugin", c.plugin, "connection_id", c.connID,
				"scope_id", scopeID, "scope_name", scopeName)
			if err := s.client.DeleteScope(ctx, c.plugin, c.connID, scopeID); err != nil {
				errs = append(errs, fmt.Sprintf("delete scope %s (%s): %v", scopeName, c.plugin, err))
				slog.Error("scope-cleanup: failed to delete orphaned scope",
					"team", teamID, "plugin", c.plugin, "connection_id", c.connID,
					"scope_id", scopeID, "scope_name", scopeName, "err", err)
			} else {
				label := scopeName
				if label == "" {
					label = scopeID
				}
				deleted = append(deleted, c.plugin+":"+label)
				slog.Info("scope-cleanup: deleted orphaned scope",
					"team", teamID, "plugin", c.plugin, "connection_id", c.connID,
					"scope_id", scopeID, "scope_name", scopeName)
			}
		}
	}

	// After scope cleanup, check for connections with zero remaining scopes
	// and no blueprint references — delete them and clear config keys.
	liveConnIDs := make(map[string]bool)
	for _, bp := range allBPs {
		for _, conn := range bp.Connections {
			liveConnIDs[conn.PluginName+":"+strconv.Itoa(conn.ConnectionID)] = true
		}
	}

	for _, c := range conns {
		key := c.plugin + ":" + strconv.Itoa(c.connID)
		if liveConnIDs[key] {
			continue
		}
		scopes, err := s.client.ListConnectionScopes(ctx, c.plugin, c.connID)
		if err != nil {
			errs = append(errs, fmt.Sprintf("re-check scopes (%s, conn %d): %v", c.plugin, c.connID, err))
			continue
		}
		if len(scopes) > 0 {
			continue
		}
		slog.Info("scope-cleanup: deleting unused connection",
			"team", teamID, "plugin", c.plugin, "connection_id", c.connID)
		if err := s.client.DeleteConnection(ctx, c.plugin, c.connID); err != nil {
			errs = append(errs, fmt.Sprintf("delete %s connection %d: %v", c.plugin, c.connID, err))
			slog.Error("scope-cleanup: failed to delete unused connection",
				"team", teamID, "plugin", c.plugin, "connection_id", c.connID, "err", err)
			continue
		}
		deleted = append(deleted, fmt.Sprintf("%s:connection-%d", c.plugin, c.connID))
		slog.Info("scope-cleanup: deleted unused connection",
			"team", teamID, "plugin", c.plugin, "connection_id", c.connID)

		configKey := "devlake_" + c.plugin + "_connection_id"
		if err := s.store.DeleteEntityConfigKey(ctx, "team", teamID, configKey); err != nil {
			slog.Error("scope-cleanup: failed to clear config key",
				"team", teamID, "key", configKey, "err", err)
		}
		slog.Info("scope-cleanup: cleared connection ID config key",
			"team", teamID, "plugin", c.plugin)
	}

	slog.Info("scope-cleanup: complete",
		"team", teamID, "deleted", len(deleted), "errors", len(errs))
	return deleted, errs
}

func teamUsesPlugins(sources []database.SourceRow, plugins map[string]bool) bool {
	for _, src := range sources {
		switch src.Type {
		case "gitrepos":
			for _, repo := range src.Repos {
				if plugins[repoTypeToPlugin(repo.Type)] {
					return true
				}
			}
		case "workitems":
			if plugins["jira"] {
				for _, proj := range src.Projects {
					if proj.Type == "jira" {
						return true
					}
				}
			}
		case SourceTypeTestSuites:
			if plugins[PluginTestRegistry] && len(src.Projects) > 0 {
				return true
			}
		}
	}
	return false
}

func (s *Service) EnsureJiraConnection(ctx context.Context, teamID string) error {
	connID, err := s.teamConnectionID(ctx, teamID, "jira")
	if err != nil {
		return err
	}
	if connID != 0 {
		_, err := s.client.GetConnection(ctx, "jira", connID)
		if err != nil {
			slog.Warn("EnsureJiraConnection: stored connection ID no longer exists in DevLake, clearing",
				"team", teamID, "connID", connID, "err", err)
			_ = s.store.DeleteEntityConfigKey(ctx, "team", teamID, "devlake_jira_connection_id")
		} else {
			slog.Info("EnsureJiraConnection: connection verified",
				"team", teamID, "connID", connID)
			return nil
		}
	}
	cfg, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
	if err != nil {
		return err
	}
	token := cfg["devlake_jira_token"]
	endpoint := cfg["devlake_jira_endpoint"]
	username := cfg["devlake_jira_username"]
	if token == "" || endpoint == "" || username == "" {
		slog.Warn("EnsureJiraConnection: no stored credentials to recreate connection",
			"team", teamID)
		return nil
	}
	slog.Info("EnsureJiraConnection: recreating connection from stored credentials",
		"team", teamID)
	newConnID, err := s.CreateTeamConnection(ctx, teamID, ConnectionEntry{
		Plugin:   "jira",
		Token:    token,
		Endpoint: endpoint,
		Username: username,
	})
	if err != nil {
		return err
	}
	slog.Info("EnsureJiraConnection: connection recreated",
		"team", teamID, "newConnID", newConnID)
	return nil
}

func (s *Service) CreateTeamConnection(ctx context.Context, teamID string, entry ConnectionEntry) (int, error) {
	sanitizeEntry(&entry)
	if entry.Plugin == "codecov" {
		if entry.Service == "" {
			entry.Service = "github"
		}
		if err := validateCodecovEntry(entry); err != nil {
			return 0, err
		}
	}
	if entry.Plugin == PluginTestRegistry {
		if err := validateTestRegistryEntry(entry); err != nil {
			return 0, err
		}
		cfg, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
		if err != nil {
			return 0, fmt.Errorf("read testregistry connection config: %w", err)
		}
		if err := checkTestRegistryImmutable(cfg, entry); err != nil {
			return 0, err
		}
	}
	connName := fmt.Sprintf("lfmbot-%s-%s", teamID, entry.Plugin)

	extra := map[string]string{}
	if entry.Username != "" {
		extra["username"] = entry.Username
	}
	if entry.Plugin == "codecov" {
		extra["organization"] = entry.Organization
		extra["service"] = entry.Service
	}
	if entry.Plugin == PluginTestRegistry {
		extra["organization"] = entry.Organization
		extra["ci_tool"] = entry.CITool
	}

	// Check for a leftover connection with the same name from a previous attempt.
	existing, err := s.client.ListConnections(ctx, entry.Plugin)
	if err == nil {
		for _, conn := range existing {
			if conn.Name == connName {
				slog.Info("reusing existing DevLake connection",
					"team", teamID, "plugin", entry.Plugin, "connection_id", conn.ID)
				fields := map[string]any{
					"endpoint": entry.Endpoint,
				}
				switch entry.Plugin {
				case "github":
					fields["name"] = conn.Name
					fields["authMethod"] = "AccessToken"
					fields["token"] = entry.Token
					fields["enableGraphql"] = true
				case "gitlab":
					fields["authMethod"] = "AccessToken"
					fields["token"] = entry.Token
				case "jira":
					fields["authMethod"] = "BasicAuth"
					fields["password"] = entry.Token
					fields["endpoint"] = normalizeJiraEndpoint(entry.Endpoint)
					if entry.Username != "" {
						fields["username"] = entry.Username
					}
				case "codecov":
					fields["token"] = entry.Token
					fields["organization"] = entry.Organization
					fields["service"] = entry.Service
				case PluginTestRegistry:
					delete(fields, "endpoint")
					fields["githubOrganization"] = ""
					fields["quayOrganization"] = ""
					for k, v := range testRegistryConnectionBody(entry) {
						fields[k] = v
					}
				default:
					fields["token"] = entry.Token
				}
				if patchErr := s.client.PatchConnection(ctx, entry.Plugin, conn.ID, fields); patchErr != nil {
					slog.Warn("failed to update existing connection, will try creating new",
						"plugin", entry.Plugin, "connection_id", conn.ID, "err", patchErr)
				} else {
					return s.storeConnectionConfig(ctx, teamID, entry, conn.ID)
				}
			}
		}
	}

	connID, err := s.client.CreateConnection(ctx, entry.Plugin, connName, entry.Endpoint, entry.Token, extra)
	if err != nil {
		return 0, err
	}

	return s.storeConnectionConfig(ctx, teamID, entry, connID)
}

func (s *Service) storeConnectionConfig(ctx context.Context, teamID string, entry ConnectionEntry, connID int) (int, error) {
	values := map[string]string{"devlake_" + entry.Plugin + "_connection_id": strconv.Itoa(connID)}
	if entry.Plugin == PluginTestRegistry {
		values["devlake_testregistry_ci_tool"] = entry.CITool
		values["devlake_testregistry_organization"] = entry.Organization
	}
	if entry.Token != "" && entry.Plugin != "codecov" {
		values["devlake_"+entry.Plugin+"_token"] = entry.Token
	}
	if entry.Endpoint != "" {
		values["devlake_"+entry.Plugin+"_endpoint"] = entry.Endpoint
	}
	if entry.Username != "" {
		values["devlake_"+entry.Plugin+"_username"] = entry.Username
	}
	if entry.Plugin == "codecov" {
		values["devlake_codecov_organization"] = entry.Organization
		values["devlake_codecov_service"] = entry.Service
	}
	if entry.ExpiryDate != "" {
		values["devlake_"+entry.Plugin+"_token_expiry"] = entry.ExpiryDate
	}
	if err := s.store.SetEntityConfigKeys(ctx, "team", teamID, values); err != nil {
		return 0, fmt.Errorf("store connection config: %w", err)
	}

	slog.Info("stored DevLake connection config", "team", teamID, "plugin", entry.Plugin, "connection_id", connID)
	return connID, nil
}

func (s *Service) teamConnectionPatchFields(ctx context.Context, connID int, entry ConnectionEntry) (map[string]any, error) {
	fields := map[string]any{}
	if entry.Endpoint != "" {
		fields["endpoint"] = entry.Endpoint
	}
	switch entry.Plugin {
	case "github":
		connection, err := s.client.GetConnection(ctx, entry.Plugin, connID)
		if err != nil {
			return nil, fmt.Errorf("read existing github connection: %w", err)
		}
		for _, key := range []string{"name", "endpoint"} {
			if key == "endpoint" && fields[key] != nil {
				continue
			}
			value, ok := connection[key].(string)
			if !ok || value == "" {
				return nil, fmt.Errorf("existing github connection %d has no %s", connID, key)
			}
			fields[key] = value
		}
		fields["authMethod"] = "AccessToken"
		fields["token"] = entry.Token
		fields["enableGraphql"] = true
	case "gitlab":
		fields["authMethod"] = "AccessToken"
		fields["token"] = entry.Token
	case "jira":
		fields["authMethod"] = "BasicAuth"
		fields["password"] = entry.Token
		if entry.Endpoint != "" {
			fields["endpoint"] = normalizeJiraEndpoint(entry.Endpoint)
		}
		if entry.Username != "" {
			fields["username"] = entry.Username
		}
	case "codecov":
		fields["token"] = entry.Token
		fields["organization"] = entry.Organization
		fields["service"] = entry.Service
	case PluginTestRegistry:
		fields["githubToken"] = entry.Token
		fields["project"] = testRegistryPlaceholderProject
	default:
		fields["token"] = entry.Token
	}
	return fields, nil
}

func (s *Service) UpdateTeamConnection(ctx context.Context, teamID string, entry ConnectionEntry) error {
	sanitizeEntry(&entry)
	if entry.Plugin == "codecov" {
		cfg, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
		if err != nil {
			return fmt.Errorf("read codecov connection config: %w", err)
		}
		if entry.Organization == "" {
			entry.Organization = cfg["devlake_codecov_organization"]
		}
		if entry.Service == "" {
			entry.Service = cfg["devlake_codecov_service"]
		}
		if entry.Service == "" {
			entry.Service = "github"
		}
		if err := validateCodecovEntry(entry); err != nil {
			return err
		}
	}
	if entry.Plugin == PluginTestRegistry {
		cfg, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
		if err != nil {
			return fmt.Errorf("read testregistry connection config: %w", err)
		}
		if err := checkTestRegistryImmutable(cfg, entry); err != nil {
			return err
		}
		entry.CITool = cfg["devlake_testregistry_ci_tool"]
		entry.Organization = cfg["devlake_testregistry_organization"]
		entry.Endpoint = ""
		if entry.CITool != CIToolOpenshiftCI {
			return fmt.Errorf("%w: only Openshift CI TestRegistry connections have a token to update", ErrInvalidConnection)
		}
	}
	connID, err := s.teamConnectionID(ctx, teamID, entry.Plugin)
	if err != nil {
		return fmt.Errorf("lookup connection: %w", err)
	}
	if connID == 0 {
		return fmt.Errorf("no %s connection for team %s", entry.Plugin, teamID)
	}

	fields, err := s.teamConnectionPatchFields(ctx, connID, entry)
	if err != nil {
		return err
	}

	if err := s.client.PatchConnection(ctx, entry.Plugin, connID, fields); err != nil {
		return err
	}

	values := make(map[string]string)
	if entry.Token != "" && entry.Plugin != "codecov" {
		values["devlake_"+entry.Plugin+"_token"] = entry.Token
	}
	if entry.Endpoint != "" {
		values["devlake_"+entry.Plugin+"_endpoint"] = entry.Endpoint
	}
	if entry.Username != "" {
		values["devlake_"+entry.Plugin+"_username"] = entry.Username
	}
	if entry.Plugin == "codecov" {
		values["devlake_codecov_organization"] = entry.Organization
		values["devlake_codecov_service"] = entry.Service
	}
	if entry.ExpiryDate != "" {
		values["devlake_"+entry.Plugin+"_token_expiry"] = entry.ExpiryDate
	}
	if err := s.store.SetEntityConfigKeys(ctx, "team", teamID, values); err != nil {
		return fmt.Errorf("store connection config: %w", err)
	}

	slog.Info("updated DevLake connection", "team", teamID, "plugin", entry.Plugin, "connection_id", connID)
	return nil
}

func (s *Service) GetTokenStatus(ctx context.Context, teamID string) ([]TokenStatus, error) {
	cfg, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
	if err != nil {
		return nil, err
	}

	var result []TokenStatus
	for _, plugin := range teamPlugins {
		connIDStr := cfg["devlake_"+plugin+"_connection_id"]
		if connIDStr == "" {
			continue
		}
		connID, _ := strconv.Atoi(connIDStr)
		result = append(result, TokenStatus{
			Plugin:     plugin,
			ConnID:     connID,
			ExpiryDate: cfg["devlake_"+plugin+"_token_expiry"],
		})
	}
	return result, nil
}

func (s *Service) ListExistingScopes(ctx context.Context, plugin, excludeTeamID string) ([]ExistingScope, error) {
	allBPs, err := s.client.ListBlueprints(ctx, "")
	if err != nil {
		return nil, err
	}

	excludeLabel := ""
	if excludeTeamID != "" {
		excludeLabel = "team:" + excludeTeamID
	}

	connScopes := make(map[int][]map[string]any)
	connNames := make(map[int]string)

	conns, err := s.client.ListConnections(ctx, plugin)
	if err != nil {
		slog.Warn("failed to list connections for existing scopes", "plugin", plugin, "err", err)
	} else {
		for _, conn := range conns {
			connNames[conn.ID] = conn.Name
		}
	}

	scopesInBlueprints := make(map[ScopeKey]string)
	for _, bp := range allBPs {
		if excludeLabel != "" && hasLabel(bp.Labels, excludeLabel) {
			continue
		}
		for _, conn := range bp.Connections {
			if conn.PluginName != plugin {
				continue
			}
			for _, scope := range conn.Scopes {
				key := ScopeKey{conn.PluginName, conn.ConnectionID, scope.ScopeID}
				scopesInBlueprints[key] = bp.Name
			}
			if _, loaded := connScopes[conn.ConnectionID]; !loaded {
				scopes, err := s.client.ListConnectionScopes(ctx, plugin, conn.ConnectionID)
				if err != nil {
					slog.Warn("failed to list scopes for connection", "plugin", plugin, "conn", conn.ConnectionID, "err", err)
					continue
				}
				connScopes[conn.ConnectionID] = scopes
			}
		}
	}

	var result []ExistingScope
	for key, bpName := range scopesInBlueprints {
		fullName := ""
		for _, scope := range connScopes[key.ConnectionID] {
			fn := scopeFullName(plugin, scope)
			sid := scopeIDFromMap(plugin, scope)
			if sid == key.ScopeID {
				fullName = fn
				break
			}
		}
		if fullName == "" {
			fullName = key.ScopeID
		}
		result = append(result, ExistingScope{
			FullName:       fullName,
			ConnectionName: connNames[key.ConnectionID],
			ConnectionID:   key.ConnectionID,
		})
		_ = bpName
	}

	return result, nil
}

func (s *Service) BrowseRemoteScopes(ctx context.Context, teamID, plugin, search string) ([]BrowseResult, error) {
	if plugin == "jira" {
		return s.browseJiraScopes(ctx, teamID, search)
	}

	connID, err := s.teamConnectionID(ctx, teamID, plugin)
	if err != nil {
		return nil, fmt.Errorf("get connection: %w", err)
	}
	if connID == 0 {
		return nil, fmt.Errorf("no %s connection configured for team %s", plugin, teamID)
	}

	var rawScopes []json.RawMessage
	if search != "" {
		rawScopes, err = s.client.SearchRemoteScopes(ctx, plugin, connID, search)
		if err != nil {
			return nil, err
		}
	} else {
		rawScopes, err = s.collectRemoteScopes(ctx, plugin, connID)
		if err != nil {
			return nil, err
		}
	}

	return s.parseBrowseResults(plugin, rawScopes), nil
}

func (s *Service) browseJiraScopes(ctx context.Context, teamID, search string) ([]BrowseResult, error) {
	cfg, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
	if err != nil {
		slog.Error("browseJiraScopes: config read failed", "team", teamID, "err", err)
		return nil, fmt.Errorf("browseJiraScopes: config read failed: %w", err)
	}
	token := cfg["devlake_jira_token"]
	endpoint := cfg["devlake_jira_endpoint"]
	username := cfg["devlake_jira_username"]
	if token == "" || endpoint == "" || username == "" {
		slog.Warn("browseJiraScopes: missing credentials",
			"team", teamID, "hasToken", token != "", "hasEndpoint", endpoint != "", "hasUsername", username != "")
		return nil, fmt.Errorf("browseJiraScopes: missing Jira credentials for team %s", teamID)
	}

	entry := ConnectionEntry{
		Plugin:   "jira",
		Endpoint: endpoint,
		Token:    token,
		Username: username,
	}
	sanitizeEntry(&entry)

	results, err := s.proxyBrowseJira(ctx, entry, search)
	if err != nil {
		return nil, err
	}

	out := make([]BrowseResult, 0, len(results))
	for _, r := range results {
		out = append(out, BrowseResult{
			Plugin:   "jira",
			FullName: "Board " + r.BoardID + " - " + r.Name,
			Owner:    r.Key,
			Name:     r.Name,
			BoardID:  r.BoardID,
		})
	}
	return out, nil
}

func (s *Service) BrowseRemoteScopeCount(ctx context.Context, teamID, plugin string) (int, error) {
	connID, err := s.teamConnectionID(ctx, teamID, plugin)
	if err != nil {
		return 0, fmt.Errorf("get connection: %w", err)
	}
	if connID == 0 {
		return 0, nil
	}
	scopes, err := s.collectRemoteScopes(ctx, plugin, connID)
	if err != nil {
		return 0, err
	}
	return len(scopes), nil
}

func (s *Service) collectRemoteScopes(ctx context.Context, plugin string, connID int) ([]json.RawMessage, error) {
	topLevel, err := s.client.ListRemoteScopes(ctx, plugin, connID, "")
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	addScope := func(data json.RawMessage) json.RawMessage {
		id := scopeIDFromRaw(plugin, data)
		if id != "" && seen[id] {
			return nil
		}
		if id != "" {
			seen[id] = true
		}
		return data
	}

	var out []json.RawMessage
	var groups []string
	for _, node := range topLevel {
		if node.Type == "scope" && node.Data != nil && len(node.Data) > 2 {
			if d := addScope(node.Data); d != nil {
				out = append(out, d)
			}
		} else if node.Type == "group" {
			groups = append(groups, node.ID)
		}
	}

	for _, gid := range groups {
		children, err := s.client.ListRemoteScopes(ctx, plugin, connID, gid)
		if err != nil {
			slog.Warn("browse: failed to list group children", "plugin", plugin, "group", gid, "err", err)
			continue
		}
		for _, node := range children {
			if len(node.Data) > 2 {
				if d := addScope(node.Data); d != nil {
					out = append(out, d)
				}
			}
		}
	}

	return out, nil
}

func scopeIDFromRaw(plugin string, data json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return scopeIDFromMap(plugin, m)
}

func (s *Service) parseBrowseResults(plugin string, rawScopes []json.RawMessage) []BrowseResult {
	var out []BrowseResult
	for _, raw := range rawScopes {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		br := BrowseResult{Plugin: plugin}
		switch plugin {
		case "github":
			if fn, ok := m["fullName"].(string); ok {
				br.FullName = fn
				parts := strings.SplitN(fn, "/", 2)
				if len(parts) == 2 {
					br.Owner = parts[0]
					br.Name = parts[1]
				}
			}
		case "gitlab":
			if pn, ok := m["pathWithNamespace"].(string); ok {
				br.FullName = pn
				parts := strings.Split(pn, "/")
				if len(parts) >= 2 {
					br.Name = parts[len(parts)-1]
					br.Owner = strings.Join(parts[:len(parts)-1], "/")
				}
			}
		case "jira":
			name, _ := m["name"].(string)
			key, _ := m["projectKey"].(string)
			boardID := scopeIDFromMap("jira", m)
			br.Name = name
			if key != "" {
				br.FullName = key + " - " + name
				br.Owner = key
			} else {
				br.FullName = name
			}
			if boardID != "" {
				br.BoardID = boardID
			}
		case PluginTestRegistry:
			br.FullName, _ = m["fullName"].(string)
			br.Name, _ = m["name"].(string)
		}
		if br.FullName != "" {
			out = append(out, br)
		}
	}
	return out
}

func (s *Service) TeamConnectionID(ctx context.Context, teamID, plugin string) (int, error) {
	return s.teamConnectionID(ctx, teamID, plugin)
}

func (s *Service) teamConnectionID(ctx context.Context, teamID, plugin string) (int, error) {
	cfg, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
	if err != nil {
		return 0, err
	}
	key := "devlake_" + plugin + "_connection_id"
	v, ok := cfg[key]
	if !ok || v == "" {
		return 0, nil
	}
	id, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid connection ID for %s: %q", plugin, v)
	}
	return id, nil
}

func (s *Service) buildDesiredState(ctx context.Context, teamID string, result *SyncResult) (map[string]*desiredBlueprint, error) {
	sources, err := s.store.ListSourceRows(ctx, teamID)
	if err != nil {
		return nil, err
	}
	config, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
	if err != nil {
		return nil, err
	}
	codecovConnID, err := s.teamConnectionID(ctx, teamID, "codecov")
	if err != nil {
		return nil, fmt.Errorf("lookup codecov connection: %w", err)
	}
	codecovOrg := config["devlake_codecov_organization"]
	codecovService := config["devlake_codecov_service"]
	if codecovConnID != 0 && codecovOrg == "" {
		result.Errors = append(result.Errors, "Codecov connection is missing an organization. Update it in team settings.")
	}

	desired := make(map[string]*desiredBlueprint)
	for _, src := range sources {
		switch src.Type {
		case "gitrepos":
			for _, repo := range src.Repos {
				codecovCoversRepo := codecovConnID != 0 && codecovOrg != "" && CodecovMatchesRepo(codecovService, codecovOrg, repo)
				if codecovCoversRepo {
					bpName := blueprintName(teamID, "codecov", codecovOrg)
					bp, ok := desired[bpName]
					if !ok {
						bp = &desiredBlueprint{name: bpName, plugin: "codecov", connID: codecovConnID}
						desired[bpName] = bp
					}
					bp.repoSpecs = append(bp.repoSpecs, repoSpec{owner: codecovOrg, name: repo.Name})
				}
				plugin := repoTypeToPlugin(repo.Type)
				if plugin == "" {
					continue
				}
				connID, err := s.teamConnectionID(ctx, teamID, plugin)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("lookup %s connection: %v", plugin, err))
					continue
				}
				if connID == 0 {
					if !codecovCoversRepo {
						result.Errors = append(result.Errors, fmt.Sprintf("No DevLake connection for %s. Add one in team settings.", plugin))
					}
					continue
				}
				bpName := blueprintName(teamID, plugin, repo.Owner)
				bp, ok := desired[bpName]
				if !ok {
					bp = &desiredBlueprint{
						name:   bpName,
						plugin: plugin,
						connID: connID,
					}
					desired[bpName] = bp
				}
				bp.repoSpecs = append(bp.repoSpecs, repoSpec{
					owner: repo.Owner,
					name:  repo.Name,
				})
			}
		case "workitems":
			for _, proj := range src.Projects {
				slog.Info("buildDesiredState: processing workitem project",
					"team", teamID, "projID", proj.ID, "type", proj.Type,
					"projectName", proj.ProjectName, "jiraProject", proj.JiraProject,
					"boardID", proj.BoardID)
				if proj.Type != "jira" {
					slog.Info("buildDesiredState: skipping non-jira project", "team", teamID, "type", proj.Type)
					continue
				}
				connID, err := s.teamConnectionID(ctx, teamID, "jira")
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("lookup jira connection: %v", err))
					slog.Error("buildDesiredState: jira connection lookup failed", "team", teamID, "err", err)
					continue
				}
				if connID == 0 {
					result.Errors = append(result.Errors, "No DevLake connection for jira. Add one in team settings.")
					slog.Warn("buildDesiredState: no jira connection configured", "team", teamID)
					continue
				}
				boardID := proj.BoardID
				projectKey := proj.JiraProject
				projectName := proj.ProjectName
				if boardID == "" && projectKey == "" && projectName == "" {
					result.Errors = append(result.Errors, "jira project has no board ID, key, or name, skipping")
					slog.Warn("buildDesiredState: jira project has no identifiers", "team", teamID, "projID", proj.ID)
					continue
				}
				bpKey := boardID
				if bpKey == "" {
					bpKey = projectKey
				}
				if bpKey == "" {
					bpKey = projectName
				}
				bpName := blueprintName(teamID, "jira", bpKey)
				slog.Info("buildDesiredState: jira desired blueprint",
					"team", teamID, "bpName", bpName, "connID", connID,
					"boardID", boardID, "projectKey", projectKey, "projectName", projectName)
				if _, ok := desired[bpName]; !ok {
					desired[bpName] = &desiredBlueprint{
						name:   bpName,
						plugin: "jira",
						connID: connID,
						repoSpecs: []repoSpec{{
							owner:   projectKey,
							name:    projectName,
							boardID: boardID,
						}},
					}
				}
			}
		case SourceTypeTestSuites:
			s.addTestRegistryDesired(ctx, teamID, config, src, desired, result)
		}
	}
	return desired, nil
}

func CodecovMatchesRepo(service, organization string, repo database.RepoRow) bool {
	plugin := repoTypeToPlugin(repo.Type)
	switch service {
	case "", "github", "github_enterprise":
		return plugin == "github" && strings.EqualFold(repo.Owner, organization)
	case "gitlab", "gitlab_enterprise":
		return plugin == "gitlab" && strings.EqualFold(repo.Owner, organization)
	default:
		return false
	}
}

func (s *Service) resolveScopes(ctx context.Context, teamID string, desired map[string]*desiredBlueprint, result *SyncResult) {
	for _, bp := range desired {
		if bp.plugin == PluginTestRegistry {
			resolveTestRegistryScopes(bp, result)
			continue
		}
		var resolved []repoSpec
		for _, spec := range bp.repoSpecs {
			fullName := spec.owner + "/" + spec.name

			var matched json.RawMessage
			var searchResults []json.RawMessage

			if bp.plugin == "jira" && spec.boardID != "" {
				slog.Info("resolveScopes: jira board ID provided, using directly",
					"connID", bp.connID, "boardID", spec.boardID)
				boardIDNum, err := strconv.Atoi(spec.boardID)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("resolve %s: invalid board ID %q", fullName, spec.boardID))
					continue
				}
				boardName := spec.name
				if boardName == "" {
					boardName = fmt.Sprintf("Board %s", spec.boardID)
				}
				raw, err := json.Marshal(map[string]any{
					"boardId":      boardIDNum,
					"name":         boardName,
					"connectionId": bp.connID,
				})
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("resolve %s: %v", fullName, err))
					continue
				}
				spec.scopeID = spec.boardID
				spec.rawData = raw
				resolved = append(resolved, spec)
				result.Resolved = append(result.Resolved, fullName)
				slog.Info("scope resolved via board ID",
					"plugin", bp.plugin, "connID", bp.connID, "boardID", spec.boardID,
					"name", boardName)
				continue
			} else if bp.plugin == "jira" {
				var resolveErr string
				matched, resolveErr = s.resolveJiraScope(ctx, teamID, bp.connID, spec, fullName)
				if resolveErr != "" {
					result.Errors = append(result.Errors, fmt.Sprintf("resolve %s: %s", fullName, resolveErr))
					continue
				}
			} else {
				searchTerm := fullName
				if bp.plugin == "gitlab" || bp.plugin == "codecov" {
					searchTerm = spec.name
				}
				var err error
				searchResults, err = s.client.SearchRemoteScopes(ctx, bp.plugin, bp.connID, searchTerm)
				if err != nil {
					slog.Warn("scope resolution via DevLake failed, will try direct API fallback",
						"plugin", bp.plugin, "search", searchTerm, "err", err)
				} else {
					matched = matchScope(bp.plugin, searchResults, fullName)
				}
			}

			if matched == nil && (bp.plugin == "github" || bp.plugin == "gitlab") {
				slog.Info("search returned no match, trying direct API lookup (fork fallback)",
					"plugin", bp.plugin, "scope", fullName)
				lookup := s.LookupRepoForTeam(ctx, teamID, bp.plugin, spec.owner, spec.name)
				if lookup.Valid && lookup.RawData != nil {
					scopeData, err := s.buildScopeFromLookup(bp.plugin, lookup.RawData)
					if err == nil {
						matched = scopeData
						slog.Info("fork fallback resolved scope via direct API",
							"plugin", bp.plugin, "scope", fullName)
					} else {
						slog.Warn("fork fallback: failed to build scope data",
							"plugin", bp.plugin, "scope", fullName, "err", err)
					}
				} else if lookup.Error != "" {
					slog.Warn("fork fallback: lookup failed",
						"plugin", bp.plugin, "scope", fullName, "err", lookup.Error)
				}
			}

			if matched == nil {
				result.Errors = append(result.Errors, fmt.Sprintf("resolve %s: no results found", fullName))
				result.Failed = append(result.Failed, fullName)
				attrs := []any{"plugin", bp.plugin, "scope", fullName, "searchResults", len(searchResults)}
				if len(searchResults) > 0 {
					var first map[string]any
					if err := json.Unmarshal(searchResults[0], &first); err == nil {
						if fn, ok := first["fullName"].(string); ok {
							attrs = append(attrs, "firstResult", fn)
						} else if pn, ok := first["pathWithNamespace"].(string); ok {
							attrs = append(attrs, "firstResult", pn)
						}
					}
				}
				slog.Warn("scope not found", attrs...)
				continue
			}

			idField := scopeIDField(bp.plugin)
			slog.Info("resolveScopes: extracting scope ID",
				"plugin", bp.plugin, "idField", idField, "matchedData", string(matched))
			var scopeID string
			var err error
			if bp.plugin == "codecov" {
				scopeID = scopeIDFromRaw(bp.plugin, matched)
			} else {
				scopeID, err = extractNumericID(matched, idField)
			}
			if err != nil || scopeID == "" {
				if err == nil {
					err = fmt.Errorf("field %q not found in scope data", idField)
				}
				result.Errors = append(result.Errors, fmt.Sprintf("resolve %s: %v", fullName, err))
				slog.Error("resolveScopes: extractNumericID failed",
					"plugin", bp.plugin, "scope", fullName, "idField", idField, "err", err,
					"rawData", string(matched))
				continue
			}

			spec.scopeID = scopeID
			spec.rawData = matched
			resolved = append(resolved, spec)
			result.Resolved = append(result.Resolved, fullName)
			slog.Info("scope resolved", "plugin", bp.plugin, "connID", bp.connID, "scope", fullName, "scopeID", scopeID)
		}
		bp.repoSpecs = resolved
	}
}

func (s *Service) resolveJiraScope(ctx context.Context, teamID string, connID int, spec repoSpec, fullName string) (json.RawMessage, string) {
	slog.Info("resolveJiraScope: starting",
		"connID", connID, "spec.owner", spec.owner, "spec.name", spec.name, "fullName", fullName)

	projectKey := spec.owner
	if projectKey == "" && spec.name != "" {
		slog.Info("resolveJiraScope: no project key, resolving from name",
			"connID", connID, "projectName", spec.name)
		projectKey = s.resolveJiraProjectKey(ctx, connID, spec.name)
		if projectKey != "" {
			slog.Info("resolveJiraScope: resolved jira project key from name",
				"name", spec.name, "key", projectKey)
		} else {
			slog.Warn("resolveJiraScope: could not resolve project key from name",
				"name", spec.name, "connID", connID)
		}
	}

	targetProjectID := 0
	if projectKey != "" {
		targetProjectID = s.resolveJiraNumericProjectID(ctx, teamID, projectKey)
		slog.Info("resolveJiraScope: numeric project ID",
			"projectKey", projectKey, "targetProjectID", targetProjectID)
	}

	var matched json.RawMessage
	if projectKey != "" {
		slog.Info("resolveJiraScope: listing boards for project",
			"connID", connID, "projectKey", projectKey)
		boards, err := s.client.ListRemoteScopes(ctx, "jira", connID, projectKey)
		if err != nil {
			slog.Warn("resolveJiraScope: jira remote-scopes list failed",
				"project", projectKey, "connID", connID, "err", err)
		} else {
			slog.Info("resolveJiraScope: boards returned from remote-scopes",
				"projectKey", projectKey, "connID", connID, "boardCount", len(boards))
			matched = s.matchJiraBoard(boards, spec.name, targetProjectID)
			if matched != nil {
				slog.Info("resolveJiraScope: matchJiraBoard returned a match",
					"projectKey", projectKey, "matchedData", string(matched))
			} else {
				slog.Warn("resolveJiraScope: matchJiraBoard returned nil",
					"projectKey", projectKey, "spec.name", spec.name)
			}
		}
	} else {
		slog.Warn("resolveJiraScope: no project key available, skipping remote-scopes list",
			"spec.owner", spec.owner, "spec.name", spec.name)
	}

	if matched == nil && spec.name != "" {
		slog.Info("resolveJiraScope: trying search-remote-scopes fallback",
			"connID", connID, "search", spec.name)
		searchResults, err := s.client.SearchRemoteScopes(ctx, "jira", connID, spec.name)
		if err != nil {
			slog.Warn("resolveJiraScope: search-remote-scopes failed",
				"plugin", "jira", "search", spec.name, "err", err)
			return nil, err.Error()
		}
		slog.Info("resolveJiraScope: search-remote-scopes results",
			"search", spec.name, "resultCount", len(searchResults))
		matched = s.matchJiraSearchResult(searchResults, targetProjectID)
		if matched != nil {
			slog.Info("resolveJiraScope: search matched",
				"fullName", fullName, "matchedData", string(matched))
		} else {
			slog.Warn("resolveJiraScope: search returned no usable match",
				"fullName", fullName, "resultCount", len(searchResults))
		}
	}

	return matched, ""
}

func (s *Service) resolveJiraNumericProjectID(ctx context.Context, teamID, projectKey string) int {
	cfg, err := s.store.GetEntityConfigKV(ctx, "team", teamID)
	if err != nil {
		slog.Warn("resolveJiraNumericProjectID: config read failed", "team", teamID, "err", err)
		return 0
	}
	token := cfg["devlake_jira_token"]
	endpoint := cfg["devlake_jira_endpoint"]
	username := cfg["devlake_jira_username"]
	if token == "" || endpoint == "" || username == "" {
		slog.Warn("resolveJiraNumericProjectID: missing credentials", "team", teamID)
		return 0
	}

	endpoint = strings.TrimSuffix(strings.TrimRight(endpoint, "/"), "/rest")
	reqURL := fmt.Sprintf("%s/rest/api/2/project/%s", endpoint, url.PathEscape(projectKey))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		slog.Warn("resolveJiraNumericProjectID: request creation failed", "err", err)
		return 0
	}
	req.SetBasicAuth(username, token)

	resp, err := s.httpClient().Do(req)
	if err != nil {
		slog.Warn("resolveJiraNumericProjectID: request failed", "url", reqURL, "err", err)
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("resolveJiraNumericProjectID: unexpected status", "status", resp.StatusCode, "projectKey", projectKey)
		return 0
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return 0
	}

	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &project); err != nil {
		slog.Warn("resolveJiraNumericProjectID: parse failed", "err", err)
		return 0
	}

	id, err := strconv.Atoi(project.ID)
	if err != nil {
		slog.Warn("resolveJiraNumericProjectID: non-numeric ID", "id", project.ID)
		return 0
	}
	slog.Info("resolveJiraNumericProjectID: resolved", "projectKey", projectKey, "projectID", id)
	return id
}

func (s *Service) resolveJiraProjectKey(ctx context.Context, connID int, projectName string) string {
	slog.Info("resolveJiraProjectKey: listing all projects", "connID", connID, "projectName", projectName)
	topLevel, err := s.client.ListRemoteScopes(ctx, "jira", connID, "")
	if err != nil {
		slog.Warn("resolveJiraProjectKey: failed to list projects", "connID", connID, "err", err)
		return ""
	}
	slog.Info("resolveJiraProjectKey: got top-level entries", "connID", connID, "count", len(topLevel))
	lowerName := strings.ToLower(projectName)
	for _, node := range topLevel {
		slog.Debug("resolveJiraProjectKey: entry",
			"type", node.Type, "id", node.ID, "name", node.Name)
		if node.Type != "group" {
			continue
		}
		if strings.ToLower(node.Name) == lowerName {
			slog.Info("resolveJiraProjectKey: matched",
				"projectName", projectName, "key", node.ID)
			return node.ID
		}
	}
	slog.Warn("resolveJiraProjectKey: no match found",
		"projectName", projectName, "connID", connID, "groupCount", len(topLevel))
	return ""
}

func (s *Service) matchJiraBoard(boards []remoteScopeNode, projectName string, targetProjectID int) json.RawMessage {
	slog.Info("matchJiraBoard: filtering scope nodes",
		"totalNodes", len(boards), "projectName", projectName, "targetProjectID", targetProjectID)

	var scopeNodes []remoteScopeNode
	for _, node := range boards {
		if node.Type != "scope" || len(node.Data) <= 2 {
			continue
		}
		scopeNodes = append(scopeNodes, node)
	}
	if len(scopeNodes) == 0 {
		slog.Warn("matchJiraBoard: no scope nodes found in boards")
		return nil
	}

	if targetProjectID > 0 {
		var filtered []remoteScopeNode
		for _, node := range scopeNodes {
			var m map[string]any
			if err := json.Unmarshal(node.Data, &m); err != nil {
				continue
			}
			pid, _ := m["projectId"].(float64)
			if int(pid) == targetProjectID {
				filtered = append(filtered, node)
			}
		}
		slog.Info("matchJiraBoard: filtered by projectId",
			"targetProjectID", targetProjectID, "before", len(scopeNodes), "after", len(filtered))
		scopeNodes = filtered
	}

	if len(scopeNodes) == 0 {
		slog.Warn("matchJiraBoard: no boards match target projectId", "targetProjectID", targetProjectID)
		return nil
	}
	if len(scopeNodes) == 1 {
		slog.Info("matchJiraBoard: single board after filtering, selecting it",
			"id", scopeNodes[0].ID, "name", scopeNodes[0].Name)
		return scopeNodes[0].Data
	}

	if projectName != "" {
		lowerName := strings.ToLower(projectName)
		for _, node := range scopeNodes {
			if strings.Contains(strings.ToLower(node.Name), lowerName) {
				slog.Info("matchJiraBoard: matched by name",
					"id", node.ID, "name", node.Name, "searchName", projectName)
				return node.Data
			}
		}
	}

	if targetProjectID > 0 {
		slog.Info("matchJiraBoard: no name match, returning first board filtered by projectId",
			"id", scopeNodes[0].ID, "name", scopeNodes[0].Name)
		return scopeNodes[0].Data
	}

	slog.Warn("matchJiraBoard: no projectId filter and no name match, returning nil to avoid wrong board",
		"boardCount", len(scopeNodes), "projectName", projectName)
	return nil
}

func (s *Service) matchJiraSearchResult(results []json.RawMessage, targetProjectID int) json.RawMessage {
	if len(results) == 0 {
		return nil
	}
	if targetProjectID > 0 {
		for _, raw := range results {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				continue
			}
			pid, _ := m["projectId"].(float64)
			if int(pid) == targetProjectID {
				slog.Info("matchJiraSearchResult: matched by projectId",
					"targetProjectID", targetProjectID, "data", string(raw))
				return raw
			}
		}
		slog.Warn("matchJiraSearchResult: no search result matches projectId", "targetProjectID", targetProjectID)
		return nil
	}
	if len(results) == 1 {
		slog.Info("matchJiraSearchResult: single result, using it")
		return results[0]
	}
	slog.Warn("matchJiraSearchResult: multiple results and no projectId filter, returning nil",
		"count", len(results))
	return nil
}

func matchScope(plugin string, results []json.RawMessage, fullName string) json.RawMessage {
	if len(results) == 0 {
		return nil
	}

	nameField := "fullName"
	switch plugin {
	case "gitlab":
		nameField = "pathWithNamespace"
	case "codecov":
		nameField = "codecovId"
	}

	for _, raw := range results {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if val, _ := m[nameField].(string); val == fullName {
			return raw
		}
	}
	return nil
}

func (s *Service) registerScopes(ctx context.Context, desired map[string]*desiredBlueprint, result *SyncResult) {
	type regKey struct {
		plugin string
		connID int
	}
	grouped := make(map[regKey][]json.RawMessage)
	for _, bp := range desired {
		k := regKey{bp.plugin, bp.connID}
		for _, spec := range bp.repoSpecs {
			if spec.rawData != nil {
				grouped[k] = append(grouped[k], spec.rawData)
				slog.Info("registerScopes: queuing scope",
					"plugin", bp.plugin, "connID", bp.connID,
					"scopeID", spec.scopeID, "data", string(spec.rawData))
			} else {
				slog.Warn("registerScopes: spec has nil rawData, skipping",
					"plugin", bp.plugin, "connID", bp.connID,
					"owner", spec.owner, "name", spec.name)
			}
		}
	}
	if len(grouped) == 0 {
		slog.Info("registerScopes: nothing to register")
	}
	for k, data := range grouped {
		slog.Info("registerScopes: sending PUT request",
			"plugin", k.plugin, "connID", k.connID, "scopeCount", len(data))
		if err := s.client.RegisterScopes(ctx, k.plugin, k.connID, data); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("register scopes (%s): %v", k.plugin, err))
			slog.Warn("registerScopes: failed", "plugin", k.plugin, "connID", k.connID, "count", len(data), "err", err)
		} else {
			slog.Info("registerScopes: success", "plugin", k.plugin, "connID", k.connID, "count", len(data))
		}
	}
}

func (s *Service) findOrCreateScopeConfig(ctx context.Context, plugin string, connID int, name string, body map[string]any) (int, error) {
	configID, err := s.client.CreateScopeConfig(ctx, plugin, connID, body)
	if err == nil {
		return configID, nil
	}
	slog.Info("scope config creation failed, searching for existing config by name",
		"plugin", plugin, "connID", connID, "name", name, "createErr", err)
	configs, listErr := s.client.ListScopeConfigs(ctx, plugin, connID)
	if listErr != nil {
		return 0, fmt.Errorf("create failed: %w; list fallback also failed: %w", err, listErr)
	}
	for _, cfg := range configs {
		if n, _ := cfg["name"].(string); n == name {
			if id, ok := cfg["id"].(float64); ok && id > 0 {
				slog.Info("found existing scope config by name",
					"plugin", plugin, "connID", connID, "name", name, "configID", int(id))
				return int(id), nil
			}
		}
	}
	return 0, fmt.Errorf("create failed and no existing config found with name %q: %w", name, err)
}

func (s *Service) ensureJiraScopeConfigs(ctx context.Context, teamID string, desired map[string]*desiredBlueprint) {
	for _, bp := range desired {
		if bp.plugin != "jira" {
			continue
		}
		scopes, err := s.client.ListConnectionScopes(ctx, "jira", bp.connID)
		if err != nil {
			slog.Warn("ensureJiraScopeConfigs: failed to list scopes",
				"team", teamID, "connID", bp.connID, "err", err)
			continue
		}
		for _, scope := range scopes {
			scopeID := scopeIDFromMap("jira", scope)
			if scopeID == "" {
				continue
			}
			if cfgID, _ := scope["scopeConfigId"].(float64); cfgID > 0 {
				continue
			}
			configName := fmt.Sprintf("%s-conn%d-board-%s", teamID, bp.connID, scopeID)
			body := map[string]any{
				"name":     configName,
				"entities": []string{"TICKET"},
			}
			configID, err := s.findOrCreateScopeConfig(ctx, "jira", bp.connID, configName, body)
			if err != nil {
				slog.Warn("ensureJiraScopeConfigs: failed to get config",
					"team", teamID, "scopeID", scopeID, "err", err)
				continue
			}
			if err := s.client.PatchScope(ctx, "jira", bp.connID, scopeID, configID); err != nil {
				slog.Warn("ensureJiraScopeConfigs: failed to attach config",
					"team", teamID, "scopeID", scopeID, "configID", configID, "err", err)
				continue
			}
			slog.Info("ensureJiraScopeConfigs: attached default config",
				"team", teamID, "scopeID", scopeID, "configID", configID)
		}
	}
}

func (s *Service) dedup(ctx context.Context, teamID string, desired map[string]*desiredBlueprint, result *SyncResult) error {
	allBPs, err := s.client.ListBlueprints(ctx, "")
	if err != nil {
		return err
	}

	endpoints, err := s.connectionEndpointKeys(ctx, allBPs, desired)
	if err != nil {
		return err
	}

	teamLabel := "team:" + teamID
	globalScopes := make(map[scopeDedupKey]scopeOwner)
	for _, bp := range allBPs {
		if hasLabel(bp.Labels, teamLabel) && hasLabel(bp.Labels, "managed:lfmbot") {
			continue
		}
		owner := scopeOwner{blueprint: bp.Name, team: teamIDFromLabels(bp.Labels)}
		for _, conn := range bp.Connections {
			endpoint := endpointForKey(endpoints, conn.PluginName, conn.ConnectionID)
			for _, scope := range conn.Scopes {
				globalScopes[scopeDedupKey{
					pluginName: conn.PluginName,
					endpoint:   endpoint,
					scopeID:    scope.ScopeID,
				}] = owner
			}
		}
	}

	for _, bp := range desired {
		var kept []repoSpec
		for _, spec := range bp.repoSpecs {
			key := scopeDedupKey{
				pluginName: bp.plugin,
				endpoint:   endpointForKey(endpoints, bp.plugin, bp.connID),
				scopeID:    spec.scopeID,
			}
			if owner, exists := globalScopes[key]; exists {
				label := spec.owner + "/" + spec.name
				if bp.plugin == PluginTestRegistry {
					label = spec.name
				}
				msg := fmt.Sprintf("%s already collected by %s; existing data will be used for this team's metrics",
					label, owner.describe())
				result.Skipped = append(result.Skipped, msg)
				slog.Info("scope dedup: skipping",
					"scope", label, "existing_blueprint", owner.blueprint,
					"owner_team", owner.team, "plugin", bp.plugin, "endpoint", key.endpoint)
				continue
			}
			kept = append(kept, spec)
		}
		bp.repoSpecs = kept
	}
	return nil
}

// connectionEndpointKeys resolves, per plugin, the provider endpoint of every
// connection referenced by the given blueprints or desired state. Dedup keys
// scopes by endpoint rather than connection ID so the same repo/project/board
// onboarded through two connections to the same provider instance still
// collides, while two distinct provider instances never do.
func (s *Service) connectionEndpointKeys(ctx context.Context, allBPs []Blueprint, desired map[string]*desiredBlueprint) (map[string]map[int]string, error) {
	plugins := make(map[string]bool)
	for _, bp := range allBPs {
		for _, conn := range bp.Connections {
			if conn.PluginName != "" {
				plugins[conn.PluginName] = true
			}
		}
	}
	for _, bp := range desired {
		if bp.plugin != "" {
			plugins[bp.plugin] = true
		}
	}
	endpoints := make(map[string]map[int]string, len(plugins))
	for plugin := range plugins {
		conns, err := s.client.ListConnections(ctx, plugin)
		if err != nil {
			return nil, fmt.Errorf("list %s connections: %w", plugin, err)
		}
		keys := make(map[int]string, len(conns))
		for _, conn := range conns {
			if plugin == PluginTestRegistry {
				keys[conn.ID] = testRegistryEndpointKey(conn)
				continue
			}
			keys[conn.ID] = normalizeScopeEndpoint(conn.Endpoint)
		}
		endpoints[plugin] = keys
	}
	return endpoints, nil
}

func endpointForKey(endpoints map[string]map[int]string, plugin string, connID int) string {
	if key := endpoints[plugin][connID]; key != "" {
		return key
	}
	// No endpoint metadata (plugin does not expose one, or the connection is
	// no longer listed) — fall back to a per-connection key so distinct
	// connections never falsely collide.
	return fmt.Sprintf("conn:%d", connID)
}

// normalizeScopeEndpoint canonicalizes a provider endpoint for use as a
// dedup key by reducing it to the host plus port.
func normalizeScopeEndpoint(endpoint string) string {
	e := strings.ToLower(strings.TrimSpace(endpoint))
	if !strings.Contains(e, "://") && !strings.HasPrefix(e, "//") {
		e = "//" + e
	}
	u, err := url.Parse(e)
	if err != nil || u.Host == "" {
		return e
	}
	return u.Host
}

func teamIDFromLabels(labels []string) string {
	for _, label := range labels {
		if teamID, ok := strings.CutPrefix(label, "team:"); ok {
			return teamID
		}
	}
	return ""
}

func (s *Service) reconcile(ctx context.Context, teamID string, desired map[string]*desiredBlueprint, result *SyncResult) error {
	teamLabel := "team:" + teamID

	for _, dbp := range desired {
		if dbp.plugin != "jira" {
			continue
		}
		scopes, err := s.client.ListConnectionScopes(ctx, "jira", dbp.connID)
		if err != nil {
			slog.Warn("reconcile-debug: failed to list jira scopes", "connID", dbp.connID, "err", err)
		} else {
			for _, scope := range scopes {
				scopeID := scopeIDFromMap("jira", scope)
				cfgID, _ := scope["scopeConfigId"].(float64)
				scopeName, _ := scope["name"].(string)
				slog.Info("reconcile-debug: jira scope on connection",
					"connID", dbp.connID, "scopeID", scopeID, "name", scopeName,
					"scopeConfigId", int(cfgID))
			}
		}
		configs, err := s.client.ListScopeConfigs(ctx, "jira", dbp.connID)
		if err != nil {
			slog.Warn("reconcile-debug: failed to list jira scope configs", "connID", dbp.connID, "err", err)
		} else {
			slog.Info("reconcile-debug: jira scope configs on connection",
				"connID", dbp.connID, "count", len(configs))
			for _, cfg := range configs {
				cfgID, _ := cfg["id"].(float64)
				cfgName, _ := cfg["name"].(string)
				slog.Info("reconcile-debug: scope config",
					"connID", dbp.connID, "id", int(cfgID), "name", cfgName)
			}
		}
	}

	existing, err := s.client.ListBlueprints(ctx, teamLabel)
	if err != nil {
		return err
	}

	existingByName := make(map[string]Blueprint)
	desiredNames := make(map[string]bool)
	for name := range desired {
		desiredNames[name] = true
	}
	for _, bp := range existing {
		if !hasLabel(bp.Labels, "managed:lfmbot") {
			continue
		}
		if desiredNames[bp.Name] {
			existingByName[bp.Name] = bp
		} else {
			for dName := range desiredNames {
				if strings.HasPrefix(bp.Name, dName+"-v") {
					slog.Info("reconcile: matched renamed blueprint",
						"desired", dName, "actual", bp.Name, "blueprintID", bp.ID)
					existingByName[dName] = bp
					break
				}
			}
			if _, matched := existingByName[bp.Name]; !matched {
				existingByName[bp.Name] = bp
			}
		}
	}

	labels := []string{"managed:lfmbot", teamLabel}

	slog.Info("reconcile: starting",
		"team", teamID, "desiredCount", len(desired), "existingCount", len(existing))
	for name, dbp := range desired {
		slog.Info("reconcile: processing desired blueprint",
			"name", name, "plugin", dbp.plugin, "connID", dbp.connID,
			"repoSpecCount", len(dbp.repoSpecs))
		for i, spec := range dbp.repoSpecs {
			slog.Info("reconcile: repoSpec detail",
				"name", name, "index", i,
				"owner", spec.owner, "specName", spec.name,
				"scopeID", spec.scopeID, "hasRawData", spec.rawData != nil)
		}
		if len(dbp.repoSpecs) == 0 {
			slog.Info("reconcile: desired blueprint has 0 repoSpecs, checking for deletion",
				"name", name)
			if ebp, ok := existingByName[name]; ok {
				if err := s.client.DeleteBlueprint(ctx, ebp.ID); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("delete %q: %v", name, err))
					slog.Warn("blueprint deletion failed", "name", name, "blueprintID", ebp.ID, "err", err)
				} else {
					result.Deleted = append(result.Deleted, name)
					slog.Info("blueprint deleted (empty)", "name", name, "blueprintID", ebp.ID)
				}
			}
			continue
		}

		scopes := make([]Scope, len(dbp.repoSpecs))
		for i, spec := range dbp.repoSpecs {
			scopes[i] = Scope{ScopeID: spec.scopeID}
		}

		timeAfter := time.Now().AddDate(0, 0, -90).Format("2006-01-02T15:04:05Z")
		bp := Blueprint{
			Name:            name,
			Mode:            "NORMAL",
			Enable:          true,
			CronConfig:      spreadCron(s.config.CronConfig, name),
			IsManual:        false,
			Labels:          labels,
			SkipFailedTasks: true,
			TimeAfter:       timeAfter,
			Connections: []Connection{{
				PluginName:   dbp.plugin,
				ConnectionID: dbp.connID,
				Scopes:       scopes,
			}},
		}

		if ebp, ok := existingByName[name]; ok {
			if scopesEqual(ebp.Connections, bp.Connections) && ebp.SkipFailedTasks == bp.SkipFailedTasks {
				delete(existingByName, name)
				slog.Info("blueprint unchanged", "name", name, "blueprintID", ebp.ID, "plugin", dbp.plugin, "connID", dbp.connID)
				continue
			}
			bp.ID = ebp.ID
			if err := s.client.PatchBlueprint(ctx, ebp.ID, bp); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("update %q: %v", name, err))
				slog.Warn("blueprint update failed", "name", name, "blueprintID", ebp.ID, "err", err)
			} else {
				result.Updated = append(result.Updated, name)
				slog.Info("blueprint updated", "name", name, "blueprintID", ebp.ID, "plugin", dbp.plugin, "connID", dbp.connID, "scopes", len(scopes))
			}
		} else {
			created, err := s.client.CreateBlueprint(ctx, bp)
			if err != nil {
				slog.Warn("blueprint creation failed, retrying with unique name",
					"name", name, "plugin", dbp.plugin, "connID", dbp.connID, "err", err)
				uniqueName := fmt.Sprintf("%s-v%d", name, time.Now().Unix())
				bp.Name = uniqueName
				created, err = s.client.CreateBlueprint(ctx, bp)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("create %q: %v", name, err))
					slog.Warn("blueprint creation failed with unique name too",
						"name", uniqueName, "err", err)
				} else {
					result.Created = append(result.Created, uniqueName)
					slog.Info("blueprint created with unique name",
						"originalName", name, "actualName", uniqueName,
						"blueprintID", created.ID, "plugin", dbp.plugin,
						"connID", dbp.connID, "scopes", len(scopes))
				}
			} else {
				result.Created = append(result.Created, name)
				slog.Info("blueprint created", "name", name, "blueprintID", created.ID, "plugin", dbp.plugin, "connID", dbp.connID, "scopes", len(scopes))
			}
		}

		delete(existingByName, name)
	}

	for name, ebp := range existingByName {
		if err := s.client.DeleteBlueprint(ctx, ebp.ID); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("delete %q: %v", name, err))
			slog.Warn("blueprint deletion failed", "name", name, "blueprintID", ebp.ID, "err", err)
		} else {
			result.Deleted = append(result.Deleted, name)
			slog.Info("blueprint deleted (orphan)", "name", name, "blueprintID", ebp.ID)
		}
	}

	return nil
}

func scopesEqual(a, b []Connection) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].PluginName != b[i].PluginName || a[i].ConnectionID != b[i].ConnectionID {
			return false
		}
		if len(a[i].Scopes) != len(b[i].Scopes) {
			return false
		}
		aSet := make(map[string]bool)
		for _, s := range a[i].Scopes {
			aSet[s.ScopeID] = true
		}
		for _, s := range b[i].Scopes {
			if !aSet[s.ScopeID] {
				return false
			}
		}
	}
	return true
}

func repoTypeToPlugin(repoType string) string {
	switch strings.ToLower(repoType) {
	case "github":
		return "github"
	case "gitlab":
		return "gitlab"
	default:
		return ""
	}
}

func blueprintName(teamID, plugin, owner string) string {
	return fmt.Sprintf("lfmbot-%s-%s-%s", teamID, plugin, owner)
}

func scopeIDField(plugin string) string {
	switch plugin {
	case "github":
		return "githubId"
	case "gitlab":
		return "gitlabId"
	case "jira":
		return "boardId"
	case "codecov":
		return "codecovId"
	case PluginTestRegistry:
		return "fullName"
	default:
		return "id"
	}
}

func scopeIDFromMap(plugin string, scope map[string]any) string {
	field := scopeIDField(plugin)
	v, ok := scope[field]
	if !ok {
		return ""
	}
	switch n := v.(type) {
	case float64:
		return strconv.FormatInt(int64(n), 10)
	case string:
		return n
	default:
		return fmt.Sprint(v)
	}
}

func hasLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

// spreadCron replaces the minute and hour fields of baseCron with
// values derived from a SHA-256 hash of seed, distributing blueprints
// evenly across the day while keeping the day/month/weekday fields.
func spreadCron(baseCron, seed string) string {
	h := sha256.Sum256([]byte(seed))
	minute := binary.BigEndian.Uint32(h[0:4]) % 60
	hour := binary.BigEndian.Uint32(h[4:8]) % 24

	fields := strings.Fields(baseCron)
	if len(fields) < 5 {
		return fmt.Sprintf("%d %d * * *", minute, hour)
	}
	fields[0] = strconv.FormatUint(uint64(minute), 10)
	fields[1] = strconv.FormatUint(uint64(hour), 10)
	return strings.Join(fields, " ")
}
