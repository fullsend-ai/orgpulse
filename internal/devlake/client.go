package devlake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// maxResponseBody caps how much of an upstream response is read.
const maxResponseBody = 64 << 20

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}

func (c *Client) ListBlueprints(ctx context.Context, label string) ([]Blueprint, error) {
	var all []Blueprint
	page := 1
	const pageSize = 100
	for {
		u := fmt.Sprintf("%s/blueprints?pageSize=%d&page=%d", c.baseURL, pageSize, page)
		if label != "" {
			u += "&label=" + url.QueryEscape(label)
		}
		var resp blueprintListResponse
		if err := c.doJSON(ctx, http.MethodGet, u, nil, &resp); err != nil {
			return nil, fmt.Errorf("list blueprints: %w", err)
		}
		all = append(all, resp.Blueprints...)
		if int64(len(all)) >= resp.Count {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) CreateBlueprint(ctx context.Context, bp Blueprint) (*Blueprint, error) {
	u := c.baseURL + "/blueprints"
	var created Blueprint
	if err := c.doJSON(ctx, http.MethodPost, u, bp, &created); err != nil {
		return nil, fmt.Errorf("create blueprint %q: %w", bp.Name, err)
	}
	return &created, nil
}

func (c *Client) PatchBlueprint(ctx context.Context, id int64, bp Blueprint) error {
	u := fmt.Sprintf("%s/blueprints/%d", c.baseURL, id)
	if err := c.doJSON(ctx, http.MethodPatch, u, bp, nil); err != nil {
		return fmt.Errorf("patch blueprint %d: %w", id, err)
	}
	return nil
}

func (c *Client) PatchBlueprintLabels(ctx context.Context, id int64, labels []string) error {
	u := fmt.Sprintf("%s/blueprints/%d", c.baseURL, id)
	if err := c.doJSON(ctx, http.MethodPatch, u, map[string]any{"labels": labels}, nil); err != nil {
		return fmt.Errorf("patch blueprint %d labels: %w", id, err)
	}
	return nil
}

func (c *Client) DeleteBlueprint(ctx context.Context, id int64) error {
	u := fmt.Sprintf("%s/blueprints/%d", c.baseURL, id)
	if err := c.doJSON(ctx, http.MethodDelete, u, nil, nil); err != nil {
		return fmt.Errorf("delete blueprint %d: %w", id, err)
	}
	return nil
}

func (c *Client) TriggerBlueprint(ctx context.Context, id int64) error {
	u := fmt.Sprintf("%s/blueprints/%d/trigger", c.baseURL, id)
	if err := c.doJSON(ctx, http.MethodPost, u, nil, nil); err != nil {
		return fmt.Errorf("trigger blueprint %d: %w", id, err)
	}
	return nil
}

type searchRemoteScopesResponse struct {
	Children []struct {
		Data json.RawMessage `json:"data"`
	} `json:"children"`
}

func (c *Client) SearchRemoteScopes(ctx context.Context, plugin string, connID int, search string) ([]json.RawMessage, error) {
	u := fmt.Sprintf("%s/plugins/%s/connections/%d/search-remote-scopes?search=%s&page=1&pageSize=50",
		c.baseURL, url.PathEscape(plugin), connID, url.QueryEscape(search))
	var resp searchRemoteScopesResponse
	if err := c.doJSON(ctx, http.MethodGet, u, nil, &resp); err != nil {
		return nil, fmt.Errorf("search remote scopes (%s, %q): %w", plugin, search, err)
	}
	var out []json.RawMessage
	for _, child := range resp.Children {
		out = append(out, child.Data)
	}
	return out, nil
}

type remoteScopesResponse struct {
	Children      []remoteScopeNode `json:"children"`
	NextPageToken string            `json:"nextPageToken"`
}

type remoteScopeNode struct {
	Type     string          `json:"type"`
	ParentID *string         `json:"parentId"`
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Data     json.RawMessage `json:"data"`
}

func (c *Client) ListRemoteScopes(ctx context.Context, plugin string, connID int, groupID string) ([]remoteScopeNode, error) {
	var all []remoteScopeNode
	pageToken := ""
	for {
		u := fmt.Sprintf("%s/plugins/%s/connections/%d/remote-scopes?groupId=%s&pageToken=%s&pageSize=200",
			c.baseURL, url.PathEscape(plugin), connID, url.QueryEscape(groupID), url.QueryEscape(pageToken))
		var resp remoteScopesResponse
		if err := c.doJSON(ctx, http.MethodGet, u, nil, &resp); err != nil {
			return nil, fmt.Errorf("list remote scopes (%s, conn %d): %w", plugin, connID, err)
		}
		all = append(all, resp.Children...)
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return all, nil
}

type putScopesRequest struct {
	Data []json.RawMessage `json:"data"`
}

func (c *Client) RegisterScopes(ctx context.Context, plugin string, connID int, data []json.RawMessage) error {
	u := fmt.Sprintf("%s/plugins/%s/connections/%d/scopes", c.baseURL, url.PathEscape(plugin), connID)
	body := putScopesRequest{Data: data}
	if err := c.doJSON(ctx, http.MethodPut, u, body, nil); err != nil {
		return fmt.Errorf("register scopes (%s, conn %d): %w", plugin, connID, err)
	}
	return nil
}

func (c *Client) ListScopeConfigs(ctx context.Context, plugin string, connID int) ([]map[string]any, error) {
	u := fmt.Sprintf("%s/plugins/%s/connections/%d/scope-configs?pageSize=100&page=1",
		c.baseURL, url.PathEscape(plugin), connID)
	var raw json.RawMessage
	if err := c.doJSON(ctx, http.MethodGet, u, nil, &raw); err != nil {
		return nil, fmt.Errorf("list scope configs (%s, conn %d): %w", plugin, connID, err)
	}
	var wrapped struct {
		ScopeConfigs []map[string]any `json:"scopeConfigs"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.ScopeConfigs) > 0 {
		return wrapped.ScopeConfigs, nil
	}
	var plain []map[string]any
	if err := json.Unmarshal(raw, &plain); err == nil {
		return plain, nil
	}
	return nil, nil
}

func (c *Client) CreateScopeConfig(ctx context.Context, plugin string, connID int, body map[string]any) (int, error) {
	u := fmt.Sprintf("%s/plugins/%s/connections/%d/scope-configs", c.baseURL, url.PathEscape(plugin), connID)
	var resp struct {
		ID int `json:"id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, u, body, &resp); err != nil {
		return 0, fmt.Errorf("create scope config (%s, conn %d): %w", plugin, connID, err)
	}
	return resp.ID, nil
}

func (c *Client) UpdateScopeConfig(ctx context.Context, plugin string, connID int, configID int, body map[string]any) error {
	u := fmt.Sprintf("%s/plugins/%s/connections/%d/scope-configs/%d",
		c.baseURL, url.PathEscape(plugin), connID, configID)
	if err := c.doJSON(ctx, http.MethodPatch, u, body, nil); err != nil {
		return fmt.Errorf("update scope config (%s, conn %d, config %d): %w", plugin, connID, configID, err)
	}
	return nil
}

func (c *Client) PatchScope(ctx context.Context, plugin string, connID int, scopeID string, scopeConfigID int) error {
	u := fmt.Sprintf("%s/plugins/%s/connections/%d/scopes/%s",
		c.baseURL, url.PathEscape(plugin), connID, url.PathEscape(scopeID))
	body := map[string]any{"scopeConfigId": scopeConfigID}
	if err := c.doJSON(ctx, http.MethodPatch, u, body, nil); err != nil {
		return fmt.Errorf("patch scope (%s, conn %d, scope %s): %w", plugin, connID, scopeID, err)
	}
	return nil
}

func (c *Client) CreateConnection(ctx context.Context, plugin, name, endpoint, token string, extra map[string]string) (int, error) {
	u := fmt.Sprintf("%s/plugins/%s/connections", c.baseURL, url.PathEscape(plugin))

	body := map[string]any{
		"name":     name,
		"endpoint": endpoint,
	}

	switch plugin {
	case "github":
		body["authMethod"] = "AccessToken"
		body["token"] = token
		body["enableGraphql"] = true
	case "gitlab":
		body["authMethod"] = "AccessToken"
		body["token"] = token
	case "jira":
		body["authMethod"] = "BasicAuth"
		body["password"] = token
		body["endpoint"] = normalizeJiraEndpoint(endpoint)
		if username, ok := extra["username"]; ok {
			body["username"] = username
		}
	case "codecov":
		body["token"] = token
		body["organization"] = extra["organization"]
		body["service"] = extra["service"]
	case PluginTestRegistry:
		delete(body, "endpoint")
		for k, v := range testRegistryConnectionBody(ConnectionEntry{
			CITool:       extra["ci_tool"],
			Organization: extra["organization"],
			Token:        token,
		}) {
			body[k] = v
		}
	default:
		body["token"] = token
	}

	var resp connectionInfo
	if err := c.doJSON(ctx, http.MethodPost, u, body, &resp); err != nil {
		return 0, fmt.Errorf("create %s connection %q: %w", plugin, name, err)
	}
	return resp.ID, nil
}

// TestPluginConnection delegates a connection test to the DevLake plugin so the
// dashboard and DevLake agree on whether a token is usable: what DevLake rejects
// at collection time.
func (c *Client) TestPluginConnection(ctx context.Context, plugin string, body map[string]any) error {
	u := fmt.Sprintf("%s/plugins/%s/test", c.baseURL, url.PathEscape(plugin))
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := c.doJSON(ctx, http.MethodPost, u, body, &resp); err != nil {
		return fmt.Errorf("test %s connection: %w", plugin, err)
	}
	if !resp.Success {
		return fmt.Errorf("test %s connection: %s", plugin, resp.Message)
	}
	return nil
}

func (c *Client) TestCodecovConnection(ctx context.Context, entry ConnectionEntry) error {
	return c.TestPluginConnection(ctx, "codecov", map[string]any{
		"endpoint":     entry.Endpoint,
		"token":        entry.Token,
		"organization": entry.Organization,
		"service":      entry.Service,
	})
}

func (c *Client) TestGitHubConnection(ctx context.Context, entry ConnectionEntry) error {
	err := c.TestPluginConnection(ctx, "github", map[string]any{
		"endpoint":   entry.Endpoint,
		"token":      entry.Token,
		"authMethod": "AccessToken",
	})
	if err == nil {
		return nil
	}
	// DevLake reports scope failures as "Please check the field(s) read:user, read:org",
	// which reads like a form validation hint. Restate it as the full permission set
	// a GitHub classic PAT needs for DevLake to collect issues, PRs, and accounts.
	if strings.Contains(err.Error(), "Please check the field(s)") {
		return fmt.Errorf("test github connection: The provided token is missing one or more of the following permissions: repo, read:user and read:org")
	}
	return err
}

func (c *Client) PatchConnection(ctx context.Context, plugin string, connID int, fields map[string]any) error {
	u := fmt.Sprintf("%s/plugins/%s/connections/%d", c.baseURL, url.PathEscape(plugin), connID)
	if err := c.doJSON(ctx, http.MethodPatch, u, fields, nil); err != nil {
		return fmt.Errorf("patch %s connection %d: %w", plugin, connID, err)
	}
	return nil
}

func (c *Client) HasRunningPipelines(ctx context.Context) (bool, error) {
	u := fmt.Sprintf("%s/pipelines?status=TASK_RUNNING&pageSize=1&page=1", c.baseURL)
	var resp struct {
		Count int64 `json:"count"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, &resp); err != nil {
		return false, fmt.Errorf("check running pipelines: %w", err)
	}
	return resp.Count > 0, nil
}

func (c *Client) DeleteConnection(ctx context.Context, plugin string, connID int) error {
	u := fmt.Sprintf("%s/plugins/%s/connections/%d", c.baseURL, url.PathEscape(plugin), connID)
	if err := c.doJSON(ctx, http.MethodDelete, u, nil, nil); err != nil {
		return fmt.Errorf("delete %s connection %d: %w", plugin, connID, err)
	}
	return nil
}

func (c *Client) GetScope(ctx context.Context, plugin string, connID int, scopeID string) (map[string]any, error) {
	u := fmt.Sprintf("%s/plugins/%s/connections/%d/scopes/%s",
		c.baseURL, url.PathEscape(plugin), connID, url.PathEscape(scopeID))
	var resp map[string]any
	if err := c.doJSON(ctx, http.MethodGet, u, nil, &resp); err != nil {
		return nil, fmt.Errorf("get scope %s (%s, conn %d): %w", scopeID, plugin, connID, err)
	}
	return resp, nil
}

func (c *Client) DeleteScope(ctx context.Context, plugin string, connID int, scopeID string) error {
	u := fmt.Sprintf("%s/plugins/%s/connections/%d/scopes/%s", c.baseURL, url.PathEscape(plugin), connID, url.PathEscape(scopeID))
	if err := c.doJSON(ctx, http.MethodDelete, u, nil, nil); err != nil {
		return fmt.Errorf("delete scope %s (%s, conn %d): %w", scopeID, plugin, connID, err)
	}
	return nil
}

func (c *Client) GetConnection(ctx context.Context, plugin string, connID int) (map[string]any, error) {
	u := fmt.Sprintf("%s/plugins/%s/connections/%d", c.baseURL, url.PathEscape(plugin), connID)
	var resp map[string]any
	if err := c.doJSON(ctx, http.MethodGet, u, nil, &resp); err != nil {
		return nil, fmt.Errorf("get %s connection %d: %w", plugin, connID, err)
	}
	return resp, nil
}

func (c *Client) ListConnections(ctx context.Context, plugin string) ([]connectionInfo, error) {
	u := fmt.Sprintf("%s/plugins/%s/connections", c.baseURL, url.PathEscape(plugin))
	var conns []connectionInfo
	if err := c.doJSON(ctx, http.MethodGet, u, nil, &conns); err != nil {
		return nil, fmt.Errorf("list %s connections: %w", plugin, err)
	}
	return conns, nil
}

func (c *Client) ListConnectionScopes(ctx context.Context, plugin string, connID int) ([]map[string]any, error) {
	var all []map[string]any
	page := 1
	const pageSize = 100
	for {
		u := fmt.Sprintf("%s/plugins/%s/connections/%d/scopes?pageSize=%d&page=%d",
			c.baseURL, url.PathEscape(plugin), connID, pageSize, page)
		var resp scopeListResponse
		if err := c.doJSON(ctx, http.MethodGet, u, nil, &resp); err != nil {
			return nil, fmt.Errorf("list scopes (%s, conn %d): %w", plugin, connID, err)
		}
		for _, d := range resp.Scopes {
			if d.Scope != nil {
				all = append(all, d.Scope)
			}
		}
		if int64(len(all)) >= resp.Count {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) doJSON(ctx context.Context, method, rawURL string, body any, out any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 2000))
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func extractNumericID(data json.RawMessage, field string) (string, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return "", err
	}
	raw, ok := m[field]
	if !ok {
		return "", fmt.Errorf("field %q not found in scope data", field)
	}
	var num float64
	if err := json.Unmarshal(raw, &num); err != nil {
		return "", fmt.Errorf("field %q is not a number: %w", field, err)
	}
	return strconv.FormatInt(int64(num), 10), nil
}

func scopeFullName(plugin string, scope map[string]any) string {
	switch plugin {
	case "github":
		if fn, ok := scope["fullName"].(string); ok {
			return fn
		}
	case "gitlab":
		if pn, ok := scope["pathWithNamespace"].(string); ok {
			return pn
		}
	case "jira":
		if name, ok := scope["name"].(string); ok {
			return name
		}
	case "codecov", PluginTestRegistry:
		if name, ok := scope["fullName"].(string); ok {
			return name
		}
	}
	return ""
}

func normalizeJiraEndpoint(endpoint string) string {
	ep := strings.TrimRight(endpoint, "/")
	if !strings.HasSuffix(ep, "/rest") {
		ep += "/rest"
	}
	return ep + "/"
}
