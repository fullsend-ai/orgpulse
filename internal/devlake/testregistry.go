package devlake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/fullsend-ai/orgpulse/internal/database"
)

const (
	PluginTestRegistry = "testregistry"

	CIToolOpenshiftCI = "Openshift CI"
	CIToolTektonCI    = "Tekton CI"

	// DevLake's testregistry plugin marks the connection's "project" field as
	// required but never reads it, and we use blueprints rather than DevLake
	// projects. Send a fixed placeholder until the plugin drops the requirement.
	testRegistryPlaceholderProject = "lfmbot-placeholder"
)

// ErrInvalidConnection marks connection requests rejected because of the
// caller's input rather than a DevLake failure.
var ErrInvalidConnection = errors.New("invalid connection")

var githubExpiryRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})`)

// teamPlugins lists every plugin whose connection is stored per team as
// devlake_<plugin>_connection_id.
var teamPlugins = []string{"github", "gitlab", "jira", "codecov", PluginTestRegistry}

// TestRegistryScopeName derives the short scope name from the stored full
// name. Tekton CI scopes are "<quay-org>/<repo>" (repo may contain slashes);
// Openshift CI scopes are the bare repo name.
func TestRegistryScopeName(ciTool, organization, fullName string) string {
	if ciTool == CIToolTektonCI && organization != "" {
		if name, ok := strings.CutPrefix(fullName, organization+"/"); ok && name != "" {
			return name
		}
	}
	return fullName
}

func validateTestRegistryEntry(entry ConnectionEntry) error {
	switch entry.CITool {
	case CIToolOpenshiftCI:
		if entry.Organization == "" {
			return fmt.Errorf("%w: GitHub organization is required for Openshift CI", ErrInvalidConnection)
		}
		if entry.Token == "" {
			return fmt.Errorf("%w: GitHub token is required for Openshift CI", ErrInvalidConnection)
		}
	case CIToolTektonCI:
		if entry.Organization == "" {
			return fmt.Errorf("%w: Quay organization is required for Tekton CI", ErrInvalidConnection)
		}
	default:
		return fmt.Errorf("%w: ci_tool must be %q or %q", ErrInvalidConnection, CIToolOpenshiftCI, CIToolTektonCI)
	}
	return nil
}

// testRegistryConnectionBody maps a ConnectionEntry onto the DevLake
// testregistry connection fields for create/test requests.
func testRegistryConnectionBody(entry ConnectionEntry) map[string]any {
	body := map[string]any{
		"ciTool":  entry.CITool,
		"project": testRegistryPlaceholderProject,
	}
	switch entry.CITool {
	case CIToolOpenshiftCI:
		body["githubOrganization"] = entry.Organization
		body["githubToken"] = entry.Token
	case CIToolTektonCI:
		body["quayOrganization"] = entry.Organization
	}
	return body
}

// checkTestRegistryImmutable rejects changes to the CI tool or organization
// of a team's existing testregistry connection: registered scopes come from
// that source, and the collectors silently skip scopes from the other one.
func checkTestRegistryImmutable(cfg map[string]string, entry ConnectionEntry) error {
	if cfg["devlake_"+PluginTestRegistry+"_connection_id"] == "" {
		return nil
	}
	if cur := cfg["devlake_testregistry_ci_tool"]; cur != "" && entry.CITool != "" && cur != entry.CITool {
		return fmt.Errorf("%w: the CI tool of an existing TestRegistry connection cannot be changed (currently %q)", ErrInvalidConnection, cur)
	}
	if cur := cfg["devlake_testregistry_organization"]; cur != "" && entry.Organization != "" && !strings.EqualFold(cur, entry.Organization) {
		return fmt.Errorf("%w: the organization of an existing TestRegistry connection cannot be changed (currently %q)", ErrInvalidConnection, cur)
	}
	return nil
}

// testTestRegistry runs the same provider checks as the plugin's /test
// endpoint, directly against GitHub or Quay.io like the GitHub, GitLab and
// Jira tests, so testing does not depend on DevLake being reachable.
func (s *Service) testTestRegistry(ctx context.Context, entry ConnectionEntry) (*TestConnectionResult, error) {
	if err := validateTestRegistryEntry(entry); err != nil {
		return nil, err
	}
	if entry.CITool == CIToolOpenshiftCI {
		return s.testTestRegistryGitHub(ctx, entry.Organization, entry.Token)
	}
	return &TestConnectionResult{}, s.testTestRegistryQuay(ctx, entry.Organization)
}

func (s *Service) testRegistryGet(ctx context.Context, rawURL, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	return s.extHTTPClient().Do(req)
}

func (s *Service) testTestRegistryGitHub(ctx context.Context, org, token string) (*TestConnectionResult, error) {
	base := strings.TrimRight(testRegistryGitHubAPI, "/")
	resp, err := s.testRegistryGet(ctx, base+"/user", token)
	if err != nil {
		return nil, fmt.Errorf("test testregistry connection: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("GitHub token is invalid or expired")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned HTTP %d while testing the token", resp.StatusCode)
	}
	result := &TestConnectionResult{}
	if m := githubExpiryRe.FindString(resp.Header.Get("github-authentication-token-expiration")); m != "" {
		result.ExpiryDate = m
	}

	resp, err = s.testRegistryGet(ctx, base+"/orgs/"+url.PathEscape(org), token)
	if err != nil {
		return nil, fmt.Errorf("test testregistry connection: %w", err)
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return result, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("GitHub organization %q not found", org)
	case http.StatusForbidden:
		return nil, fmt.Errorf("GitHub token does not have access to organization %q", org)
	default:
		return nil, fmt.Errorf("GitHub API returned HTTP %d for organization %q", resp.StatusCode, org)
	}
}

func (s *Service) testTestRegistryQuay(ctx context.Context, org string) error {
	q := url.Values{"namespace": {org}, "public": {"true"}, "limit": {"1"}}
	resp, err := s.testRegistryGet(ctx, strings.TrimRight(testRegistryQuayAPI, "/")+"/api/v1/repository?"+q.Encode(), "")
	if err != nil {
		return fmt.Errorf("test testregistry connection: %w", err)
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("quay.io organization %q not found or not accessible", org)
	default:
		return fmt.Errorf("quay.io API returned HTTP %d for organization %q", resp.StatusCode, org)
	}
}

// testRegistryScopeData builds the scope payload registered with DevLake.
// Scopes are identified by fullName, so no remote lookup is required.
func testRegistryScopeData(connID int, ciTool, organization, fullName string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"connectionId": connID,
		"fullName":     fullName,
		"name":         TestRegistryScopeName(ciTool, organization, fullName),
	})
}

// testRegistryEndpointKey gives testregistry connections (which have no
// endpoint) a dedup key so the same scope collected through two connections
// to the same CI source still collides.
func testRegistryEndpointKey(conn connectionInfo) string {
	switch conn.CITool {
	case CIToolOpenshiftCI:
		if conn.GitHubOrganization != "" {
			return "openshift-ci:" + strings.ToLower(conn.GitHubOrganization)
		}
	case CIToolTektonCI:
		if conn.QuayOrganization != "" {
			return "tekton-ci:" + strings.ToLower(conn.QuayOrganization)
		}
	}
	return ""
}

// ValidateTestRegistryScopes checks that every fullName is offered by the
// team's testregistry connection and returns the canonical names.
func (s *Service) ValidateTestRegistryScopes(ctx context.Context, teamID string, fullNames []string) ([]string, error) {
	connID, err := s.teamConnectionID(ctx, teamID, PluginTestRegistry)
	if err != nil {
		return nil, fmt.Errorf("lookup testregistry connection: %w", err)
	}
	if connID == 0 {
		return nil, fmt.Errorf("no TestRegistry connection configured for this team")
	}
	raw, err := s.collectRemoteScopes(ctx, PluginTestRegistry, connID)
	if err != nil {
		return nil, fmt.Errorf("list TestRegistry scopes: %w", err)
	}
	available := make(map[string]string, len(raw))
	for _, r := range raw {
		if id := scopeIDFromRaw(PluginTestRegistry, r); id != "" {
			available[strings.ToLower(id)] = id
		}
	}
	out := make([]string, 0, len(fullNames))
	for _, fn := range fullNames {
		canonical, ok := available[strings.ToLower(strings.TrimSpace(fn))]
		if !ok {
			return nil, fmt.Errorf("%q is not offered by the team's TestRegistry connection", fn)
		}
		out = append(out, canonical)
	}
	return out, nil
}

// SourceTypeTestSuites is the team source holding TestRegistry scopes. Each
// scope is stored as a workitem project row with Type "testregistry" and the
// scope full name in ProjectName.
const SourceTypeTestSuites = "testsuites"

func (s *Service) addTestRegistryDesired(ctx context.Context, teamID string, config map[string]string, src database.SourceRow, desired map[string]*desiredBlueprint, result *SyncResult) {
	if len(src.Projects) == 0 {
		return
	}
	connID, err := s.teamConnectionID(ctx, teamID, PluginTestRegistry)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("lookup testregistry connection: %v", err))
		return
	}
	if connID == 0 {
		result.Errors = append(result.Errors, "No DevLake connection for testregistry. Add one in team settings.")
		return
	}
	ciTool := config["devlake_testregistry_ci_tool"]
	org := config["devlake_testregistry_organization"]
	if org == "" {
		result.Errors = append(result.Errors, "TestRegistry connection is missing an organization. Recreate it in team settings.")
		return
	}
	bpName := blueprintName(teamID, PluginTestRegistry, org)
	bp, ok := desired[bpName]
	if !ok {
		bp = &desiredBlueprint{name: bpName, plugin: PluginTestRegistry, connID: connID, ciTool: ciTool}
		desired[bpName] = bp
	}
	for _, proj := range src.Projects {
		if proj.Type != PluginTestRegistry || strings.TrimSpace(proj.ProjectName) == "" {
			continue
		}
		bp.repoSpecs = append(bp.repoSpecs, repoSpec{owner: org, name: strings.TrimSpace(proj.ProjectName)})
	}
}

// resolveTestRegistryScopes builds scope payloads directly: testregistry
// scopes are keyed by fullName, which was validated when the item was added.
func resolveTestRegistryScopes(bp *desiredBlueprint, result *SyncResult) {
	var resolved []repoSpec
	for _, spec := range bp.repoSpecs {
		raw, err := testRegistryScopeData(bp.connID, bp.ciTool, spec.owner, spec.name)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("resolve %s: %v", spec.name, err))
			result.Failed = append(result.Failed, spec.name)
			continue
		}
		spec.scopeID = spec.name
		spec.rawData = raw
		resolved = append(resolved, spec)
		result.Resolved = append(result.Resolved, spec.name)
	}
	bp.repoSpecs = resolved
}

// Provider APIs the testregistry plugin lists scopes from. Variables so tests
// can point them at a local server.
var (
	testRegistryGitHubAPI = "https://api.github.com"
	testRegistryQuayAPI   = "https://quay.io"
)

// proxyBrowseTestRegistry lists the scopes DevLake's testregistry plugin
// would offer for unsaved credentials, mirroring its remote-scope logic, so
// the onboarding wizard can browse before the connection exists.
func (s *Service) proxyBrowseTestRegistry(ctx context.Context, entry ConnectionEntry) ([]ProxyBrowseResult, error) {
	if err := validateTestRegistryEntry(entry); err != nil {
		return nil, err
	}
	if entry.CITool == CIToolOpenshiftCI {
		return s.browseOpenshiftCIConfigs(ctx, entry.Organization, entry.Token)
	}
	return s.browseQuayRepositories(ctx, entry.Organization)
}

func (s *Service) browseOpenshiftCIConfigs(ctx context.Context, org, token string) ([]ProxyBrowseResult, error) {
	client := s.extHTTPClient()
	base := fmt.Sprintf("%s/repos/openshift/release/contents/ci-operator/config/%s",
		strings.TrimRight(testRegistryGitHubAPI, "/"), url.PathEscape(org))
	for _, ref := range []string{"master", "main"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?ref="+ref, nil)
		if err != nil {
			return nil, fmt.Errorf("browse openshift ci: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("browse openshift ci: %w", err)
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			continue
		}
		if resp.StatusCode >= 400 {
			resp.Body.Close()
			return nil, fmt.Errorf("browse openshift ci: GitHub returned HTTP %d", resp.StatusCode)
		}
		var items []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		err = json.NewDecoder(resp.Body).Decode(&items)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("browse openshift ci: decode: %w", err)
		}
		out := make([]ProxyBrowseResult, 0, len(items))
		for _, it := range items {
			isYAML := it.Type == "file" && (strings.HasSuffix(it.Name, ".yaml") || strings.HasSuffix(it.Name, ".yml"))
			if it.Type == "dir" || isYAML {
				out = append(out, ProxyBrowseResult{Key: it.Name, Name: it.Name})
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("browse openshift ci: no CI configs for %q in openshift/release ci-operator/config", org)
}

func (s *Service) browseQuayRepositories(ctx context.Context, org string) ([]ProxyBrowseResult, error) {
	client := s.extHTTPClient()
	var out []ProxyBrowseResult
	nextPage := ""
	for range 100 {
		q := url.Values{"namespace": {org}, "public": {"true"}}
		if nextPage != "" {
			q.Set("next_page", nextPage)
		}
		reqURL := strings.TrimRight(testRegistryQuayAPI, "/") + "/api/v1/repository?" + q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("browse quay: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("browse quay: %w", err)
		}
		if resp.StatusCode >= 400 {
			resp.Body.Close()
			return nil, fmt.Errorf("browse quay: Quay.io returned HTTP %d", resp.StatusCode)
		}
		var page struct {
			Repositories []struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
			} `json:"repositories"`
			NextPage string `json:"next_page"`
		}
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("browse quay: decode: %w", err)
		}
		for _, r := range page.Repositories {
			out = append(out, ProxyBrowseResult{Key: r.Namespace + "/" + r.Name, Name: r.Name})
		}
		if page.NextPage == "" {
			return out, nil
		}
		nextPage = page.NextPage
	}
	return out, nil
}
