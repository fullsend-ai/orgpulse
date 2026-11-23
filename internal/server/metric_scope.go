package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/fullsend-ai/orgpulse/internal/authz"
	"github.com/fullsend-ai/orgpulse/internal/database"
)

const maxMetricBody = 1 << 20

var (
	errUnauthenticated = errors.New("unauthenticated")
	errMetricDenied    = errors.New("metric scope denied")
	errMetricBody      = errors.New("invalid metric body")
)

type metricQuery struct {
	Owner       stringList  `json:"owner"`
	Name        stringList  `json:"name"`
	Projects    projectList `json:"projects"`
	JiraProject string      `json:"jiraproject"`
	JiraSnake   string      `json:"jira_project"`
}

type repoRef struct {
	Owner string
	Name  string
}

type projectRef struct {
	ID   string
	Name string
	Jira string
}

type stringList []string

func (s *stringList) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var one string
		if err := json.Unmarshal(data, &one); err != nil {
			return err
		}
		*s = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

type projectList []projectRef

func (p *projectList) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, item := range raw {
		ref, err := decodeProject(item)
		if err != nil {
			return err
		}
		*p = append(*p, ref)
	}
	return nil
}

func decodeProject(item json.RawMessage) (projectRef, error) {
	trimmed := strings.TrimSpace(string(item))
	if trimmed == "" || trimmed == "null" {
		return projectRef{}, nil
	}
	if trimmed[0] == '"' {
		var id string
		if err := json.Unmarshal(item, &id); err != nil {
			return projectRef{}, err
		}
		return projectRef{ID: id}, nil
	}
	var obj struct {
		ID          string `json:"id"`
		ProjectName string `json:"projectname"`
		NameSnake   string `json:"project_name"`
		Jira        string `json:"jiraproject"`
		JiraSnake   string `json:"jira_project"`
	}
	if err := json.Unmarshal(item, &obj); err != nil {
		return projectRef{}, err
	}
	name := obj.ProjectName
	if name == "" {
		name = obj.NameSnake
	}
	jira := obj.Jira
	if jira == "" {
		jira = obj.JiraSnake
	}
	return projectRef{ID: obj.ID, Name: name, Jira: jira}, nil
}

var (
	metricScopeKeys  = []string{"owner", "name", "projects", "jiraproject", "jira_project"}
	projectScopeKeys = []string{"id", "projectname", "project_name", "jiraproject", "jira_project"}
)

// nonCanonicalKey reports whether key is not one of canon but matches one
// case-insensitively. encoding/json would read it into the scope field
// while the workflow, which reads keys exactly, would not, so authorization
// and execution would disagree on the scope.
func nonCanonicalKey(key string, canon []string) bool {
	for _, c := range canon {
		if key != c && strings.EqualFold(key, c) {
			return true
		}
	}
	return false
}

// checkScopeKeys rejects bodies whose scope keys are duplicated or differ
// from the canonical spelling only by case, including in project objects.
func checkScopeKeys(body []byte) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, _ := dec.Token()
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil
	}
	return checkObjectKeys(dec, metricScopeKeys, true)
}

func checkObjectKeys(dec *json.Decoder, canon []string, top bool) error {
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return errMetricBody
		}
		key, _ := tok.(string)
		if nonCanonicalKey(key, canon) || (seen[key] && slices.Contains(canon, key)) {
			return errMetricBody
		}
		seen[key] = true
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return errMetricBody
		}
		if top && key == "projects" {
			if err := checkProjectKeys(value); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkProjectKeys(value json.RawMessage) error {
	var items []json.RawMessage
	_ = json.Unmarshal(value, &items)
	for _, item := range items {
		dec := json.NewDecoder(bytes.NewReader(item))
		tok, _ := dec.Token()
		if delim, ok := tok.(json.Delim); ok && delim == '{' {
			if err := checkObjectKeys(dec, projectScopeKeys, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkScopeInput(input map[string]any) error {
	for k, v := range input {
		if nonCanonicalKey(k, metricScopeKeys) {
			return errMetricBody
		}
		if k != "projects" {
			continue
		}
		items, _ := v.([]any)
		for _, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			for pk := range obj {
				if nonCanonicalKey(pk, projectScopeKeys) {
					return errMetricBody
				}
			}
		}
	}
	return nil
}

func parseMetricBody(body []byte) (metricQuery, error) {
	if len(bytesTrim(body)) == 0 {
		return metricQuery{}, nil
	}
	var query metricQuery
	if err := json.Unmarshal(body, &query); err != nil {
		return metricQuery{}, errMetricBody
	}
	return query, nil
}

func bytesTrim(body []byte) []byte {
	return []byte(strings.TrimSpace(string(body)))
}

func parseMetricValues(query metricQuery, values url.Values) metricQuery {
	if len(query.Owner) == 0 {
		query.Owner = values["owner"]
	}
	if len(query.Name) == 0 {
		query.Name = values["name"]
	}
	if query.JiraProject == "" {
		query.JiraProject = values.Get("jiraproject")
	}
	if query.JiraSnake == "" {
		query.JiraSnake = values.Get("jira_project")
	}
	return query
}

func (q metricQuery) repos() ([]repoRef, error) {
	if len(q.Owner) == 0 && len(q.Name) == 0 {
		return nil, nil
	}
	if len(q.Owner) != len(q.Name) {
		return nil, errMetricBody
	}
	var repos []repoRef
	for i := range q.Owner {
		owner := strings.TrimSpace(q.Owner[i])
		name := strings.TrimSpace(q.Name[i])
		if owner == "" && name == "" {
			continue
		}
		if owner == "" || name == "" {
			return nil, errMetricBody
		}
		repos = append(repos, repoRef{Owner: owner, Name: name})
	}
	return repos, nil
}

func (q metricQuery) jira() string {
	if strings.TrimSpace(q.JiraProject) != "" {
		return strings.TrimSpace(q.JiraProject)
	}
	return strings.TrimSpace(q.JiraSnake)
}

type scopeIndex struct {
	repos    map[string]map[string]struct{}
	projects map[string]map[string]struct{}
}

func newScopeIndex() scopeIndex {
	return scopeIndex{
		repos:    map[string]map[string]struct{}{},
		projects: map[string]map[string]struct{}{},
	}
}

func (idx scopeIndex) addRepo(owner, name, teamID string) {
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	if owner == "" || name == "" || teamID == "" {
		return
	}
	key := strings.ToLower(owner) + "/" + strings.ToLower(name)
	idx.add(idx.repos, key, teamID)
}

func (idx scopeIndex) addProject(teamID string, ids ...string) {
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || teamID == "" {
			continue
		}
		idx.add(idx.projects, strings.ToLower(id), teamID)
	}
}

func (idx scopeIndex) add(dst map[string]map[string]struct{}, key, teamID string) {
	if dst[key] == nil {
		dst[key] = map[string]struct{}{}
	}
	dst[key][teamID] = struct{}{}
}

func (a *Authorizer) loadScope(ctx context.Context) (scopeIndex, error) {
	idx := newScopeIndex()
	teams, err := a.store.ListTeamRows(ctx)
	if err != nil {
		return idx, err
	}
	for _, team := range teams {
		if err := addTeamSources(ctx, a.store, idx, team.ID); err != nil {
			return idx, err
		}
	}
	products, err := a.store.ListProductRows(ctx)
	if err != nil {
		return idx, err
	}
	for _, product := range products {
		if err := addProductOverrides(ctx, a.store, idx, product.ID); err != nil {
			return idx, err
		}
	}
	return idx, nil
}

func addTeamSources(ctx context.Context, store database.ConfigStore, idx scopeIndex, teamID string) error {
	sources, err := store.ListSourceRows(ctx, teamID)
	if err != nil {
		return err
	}
	for _, source := range sources {
		for _, repo := range source.Repos {
			idx.addRepo(repo.Owner, repo.Name, teamID)
		}
		for _, project := range source.Projects {
			idx.addProject(teamID, project.ID, project.ProjectName, project.JiraProject)
		}
	}
	return nil
}

func addProductOverrides(ctx context.Context, store database.ConfigStore, idx scopeIndex, productID string) error {
	overrides, err := store.GetProductOverrideRows(ctx, productID)
	if err != nil {
		return err
	}
	for _, override := range overrides {
		teamIDs := override.Teams
		if len(teamIDs) == 0 {
			links, err := store.GetProductTeamLinks(ctx, productID)
			if err != nil {
				return err
			}
			for _, link := range links {
				teamIDs = append(teamIDs, link.ID)
			}
		}
		for _, teamID := range teamIDs {
			idx.addRepo(override.Owner, override.Name, teamID)
		}
	}
	return nil
}

func (idx scopeIndex) allows(g authz.Grant, repos []repoRef, projects []projectRef, jira string) bool {
	if len(repos) == 0 && len(projects) == 0 && jira == "" {
		return false
	}
	for _, repo := range repos {
		if !idx.teamAllowed(g, idx.repos[repoKey(repo.Owner, repo.Name)]) {
			return false
		}
	}
	for _, project := range projects {
		if !projectAllowed(g, idx, project) {
			return false
		}
	}
	if jira != "" && !idx.teamAllowed(g, idx.projects[strings.ToLower(jira)]) {
		return false
	}
	return true
}

func projectAllowed(g authz.Grant, idx scopeIndex, project projectRef) bool {
	ids := []string{project.ID, project.Name, project.Jira}
	checked := false
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		checked = true
		if !idx.teamAllowed(g, idx.projects[strings.ToLower(id)]) {
			return false
		}
	}
	return checked
}

func (idx scopeIndex) teamAllowed(g authz.Grant, teams map[string]struct{}) bool {
	if len(teams) == 0 {
		return false
	}
	for teamID := range teams {
		if g.CanReadTeam(teamID) {
			return true
		}
	}
	return false
}

func repoKey(owner, name string) string {
	return strings.ToLower(strings.TrimSpace(owner)) + "/" + strings.ToLower(strings.TrimSpace(name))
}

func authorizeMetricRaw(ctx context.Context, az *Authorizer, ident Identity, body []byte, values url.Values) error {
	if !ident.Known() {
		return errUnauthenticated
	}
	if ident.IsAdmin {
		return nil
	}
	if err := checkScopeKeys(body); err != nil {
		return err
	}
	query, err := parseMetricBody(body)
	if err != nil {
		return err
	}
	return authorizeParsed(ctx, az, ident, parseMetricValues(query, values))
}

func authorizeMetricInput(ctx context.Context, az *Authorizer, ident Identity, input map[string]any) error {
	if !ident.Known() {
		return errUnauthenticated
	}
	if ident.IsAdmin {
		return nil
	}
	if err := checkScopeInput(input); err != nil {
		return err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return errMetricBody
	}
	query, err := parseMetricBody(body)
	if err != nil {
		return err
	}
	return authorizeParsed(ctx, az, ident, query)
}

func authorizeParsed(ctx context.Context, az *Authorizer, ident Identity, query metricQuery) error {
	if az == nil {
		return errMetricDenied
	}
	grant, ok := grantFromContext(ctx)
	if !ok {
		var err error
		grant, err = az.Resolve(ctx, ident)
		if err != nil {
			return err
		}
	}
	if grant.Bypass || grant.Operator {
		return nil
	}
	repos, err := query.repos()
	if err != nil {
		return err
	}
	idx, err := az.loadScope(ctx)
	if err != nil {
		return err
	}
	if !idx.allows(grant, repos, query.Projects, query.jira()) {
		return errMetricDenied
	}
	return nil
}

func writeMetricError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errUnauthenticated):
		writeError(w, http.StatusUnauthorized, "authentication required")
	case errors.Is(err, errMetricBody):
		writeError(w, http.StatusBadRequest, "invalid JSON body")
	case errors.Is(err, errMetricDenied):
		writeError(w, http.StatusForbidden, "access denied")
	default:
		writeError(w, http.StatusInternalServerError, "auth error")
	}
}
