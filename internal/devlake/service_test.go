package devlake

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/database"
)

// ---------------------------------------------------------------------------
// Fake DevLake API server
// ---------------------------------------------------------------------------

type fakeRemoteGroup struct {
	key    string
	name   string
	boards []fakeRemoteBoard
}

type fakeRemoteBoard struct {
	id   int
	name string
}

type fakeDevLake struct {
	mu                sync.Mutex
	blueprints        []Blueprint
	nextBPID          int64
	connections       map[string][]connectionInfo // plugin -> connections
	connectionBodies  map[string]map[int]map[string]any
	connectionPatches map[string]map[int]map[string]any
	nextConnID        int
	scopes            map[string]map[int][]json.RawMessage // plugin -> connID -> scope data
	failTokens        map[string]string                    // tokens that should fail test-connection -> DevLake-style failure message
	lastTestBody      map[string]any                       // most recent /plugins/{plugin}/test request body
	triggeredBPs      []int64
	remoteGroups      []fakeRemoteGroup            // Jira project/board hierarchy
	remoteScopeData   map[string][]json.RawMessage // plugin -> flat remote-scope list override
}

func newFakeDevLake() *fakeDevLake {
	return &fakeDevLake{
		nextBPID:          1,
		connections:       make(map[string][]connectionInfo),
		connectionBodies:  make(map[string]map[int]map[string]any),
		connectionPatches: make(map[string]map[int]map[string]any),
		nextConnID:        1,
		scopes:            make(map[string]map[int][]json.RawMessage),
		failTokens:        make(map[string]string),
	}
}

func (f *fakeDevLake) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /blueprints", f.listBlueprints)
	mux.HandleFunc("POST /blueprints", f.createBlueprint)
	mux.HandleFunc("PATCH /blueprints/{id}", f.patchBlueprint)
	mux.HandleFunc("DELETE /blueprints/{id}", f.deleteBlueprint)
	mux.HandleFunc("POST /blueprints/{id}/trigger", f.triggerBlueprint)

	mux.HandleFunc("GET /plugins/{plugin}/connections", f.listConnections)
	mux.HandleFunc("POST /plugins/{plugin}/connections", f.createConnection)
	mux.HandleFunc("GET /plugins/{plugin}/connections/{connID}", f.getConnection)
	mux.HandleFunc("PATCH /plugins/{plugin}/connections/{connID}", f.patchConnection)
	mux.HandleFunc("DELETE /plugins/{plugin}/connections/{connID}", f.deleteConnection)

	mux.HandleFunc("POST /plugins/{plugin}/test", f.testConnection)
	mux.HandleFunc("GET /plugins/{plugin}/connections/{connID}/search-remote-scopes", f.searchRemoteScopes)
	mux.HandleFunc("GET /plugins/{plugin}/connections/{connID}/remote-scopes", f.listRemoteScopes)
	mux.HandleFunc("PUT /plugins/{plugin}/connections/{connID}/scopes", f.registerScopes)
	mux.HandleFunc("GET /plugins/{plugin}/connections/{connID}/scopes/", f.listConnectionScopes)
	mux.HandleFunc("DELETE /plugins/{plugin}/connections/{connID}/scopes/{scopeID}", f.deleteScope)

	return mux
}

func (f *fakeDevLake) listBlueprints(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	label := r.URL.Query().Get("label")
	var filtered []Blueprint
	for _, bp := range f.blueprints {
		if label == "" || hasLabel(bp.Labels, label) {
			filtered = append(filtered, bp)
		}
	}
	writeJSON(w, http.StatusOK, blueprintListResponse{
		Blueprints: filtered,
		Count:      int64(len(filtered)),
	})
}

func (f *fakeDevLake) createBlueprint(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var bp Blueprint
	if err := json.NewDecoder(r.Body).Decode(&bp); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	bp.ID = f.nextBPID
	f.nextBPID++
	f.blueprints = append(f.blueprints, bp)
	writeJSON(w, http.StatusOK, bp)
}

func (f *fakeDevLake) patchBlueprint(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for i, bp := range f.blueprints {
		if bp.ID == id {
			if len(body) == 1 && body["labels"] != nil {
				var labels []string
				if err := json.Unmarshal(body["labels"], &labels); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				bp.Labels = labels
				f.blueprints[i] = bp
				writeJSON(w, http.StatusOK, bp)
				return
			}
			encoded, err := json.Marshal(body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var patch Blueprint
			if err := json.Unmarshal(encoded, &patch); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			patch.ID = id
			f.blueprints[i] = patch
			writeJSON(w, http.StatusOK, patch)
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func (f *fakeDevLake) deleteBlueprint(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	for i, bp := range f.blueprints {
		if bp.ID == id {
			f.blueprints = append(f.blueprints[:i], f.blueprints[i+1:]...)
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func (f *fakeDevLake) triggerBlueprint(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	for _, bp := range f.blueprints {
		if bp.ID == id {
			f.triggeredBPs = append(f.triggeredBPs, id)
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func (f *fakeDevLake) listConnections(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	plugin := r.PathValue("plugin")
	writeJSON(w, http.StatusOK, f.connections[plugin])
}

func (f *fakeDevLake) createConnection(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	plugin := r.PathValue("plugin")
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := f.nextConnID
	f.nextConnID++
	name, _ := body["name"].(string)
	endpoint, _ := body["endpoint"].(string)
	organization, _ := body["organization"].(string)
	service, _ := body["service"].(string)
	ciTool, _ := body["ciTool"].(string)
	githubOrg, _ := body["githubOrganization"].(string)
	quayOrg, _ := body["quayOrganization"].(string)
	f.connections[plugin] = append(f.connections[plugin], connectionInfo{
		ID: id, Name: name, Endpoint: endpoint, Organization: organization, Service: service,
		CITool: ciTool, GitHubOrganization: githubOrg, QuayOrganization: quayOrg,
	})
	if f.connectionBodies[plugin] == nil {
		f.connectionBodies[plugin] = make(map[int]map[string]any)
	}
	f.connectionBodies[plugin][id] = body
	writeJSON(w, http.StatusOK, connectionInfo{ID: id, Name: name})
}

func (f *fakeDevLake) getConnection(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	plugin := r.PathValue("plugin")
	connID, _ := strconv.Atoi(r.PathValue("connID"))
	for _, c := range f.connections[plugin] {
		if c.ID == connID {
			body := make(map[string]any, len(f.connectionBodies[plugin][connID])+2)
			for key, value := range f.connectionBodies[plugin][connID] {
				body[key] = value
			}
			body["id"] = c.ID
			body["name"] = c.Name
			body["endpoint"] = c.Endpoint
			writeJSON(w, http.StatusOK, body)
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func (f *fakeDevLake) patchConnection(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	plugin := r.PathValue("plugin")
	connID, _ := strconv.Atoi(r.PathValue("connID"))
	if f.connectionPatches[plugin] == nil {
		f.connectionPatches[plugin] = make(map[int]map[string]any)
	}
	patch := make(map[string]any, len(body))
	for key, value := range body {
		patch[key] = value
	}
	f.connectionPatches[plugin][connID] = patch
	if f.connectionBodies[plugin] == nil {
		f.connectionBodies[plugin] = make(map[int]map[string]any)
	}
	if f.connectionBodies[plugin][connID] == nil {
		f.connectionBodies[plugin][connID] = make(map[string]any)
	}
	for key, value := range body {
		f.connectionBodies[plugin][connID][key] = value
	}
	w.WriteHeader(http.StatusOK)
}

func (f *fakeDevLake) deleteConnection(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	plugin := r.PathValue("plugin")
	connID, _ := strconv.Atoi(r.PathValue("connID"))
	var kept []connectionInfo
	for _, c := range f.connections[plugin] {
		if c.ID != connID {
			kept = append(kept, c)
		}
	}
	f.connections[plugin] = kept
	delete(f.connectionBodies[plugin], connID)
	delete(f.scopes[plugin], connID)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeDevLake) testConnection(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	token, _ := body["token"].(string)
	f.mu.Lock()
	failMessage, shouldFail := f.failTokens[token]
	f.lastTestBody = body
	f.mu.Unlock()
	if shouldFail {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]any{"success": false, "message": failMessage}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(map[string]any{"success": true, "message": ""})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (f *fakeDevLake) searchRemoteScopes(w http.ResponseWriter, r *http.Request) {
	plugin := r.PathValue("plugin")
	search := r.URL.Query().Get("search")
	if plugin == "codecov" {
		connID, _ := strconv.Atoi(r.PathValue("connID"))
		f.mu.Lock()
		organization, _ := f.connectionBodies[plugin][connID]["organization"].(string)
		f.mu.Unlock()
		fullName := organization + "/" + search
		raw, err := json.Marshal(map[string]any{"codecovId": fullName, "fullName": fullName, "name": search})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, searchRemoteScopesResponse{Children: []struct {
			Data json.RawMessage `json:"data"`
		}{{Data: raw}}})
		return
	}

	idField := scopeIDField(plugin)
	nameField := "fullName"
	switch plugin {
	case "gitlab":
		nameField = "pathWithNamespace"
	case "jira":
		nameField = "name"
	}

	scopeData := map[string]any{
		idField:   42,
		nameField: search,
	}
	raw, err := json.Marshal(scopeData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := searchRemoteScopesResponse{
		Children: []struct {
			Data json.RawMessage `json:"data"`
		}{
			{Data: raw},
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

func (f *fakeDevLake) listRemoteScopes(w http.ResponseWriter, r *http.Request) {
	plugin := r.PathValue("plugin")
	groupID := r.URL.Query().Get("groupId")

	f.mu.Lock()
	groups := f.remoteGroups
	override, hasOverride := f.remoteScopeData[plugin]
	f.mu.Unlock()

	if hasOverride {
		children := make([]remoteScopeNode, 0, len(override))
		for _, data := range override {
			children = append(children, remoteScopeNode{Type: "scope", Data: data})
		}
		writeJSON(w, http.StatusOK, remoteScopesResponse{Children: children})
		return
	}

	if len(groups) > 0 {
		var children []remoteScopeNode
		if groupID == "" {
			for _, g := range groups {
				children = append(children, remoteScopeNode{
					Type: "group", ID: g.key, Name: g.name,
				})
			}
		} else {
			for _, g := range groups {
				if g.key != groupID {
					continue
				}
				for _, b := range g.boards {
					data, err := json.Marshal(struct {
						BoardID int    `json:"boardId"`
						Name    string `json:"name"`
						Key     string `json:"projectKey"`
					}{b.id, b.name, g.key})
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					children = append(children, remoteScopeNode{
						Type: "scope", ID: strconv.Itoa(b.id), Name: b.name, Data: data,
					})
				}
			}
		}
		writeJSON(w, http.StatusOK, remoteScopesResponse{Children: children})
		return
	}

	idField := scopeIDField(plugin)
	nameField := "name"
	if groupID == "" {
		nameField = "fullName"
	}

	scopeData := map[string]any{
		idField:   42,
		nameField: groupID,
	}
	raw, err := json.Marshal(scopeData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := remoteScopesResponse{
		Children: []remoteScopeNode{
			{Type: "scope", ID: groupID, Name: groupID, Data: raw},
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

func (f *fakeDevLake) registerScopes(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	plugin := r.PathValue("plugin")
	connID, _ := strconv.Atoi(r.PathValue("connID"))

	var body putScopesRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if f.scopes[plugin] == nil {
		f.scopes[plugin] = make(map[int][]json.RawMessage)
	}
	f.scopes[plugin][connID] = append(f.scopes[plugin][connID], body.Data...)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeDevLake) listConnectionScopes(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	plugin := r.PathValue("plugin")
	connID, _ := strconv.Atoi(r.PathValue("connID"))

	var details []scopeDetail
	if scopes, ok := f.scopes[plugin]; ok {
		for _, raw := range scopes[connID] {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			details = append(details, scopeDetail{Scope: m})
		}
	}
	writeJSON(w, http.StatusOK, scopeListResponse{Scopes: details, Count: int64(len(details))})
}

func (f *fakeDevLake) deleteScope(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	plugin := r.PathValue("plugin")
	connID, _ := strconv.Atoi(r.PathValue("connID"))
	scopeID := r.PathValue("scopeID")

	list := f.scopes[plugin][connID]
	kept := list[:0]
	for _, raw := range list {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if scopeIDFromMap(plugin, m) == scopeID {
			continue
		}
		kept = append(kept, raw)
	}
	if f.scopes[plugin] != nil {
		f.scopes[plugin][connID] = kept
	}
	w.WriteHeader(http.StatusOK)
}

func writeJSON(w http.ResponseWriter, code int, v any) { //nolint:unparam
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	err := json.NewEncoder(w).Encode(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// ---------------------------------------------------------------------------
// Fake ConfigStore — only the methods the Service uses
// ---------------------------------------------------------------------------

type fakeConfigStore struct {
	mu              sync.Mutex
	entityConfig    map[string]map[string]string // "type:id" -> kv
	sources         map[string][]database.SourceRow
	teams           []database.TeamRow
	failConfigBatch bool
}

func newFakeConfigStore() *fakeConfigStore {
	return &fakeConfigStore{
		entityConfig: make(map[string]map[string]string),
		sources:      make(map[string][]database.SourceRow),
	}
}

func (s *fakeConfigStore) PingContext(_ context.Context) error { return nil }

func (s *fakeConfigStore) entityKey(entityType, entityID string) string {
	return entityType + ":" + entityID
}

func (s *fakeConfigStore) GetEntityConfigKV(_ context.Context, entityType, entityID string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kv := s.entityConfig[s.entityKey(entityType, entityID)]
	if kv == nil {
		return map[string]string{}, nil
	}
	cp := make(map[string]string, len(kv))
	for k, v := range kv {
		cp[k] = v
	}
	return cp, nil
}

func (s *fakeConfigStore) SetEntityConfigKey(_ context.Context, entityType, entityID, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ek := s.entityKey(entityType, entityID)
	if s.entityConfig[ek] == nil {
		s.entityConfig[ek] = make(map[string]string)
	}
	s.entityConfig[ek][key] = value
	return nil
}

func (s *fakeConfigStore) SetEntityConfigKeys(_ context.Context, entityType, entityID string, values map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failConfigBatch {
		return fmt.Errorf("injected config write failure")
	}
	ek := s.entityKey(entityType, entityID)
	if s.entityConfig[ek] == nil {
		s.entityConfig[ek] = make(map[string]string)
	}
	for key, value := range values {
		s.entityConfig[ek][key] = value
	}
	return nil
}

func (s *fakeConfigStore) ReplaceEntityConfig(_ context.Context, entityType, entityID string, config map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entityConfig[s.entityKey(entityType, entityID)] = config
	return nil
}

func (s *fakeConfigStore) DeleteEntityConfigKey(_ context.Context, entityType, entityID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.entityConfig[s.entityKey(entityType, entityID)]; ok {
		delete(m, key)
	}
	return nil
}

func (s *fakeConfigStore) DeleteEntityConfig(_ context.Context, entityType, entityID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entityConfig, s.entityKey(entityType, entityID))
	return nil
}

func (s *fakeConfigStore) ListSourceRows(_ context.Context, teamID string) ([]database.SourceRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sources[teamID], nil
}

// Stub out remaining ConfigStore methods — not used by devlake.Service.
func (s *fakeConfigStore) ListOrgRows(context.Context) ([]database.OrgRow, error) { return nil, nil }
func (s *fakeConfigStore) GetOrgRow(context.Context, string) (*database.OrgRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) CreateOrg(context.Context, database.OrgRow) error         { return nil }
func (s *fakeConfigStore) UpdateOrg(context.Context, string, database.OrgRow) error { return nil }
func (s *fakeConfigStore) DeleteOrg(context.Context, string) error                  { return nil }
func (s *fakeConfigStore) ListProductRows(context.Context) ([]database.ProductRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) GetProductRow(context.Context, string) (*database.ProductRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) CreateProduct(context.Context, database.ProductRow) error { return nil }
func (s *fakeConfigStore) UpdateProduct(context.Context, string, database.ProductRow) error {
	return nil
}
func (s *fakeConfigStore) DeleteProduct(context.Context, string) error { return nil }
func (s *fakeConfigStore) ListTeamRows(context.Context) ([]database.TeamRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.teams, nil
}
func (s *fakeConfigStore) GetTeamRow(context.Context, string) (*database.TeamRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) CreateTeam(context.Context, database.TeamRow) error         { return nil }
func (s *fakeConfigStore) UpdateTeam(context.Context, string, database.TeamRow) error { return nil }
func (s *fakeConfigStore) DeleteTeam(context.Context, string) error                   { return nil }
func (s *fakeConfigStore) GetOrgProductLinks(context.Context, string) ([]database.RelEntry, error) {
	return nil, nil
}
func (s *fakeConfigStore) ReplaceOrgProductLinks(context.Context, string, []database.RelEntry) error {
	return nil
}
func (s *fakeConfigStore) GetProductTeamLinks(context.Context, string) ([]database.RelEntry, error) {
	return nil, nil
}
func (s *fakeConfigStore) ReplaceProductTeamLinks(context.Context, string, []database.RelEntry) error {
	return nil
}
func (s *fakeConfigStore) GetTeamProductLabels(context.Context, string) ([]string, error) {
	return nil, nil
}
func (s *fakeConfigStore) ReplaceTeamProductLabels(context.Context, string, []string) error {
	return nil
}
func (s *fakeConfigStore) ListDashboardRows(context.Context, string, string) ([]database.DashboardRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) GetDashboardRow(context.Context, int64) (*database.DashboardRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) CreateDashboard(context.Context, database.DashboardRow) (int64, error) {
	return 0, nil
}
func (s *fakeConfigStore) UpdateDashboard(context.Context, int64, database.DashboardRow) error {
	return nil
}
func (s *fakeConfigStore) DeleteDashboard(context.Context, int64) error { return nil }
func (s *fakeConfigStore) GetSourceRow(context.Context, string, string) (*database.SourceRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) CreateSource(context.Context, string, database.SourceRow) error { return nil }
func (s *fakeConfigStore) UpdateSource(context.Context, string, string, database.SourceRow) error {
	return nil
}
func (s *fakeConfigStore) DeleteSource(context.Context, string, string) error { return nil }
func (s *fakeConfigStore) GetProductOverrideRows(context.Context, string) ([]database.OverrideRepoRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) ReplaceProductOverrides(context.Context, string, []database.OverrideRepoRow) error {
	return nil
}
func (s *fakeConfigStore) ListSubscriptionRows(context.Context) ([]database.SubscriptionRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) GetSubscriptionRow(context.Context, int64) (*database.SubscriptionRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) CreateSubscription(context.Context, database.SubscriptionRow) (int64, error) {
	return 0, nil
}
func (s *fakeConfigStore) UpdateSubscription(context.Context, int64, database.SubscriptionRow) error {
	return nil
}
func (s *fakeConfigStore) UpdateSubscriptionLastRunAt(context.Context, int64, time.Time) error {
	return nil
}
func (s *fakeConfigStore) DeleteSubscription(context.Context, int64) error { return nil }
func (s *fakeConfigStore) ListAlertRows(context.Context) ([]database.AlertRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) GetAlertRow(context.Context, int64) (*database.AlertRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) CreateAlert(context.Context, database.AlertRow) (int64, error) {
	return 0, nil
}
func (s *fakeConfigStore) UpdateAlert(context.Context, int64, database.AlertRow) error { return nil }
func (s *fakeConfigStore) DeleteAlert(context.Context, int64) error                    { return nil }
func (s *fakeConfigStore) ListCannedQueryRows(context.Context) ([]database.CannedQueryRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) GetCannedQueryRow(context.Context, int64) (*database.CannedQueryRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) CreateCannedQuery(context.Context, database.CannedQueryRow) (int64, error) {
	return 0, nil
}
func (s *fakeConfigStore) UpdateCannedQuery(context.Context, int64, database.CannedQueryRow) error {
	return nil
}
func (s *fakeConfigStore) DeleteCannedQuery(context.Context, int64) error { return nil }
func (s *fakeConfigStore) ListCannedScriptRows(context.Context) ([]database.CannedScriptRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) GetCannedScriptRow(context.Context, int64) (*database.CannedScriptRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) CreateCannedScript(context.Context, database.CannedScriptRow) (int64, error) {
	return 0, nil
}
func (s *fakeConfigStore) UpdateCannedScript(context.Context, int64, database.CannedScriptRow) error {
	return nil
}
func (s *fakeConfigStore) DeleteCannedScript(context.Context, int64) error { return nil }
func (s *fakeConfigStore) ListJsonSchemaRows(context.Context) ([]database.JsonSchemaRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) GetJsonSchemaRow(context.Context, int64) (*database.JsonSchemaRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) CreateJsonSchema(context.Context, database.JsonSchemaRow) (int64, error) {
	return 0, nil
}
func (s *fakeConfigStore) UpdateJsonSchema(context.Context, int64, database.JsonSchemaRow) error {
	return nil
}
func (s *fakeConfigStore) DeleteJsonSchema(context.Context, int64) error { return nil }
func (s *fakeConfigStore) GetJsonSchemaByName(context.Context, string) (*database.JsonSchemaRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) ListAllStoredProcedureRows(context.Context) ([]database.StoredProcedureRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) ListStoredProcedureRows(context.Context, int64) ([]database.StoredProcedureRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) GetStoredProcedureRow(context.Context, int64) (*database.StoredProcedureRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) CreateStoredProcedure(context.Context, database.StoredProcedureRow) (int64, error) {
	return 0, nil
}
func (s *fakeConfigStore) UpdateStoredProcedure(context.Context, int64, database.StoredProcedureRow) error {
	return nil
}
func (s *fakeConfigStore) DeleteStoredProcedure(context.Context, int64) error { return nil }
func (s *fakeConfigStore) ListWorkflowRows(context.Context) ([]database.WorkflowRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) GetWorkflowRow(context.Context, int64) (*database.WorkflowRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) GetWorkflowByName(context.Context, string) (*database.WorkflowRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) CreateWorkflow(context.Context, database.WorkflowRow) (int64, error) {
	return 0, nil
}
func (s *fakeConfigStore) UpdateWorkflow(context.Context, int64, database.WorkflowRow) error {
	return nil
}
func (s *fakeConfigStore) DeleteWorkflow(context.Context, int64) error { return nil }
func (s *fakeConfigStore) SaveWorkflowDraft(context.Context, int64, database.WorkflowRow) error {
	return nil
}
func (s *fakeConfigStore) PublishWorkflow(context.Context, int64, string) (*database.WorkflowRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) ListWorkflowVersions(context.Context, int64) ([]database.WorkflowVersionRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) GetWorkflowVersion(context.Context, int64, int) (*database.WorkflowVersionRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) SetProductionVersion(context.Context, int64, int) error { return nil }
func (s *fakeConfigStore) ArchiveWorkflow(context.Context, int64) error           { return nil }
func (s *fakeConfigStore) UnarchiveWorkflow(context.Context, int64) error         { return nil }
func (s *fakeConfigStore) GetWorkflowByNameAndVersion(context.Context, string, int) (*database.WorkflowRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) GetProductionWorkflowByName(context.Context, string) (*database.WorkflowRow, error) {
	return nil, nil
}
func (s *fakeConfigStore) GetOrgForProduct(context.Context, string) (string, error) { return "", nil }
func (s *fakeConfigStore) ListAllTeamProductLinks(context.Context) ([]database.TeamProductLink, error) {
	return nil, nil
}
func (s *fakeConfigStore) ValidateAPIKey(context.Context, string) (*database.APIKeyInfo, error) {
	return nil, nil
}
func (s *fakeConfigStore) ListAPIKeys(context.Context) ([]database.APIKeyInfo, error) {
	return nil, nil
}
func (s *fakeConfigStore) CreateAPIKey(context.Context, string, string, bool, string) (int64, error) {
	return 0, nil
}
func (s *fakeConfigStore) DeleteAPIKey(context.Context, int64) error { return nil }
func (s *fakeConfigStore) ClearConfig(context.Context) error         { return nil }
func (s *fakeConfigStore) ClearSeeds(context.Context) error          { return nil }
func (s *fakeConfigStore) ClearWorkflows(context.Context) error      { return nil }

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func setupTestService(t *testing.T) (*Service, *fakeDevLake, *fakeConfigStore) {
	t.Helper()
	fake := newFakeDevLake()
	ts := httptest.NewServer(fake.handler())
	t.Cleanup(ts.Close)

	store := newFakeConfigStore()
	cfg := Config{
		APIURL:                ts.URL,
		APIKey:                "test-key",
		CronConfig:            "0 0 * * *",
		AllowPrivateEndpoints: true,
	}
	svc := NewService(cfg, ts.Client(), store)
	return svc, fake, store
}

func addTeamSources(store *fakeConfigStore, teamID string, sources []database.SourceRow) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.sources[teamID] = sources
}

func setConnectionID(t *testing.T, store *fakeConfigStore, teamID, plugin string, connID int) {
	err := store.SetEntityConfigKey(context.Background(), "team", teamID, "devlake_"+plugin+"_connection_id", strconv.Itoa(connID))
	if err != nil {
		t.Errorf("SetEntityConfigKey: %v", err)
	}
}

func addTeam(store *fakeConfigStore, id string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.teams = append(store.teams, database.TeamRow{ID: id, Name: id})
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCreateTeamConnection(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	connID, err := svc.CreateTeamConnection(ctx, "alpha", ConnectionEntry{
		Plugin:     "github",
		Token:      "ghp_test123",
		Endpoint:   "https://api.github.com/",
		ExpiryDate: "2027-06-15",
	})
	if err != nil {
		t.Fatalf("CreateTeamConnection: %v", err)
	}
	if connID == 0 {
		t.Fatal("expected non-zero connection ID")
	}

	fake.mu.Lock()
	conns := fake.connections["github"]
	fake.mu.Unlock()
	if len(conns) != 1 {
		t.Fatalf("expected 1 github connection, got %d", len(conns))
	}
	if !strings.HasPrefix(conns[0].Name, "lfmbot-alpha-github") {
		t.Errorf("connection name = %q, want prefix lfmbot-alpha-github", conns[0].Name)
	}

	kv, _ := store.GetEntityConfigKV(ctx, "team", "alpha")
	if kv["devlake_github_connection_id"] != strconv.Itoa(connID) {
		t.Errorf("stored connection ID = %q, want %d", kv["devlake_github_connection_id"], connID)
	}
	if kv["devlake_github_token_expiry"] != "2027-06-15" {
		t.Errorf("stored expiry = %q, want 2027-06-15", kv["devlake_github_token_expiry"])
	}
}

func TestCreateTeamConnectionReusesGitHubConnectionWithRequiredFields(t *testing.T) {
	svc, fake, _ := setupTestService(t)
	ctx := context.Background()
	entry := ConnectionEntry{Plugin: "github", Token: "ghp-token", Endpoint: "https://api.github.com"}

	firstID, err := svc.CreateTeamConnection(ctx, "alpha", entry)
	if err != nil {
		t.Fatalf("create initial connection: %v", err)
	}
	secondID, err := svc.CreateTeamConnection(ctx, "alpha", entry)
	if err != nil {
		t.Fatalf("reuse existing connection: %v", err)
	}
	if secondID != firstID {
		t.Fatalf("reused connection ID = %d, want %d", secondID, firstID)
	}

	fake.mu.Lock()
	patch := fake.connectionPatches["github"][firstID]
	fake.mu.Unlock()
	if patch["name"] != "lfmbot-alpha-github" {
		t.Errorf("connection name in patch = %v, want lfmbot-alpha-github", patch["name"])
	}
	if patch["endpoint"] != entry.Endpoint {
		t.Errorf("connection endpoint in patch = %v, want %s", patch["endpoint"], entry.Endpoint)
	}
}

func TestCodecovConnectionSyncAndTrigger(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()
	entry := ConnectionEntry{
		Plugin: "codecov", Token: "coverage-secret", Endpoint: "https://api.codecov.io",
		Organization: "konflux-ci", Service: "github", ExpiryDate: "never",
	}
	if _, err := svc.TestConnection(ctx, entry); err != nil {
		t.Fatalf("test Codecov connection: %v", err)
	}
	connID, err := svc.CreateTeamConnection(ctx, "konflux", entry)
	if err != nil {
		t.Fatalf("create Codecov connection: %v", err)
	}
	if _, err := svc.CreateTeamConnection(ctx, "konflux", ConnectionEntry{
		Plugin: "github", Token: "github-secret", Endpoint: "https://api.github.com/", ExpiryDate: "never",
	}); err != nil {
		t.Fatalf("create GitHub connection: %v", err)
	}
	config, err := store.GetEntityConfigKV(ctx, "team", "konflux")
	if err != nil {
		t.Fatal(err)
	}
	if config["devlake_codecov_token"] != "" {
		t.Fatal("Codecov token must not be stored in dashboard config")
	}
	if config["devlake_codecov_organization"] != "konflux-ci" || config["devlake_codecov_service"] != "github" {
		t.Fatalf("stored Codecov metadata = %v", config)
	}
	addTeamSources(store, "konflux", []database.SourceRow{{
		ID: "gitrepos", Type: "gitrepos", Repos: []database.RepoRow{{
			ID: "konflux-ui", Type: "github", Owner: "konflux-ci", Name: "konflux-ui",
		}},
	}})
	syncResult, err := svc.SyncTeam(ctx, "konflux")
	if err != nil {
		t.Fatalf("sync team: %v", err)
	}
	if len(syncResult.Errors) != 0 {
		t.Fatalf("sync errors: %v", syncResult.Errors)
	}
	fake.mu.Lock()
	connection := fake.connectionBodies["codecov"][connID]
	blueprints := append([]Blueprint(nil), fake.blueprints...)
	fake.mu.Unlock()
	if connection["organization"] != "konflux-ci" || connection["service"] != "github" || connection["token"] != "coverage-secret" {
		t.Fatalf("Codecov connection payload is missing required fields: %v", connection)
	}
	var codecovBlueprint *Blueprint
	for i := range blueprints {
		if blueprints[i].Name == "lfmbot-konflux-codecov-konflux-ci" {
			codecovBlueprint = &blueprints[i]
		}
	}
	if codecovBlueprint == nil {
		t.Fatalf("Codecov blueprint not created: %v", syncResult.Created)
	}
	if !hasLabel(codecovBlueprint.Labels, "team:konflux") || len(codecovBlueprint.Connections) != 1 ||
		codecovBlueprint.Connections[0].PluginName != "codecov" ||
		len(codecovBlueprint.Connections[0].Scopes) != 1 ||
		codecovBlueprint.Connections[0].Scopes[0].ScopeID != "konflux-ci/konflux-ui" {
		t.Fatalf("unexpected Codecov blueprint: %+v", codecovBlueprint)
	}
	triggerResult, err := svc.TriggerTeam(ctx, "konflux")
	if err != nil {
		t.Fatalf("trigger team: %v", err)
	}
	if len(triggerResult.Triggered) != 2 || len(triggerResult.Errors) != 0 {
		t.Fatalf("unexpected trigger result: %+v", triggerResult)
	}
	if err := svc.UpdateTeamConnection(ctx, "konflux", ConnectionEntry{Plugin: "codecov", Token: "rotated-secret"}); err != nil {
		t.Fatalf("rotate Codecov token: %v", err)
	}
	config, _ = store.GetEntityConfigKV(ctx, "team", "konflux")
	if config["devlake_codecov_token"] != "" {
		t.Fatal("rotated Codecov token must not be stored in dashboard config")
	}
	fake.mu.Lock()
	rotatedToken := fake.connectionBodies["codecov"][connID]["token"]
	fake.mu.Unlock()
	if rotatedToken != "rotated-secret" {
		t.Fatalf("DevLake token was not updated: %v", rotatedToken)
	}
}

func TestCodecovOnlyTeamSyncAndTrigger(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()
	connID, err := svc.CreateTeamConnection(ctx, "coverage-only", ConnectionEntry{
		Plugin: "codecov", Token: "coverage-secret", Endpoint: "https://api.codecov.io",
		Organization: "konflux-ci", Service: "github", ExpiryDate: "never",
	})
	if err != nil {
		t.Fatal(err)
	}
	addTeamSources(store, "coverage-only", []database.SourceRow{{
		ID: "gitrepos", Type: "gitrepos", Repos: []database.RepoRow{{
			ID: "konflux-ui", Type: "github", Owner: "konflux-ci", Name: "konflux-ui",
		}},
	}})
	result, err := svc.SyncTeam(ctx, "coverage-only")
	if err != nil || len(result.Errors) != 0 || len(result.Created) != 1 {
		t.Fatalf("sync Codecov-only team: %+v, %v", result, err)
	}
	fake.mu.Lock()
	blueprints := append([]Blueprint(nil), fake.blueprints...)
	fake.mu.Unlock()
	if len(blueprints) != 1 || blueprints[0].Connections[0].PluginName != "codecov" ||
		blueprints[0].Connections[0].ConnectionID != connID ||
		blueprints[0].Connections[0].Scopes[0].ScopeID != "konflux-ci/konflux-ui" {
		t.Fatalf("unexpected Codecov-only blueprint: %+v", blueprints)
	}
	triggered, err := svc.TriggerTeam(ctx, "coverage-only")
	if err != nil || len(triggered.Errors) != 0 || len(triggered.Triggered) != 1 {
		t.Fatalf("trigger Codecov-only team: %+v, %v", triggered, err)
	}
}

func TestAttachExistingCodecovBlueprint(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()
	connID, err := svc.client.CreateConnection(ctx, "codecov", "test codecov", "https://api.codecov.io", "coverage-secret", map[string]string{
		"organization": "konflux-ci", "service": "github",
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.client.CreateBlueprint(ctx, Blueprint{
		Name: "test-Blueprint", Mode: "NORMAL", Enable: true, CronConfig: "0 0 * * *",
		Connections: []Connection{{PluginName: "codecov", ConnectionID: connID, Scopes: []Scope{{ScopeID: "konflux-ci/konflux-ui"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A blueprint can only reference a scope that exists on its connection.
	scopeData, err := json.Marshal(map[string]any{
		"codecovId": "konflux-ci/konflux-ui", "fullName": "konflux-ci/konflux-ui", "name": "konflux-ui",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.client.RegisterScopes(ctx, "codecov", connID, []json.RawMessage{scopeData}); err != nil {
		t.Fatal(err)
	}
	addTeamSources(store, "konflux", []database.SourceRow{{
		ID: "gitrepos", Type: "gitrepos", Repos: []database.RepoRow{{
			ID: "konflux-ui", Type: "github", Owner: "konflux-ci", Name: "konflux-ui",
		}},
	}})
	if _, err := svc.CreateTeamConnection(ctx, "konflux", ConnectionEntry{
		Plugin: "github", Token: "github-secret", Endpoint: "https://api.github.com/", ExpiryDate: "never",
	}); err != nil {
		t.Fatal(err)
	}
	available, err := svc.AvailableCodecov(ctx)
	if err != nil || len(available.Blueprints) != 1 || available.Blueprints[0].ID != created.ID || len(available.Connections) != 1 {
		t.Fatalf("available Codecov resources: %+v, %v", available, err)
	}
	if err := svc.AttachCodecov(ctx, "konflux", AttachCodecovRequest{ConnectionID: connID, BlueprintID: created.ID}); err != nil {
		t.Fatalf("attach Codecov blueprint: %v", err)
	}
	config, _ := store.GetEntityConfigKV(ctx, "team", "konflux")
	if config["devlake_codecov_connection_id"] != strconv.Itoa(connID) || config["devlake_codecov_token"] != "" {
		t.Fatalf("unexpected Codecov config: %v", config)
	}
	result, err := svc.SyncTeam(ctx, "konflux")
	if err != nil || len(result.Errors) != 0 || len(result.Created) != 1 || len(result.Deleted) != 0 {
		t.Fatalf("sync attached blueprint: %+v, %v", result, err)
	}
	triggered, err := svc.TriggerTeam(ctx, "konflux")
	if err != nil || len(triggered.Triggered) != 2 || !slices.Contains(triggered.Triggered, "test-Blueprint") {
		t.Fatalf("trigger attached blueprint: %+v, %v", triggered, err)
	}
	cleanup, err := svc.CleanupTeam(ctx, "konflux")
	if err != nil || len(cleanup.DeletedBlueprints) != 1 || slices.Contains(cleanup.DeletedConnections, fmt.Sprintf("codecov (conn %d)", connID)) {
		t.Fatalf("cleanup attached blueprint: %+v, %v", cleanup, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.blueprints) != 1 || fake.blueprints[0].Name != "test-Blueprint" ||
		fake.blueprints[0].CronConfig != "0 0 * * *" || hasLabel(fake.blueprints[0].Labels, "team:konflux") ||
		len(fake.connections["codecov"]) != 1 || len(fake.scopes["codecov"][connID]) != 1 {
		t.Fatalf("manual resources were changed or deleted: blueprints=%+v connections=%+v", fake.blueprints, fake.connections["codecov"])
	}
}

func TestAttachCodecovRejectsUnrelatedScope(t *testing.T) {
	svc, _, store := setupTestService(t)
	ctx := context.Background()
	connID, err := svc.client.CreateConnection(ctx, "codecov", "test codecov", "https://api.codecov.io", "coverage-secret", map[string]string{
		"organization": "konflux-ci", "service": "github",
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.client.CreateBlueprint(ctx, Blueprint{
		Name: "other", Connections: []Connection{{PluginName: "codecov", ConnectionID: connID, Scopes: []Scope{{ScopeID: "konflux-ci/other"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	addTeamSources(store, "konflux", []database.SourceRow{{
		ID: "gitrepos", Type: "gitrepos", Repos: []database.RepoRow{{ID: "konflux-ui", Type: "github", Owner: "konflux-ci", Name: "konflux-ui"}},
	}})
	if err := svc.AttachCodecov(ctx, "konflux", AttachCodecovRequest{ConnectionID: connID, BlueprintID: created.ID}); err == nil {
		t.Fatal("expected scope validation failure")
	}
	config, _ := store.GetEntityConfigKV(ctx, "team", "konflux")
	if config["devlake_codecov_connection_id"] != "" {
		t.Fatal("rejected attachment stored a connection ID")
	}
}

func TestAttachCodecovRejectsScopeOwnedByAnotherBlueprint(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()
	connID, err := svc.client.CreateConnection(ctx, "codecov", "shared", "https://api.codecov.io", "coverage-secret", map[string]string{
		"organization": "konflux-ci", "service": "github",
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{ScopeID: "konflux-ci/konflux-ui"}
	owned, err := svc.client.CreateBlueprint(ctx, Blueprint{
		Name: "owned", Labels: []string{"managed:lfmbot", "team:other"},
		Connections: []Connection{{PluginName: "codecov", ConnectionID: connID, Scopes: []Scope{scope}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	manual, err := svc.client.CreateBlueprint(ctx, Blueprint{
		Name: "manual", Connections: []Connection{{PluginName: "codecov", ConnectionID: connID, Scopes: []Scope{scope}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	addTeamSources(store, "konflux", []database.SourceRow{{
		ID: "gitrepos", Type: "gitrepos", Repos: []database.RepoRow{{ID: "repo", Type: "github", Owner: "konflux-ci", Name: "konflux-ui"}},
	}})
	if err := svc.AttachCodecov(ctx, "konflux", AttachCodecovRequest{ConnectionID: connID, BlueprintID: manual.ID}); err == nil || !strings.Contains(err.Error(), "already belongs") {
		t.Fatalf("attach error = %v, want scope ownership conflict", err)
	}
	config, _ := store.GetEntityConfigKV(ctx, "team", "konflux")
	if config["devlake_codecov_connection_id"] != "" {
		t.Fatal("rejected attachment stored a connection ID")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.blueprints) != 2 || fake.blueprints[0].ID != owned.ID || hasTeamLabel(fake.blueprints[1].Labels) {
		t.Fatalf("rejected attachment changed blueprints: %+v", fake.blueprints)
	}
}

func TestAttachCodecovRejectsScopeOwnedAcrossConnections(t *testing.T) {
	svc, _, store := setupTestService(t)
	ctx := context.Background()
	connA, err := svc.client.CreateConnection(ctx, "codecov", "org-a", "https://api.codecov.io", "secret-a", map[string]string{
		"organization": "konflux-ci", "service": "github",
	})
	if err != nil {
		t.Fatal(err)
	}
	connB, err := svc.client.CreateConnection(ctx, "codecov", "org-b", "https://api.codecov.io", "secret-b", map[string]string{
		"organization": "konflux-ci", "service": "github",
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{ScopeID: "konflux-ci/konflux-ui"}
	_, err = svc.client.CreateBlueprint(ctx, Blueprint{
		Name: "owned-on-conn-a", Labels: []string{"managed:lfmbot", "team:other"},
		Connections: []Connection{{PluginName: "codecov", ConnectionID: connA, Scopes: []Scope{scope}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	manual, err := svc.client.CreateBlueprint(ctx, Blueprint{
		Name:        "manual-on-conn-b",
		Connections: []Connection{{PluginName: "codecov", ConnectionID: connB, Scopes: []Scope{scope}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	addTeamSources(store, "konflux", []database.SourceRow{{
		ID: "gitrepos", Type: "gitrepos", Repos: []database.RepoRow{{ID: "repo", Type: "github", Owner: "konflux-ci", Name: "konflux-ui"}},
	}})
	if err := svc.AttachCodecov(ctx, "konflux", AttachCodecovRequest{ConnectionID: connB, BlueprintID: manual.ID}); err == nil || !strings.Contains(err.Error(), "already belongs") {
		t.Fatalf("attach error = %v, want cross-connection scope ownership conflict", err)
	}
}

func TestStoreConnectionConfigFailureDoesNotPersistPartialState(t *testing.T) {
	svc, _, store := setupTestService(t)
	store.failConfigBatch = true
	_, err := svc.storeConnectionConfig(context.Background(), "team", ConnectionEntry{
		Plugin: "codecov", Endpoint: "https://api.codecov.io", Organization: "konflux-ci", Service: "github", ExpiryDate: "never",
	}, 42)
	if err == nil {
		t.Fatal("expected config write failure")
	}
	config, _ := store.GetEntityConfigKV(context.Background(), "team", "team")
	if len(config) != 0 {
		t.Fatalf("partial config persisted: %+v", config)
	}
}

func TestSyncTeamInvalidCodecovConnectionKeepsManagedBlueprint(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()
	if err := store.SetEntityConfigKey(ctx, "team", "konflux", "devlake_codecov_connection_id", "invalid"); err != nil {
		t.Fatal(err)
	}
	created, err := svc.client.CreateBlueprint(ctx, Blueprint{
		Name: "lfmbot-konflux-codecov-konflux-ci", Labels: []string{"managed:lfmbot", "team:konflux"},
		Connections: []Connection{{PluginName: "codecov", ConnectionID: 42, Scopes: []Scope{{ScopeID: "konflux-ci/konflux-ui"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncTeam(ctx, "konflux"); err == nil || !strings.Contains(err.Error(), "lookup codecov connection") {
		t.Fatalf("sync error = %v, want invalid Codecov connection", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.blueprints) != 1 || fake.blueprints[0].ID != created.ID {
		t.Fatalf("sync deleted managed blueprint after config error: %+v", fake.blueprints)
	}
}

func TestCleanupCodeCovDoesNotSyncUnrelatedGitTeam(t *testing.T) {
	svc, _, store := setupTestService(t)
	ctx := context.Background()
	addTeam(store, "owner")
	addTeam(store, "unrelated")
	if _, err := svc.CreateTeamConnection(ctx, "owner", ConnectionEntry{
		Plugin: "codecov", Token: "coverage-secret", Endpoint: "https://api.codecov.io",
		Organization: "konflux-ci", Service: "github", ExpiryDate: "never",
	}); err != nil {
		t.Fatal(err)
	}
	addTeamSources(store, "owner", []database.SourceRow{{
		ID: "gitrepos", Type: "gitrepos", Repos: []database.RepoRow{{ID: "repo", Type: "github", Owner: "konflux-ci", Name: "konflux-ui"}},
	}})
	addTeamSources(store, "unrelated", []database.SourceRow{{
		ID: "gitrepos", Type: "gitrepos", Repos: []database.RepoRow{{ID: "repo", Type: "github", Owner: "another-org", Name: "other-repo"}},
	}})
	if err := store.SetEntityConfigKey(ctx, "team", "unrelated", "devlake_codecov_connection_id", "invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncTeam(ctx, "owner"); err != nil {
		t.Fatal(err)
	}
	cleanup, err := svc.CleanupTeam(ctx, "owner")
	if err != nil || len(cleanup.Migrated) != 0 {
		t.Fatalf("unexpected cleanup result: %+v, %v", cleanup, err)
	}
	for _, cleanupErr := range cleanup.Errors {
		if strings.Contains(cleanupErr, "re-sync unrelated") {
			t.Fatalf("unrelated team was re-synced: %+v", cleanup)
		}
	}
}

func TestUpdateTeamConnection(t *testing.T) {
	svc, _, store := setupTestService(t)
	ctx := context.Background()

	connID, err := svc.CreateTeamConnection(ctx, "beta", ConnectionEntry{
		Plugin:     "gitlab",
		Token:      "glpat-old",
		Endpoint:   "https://gitlab.com/api/v4",
		ExpiryDate: "2026-12-01",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = svc.UpdateTeamConnection(ctx, "beta", ConnectionEntry{
		Plugin:     "gitlab",
		Token:      "glpat-new",
		Endpoint:   "https://gitlab.com/api/v4",
		ExpiryDate: "2027-09-01",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	kv, _ := store.GetEntityConfigKV(ctx, "team", "beta")
	if kv["devlake_gitlab_connection_id"] != strconv.Itoa(connID) {
		t.Error("connection ID should not change on update")
	}
	if kv["devlake_gitlab_token_expiry"] != "2027-09-01" {
		t.Errorf("expiry not updated: got %q", kv["devlake_gitlab_token_expiry"])
	}
}

func TestUpdateTeamGitHubConnectionPreservesRequiredFields(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	connID, err := svc.CreateTeamConnection(ctx, "beta", ConnectionEntry{
		Plugin:   "github",
		Token:    "ghp-old",
		Endpoint: "https://api.github.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.UpdateTeamConnection(ctx, "beta", ConnectionEntry{
		Plugin:     "github",
		Token:      "ghp-new",
		ExpiryDate: "never",
	}); err != nil {
		t.Fatalf("update token-only entry: %v", err)
	}

	fake.mu.Lock()
	patch := fake.connectionPatches["github"][connID]
	fake.mu.Unlock()
	if got := patch["name"]; got != "lfmbot-beta-github" {
		t.Errorf("connection name = %v, want lfmbot-beta-github", got)
	}
	if got := patch["endpoint"]; got != "https://api.github.com" {
		t.Errorf("connection endpoint = %v, want https://api.github.com", got)
	}
	if got := patch["token"]; got != "ghp-new" {
		t.Errorf("connection token = %v, want ghp-new", got)
	}
	kv, _ := store.GetEntityConfigKV(ctx, "team", "beta")
	if kv["devlake_github_token"] != "ghp-new" {
		t.Errorf("stored token = %q, want ghp-new", kv["devlake_github_token"])
	}

	newEndpoint := "https://github.example/api/v3"
	if err := svc.UpdateTeamConnection(ctx, "beta", ConnectionEntry{
		Plugin: "github", Token: "ghp-new", Endpoint: newEndpoint, ExpiryDate: "never",
	}); err != nil {
		t.Fatalf("update endpoint explicitly: %v", err)
	}
	fake.mu.Lock()
	patch = fake.connectionPatches["github"][connID]
	fake.mu.Unlock()
	if got := patch["endpoint"]; got != newEndpoint {
		t.Errorf("explicit connection endpoint = %v, want %s", got, newEndpoint)
	}
}

func TestGetTokenStatus(t *testing.T) {
	svc, _, store := setupTestService(t)
	ctx := context.Background()

	setConnectionID(t, store, "gamma", "github", 10)
	if err := store.SetEntityConfigKey(ctx, "team", "gamma", "devlake_github_token_expiry", "2027-03-15"); err != nil {
		t.Errorf("set entity config key: %v", err)
	}
	setConnectionID(t, store, "gamma", "jira", 20)
	if err := store.SetEntityConfigKey(ctx, "team", "gamma", "devlake_jira_token_expiry", "2026-11-01"); err != nil {
		t.Errorf("set entity config key: %v", err)
	}

	statuses, err := svc.GetTokenStatus(ctx, "gamma")
	if err != nil {
		t.Fatalf("GetTokenStatus: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	byPlugin := make(map[string]TokenStatus)
	for _, s := range statuses {
		byPlugin[s.Plugin] = s
	}
	if byPlugin["github"].ExpiryDate != "2027-03-15" {
		t.Errorf("github expiry = %q", byPlugin["github"].ExpiryDate)
	}
	if byPlugin["jira"].ConnID != 20 {
		t.Errorf("jira connID = %d, want 20", byPlugin["jira"].ConnID)
	}
}

func TestSyncTeam_CreatesBlueprints(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	connID, _ := svc.CreateTeamConnection(ctx, "team1", ConnectionEntry{
		Plugin:     "github",
		Token:      "ghp_tok",
		Endpoint:   "https://api.github.com/",
		ExpiryDate: "2027-01-01",
	})
	_ = connID

	addTeamSources(store, "team1", []database.SourceRow{{
		ID:   "src1",
		Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "myorg", Name: "repo-a"},
			{ID: "r2", Type: "github", Owner: "myorg", Name: "repo-b"},
		},
	}})

	result, err := svc.SyncTeam(ctx, "team1")
	if err != nil {
		t.Fatalf("SyncTeam: %v", err)
	}

	if len(result.Created) != 1 {
		t.Fatalf("expected 1 created blueprint, got %d: %v", len(result.Created), result.Created)
	}
	if !strings.HasPrefix(result.Created[0], "lfmbot-team1-github-myorg") {
		t.Errorf("created blueprint = %q, want lfmbot-team1-github-myorg", result.Created[0])
	}

	fake.mu.Lock()
	bps := fake.blueprints
	fake.mu.Unlock()
	if len(bps) != 1 {
		t.Fatalf("expected 1 blueprint in DevLake, got %d", len(bps))
	}
	bp := bps[0]
	if !hasLabel(bp.Labels, "managed:lfmbot") {
		t.Errorf("blueprint missing managed:lfmbot label, got %v", bp.Labels)
	}
	if !hasLabel(bp.Labels, "team:team1") {
		t.Errorf("blueprint missing team:team1 label, got %v", bp.Labels)
	}
	if len(bp.Connections) != 1 || len(bp.Connections[0].Scopes) != 2 {
		t.Errorf("expected 1 connection with 2 scopes, got %+v", bp.Connections)
	}
	if !bp.SkipFailedTasks {
		t.Errorf("blueprint should have SkipFailedTasks=true")
	}
}

func TestSyncTeam_UpgradesExistingBlueprintSkipFailedTasks(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	_, err := svc.CreateTeamConnection(ctx, "team-upgrade", ConnectionEntry{
		Plugin:     "github",
		Token:      "ghp_tok",
		Endpoint:   "https://api.github.com/",
		ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Fatalf("CreateTeamConnection: %v", err)
	}

	addTeamSources(store, "team-upgrade", []database.SourceRow{{
		ID:   "src1",
		Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "orgX", Name: "repo1"},
		},
	}})

	fake.mu.Lock()
	fake.blueprints = append(fake.blueprints, Blueprint{
		ID:              99,
		Name:            "lfmbot-team-upgrade-github-orgX",
		Mode:            "NORMAL",
		Enable:          true,
		Labels:          []string{"managed:lfmbot", "team:team-upgrade"},
		SkipFailedTasks: false,
		Connections: []Connection{{
			PluginName:   "github",
			ConnectionID: 1,
			Scopes:       []Scope{{ScopeID: "orgX/repo1"}},
		}},
	})
	fake.mu.Unlock()

	result, err := svc.SyncTeam(ctx, "team-upgrade")
	if err != nil {
		t.Fatalf("SyncTeam: %v", err)
	}

	if len(result.Updated) != 1 {
		t.Fatalf("expected 1 updated blueprint, got created=%v updated=%v", result.Created, result.Updated)
	}

	fake.mu.Lock()
	var found *Blueprint
	for i := range fake.blueprints {
		if fake.blueprints[i].Name == "lfmbot-team-upgrade-github-orgX" {
			found = &fake.blueprints[i]
			break
		}
	}
	fake.mu.Unlock()

	if found == nil {
		t.Fatal("blueprint not found after update")
	}
	if !found.SkipFailedTasks {
		t.Errorf("existing blueprint should be upgraded to SkipFailedTasks=true")
	}
}

func TestSyncTeam_UpdatesBlueprintOnScopeChange(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	_, err := svc.CreateTeamConnection(ctx, "team2", ConnectionEntry{
		Plugin:     "github",
		Token:      "ghp_tok",
		Endpoint:   "https://api.github.com/",
		ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Errorf("CreateTeamConnection: %v", err)
	}

	addTeamSources(store, "team2", []database.SourceRow{{
		ID:   "src1",
		Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "orgA", Name: "repo1"},
		},
	}})

	result1, err := svc.SyncTeam(ctx, "team2")
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if len(result1.Created) != 1 {
		t.Fatalf("first sync: expected 1 created, got %v", result1.Created)
	}

	addTeamSources(store, "team2", []database.SourceRow{{
		ID:   "src1",
		Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "orgA", Name: "repo1"},
			{ID: "r2", Type: "github", Owner: "orgA", Name: "repo2"},
		},
	}})

	result2, err := svc.SyncTeam(ctx, "team2")
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(result2.Updated) != 1 {
		t.Fatalf("second sync: expected 1 updated, got created=%v updated=%v", result2.Created, result2.Updated)
	}

	fake.mu.Lock()
	bps := fake.blueprints
	fake.mu.Unlock()
	if len(bps) != 1 {
		t.Fatalf("expected 1 blueprint after update, got %d", len(bps))
	}
	if len(bps[0].Connections[0].Scopes) != 2 {
		t.Errorf("expected 2 scopes after update, got %d", len(bps[0].Connections[0].Scopes))
	}
}

func TestSyncTeam_DeletesBlueprintWhenSourceRemoved(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	_, err := svc.CreateTeamConnection(ctx, "team3", ConnectionEntry{
		Plugin:     "github",
		Token:      "ghp_tok",
		Endpoint:   "https://api.github.com/",
		ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Errorf("CreateTeamConnection: %v", err)
	}

	addTeamSources(store, "team3", []database.SourceRow{{
		ID:   "src1",
		Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "orgX", Name: "repo1"},
		},
	}})

	_, err = svc.SyncTeam(ctx, "team3")
	if err != nil {
		t.Errorf("SyncTeam: %v", err)
	}

	addTeamSources(store, "team3", nil)

	result, err := svc.SyncTeam(ctx, "team3")
	if err != nil {
		t.Fatalf("SyncTeam: %v", err)
	}
	if len(result.Deleted) != 1 {
		t.Fatalf("expected 1 deleted, got %v", result.Deleted)
	}

	fake.mu.Lock()
	bps := fake.blueprints
	fake.mu.Unlock()
	if len(bps) != 0 {
		t.Errorf("expected 0 blueprints after delete, got %d", len(bps))
	}
}

func TestSyncTeam_NoopWhenUnchanged(t *testing.T) {
	svc, _, store := setupTestService(t)
	ctx := context.Background()

	_, err := svc.CreateTeamConnection(ctx, "team4", ConnectionEntry{
		Plugin:     "github",
		Token:      "ghp_tok",
		Endpoint:   "https://api.github.com/",
		ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Errorf("CreateTeamConnection: %v", err)
	}

	addTeamSources(store, "team4", []database.SourceRow{{
		ID:   "src1",
		Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "orgZ", Name: "repo1"},
		},
	}})

	_, err = svc.SyncTeam(ctx, "team4")
	if err != nil {
		t.Errorf("SyncTeam: %v", err)
	}

	result, err := svc.SyncTeam(ctx, "team4")
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(result.Created)+len(result.Updated)+len(result.Deleted) != 0 {
		t.Errorf("expected no changes, got created=%v updated=%v deleted=%v",
			result.Created, result.Updated, result.Deleted)
	}
}

func TestSyncTeam_DedupSkipsScopeOwnedByOtherTeam(t *testing.T) {
	svc, _, store := setupTestService(t)
	ctx := context.Background()

	// Shared connection: the same {plugin, endpoint, scopeID} key collides for
	// both teams, so team B's copy of the scope must be skipped.
	connID, _ := svc.CreateTeamConnection(ctx, "teamA", ConnectionEntry{
		Plugin: "github", Token: "tok1", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01",
	})
	setConnectionID(t, store, "teamB", "github", connID)

	sharedRepo := database.RepoRow{ID: "r1", Type: "github", Owner: "shared-org", Name: "shared-repo"}
	addTeamSources(store, "teamA", []database.SourceRow{{
		ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{sharedRepo},
	}})

	resultA, err := svc.SyncTeam(ctx, "teamA")
	if err != nil {
		t.Fatalf("sync teamA: %v", err)
	}
	if len(resultA.Created) != 1 {
		t.Fatalf("teamA: expected 1 created, got %v", resultA.Created)
	}

	addTeamSources(store, "teamB", []database.SourceRow{{
		ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{sharedRepo},
	}})

	resultB, err := svc.SyncTeam(ctx, "teamB")
	if err != nil {
		t.Fatalf("sync teamB: %v", err)
	}
	if len(resultB.Skipped) != 1 {
		t.Fatalf("teamB: expected 1 skipped, got %v", resultB.Skipped)
	}
	if len(resultB.Created) != 0 {
		t.Errorf("teamB: should not create blueprint for duplicate scope, got %v", resultB.Created)
	}
}

func TestSyncTeam_DedupSkipsScopeOwnedByOtherTeamSeparateConnections(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	// Each team gets its own connection to the same provider instance.
	connA, err := svc.CreateTeamConnection(ctx, "teamA", ConnectionEntry{
		Plugin: "github", Token: "tokA", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Fatalf("create teamA connection: %v", err)
	}
	connB, err := svc.CreateTeamConnection(ctx, "teamB", ConnectionEntry{
		Plugin: "github", Token: "tokB", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Fatalf("create teamB connection: %v", err)
	}
	if connA == connB {
		t.Fatalf("expected separate connections, got %d for both teams", connA)
	}

	sharedRepo := database.RepoRow{ID: "r1", Type: "github", Owner: "shared-org", Name: "shared-repo"}
	addTeamSources(store, "teamA", []database.SourceRow{{
		ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{sharedRepo},
	}})
	addTeamSources(store, "teamB", []database.SourceRow{{
		ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{sharedRepo},
	}})

	resultA, err := svc.SyncTeam(ctx, "teamA")
	if err != nil {
		t.Fatalf("sync teamA: %v", err)
	}
	if len(resultA.Created) != 1 {
		t.Fatalf("teamA: expected 1 created, got %v", resultA.Created)
	}

	resultB, err := svc.SyncTeam(ctx, "teamB")
	if err != nil {
		t.Fatalf("sync teamB: %v", err)
	}
	if len(resultB.Skipped) != 1 {
		t.Fatalf("teamB: expected 1 skipped, got %v", resultB.Skipped)
	}
	wantMsg := "shared-org/shared-repo already collected by team teamA (blueprint \"lfmbot-teamA-github-shared-org\"); existing data will be used for this team's metrics"
	if resultB.Skipped[0] != wantMsg {
		t.Errorf("teamB skip message = %q, want %q", resultB.Skipped[0], wantMsg)
	}
	if len(resultB.Created) != 0 {
		t.Errorf("teamB: should not create blueprint for duplicate scope, got %v", resultB.Created)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.blueprints) != 1 || !hasLabel(fake.blueprints[0].Labels, "team:teamA") {
		t.Fatalf("expected 1 blueprint owned by teamA, got %+v", fake.blueprints)
	}
	if len(fake.scopes["github"][connB]) != 0 {
		t.Errorf("scope must not be registered on teamB's connection, got %d entries",
			len(fake.scopes["github"][connB]))
	}
	if len(fake.scopes["github"][connA]) != 1 {
		t.Errorf("scope should be registered on teamA's connection only, got %d entries",
			len(fake.scopes["github"][connA]))
	}
}

func TestNormalizeScopeEndpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		want     string
	}{
		{"https://gitlab.example.com", "gitlab.example.com"},
		{"https://gitlab.example.com/", "gitlab.example.com"},
		{"http://gitlab.example.com", "gitlab.example.com"},
		{"https://gitlab.example.com/api/v4", "gitlab.example.com"},
		{"https://gitlab.example.com/api/v4/", "gitlab.example.com"},
		{"HTTPS://GitLab.Example.COM/api/v4/", "gitlab.example.com"},
		{"gitlab.example.com", "gitlab.example.com"},
		{"gitlab.example.com/api/v4", "gitlab.example.com"},
		{"https://gitlab.example.com:8443/api/v4", "gitlab.example.com:8443"},
		{"https://user:pass@gitlab.example.com/api/v4", "gitlab.example.com"},
		{"https://gitlab.example.com/api/v4?foo=bar#baz", "gitlab.example.com"},
		{"https://example.com/gitlab/api/v4", "example.com"},
		{"https://example.com/anything/else", "example.com"},
		{"https://api.github.com/", "api.github.com"},
		{"https://Jira.Example.COM/", "jira.example.com"},
		{"https://api.codecov.io", "api.codecov.io"},
		{"https://other.example.com", "other.example.com"},
	}
	for _, tt := range tests {
		if got := normalizeScopeEndpoint(tt.endpoint); got != tt.want {
			t.Errorf("normalizeScopeEndpoint(%q) = %q, want %q", tt.endpoint, got, tt.want)
		}
	}
}

func TestSyncTeam_DedupSkipsGitLabScopeAcrossEndpointFormats(t *testing.T) {
	svc, fake, store := setupTestService(t)
	glServer := fakeGitLabAPI(t)
	ctx := context.Background()

	// GitLab accepts both the host URL and the API root (/api/v4) as a
	// connection endpoint; both connections target the same instance.
	connA, err := svc.CreateTeamConnection(ctx, "teamA", ConnectionEntry{
		Plugin: "gitlab", Token: "glpat-A", Endpoint: glServer.URL, ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Fatalf("create teamA connection: %v", err)
	}
	connB, err := svc.CreateTeamConnection(ctx, "teamB", ConnectionEntry{
		Plugin: "gitlab", Token: "glpat-B", Endpoint: glServer.URL + "/api/v4/", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Fatalf("create teamB connection: %v", err)
	}

	sharedProject := database.RepoRow{ID: "r1", Type: "gitlab", Owner: "shared-group", Name: "shared-project"}
	addTeamSources(store, "teamA", []database.SourceRow{{
		ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{sharedProject},
	}})
	addTeamSources(store, "teamB", []database.SourceRow{{
		ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{sharedProject},
	}})

	resultA, err := svc.SyncTeam(ctx, "teamA")
	if err != nil {
		t.Fatalf("sync teamA: %v", err)
	}
	if len(resultA.Created) != 1 {
		t.Fatalf("teamA: expected 1 created, got created=%v errors=%v", resultA.Created, resultA.Errors)
	}

	resultB, err := svc.SyncTeam(ctx, "teamB")
	if err != nil {
		t.Fatalf("sync teamB: %v", err)
	}
	if len(resultB.Skipped) != 1 {
		t.Fatalf("teamB: expected 1 skipped, got skipped=%v created=%v errors=%v",
			resultB.Skipped, resultB.Created, resultB.Errors)
	}
	wantMsg := "shared-group/shared-project already collected by team teamA (blueprint \"lfmbot-teamA-gitlab-shared-group\"); existing data will be used for this team's metrics"
	if resultB.Skipped[0] != wantMsg {
		t.Errorf("teamB skip message = %q, want %q", resultB.Skipped[0], wantMsg)
	}
	if len(resultB.Created) != 0 {
		t.Errorf("teamB: should not create blueprint for duplicate scope, got %v", resultB.Created)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.blueprints) != 1 || !hasLabel(fake.blueprints[0].Labels, "team:teamA") {
		t.Fatalf("expected 1 blueprint owned by teamA, got %+v", fake.blueprints)
	}
	if len(fake.scopes["gitlab"][connB]) != 0 {
		t.Errorf("scope must not be registered on teamB's connection, got %d entries",
			len(fake.scopes["gitlab"][connB]))
	}
	if len(fake.scopes["gitlab"][connA]) != 1 {
		t.Errorf("scope should be registered on teamA's connection only, got %d entries",
			len(fake.scopes["gitlab"][connA]))
	}
}

func TestSyncTeam_DedupSkipsJiraBoardOwnedByOtherTeamSeparateConnections(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	connA, err := svc.CreateTeamConnection(ctx, "teamA", ConnectionEntry{
		Plugin: "jira", Token: "tokA", Endpoint: "https://jira.example.com/", Username: "user@example.com", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Fatalf("create teamA connection: %v", err)
	}
	connB, err := svc.CreateTeamConnection(ctx, "teamB", ConnectionEntry{
		Plugin: "jira", Token: "tokB", Endpoint: "https://jira.example.com/", Username: "user@example.com", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Fatalf("create teamB connection: %v", err)
	}

	sharedBoard := []database.SourceRow{{
		ID:   "workitems",
		Type: "workitems",
		Projects: []database.WorkitemProjRow{
			{ID: "p1", Type: "jira", BoardID: "77", ProjectName: "Shared Board"},
		},
	}}
	addTeamSources(store, "teamA", sharedBoard)
	addTeamSources(store, "teamB", sharedBoard)

	resultA, err := svc.SyncTeam(ctx, "teamA")
	if err != nil {
		t.Fatalf("sync teamA: %v", err)
	}
	if len(resultA.Created) != 1 {
		t.Fatalf("teamA: expected 1 created, got %v", resultA.Created)
	}

	resultB, err := svc.SyncTeam(ctx, "teamB")
	if err != nil {
		t.Fatalf("sync teamB: %v", err)
	}
	if len(resultB.Skipped) != 1 {
		t.Fatalf("teamB: expected 1 skipped, got %v", resultB.Skipped)
	}
	if len(resultB.Created) != 0 {
		t.Errorf("teamB: should not create blueprint for duplicate board, got %v", resultB.Created)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.blueprints) != 1 || !hasLabel(fake.blueprints[0].Labels, "team:teamA") {
		t.Fatalf("expected 1 blueprint owned by teamA, got %+v", fake.blueprints)
	}
	if len(fake.scopes["jira"][connB]) != 0 {
		t.Errorf("board must not be registered on teamB's connection, got %d entries",
			len(fake.scopes["jira"][connB]))
	}
	if len(fake.scopes["jira"][connA]) != 1 {
		t.Errorf("board should be registered on teamA's connection only, got %d entries",
			len(fake.scopes["jira"][connA]))
	}
}

func TestSyncTeam_DedupDifferentEndpointsNoFalseCollision(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	// Two GitHub Enterprise instances with colliding numeric scope IDs.
	_, err := svc.CreateTeamConnection(ctx, "teamA", ConnectionEntry{
		Plugin: "github", Token: "tokA", Endpoint: "https://ghe-one.example.com/", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Fatalf("create teamA connection: %v", err)
	}
	_, err = svc.CreateTeamConnection(ctx, "teamB", ConnectionEntry{
		Plugin: "github", Token: "tokB", Endpoint: "https://ghe-two.example.com/", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Fatalf("create teamB connection: %v", err)
	}

	sameRepo := database.RepoRow{ID: "r1", Type: "github", Owner: "org", Name: "repo"}
	addTeamSources(store, "teamA", []database.SourceRow{{
		ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{sameRepo},
	}})
	addTeamSources(store, "teamB", []database.SourceRow{{
		ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{sameRepo},
	}})

	resultA, err := svc.SyncTeam(ctx, "teamA")
	if err != nil {
		t.Fatalf("sync teamA: %v", err)
	}
	if len(resultA.Created) != 1 || len(resultA.Skipped) != 0 {
		t.Fatalf("teamA: expected 1 created and no skips, got created=%v skipped=%v", resultA.Created, resultA.Skipped)
	}

	resultB, err := svc.SyncTeam(ctx, "teamB")
	if err != nil {
		t.Fatalf("sync teamB: %v", err)
	}
	if len(resultB.Created) != 1 || len(resultB.Skipped) != 0 {
		t.Fatalf("teamB: expected 1 created and no skips, got created=%v skipped=%v", resultB.Created, resultB.Skipped)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.blueprints) != 2 {
		t.Fatalf("expected 2 blueprints (one per team), got %d", len(fake.blueprints))
	}
}

func TestSyncTeam_MultiplePlugins(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	_, err := svc.CreateTeamConnection(ctx, "multi", ConnectionEntry{
		Plugin: "github", Token: "tok", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Errorf("CreateTeamConnection: %v", err)
	}
	_, err = svc.CreateTeamConnection(ctx, "multi", ConnectionEntry{
		Plugin: "jira", Token: "tok", Endpoint: "https://jira.example.com/", Username: "user@example.com", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Errorf("CreateTeamConnection: %v", err)
	}

	addTeamSources(store, "multi", []database.SourceRow{
		{
			ID:   "repos",
			Type: "gitrepos",
			Repos: []database.RepoRow{
				{ID: "r1", Type: "github", Owner: "org", Name: "repo"},
			},
		},
		{
			ID:   "work",
			Type: "workitems",
			Projects: []database.WorkitemProjRow{
				{ID: "p1", Type: "jira", JiraProject: "PROJ"},
			},
		},
	})

	result, err := svc.SyncTeam(ctx, "multi")
	if err != nil {
		t.Fatalf("SyncTeam: %v", err)
	}
	if len(result.Created) != 2 {
		t.Fatalf("expected 2 blueprints (github+jira), got %d: %v", len(result.Created), result.Created)
	}

	fake.mu.Lock()
	bps := fake.blueprints
	fake.mu.Unlock()

	plugins := make(map[string]bool)
	for _, bp := range bps {
		if len(bp.Connections) > 0 {
			plugins[bp.Connections[0].PluginName] = true
		}
	}
	if !plugins["github"] || !plugins["jira"] {
		t.Errorf("expected github and jira blueprints, got plugins: %v", plugins)
	}
}

func TestSyncTeam_MultipleOwnersSeparateBlueprints(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	_, err := svc.CreateTeamConnection(ctx, "team5", ConnectionEntry{
		Plugin: "github", Token: "tok", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Errorf("CreateTeamConnection: %v", err)
	}

	addTeamSources(store, "team5", []database.SourceRow{{
		ID:   "src1",
		Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "orgA", Name: "repo1"},
			{ID: "r2", Type: "github", Owner: "orgB", Name: "repo2"},
		},
	}})

	result, err := svc.SyncTeam(ctx, "team5")
	if err != nil {
		t.Fatalf("SyncTeam: %v", err)
	}
	if len(result.Created) != 2 {
		t.Fatalf("expected 2 blueprints for 2 owners, got %d: %v", len(result.Created), result.Created)
	}

	fake.mu.Lock()
	bps := fake.blueprints
	fake.mu.Unlock()

	names := make(map[string]bool)
	for _, bp := range bps {
		names[bp.Name] = true
	}
	wantA := "lfmbot-team5-github-orgA"
	wantB := "lfmbot-team5-github-orgB"
	if !names[wantA] || !names[wantB] {
		t.Errorf("expected blueprints %q and %q, got %v", wantA, wantB, names)
	}
}

func TestSyncTeam_MissingConnectionReportsError(t *testing.T) {
	svc, _, store := setupTestService(t)
	ctx := context.Background()

	addTeamSources(store, "noconn", []database.SourceRow{{
		ID:   "src1",
		Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "org", Name: "repo"},
		},
	}})

	result, err := svc.SyncTeam(ctx, "noconn")
	if err != nil {
		t.Fatalf("SyncTeam: %v", err)
	}
	if len(result.Errors) == 0 {
		t.Error("expected error about missing connection, got none")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "No DevLake connection") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'No DevLake connection' error, got %v", result.Errors)
	}
	if len(result.Created) != 0 {
		t.Errorf("should not create blueprints without connection, got %v", result.Created)
	}
}

func TestSyncTeam_CronSpreadApplied(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	_, err := svc.CreateTeamConnection(ctx, "cron-team", ConnectionEntry{
		Plugin: "github", Token: "tok", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Errorf("CreateTeamConnection: %v", err)
	}

	addTeamSources(store, "cron-team", []database.SourceRow{{
		ID:   "src1",
		Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "org", Name: "repo"},
		},
	}})

	_, err = svc.SyncTeam(ctx, "cron-team")
	if err != nil {
		t.Errorf("SyncTeam: %v", err)
	}

	fake.mu.Lock()
	bp := fake.blueprints[0]
	fake.mu.Unlock()

	if bp.CronConfig == "0 0 * * *" {
		t.Error("cron should be spread, but got the base config unchanged")
	}
	fields := strings.Fields(bp.CronConfig)
	if len(fields) != 5 {
		t.Fatalf("expected 5 cron fields, got %d: %q", len(fields), bp.CronConfig)
	}
	if fields[2] != "*" || fields[3] != "*" || fields[4] != "*" {
		t.Errorf("day/month/weekday should be preserved as *, got %q", bp.CronConfig)
	}
}

func TestCleanupTeam_DeletesBlueprints(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	addTeam(store, "doomed")
	_, err := svc.CreateTeamConnection(ctx, "doomed", ConnectionEntry{
		Plugin: "github", Token: "tok", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Errorf("CreateTeamConnection: %v", err)
	}
	addTeamSources(store, "doomed", []database.SourceRow{{
		ID: "src1", Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "org", Name: "repo1"},
			{ID: "r2", Type: "github", Owner: "org", Name: "repo2"},
		},
	}})
	_, err = svc.SyncTeam(ctx, "doomed")
	if err != nil {
		t.Errorf("SyncTeam: %v", err)
	}

	fake.mu.Lock()
	bpCount := len(fake.blueprints)
	fake.mu.Unlock()
	if bpCount != 1 {
		t.Fatalf("precondition: expected 1 blueprint, got %d", bpCount)
	}

	result, err := svc.CleanupTeam(ctx, "doomed")
	if err != nil {
		t.Fatalf("CleanupTeam: %v", err)
	}
	if len(result.DeletedBlueprints) != 1 {
		t.Errorf("expected 1 deleted blueprint, got %v", result.DeletedBlueprints)
	}

	fake.mu.Lock()
	bpCount = len(fake.blueprints)
	fake.mu.Unlock()
	if bpCount != 0 {
		t.Errorf("expected 0 blueprints after cleanup, got %d", bpCount)
	}
}

func TestCleanupTeam_MigratesScopesToOtherTeam(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	addTeam(store, "owner")
	addTeam(store, "waiting")

	// Both teams share the same connection (the original dedup scenario).
	connID, _ := svc.CreateTeamConnection(ctx, "owner", ConnectionEntry{
		Plugin: "github", Token: "tok", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01",
	})
	setConnectionID(t, store, "waiting", "github", connID)

	sharedRepo := database.RepoRow{ID: "r1", Type: "github", Owner: "shared", Name: "repo"}
	addTeamSources(store, "owner", []database.SourceRow{{
		ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{sharedRepo},
	}})
	addTeamSources(store, "waiting", []database.SourceRow{{
		ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{sharedRepo},
	}})

	// Owner syncs first — claims the scope.
	resultOwner, _ := svc.SyncTeam(ctx, "owner")
	if len(resultOwner.Created) != 1 {
		t.Fatalf("owner sync: expected 1 created, got %v", resultOwner.Created)
	}

	// Waiting team syncs — scope is deduped.
	resultWaiting, _ := svc.SyncTeam(ctx, "waiting")
	if len(resultWaiting.Skipped) != 1 {
		t.Fatalf("waiting sync: expected 1 skipped, got %v", resultWaiting.Skipped)
	}

	fake.mu.Lock()
	bpsBefore := len(fake.blueprints)
	fake.mu.Unlock()
	if bpsBefore != 1 {
		t.Fatalf("precondition: expected 1 blueprint (owner's), got %d", bpsBefore)
	}

	// Delete the owner team — cleanup should migrate the scope to the waiting team.
	cleanup, err := svc.CleanupTeam(ctx, "owner")
	if err != nil {
		t.Fatalf("CleanupTeam: %v", err)
	}
	if len(cleanup.DeletedBlueprints) != 1 {
		t.Errorf("expected 1 deleted blueprint, got %v", cleanup.DeletedBlueprints)
	}
	if len(cleanup.Migrated) != 1 {
		t.Fatalf("expected 1 migrated entry, got %v", cleanup.Migrated)
	}
	if !strings.Contains(cleanup.Migrated[0], "waiting") {
		t.Errorf("migration should reference team 'waiting', got %q", cleanup.Migrated[0])
	}

	// Verify: the waiting team now owns a blueprint with the shared scope.
	fake.mu.Lock()
	var waitingBPs []Blueprint
	for _, bp := range fake.blueprints {
		if hasLabel(bp.Labels, "team:waiting") {
			waitingBPs = append(waitingBPs, bp)
		}
	}
	fake.mu.Unlock()
	if len(waitingBPs) != 1 {
		t.Fatalf("expected 1 blueprint for waiting team, got %d", len(waitingBPs))
	}
	if len(waitingBPs[0].Connections[0].Scopes) != 1 {
		t.Errorf("expected 1 scope in waiting team's blueprint, got %d", len(waitingBPs[0].Connections[0].Scopes))
	}
}

func TestCleanupTeam_MigratesScopesToOtherTeamSeparateConnections(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	addTeam(store, "owner")
	addTeam(store, "waiting")

	// Separate connections to the same provider instance — the scope is owned
	// by the first team to sync, and collection must move to the surviving
	// team's own connection when the owner is offboarded.
	ownerConn, err := svc.CreateTeamConnection(ctx, "owner", ConnectionEntry{
		Plugin: "github", Token: "tokA", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Fatalf("create owner connection: %v", err)
	}
	waitingConn, err := svc.CreateTeamConnection(ctx, "waiting", ConnectionEntry{
		Plugin: "github", Token: "tokB", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Fatalf("create waiting connection: %v", err)
	}

	sharedRepo := database.RepoRow{ID: "r1", Type: "github", Owner: "shared", Name: "repo"}
	addTeamSources(store, "owner", []database.SourceRow{{
		ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{sharedRepo},
	}})
	addTeamSources(store, "waiting", []database.SourceRow{{
		ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{sharedRepo},
	}})

	resultOwner, err := svc.SyncTeam(ctx, "owner")
	if err != nil {
		t.Fatalf("owner sync: %v", err)
	}
	if len(resultOwner.Created) != 1 {
		t.Fatalf("owner sync: expected 1 created, got %v", resultOwner.Created)
	}

	resultWaiting, err := svc.SyncTeam(ctx, "waiting")
	if err != nil {
		t.Fatalf("waiting sync: %v", err)
	}
	if len(resultWaiting.Skipped) != 1 {
		t.Fatalf("waiting sync: expected 1 skipped, got %v", resultWaiting.Skipped)
	}

	cleanup, err := svc.CleanupTeam(ctx, "owner")
	if err != nil {
		t.Fatalf("CleanupTeam: %v", err)
	}
	if len(cleanup.DeletedBlueprints) != 1 {
		t.Errorf("expected 1 deleted blueprint, got %v", cleanup.DeletedBlueprints)
	}
	if len(cleanup.Migrated) != 1 {
		t.Fatalf("expected 1 migrated entry, got %v", cleanup.Migrated)
	}
	if !strings.Contains(cleanup.Migrated[0], "waiting") {
		t.Errorf("migration should reference team 'waiting', got %q", cleanup.Migrated[0])
	}
	if len(cleanup.DeletedScopes) != 1 {
		t.Errorf("expected 1 deleted scope on the owner's connection, got %v", cleanup.DeletedScopes)
	}
	if len(cleanup.DeletedConnections) != 1 {
		t.Errorf("expected 1 deleted connection (owner's), got %v", cleanup.DeletedConnections)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	var waitingBPs []Blueprint
	for _, bp := range fake.blueprints {
		if hasLabel(bp.Labels, "team:waiting") {
			waitingBPs = append(waitingBPs, bp)
		}
	}
	if len(waitingBPs) != 1 {
		t.Fatalf("expected 1 blueprint for waiting team, got %d", len(waitingBPs))
	}
	if len(waitingBPs[0].Connections[0].Scopes) != 1 {
		t.Errorf("expected 1 scope in waiting team's blueprint, got %d", len(waitingBPs[0].Connections[0].Scopes))
	}
	if len(fake.scopes["github"][ownerConn]) != 0 {
		t.Errorf("owner's scope copy should have been purged, got %d entries",
			len(fake.scopes["github"][ownerConn]))
	}
	if len(fake.scopes["github"][waitingConn]) != 1 {
		t.Errorf("scope should be registered on the waiting team's connection, got %d entries",
			len(fake.scopes["github"][waitingConn]))
	}
	if len(fake.connections["github"]) != 1 {
		t.Errorf("expected only the waiting team's github connection to remain, got %d", len(fake.connections["github"]))
	}
}

func TestCleanupTeam_NoMigrationWhenNoOtherTeamWantsScope(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	addTeam(store, "solo")
	_, err := svc.CreateTeamConnection(ctx, "solo", ConnectionEntry{
		Plugin: "github", Token: "tok", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Errorf("CreateTeamConnection: %v", err)
	}
	addTeamSources(store, "solo", []database.SourceRow{{
		ID: "src1", Type: "gitrepos",
		Repos: []database.RepoRow{{ID: "r1", Type: "github", Owner: "org", Name: "unique-repo"}},
	}})
	_, err = svc.SyncTeam(ctx, "solo")
	if err != nil {
		t.Errorf("SyncTeam: %v", err)
	}

	cleanup, err := svc.CleanupTeam(ctx, "solo")
	if err != nil {
		t.Fatalf("CleanupTeam: %v", err)
	}
	if len(cleanup.DeletedBlueprints) != 1 {
		t.Errorf("expected 1 deleted, got %v", cleanup.DeletedBlueprints)
	}
	if len(cleanup.Migrated) != 0 {
		t.Errorf("expected no migrations, got %v", cleanup.Migrated)
	}

	fake.mu.Lock()
	bps := len(fake.blueprints)
	fake.mu.Unlock()
	if bps != 0 {
		t.Errorf("expected 0 blueprints, got %d", bps)
	}
}

func TestCleanupTeam_MigratesMultiplePlugins(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	addTeam(store, "leaving")
	addTeam(store, "inheritor")

	ghConnID, _ := svc.CreateTeamConnection(ctx, "leaving", ConnectionEntry{
		Plugin: "github", Token: "tok", Endpoint: "https://api.github.com/", ExpiryDate: "2027-01-01",
	})
	jiraConnID, _ := svc.CreateTeamConnection(ctx, "leaving", ConnectionEntry{
		Plugin: "jira", Token: "tok", Endpoint: "https://jira.example.com/", Username: "u@e.com", ExpiryDate: "2027-01-01",
	})
	setConnectionID(t, store, "inheritor", "github", ghConnID)
	setConnectionID(t, store, "inheritor", "jira", jiraConnID)

	addTeamSources(store, "leaving", []database.SourceRow{
		{ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "org", Name: "repo"},
		}},
		{ID: "src2", Type: "workitems", Projects: []database.WorkitemProjRow{
			{ID: "p1", Type: "jira", JiraProject: "PROJ"},
		}},
	})
	addTeamSources(store, "inheritor", []database.SourceRow{
		{ID: "src1", Type: "gitrepos", Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "org", Name: "repo"},
		}},
		{ID: "src2", Type: "workitems", Projects: []database.WorkitemProjRow{
			{ID: "p1", Type: "jira", JiraProject: "PROJ"},
		}},
	})

	_, err := svc.SyncTeam(ctx, "leaving")
	if err != nil {
		t.Errorf("SyncTeam: %v", err)
	}

	// Inheritor is blocked by dedup on both plugins.
	waitResult, _ := svc.SyncTeam(ctx, "inheritor")
	if len(waitResult.Skipped) != 2 {
		t.Fatalf("expected 2 skipped for inheritor, got %v", waitResult.Skipped)
	}

	cleanup, err := svc.CleanupTeam(ctx, "leaving")
	if err != nil {
		t.Fatalf("CleanupTeam: %v", err)
	}
	if len(cleanup.DeletedBlueprints) != 2 {
		t.Errorf("expected 2 deleted blueprints, got %v", cleanup.DeletedBlueprints)
	}
	if len(cleanup.Migrated) != 2 {
		t.Errorf("expected 2 migrated entries, got %v", cleanup.Migrated)
	}

	fake.mu.Lock()
	var inheritorBPs []Blueprint
	for _, bp := range fake.blueprints {
		if hasLabel(bp.Labels, "team:inheritor") {
			inheritorBPs = append(inheritorBPs, bp)
		}
	}
	fake.mu.Unlock()
	if len(inheritorBPs) != 2 {
		t.Fatalf("expected 2 blueprints for inheritor (github+jira), got %d", len(inheritorBPs))
	}

	plugins := make(map[string]bool)
	for _, bp := range inheritorBPs {
		plugins[bp.Connections[0].PluginName] = true
	}
	if !plugins["github"] || !plugins["jira"] {
		t.Errorf("expected github and jira blueprints, got %v", plugins)
	}
}

func TestCleanupTeam_NoopWhenNoBlueprintsExist(t *testing.T) {
	svc, _, store := setupTestService(t)
	ctx := context.Background()

	addTeam(store, "empty")

	cleanup, err := svc.CleanupTeam(ctx, "empty")
	if err != nil {
		t.Fatalf("CleanupTeam: %v", err)
	}
	if len(cleanup.DeletedBlueprints) != 0 {
		t.Errorf("expected no deletions, got %v", cleanup.DeletedBlueprints)
	}
	if len(cleanup.Migrated) != 0 {
		t.Errorf("expected no migrations, got %v", cleanup.Migrated)
	}
	if len(cleanup.Errors) != 0 {
		t.Errorf("expected no errors, got %v", cleanup.Errors)
	}
}

func TestTestConnection_Success(t *testing.T) {
	svc, _, _ := setupTestService(t)
	ctx := context.Background()

	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" && r.Header.Get("Authorization") == "token ghp_valid" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer providerSrv.Close()

	result, err := svc.TestConnection(ctx, ConnectionEntry{
		Plugin:   "github",
		Token:    "ghp_valid",
		Endpoint: providerSrv.URL,
	})
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestTestConnection_Failure(t *testing.T) {
	svc, _, _ := setupTestService(t)
	ctx := context.Background()

	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer providerSrv.Close()

	result, err := svc.TestConnection(ctx, ConnectionEntry{
		Plugin:   "github",
		Token:    "ghp_bad",
		Endpoint: providerSrv.URL,
	})
	if err == nil {
		t.Error("expected error for bad token, got nil")
	}
	if result != nil {
		t.Error("expected nil result on failure")
	}
}

func TestTestConnection_GitHub_MissingScopes(t *testing.T) {
	svc, fake, _ := setupTestService(t)
	ctx := context.Background()

	// GitHub accepts a repo-only classic PAT for GET /user, so the provider call
	// succeeds; only DevLake's plugin test catches the missing scopes.
	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" && r.Header.Get("Authorization") == "token ghp_test1234" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer providerSrv.Close()

	fake.mu.Lock()
	fake.failTokens["ghp_test1234"] = "Please check the field(s) read:user, read:org"
	fake.mu.Unlock()

	_, err := svc.TestConnection(ctx, ConnectionEntry{
		Plugin:   "github",
		Token:    "ghp_test1234",
		Endpoint: providerSrv.URL,
	})
	if err == nil {
		t.Fatal("expected error for token missing required scopes, got nil")
	}
	want := "The provided token is missing one or more of the following permissions: repo, read:user and read:org"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not explain the missing permissions: want %q", err.Error(), want)
	}
	if strings.Contains(err.Error(), "Please check the field(s)") {
		t.Errorf("error %q leaks DevLake's raw message", err.Error())
	}

	fake.mu.Lock()
	sentBody := fake.lastTestBody
	fake.mu.Unlock()
	if sentBody["authMethod"] != "AccessToken" {
		t.Errorf("DevLake test authMethod = %v, want AccessToken", sentBody["authMethod"])
	}
	if sentBody["endpoint"] != providerSrv.URL {
		t.Errorf("DevLake test endpoint = %v, want %s", sentBody["endpoint"], providerSrv.URL)
	}
	if sentBody["token"] != "ghp_test1234" {
		t.Errorf("DevLake test token = %v, want ghp_test1234", sentBody["token"])
	}
}

func TestTestConnection_Jira(t *testing.T) {
	svc, _, _ := setupTestService(t)
	ctx := context.Background()

	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/2/myself" {
			user, _, ok := r.BasicAuth()
			if ok && user == "user@company.com" {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer providerSrv.Close()

	result, err := svc.TestConnection(ctx, ConnectionEntry{
		Plugin:   "jira",
		Token:    "jira_tok",
		Endpoint: providerSrv.URL,
		Username: "user@company.com",
	})
	if err != nil {
		t.Errorf("expected success for jira, got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestProxyLookup_GitHub_Found(t *testing.T) {
	ghServer := fakeGitHubAPI(t)
	svc := NewService(Config{AllowPrivateEndpoints: true}, ghServer.Client(), newFakeConfigStore())
	result := svc.ProxyLookup(context.Background(), ConnectionEntry{
		Plugin: "github", Endpoint: ghServer.URL, Token: "ghp_test",
	}, "myorg", "my-repo")
	if result.Error != "" || !result.Valid {
		t.Fatalf("ProxyLookup: %+v", result)
	}
	if result.CanonicalName != "myorg/my-repo" {
		t.Fatalf("canonical_name = %q", result.CanonicalName)
	}
}

func TestProxyLookup_GitHub_NotFound(t *testing.T) {
	ghServer := fakeGitHubAPI(t)
	svc := NewService(Config{AllowPrivateEndpoints: true}, ghServer.Client(), newFakeConfigStore())
	result := svc.ProxyLookup(context.Background(), ConnectionEntry{
		Plugin: "github", Endpoint: ghServer.URL, Token: "ghp_test",
	}, "myorg", "missing-repo")
	if result.Valid || result.Error == "" {
		t.Fatalf("expected not-found error, got %+v", result)
	}
}

func TestProxyBrowseJira_UsesQuery(t *testing.T) {
	var receivedName string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/agile/1.0/board", func(w http.ResponseWriter, r *http.Request) {
		receivedName = r.URL.Query().Get("name")
		writeJSON(w, http.StatusOK, map[string]any{
			"isLast": true,
			"values": []map[string]any{
				{
					"id":   42,
					"name": "Conforma",
					"location": map[string]string{
						"projectKey":  "EC",
						"projectName": "Conforma",
					},
				},
			},
		})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	svc := NewService(Config{AllowPrivateEndpoints: true}, ts.Client(), newFakeConfigStore())
	results, err := svc.ProxyBrowse(context.Background(), ConnectionEntry{
		Plugin:   "jira",
		Endpoint: ts.URL,
		Token:    "jira-token",
		Username: "user@example.com",
	}, "Conforma")
	if err != nil {
		t.Fatalf("ProxyBrowse: %v", err)
	}
	if receivedName != "Conforma" {
		t.Fatalf("name = %q, want Conforma", receivedName)
	}
	if len(results) != 1 || results[0].Key != "EC" || results[0].Name != "Conforma" || results[0].BoardID != "42" {
		t.Fatalf("results = %+v, want EC / Conforma / 42", results)
	}
}

func TestTestConnection_GitLab(t *testing.T) {
	svc, _, _ := setupTestService(t)
	ctx := context.Background()

	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/personal_access_tokens/self" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path == "/api/v4/user" && r.Header.Get("PRIVATE-TOKEN") == "glpat_valid" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer providerSrv.Close()

	result, err := svc.TestConnection(ctx, ConnectionEntry{
		Plugin:   "gitlab",
		Token:    "glpat_valid",
		Endpoint: providerSrv.URL,
	})
	if err != nil {
		t.Errorf("expected success for gitlab, got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestTestConnection_GitLab_Scopes(t *testing.T) {
	svc, _, _ := setupTestService(t)
	ctx := context.Background()

	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/personal_access_tokens/self" && r.Header.Get("PRIVATE-TOKEN") == "glpat_valid" {
			writeJSON(w, http.StatusOK, map[string]any{
				"expires_at": "2027-01-01",
				"scopes":     []string{"read_api"},
			})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer providerSrv.Close()

	result, err := svc.TestConnection(ctx, ConnectionEntry{
		Plugin:   "gitlab",
		Token:    "glpat_valid",
		Endpoint: providerSrv.URL,
	})
	if err != nil {
		t.Errorf("expected success for token with read_api scope, got %v", err)
	}
	if result == nil || result.ExpiryDate != "2027-01-01" {
		t.Errorf("expected expiry date 2027-01-01, got %+v", result)
	}
}

func TestTestConnection_GitLab_MissingScope(t *testing.T) {
	svc, _, _ := setupTestService(t)
	ctx := context.Background()

	// A repository-only token authenticates and is introspectable, but cannot
	// list projects — the wizard's repo browser would fail on it.
	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/personal_access_tokens/self" {
			writeJSON(w, http.StatusOK, map[string]any{
				"scopes": []string{"read_repository"},
			})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer providerSrv.Close()

	_, err := svc.TestConnection(ctx, ConnectionEntry{
		Plugin:   "gitlab",
		Token:    "glpat_test",
		Endpoint: providerSrv.URL,
	})
	if err == nil {
		t.Fatal("expected error for token without read_api scope, got nil")
	}
	want := "The provided token is missing the required permission: read_api (or api)"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not explain the missing permission: want %q", err.Error(), want)
	}
}

// ---------------------------------------------------------------------------
// LookupRepo tests
// ---------------------------------------------------------------------------

func fakeGitHubAPI(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/{owner}/{name}", func(w http.ResponseWriter, r *http.Request) {
		owner := r.PathValue("owner")
		name := r.PathValue("name")
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if owner == "myorg" && name == "missing-repo" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		repo := map[string]any{
			"id":        12345,
			"full_name": owner + "/" + name,
			"name":      name,
			"html_url":  fmt.Sprintf("https://github.com/%s/%s", owner, name),
			"clone_url": fmt.Sprintf("https://github.com/%s/%s.git", owner, name),
			"archived":  name == "archived-repo",
			"owner":     map[string]any{"login": owner},
		}
		writeJSON(w, http.StatusOK, repo)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func fakeGitLabAPI(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v4/projects/{path}", func(w http.ResponseWriter, r *http.Request) {
		path := r.PathValue("path")
		if r.Header.Get("PRIVATE-TOKEN") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if path == "missing%2Fproject" || path == "missing/project" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		decoded := strings.ReplaceAll(path, "%2F", "/")
		project := map[string]any{
			"id":                  999,
			"path_with_namespace": decoded,
			"name":                decoded[strings.LastIndex(decoded, "/")+1:],
			"archived":            false,
		}
		writeJSON(w, http.StatusOK, project)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func fakeJiraAPI(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/api/2/project/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		if key == "MISSING" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		project := map[string]any{
			"key":  key,
			"name": "Project " + key,
		}
		writeJSON(w, http.StatusOK, project)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestLookupRepo_GitHub_Found(t *testing.T) {
	ghServer := fakeGitHubAPI(t)
	svc, _, _ := setupTestService(t)

	result := svc.LookupRepo(context.Background(), "github", ghServer.URL, "ghp_test", "myorg", "my-repo", nil)
	if !result.Valid {
		t.Fatalf("expected valid, got error: %s", result.Error)
	}
	if result.CanonicalName != "myorg/my-repo" {
		t.Errorf("canonical_name = %q, want myorg/my-repo", result.CanonicalName)
	}
	if result.RawData == nil {
		t.Error("expected RawData to be populated")
	}
}

func TestLookupRepo_GitHub_NotFound(t *testing.T) {
	ghServer := fakeGitHubAPI(t)
	svc, _, _ := setupTestService(t)

	result := svc.LookupRepo(context.Background(), "github", ghServer.URL, "ghp_test", "myorg", "missing-repo", nil)
	if result.Valid {
		t.Error("expected invalid for missing repo")
	}
	if result.Error != "Repository not found" {
		t.Errorf("error = %q, want 'Repository not found'", result.Error)
	}
}

func TestLookupRepo_GitHub_Archived(t *testing.T) {
	ghServer := fakeGitHubAPI(t)
	svc, _, _ := setupTestService(t)

	result := svc.LookupRepo(context.Background(), "github", ghServer.URL, "ghp_test", "myorg", "archived-repo", nil)
	if !result.Valid {
		t.Fatalf("expected valid, got error: %s", result.Error)
	}
	if !result.Archived {
		t.Error("expected Archived=true")
	}
	if result.Warning == "" {
		t.Error("expected warning about archived repo")
	}
}

func TestLookupRepo_GitHub_Unauthorized(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/{owner}/{name}", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	svc, _, _ := setupTestService(t)

	result := svc.LookupRepo(context.Background(), "github", ts.URL, "bad-token", "myorg", "repo", nil)
	if result.Valid {
		t.Error("expected invalid for unauthorized")
	}
	if !strings.Contains(result.Error, "Token expired") {
		t.Errorf("error = %q, want to contain 'Token expired'", result.Error)
	}
}

func TestLookupRepo_GitLab_Found(t *testing.T) {
	glServer := fakeGitLabAPI(t)
	svc, _, _ := setupTestService(t)

	result := svc.LookupRepo(context.Background(), "gitlab", glServer.URL, "glpat-test", "group/subgroup", "project", nil)
	if !result.Valid {
		t.Fatalf("expected valid, got error: %s", result.Error)
	}
	if result.CanonicalName != "group/subgroup/project" {
		t.Errorf("canonical_name = %q, want group/subgroup/project", result.CanonicalName)
	}
}

func TestLookupRepo_GitLab_StripsAPIPath(t *testing.T) {
	glServer := fakeGitLabAPI(t)
	svc, _, _ := setupTestService(t)

	result := svc.LookupRepo(context.Background(), "gitlab", glServer.URL+"/api/v4", "glpat-test", "group", "project", nil)
	if !result.Valid {
		t.Fatalf("expected valid even with /api/v4 suffix, got error: %s", result.Error)
	}
}

func TestLookupRepo_Jira_Found(t *testing.T) {
	jiraServer := fakeJiraAPI(t)
	svc, _, _ := setupTestService(t)

	extra := map[string]string{"username": "user@example.com"}
	result := svc.LookupRepo(context.Background(), "jira", jiraServer.URL, "jira-token", "MYPROJ", "", extra)
	if !result.Valid {
		t.Fatalf("expected valid, got error: %s", result.Error)
	}
	if result.CanonicalName != "MYPROJ" {
		t.Errorf("canonical_name = %q, want MYPROJ", result.CanonicalName)
	}
}

func TestLookupRepo_Jira_NotFound(t *testing.T) {
	jiraServer := fakeJiraAPI(t)
	svc, _, _ := setupTestService(t)

	extra := map[string]string{"username": "user@example.com"}
	result := svc.LookupRepo(context.Background(), "jira", jiraServer.URL, "jira-token", "MISSING", "", extra)
	if result.Valid {
		t.Error("expected invalid for missing project")
	}
	if result.Error != "Project not found" {
		t.Errorf("error = %q, want 'Project not found'", result.Error)
	}
}

func TestLookupRepo_Timeout(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		writeJSON(w, http.StatusOK, map[string]any{"full_name": "org/repo"})
	}))
	defer slowServer.Close()
	svc, _, _ := setupTestService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := svc.LookupRepo(ctx, "github", slowServer.URL, "ghp_test", "org", "repo", nil)
	if !result.Valid {
		t.Error("expected valid=true on timeout (graceful fallback)")
	}
	if result.Warning == "" {
		t.Error("expected warning on timeout")
	}
}

func TestLookupRepo_UnsupportedPlugin(t *testing.T) {
	svc, _, _ := setupTestService(t)
	result := svc.LookupRepo(context.Background(), "bitbucket", "https://example.com", "tok", "org", "repo", nil)
	if result.Valid {
		t.Error("expected invalid for unsupported plugin")
	}
	if !strings.Contains(result.Error, "not supported") {
		t.Errorf("error = %q, want to contain 'not supported'", result.Error)
	}
}

// ---------------------------------------------------------------------------
// LookupRepoForTeam tests
// ---------------------------------------------------------------------------

func TestLookupRepoForTeam_ReadsTokenFromStore(t *testing.T) {
	ghServer := fakeGitHubAPI(t)
	svc, _, store := setupTestService(t)
	ctx := context.Background()

	_ = store.SetEntityConfigKey(ctx, "team", "team1", "devlake_github_token", "ghp_stored")
	_ = store.SetEntityConfigKey(ctx, "team", "team1", "devlake_github_endpoint", ghServer.URL)

	result := svc.LookupRepoForTeam(ctx, "team1", "github", "myorg", "my-repo")
	if !result.Valid {
		t.Fatalf("expected valid, got error: %s", result.Error)
	}
	if result.CanonicalName != "myorg/my-repo" {
		t.Errorf("canonical_name = %q", result.CanonicalName)
	}
}

func TestLookupRepoForTeam_NoToken(t *testing.T) {
	svc, _, _ := setupTestService(t)
	result := svc.LookupRepoForTeam(context.Background(), "team-no-conn", "github", "org", "repo")
	if result.Valid {
		t.Error("expected invalid when no token configured")
	}
	if !strings.Contains(result.Error, "No github connection") {
		t.Errorf("error = %q, want to contain 'No github connection'", result.Error)
	}
}

func TestLookupRepoForTeam_CodecovOnly(t *testing.T) {
	svc, _, store := setupTestService(t)
	ctx := context.Background()
	if err := store.SetEntityConfigKeys(ctx, "team", "team-coverage", map[string]string{
		"devlake_codecov_connection_id": "42",
		"devlake_codecov_organization":  "konflux-ci",
		"devlake_codecov_service":       "github",
	}); err != nil {
		t.Fatal(err)
	}
	result := svc.LookupRepoForTeam(ctx, "team-coverage", "github", "konflux-ci", "konflux-ui")
	if !result.Valid || result.Warning == "" {
		t.Fatalf("matching Codecov-only repo rejected: %+v", result)
	}
	result = svc.LookupRepoForTeam(ctx, "team-coverage", "github", "other-org", "repo")
	if result.Valid {
		t.Fatalf("unrelated repo accepted without Git provider token: %+v", result)
	}
}

// ---------------------------------------------------------------------------
// ListRemoteScopes pagination tests
// ---------------------------------------------------------------------------

func TestListRemoteScopes_Pagination(t *testing.T) {
	pageCount := 0
	pagServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageCount++
		token := r.URL.Query().Get("pageToken")
		var resp remoteScopesResponse
		switch token {
		case "":
			resp = remoteScopesResponse{
				Children: []remoteScopeNode{
					{Type: "scope", ID: "1", Name: "repo-1", Data: json.RawMessage(`{"githubId":1,"fullName":"org/repo-1"}`)},
				},
				NextPageToken: "page2",
			}
		case "page2":
			resp = remoteScopesResponse{
				Children: []remoteScopeNode{
					{Type: "scope", ID: "2", Name: "repo-2", Data: json.RawMessage(`{"githubId":2,"fullName":"org/repo-2"}`)},
				},
				NextPageToken: "page3",
			}
		case "page3":
			resp = remoteScopesResponse{
				Children: []remoteScopeNode{
					{Type: "scope", ID: "3", Name: "repo-3", Data: json.RawMessage(`{"githubId":3,"fullName":"org/repo-3"}`)},
				},
				NextPageToken: "",
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}))
	defer pagServer.Close()

	client := NewClient(pagServer.URL, "test", pagServer.Client())
	nodes, err := client.ListRemoteScopes(context.Background(), "github", 1, "")
	if err != nil {
		t.Fatalf("ListRemoteScopes: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("got %d nodes, want 3", len(nodes))
	}
	if pageCount != 3 {
		t.Errorf("expected 3 page requests, got %d", pageCount)
	}
}

func TestListRemoteScopes_SinglePage(t *testing.T) {
	pagServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := remoteScopesResponse{
			Children: []remoteScopeNode{
				{Type: "scope", ID: "1", Name: "repo-1", Data: json.RawMessage(`{"githubId":1}`)},
			},
			NextPageToken: "",
		}
		writeJSON(w, http.StatusOK, resp)
	}))
	defer pagServer.Close()

	client := NewClient(pagServer.URL, "test", pagServer.Client())
	nodes, err := client.ListRemoteScopes(context.Background(), "github", 1, "")
	if err != nil {
		t.Fatalf("ListRemoteScopes: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("got %d nodes, want 1", len(nodes))
	}
}

// ---------------------------------------------------------------------------
// collectRemoteScopes dedup tests
// ---------------------------------------------------------------------------

func TestCollectRemoteScopes_Dedup(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		groupID := r.URL.Query().Get("groupId")
		if groupID == "" {
			resp := remoteScopesResponse{
				Children: []remoteScopeNode{
					{Type: "scope", ID: "1", Name: "repo-1", Data: json.RawMessage(`{"githubId":42,"fullName":"org/repo-1"}`)},
					{Type: "group", ID: "org", Name: "org"},
				},
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
		resp := remoteScopesResponse{
			Children: []remoteScopeNode{
				{Type: "scope", ID: "1", Name: "repo-1", Data: json.RawMessage(`{"githubId":42,"fullName":"org/repo-1"}`)},
				{Type: "scope", ID: "2", Name: "repo-2", Data: json.RawMessage(`{"githubId":43,"fullName":"org/repo-2"}`)},
			},
		}
		writeJSON(w, http.StatusOK, resp)
	}))
	defer ts.Close()

	store := newFakeConfigStore()
	cfg := Config{APIURL: ts.URL, APIKey: "test", AllowPrivateEndpoints: true}
	svc := NewService(cfg, ts.Client(), store)

	scopes, err := svc.collectRemoteScopes(context.Background(), "github", 1)
	if err != nil {
		t.Fatalf("collectRemoteScopes: %v", err)
	}
	if len(scopes) != 2 {
		t.Errorf("got %d scopes, want 2 (deduped). repo-1 should appear once", len(scopes))
	}
}

// ---------------------------------------------------------------------------
// resolveScopes fork fallback tests
// ---------------------------------------------------------------------------

func TestResolveScopes_ForkFallback(t *testing.T) {
	ghServer := fakeGitHubAPI(t)

	devlakeMux := http.NewServeMux()
	devlakeMux.HandleFunc("GET /plugins/{plugin}/connections/{connID}/search-remote-scopes", func(w http.ResponseWriter, r *http.Request) {
		search := r.URL.Query().Get("search")
		if strings.Contains(search, "forked-repo") {
			writeJSON(w, http.StatusOK, searchRemoteScopesResponse{})
			return
		}
		writeJSON(w, http.StatusOK, searchRemoteScopesResponse{
			Children: []struct {
				Data json.RawMessage `json:"data"`
			}{{Data: json.RawMessage(fmt.Sprintf(`{"githubId":42,"fullName":%q}`, search))}},
		})
	})
	devlakeMux.HandleFunc("GET /plugins/{plugin}/connections", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, []connectionInfo{})
	})
	devlakeMux.HandleFunc("POST /plugins/{plugin}/connections", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, connectionInfo{ID: 1, Name: "test"})
	})
	devlakeMux.HandleFunc("PATCH /plugins/{plugin}/connections/{connID}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	devlakeMux.HandleFunc("POST /plugins/{plugin}/test-connections", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})

	ts := httptest.NewServer(devlakeMux)
	defer ts.Close()

	store := newFakeConfigStore()
	ctx := context.Background()
	_ = store.SetEntityConfigKey(ctx, "team", "team-fork", "devlake_github_token", "ghp_test")
	_ = store.SetEntityConfigKey(ctx, "team", "team-fork", "devlake_github_endpoint", ghServer.URL)
	_ = store.SetEntityConfigKey(ctx, "team", "team-fork", "devlake_github_connection_id", "1")

	cfg := Config{APIURL: ts.URL, APIKey: "test", AllowPrivateEndpoints: true, CronConfig: "0 0 * * *"}
	svc := NewService(cfg, ts.Client(), store)

	desired := map[string]*desiredBlueprint{
		"lfmbot-team-fork-github-myorg": {
			name:   "lfmbot-team-fork-github-myorg",
			plugin: "github",
			connID: 1,
			repoSpecs: []repoSpec{
				{owner: "myorg", name: "forked-repo"},
			},
		},
	}

	result := &SyncResult{}
	svc.resolveScopes(ctx, "team-fork", desired, result)

	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved (via fork fallback), got %d. errors: %v", len(result.Resolved), result.Errors)
	}
	if result.Resolved[0] != "myorg/forked-repo" {
		t.Errorf("resolved = %v, want myorg/forked-repo", result.Resolved)
	}
}

func TestResolveScopes_NormalMatch(t *testing.T) {
	svc, _, store := setupTestService(t)
	ctx := context.Background()

	connID, _ := svc.CreateTeamConnection(ctx, "team-normal", ConnectionEntry{
		Plugin:   "github",
		Token:    "ghp_test",
		Endpoint: "https://api.github.com/",
	})

	addTeamSources(store, "team-normal", []database.SourceRow{{
		ID:   "src1",
		Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "myorg", Name: "repo-a"},
		},
	}})

	desired := map[string]*desiredBlueprint{
		"lfmbot-team-normal-github-myorg": {
			name:   "lfmbot-team-normal-github-myorg",
			plugin: "github",
			connID: connID,
			repoSpecs: []repoSpec{
				{owner: "myorg", name: "repo-a"},
			},
		},
	}

	result := &SyncResult{}
	svc.resolveScopes(ctx, "team-normal", desired, result)

	if len(result.Failed) > 0 {
		t.Errorf("expected no failures, got %v", result.Failed)
	}
	if len(result.Resolved) != 1 {
		t.Errorf("expected 1 resolved, got %d", len(result.Resolved))
	}
}

// ---------------------------------------------------------------------------
// buildScopeFromLookup tests
// ---------------------------------------------------------------------------

func TestBuildScopeFromLookup_GitHub(t *testing.T) {
	svc, _, _ := setupTestService(t)

	rawData := json.RawMessage(`{
		"id": 12345,
		"full_name": "myorg/my-repo",
		"name": "my-repo",
		"html_url": "https://github.com/myorg/my-repo",
		"clone_url": "https://github.com/myorg/my-repo.git",
		"description": "A test repo",
		"language": "Go",
		"created_at": "2024-01-01T00:00:00Z",
		"updated_at": "2024-06-01T00:00:00Z",
		"owner": {"login": "myorg"}
	}`)

	scopeData, err := svc.buildScopeFromLookup("github", rawData)
	if err != nil {
		t.Fatalf("buildScopeFromLookup: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(scopeData, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["fullName"] != "myorg/my-repo" {
		t.Errorf("fullName = %v", m["fullName"])
	}
	if m["githubId"] != float64(12345) {
		t.Errorf("githubId = %v", m["githubId"])
	}
	if m["owner"] != "myorg" {
		t.Errorf("owner = %v", m["owner"])
	}
}

func TestBuildScopeFromLookup_GitLab(t *testing.T) {
	svc, _, _ := setupTestService(t)

	rawData := json.RawMessage(`{
		"id": 999,
		"path_with_namespace": "group/subgroup/project",
		"name": "project",
		"http_url_to_repo": "https://gitlab.com/group/subgroup/project",
		"created_at": "2024-01-01T00:00:00Z",
		"last_activity_at": "2024-06-01T00:00:00Z"
	}`)

	scopeData, err := svc.buildScopeFromLookup("gitlab", rawData)
	if err != nil {
		t.Fatalf("buildScopeFromLookup: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(scopeData, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["pathWithNamespace"] != "group/subgroup/project" {
		t.Errorf("pathWithNamespace = %v", m["pathWithNamespace"])
	}
	if m["gitlabId"] != float64(999) {
		t.Errorf("gitlabId = %v", m["gitlabId"])
	}
}

func TestBuildScopeFromLookup_UnsupportedPlugin(t *testing.T) {
	svc, _, _ := setupTestService(t)
	_, err := svc.buildScopeFromLookup("bitbucket", json.RawMessage(`{"id":1}`))
	if err == nil {
		t.Error("expected error for unsupported plugin")
	}
}

// ---------------------------------------------------------------------------
// BrowseRemoteScopeCount tests
// ---------------------------------------------------------------------------

func TestBrowseRemoteScopeCount(t *testing.T) {
	svc, _, _ := setupTestService(t)
	ctx := context.Background()

	_, _ = svc.CreateTeamConnection(ctx, "team-count", ConnectionEntry{
		Plugin:   "github",
		Token:    "ghp_test",
		Endpoint: "https://api.github.com/",
	})

	count, err := svc.BrowseRemoteScopeCount(ctx, "team-count", "github")
	if err != nil {
		t.Fatalf("BrowseRemoteScopeCount: %v", err)
	}
	if count < 0 {
		t.Errorf("count = %d, want >= 0", count)
	}
}

func TestBrowseRemoteScopeCount_NoConnection(t *testing.T) {
	svc, _, _ := setupTestService(t)
	count, err := svc.BrowseRemoteScopeCount(context.Background(), "no-team", "github")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 for missing connection", count)
	}
}

func TestTriggerTeam(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	_, err := svc.CreateTeamConnection(ctx, "trig-team", ConnectionEntry{
		Plugin:     "github",
		Token:      "ghp_tok",
		Endpoint:   "https://api.github.com/",
		ExpiryDate: "2027-01-01",
	})
	if err != nil {
		t.Fatalf("CreateTeamConnection: %v", err)
	}

	addTeamSources(store, "trig-team", []database.SourceRow{{
		ID:   "src1",
		Type: "gitrepos",
		Repos: []database.RepoRow{
			{ID: "r1", Type: "github", Owner: "myorg", Name: "repo-a"},
		},
	}})

	if _, err := svc.SyncTeam(ctx, "trig-team"); err != nil {
		t.Fatalf("SyncTeam: %v", err)
	}

	result, err := svc.TriggerTeam(ctx, "trig-team")
	if err != nil {
		t.Fatalf("TriggerTeam: %v", err)
	}

	if len(result.Triggered) != 1 {
		t.Fatalf("expected 1 triggered, got %v", result.Triggered)
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %v", result.Errors)
	}

	fake.mu.Lock()
	triggered := fake.triggeredBPs
	fake.mu.Unlock()
	if len(triggered) != 1 {
		t.Errorf("expected 1 blueprint triggered in DevLake, got %d", len(triggered))
	}
}

func TestCleanupOrphanedScopes_DeletesUnusedConnection(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	addTeam(store, "team-a")
	connID, err := svc.CreateTeamConnection(ctx, "team-a", ConnectionEntry{
		Plugin: "jira", Token: "tok", Endpoint: "https://jira.example.com/", Username: "user@test.com",
	})
	if err != nil {
		t.Fatalf("CreateTeamConnection: %v", err)
	}

	// Verify config keys were stored.
	cfg, _ := store.GetEntityConfigKV(ctx, "team", "team-a")
	if cfg["devlake_jira_connection_id"] != strconv.Itoa(connID) {
		t.Fatalf("expected connection_id %d in config, got %q", connID, cfg["devlake_jira_connection_id"])
	}

	// Connection has zero scopes, no blueprints reference it.
	fake.mu.Lock()
	connCount := len(fake.connections["jira"])
	fake.mu.Unlock()
	if connCount != 1 {
		t.Fatalf("precondition: expected 1 jira connection, got %d", connCount)
	}

	deleted, errs := svc.CleanupOrphanedScopes(ctx, "team-a")
	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}

	// Connection should have been deleted.
	found := false
	for _, d := range deleted {
		if strings.Contains(d, "connection") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected connection deletion in result, got %v", deleted)
	}

	fake.mu.Lock()
	connCount = len(fake.connections["jira"])
	fake.mu.Unlock()
	if connCount != 0 {
		t.Errorf("expected 0 jira connections after cleanup, got %d", connCount)
	}

	// Connection ID config key should have been cleared.
	cfg, _ = store.GetEntityConfigKV(ctx, "team", "team-a")
	if v := cfg["devlake_jira_connection_id"]; v != "" {
		t.Errorf("expected config key devlake_jira_connection_id to be cleared, got %q", v)
	}
	// Credential keys are preserved for reuse.
	for _, suffix := range []string{"_token", "_endpoint", "_username"} {
		key := "devlake_jira" + suffix
		if v := cfg[key]; v == "" {
			t.Errorf("expected config key %q to be preserved, but it was cleared", key)
		}
	}
}

func TestSyncTeam_ResolvesJiraProjectByName(t *testing.T) {
	svc, fake, store := setupTestService(t)
	ctx := context.Background()

	// Set up two Jira projects with distinct boards.
	fake.mu.Lock()
	fake.remoteGroups = []fakeRemoteGroup{
		{key: "DEVSPACES", name: "Dev Spaces", boards: []fakeRemoteBoard{
			{id: 100, name: "Dev Spaces Sprint Board"},
		}},
		{key: "OSAC", name: "OSAC", boards: []fakeRemoteBoard{
			{id: 200, name: "OSAC Kanban Board"},
		}},
		{key: "CONCEPT", name: "Concept", boards: []fakeRemoteBoard{
			{id: 300, name: "Concept_Implement Milestone Compose Automation - Ch2"},
		}},
	}
	fake.mu.Unlock()

	addTeam(store, "team-ds")
	_, err := svc.CreateTeamConnection(ctx, "team-ds", ConnectionEntry{
		Plugin: "jira", Token: "tok", Endpoint: "https://jira.example.com/", Username: "user@test.com",
	})
	if err != nil {
		t.Fatalf("CreateTeamConnection: %v", err)
	}

	// Legacy import: project_name set, jira_project (key) empty.
	addTeamSources(store, "team-ds", []database.SourceRow{{
		ID: "workitems", Type: "workitems",
		Projects: []database.WorkitemProjRow{
			{ID: "p1", Type: "jira", ProjectName: "Dev Spaces", JiraProject: ""},
		},
	}})

	result, err := svc.SyncTeam(ctx, "team-ds")
	if err != nil {
		t.Fatalf("SyncTeam: %v", err)
	}
	if len(result.Failed) > 0 {
		t.Errorf("expected no failed scopes, got %v", result.Failed)
	}
	if len(result.Created) == 0 {
		t.Fatalf("expected blueprint to be created, got none; errors: %v", result.Errors)
	}

	// Verify the correct board was resolved — board 100, not 300.
	fake.mu.Lock()
	var foundScopeID string
	for _, bp := range fake.blueprints {
		for _, conn := range bp.Connections {
			for _, scope := range conn.Scopes {
				foundScopeID = scope.ScopeID
			}
		}
	}
	fake.mu.Unlock()

	if foundScopeID != "100" {
		t.Errorf("expected scope ID 100 (Dev Spaces Sprint Board), got %s", foundScopeID)
	}
}
