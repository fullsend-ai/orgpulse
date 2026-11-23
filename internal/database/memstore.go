package database

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type MemStore struct {
	mu sync.Mutex

	orgs     map[string]OrgRow
	products map[string]ProductRow
	teams    map[string]TeamRow

	orgProductLinks  map[string][]RelEntry
	productTeamLinks map[string][]RelEntry
	teamProdLabels   map[string][]string

	entityConfig map[string]map[string]string

	dashboards   map[int64]DashboardRow
	dashboardSeq int64

	sources map[string]map[string]SourceRow

	overrides map[string][]OverrideRepoRow

	subscriptions   map[int64]SubscriptionRow
	subscriptionSeq int64
	elementSeq      int64

	alerts   map[int64]AlertRow
	alertSeq int64

	cannedQueries  map[int64]CannedQueryRow
	cannedQuerySeq int64

	cannedScripts   map[int64]CannedScriptRow
	cannedScriptSeq int64

	jsonSchemas   map[int64]JsonSchemaRow
	jsonSchemaSeq int64

	storedProcedures   map[int64]StoredProcedureRow
	storedProcedureSeq int64

	workflows        map[int64]WorkflowRow
	workflowSeq      int64
	workflowQuerySeq int64
	workflowVersions map[int64][]WorkflowVersionRow

	apiKeys      map[int64]APIKeyInfo
	apiKeyHashes map[int64]string
	apiKeySeq    int64
}

func NewMemStore() *MemStore {
	s := &MemStore{}
	s.init()
	return s
}

func (s *MemStore) init() {
	s.orgs = make(map[string]OrgRow)
	s.products = make(map[string]ProductRow)
	s.teams = make(map[string]TeamRow)
	s.orgProductLinks = make(map[string][]RelEntry)
	s.productTeamLinks = make(map[string][]RelEntry)
	s.teamProdLabels = make(map[string][]string)
	s.entityConfig = make(map[string]map[string]string)
	s.dashboards = make(map[int64]DashboardRow)
	s.dashboardSeq = 0
	s.sources = make(map[string]map[string]SourceRow)
	s.overrides = make(map[string][]OverrideRepoRow)
	s.subscriptions = make(map[int64]SubscriptionRow)
	s.subscriptionSeq = 0
	s.elementSeq = 0
	s.alerts = make(map[int64]AlertRow)
	s.alertSeq = 0
	s.cannedQueries = make(map[int64]CannedQueryRow)
	s.cannedQuerySeq = 0
	s.cannedScripts = make(map[int64]CannedScriptRow)
	s.cannedScriptSeq = 0
	s.jsonSchemas = make(map[int64]JsonSchemaRow)
	s.jsonSchemaSeq = 0
	s.storedProcedures = make(map[int64]StoredProcedureRow)
	s.storedProcedureSeq = 0
	s.workflows = make(map[int64]WorkflowRow)
	s.workflowSeq = 0
	s.workflowQuerySeq = 0
	s.workflowVersions = make(map[int64][]WorkflowVersionRow)
	s.apiKeys = make(map[int64]APIKeyInfo)
	s.apiKeyHashes = make(map[int64]string)
	s.apiKeySeq = 0
}

func (s *MemStore) ClearConfig(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	return nil
}

func (s *MemStore) ClearSeeds(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	return nil
}

func (s *MemStore) ClearWorkflows(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflows = map[int64]WorkflowRow{}
	return nil
}

func (s *MemStore) PingContext(_ context.Context) error {
	return nil
}

// --- Orgs ---

func (s *MemStore) ListOrgRows(_ context.Context) ([]OrgRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]OrgRow, 0, len(s.orgs))
	for _, o := range s.orgs {
		out = append(out, o)
	}
	return out, nil
}

func (s *MemStore) GetOrgRow(_ context.Context, id string) (*OrgRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orgs[id]
	if !ok {
		return nil, nil
	}
	return &o, nil
}

func (s *MemStore) CreateOrg(_ context.Context, input OrgRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hasEntityID(s.orgs, input.ID) {
		return ErrDuplicateName
	}
	s.orgs[input.ID] = input
	return nil
}

func (s *MemStore) UpdateOrg(_ context.Context, id string, input OrgRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[id]; !ok {
		return ErrNotFound
	}
	input.ID = id
	s.orgs[id] = input
	return nil
}

func (s *MemStore) DeleteOrg(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[id]; !ok {
		return ErrNotFound
	}
	delete(s.orgs, id)
	return nil
}

// --- Products ---

func (s *MemStore) ListProductRows(_ context.Context) ([]ProductRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ProductRow, 0, len(s.products))
	for _, p := range s.products {
		out = append(out, p)
	}
	return out, nil
}

func (s *MemStore) GetProductRow(_ context.Context, id string) (*ProductRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.products[id]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (s *MemStore) CreateProduct(_ context.Context, input ProductRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hasEntityID(s.products, input.ID) {
		return ErrDuplicateName
	}
	s.products[input.ID] = input
	return nil
}

func (s *MemStore) UpdateProduct(_ context.Context, id string, input ProductRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.products[id]; !ok {
		return ErrNotFound
	}
	input.ID = id
	s.products[id] = input
	return nil
}

func (s *MemStore) DeleteProduct(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.products[id]; !ok {
		return ErrNotFound
	}
	delete(s.products, id)
	return nil
}

// --- Teams ---

func (s *MemStore) ListTeamRows(_ context.Context) ([]TeamRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TeamRow, 0, len(s.teams))
	for _, t := range s.teams {
		out = append(out, t)
	}
	return out, nil
}

func (s *MemStore) GetTeamRow(_ context.Context, id string) (*TeamRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.teams[id]
	if !ok {
		return nil, nil
	}
	return &t, nil
}

func (s *MemStore) CreateTeam(_ context.Context, input TeamRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hasEntityID(s.teams, input.ID) {
		return ErrDuplicateName
	}
	s.teams[input.ID] = input
	return nil
}

func hasEntityID[T any](items map[string]T, id string) bool {
	for existingID := range items {
		if strings.EqualFold(existingID, id) {
			return true
		}
	}
	return false
}

func (s *MemStore) UpdateTeam(_ context.Context, id string, input TeamRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.teams[id]; !ok {
		return ErrNotFound
	}
	input.ID = id
	s.teams[id] = input
	return nil
}

func (s *MemStore) DeleteTeam(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.teams[id]; !ok {
		return ErrNotFound
	}
	delete(s.teams, id)
	return nil
}

// --- Links ---

func (s *MemStore) GetOrgProductLinks(_ context.Context, orgID string) ([]RelEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.orgProductLinks[orgID], nil
}

func (s *MemStore) ReplaceOrgProductLinks(_ context.Context, orgID string, entries []RelEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orgProductLinks[orgID] = entries
	return nil
}

func (s *MemStore) GetProductTeamLinks(_ context.Context, productID string) ([]RelEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.productTeamLinks[productID], nil
}

func (s *MemStore) ReplaceProductTeamLinks(_ context.Context, productID string, entries []RelEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.productTeamLinks[productID] = entries
	return nil
}

func (s *MemStore) GetTeamProductLabels(_ context.Context, teamID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.teamProdLabels[teamID], nil
}

func (s *MemStore) ReplaceTeamProductLabels(_ context.Context, teamID string, labels []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.teamProdLabels[teamID] = labels
	return nil
}

// --- Entity config ---

func configKey(entityType, entityID string) string {
	return entityType + ":" + entityID
}

func (s *MemStore) GetEntityConfigKV(_ context.Context, entityType, entityID string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.entityConfig[configKey(entityType, entityID)]
	if !ok {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out, nil
}

func (s *MemStore) ReplaceEntityConfig(_ context.Context, entityType, entityID string, config map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make(map[string]string, len(config))
	for k, v := range config {
		cp[k] = v
	}
	s.entityConfig[configKey(entityType, entityID)] = cp
	return nil
}

func (s *MemStore) SetEntityConfigKey(_ context.Context, entityType, entityID, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ck := configKey(entityType, entityID)
	m, ok := s.entityConfig[ck]
	if !ok {
		m = make(map[string]string)
		s.entityConfig[ck] = m
	}
	m[key] = value
	return nil
}

func (s *MemStore) SetEntityConfigKeys(_ context.Context, entityType, entityID string, values map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ck := configKey(entityType, entityID)
	m, ok := s.entityConfig[ck]
	if !ok {
		m = make(map[string]string)
		s.entityConfig[ck] = m
	}
	for key, value := range values {
		m[key] = value
	}
	return nil
}

func (s *MemStore) DeleteEntityConfigKey(_ context.Context, entityType, entityID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.entityConfig[configKey(entityType, entityID)]; ok {
		delete(m, key)
	}
	return nil
}

func (s *MemStore) DeleteEntityConfig(_ context.Context, entityType, entityID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entityConfig, configKey(entityType, entityID))
	return nil
}

// --- Dashboards ---

func (s *MemStore) ListDashboardRows(_ context.Context, ownerType, ownerID string) ([]DashboardRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []DashboardRow
	for _, d := range s.dashboards {
		if d.OwnerType == ownerType && d.OwnerID == ownerID {
			out = append(out, d)
		}
	}
	if out == nil {
		out = []DashboardRow{}
	}
	return out, nil
}

func (s *MemStore) GetDashboardRow(_ context.Context, id int64) (*DashboardRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.dashboards[id]
	if !ok {
		return nil, nil
	}
	return &d, nil
}

func (s *MemStore) CreateDashboard(_ context.Context, input DashboardRow) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dashboardSeq++
	input.AutoID = s.dashboardSeq
	s.dashboards[s.dashboardSeq] = input
	return s.dashboardSeq, nil
}

func (s *MemStore) UpdateDashboard(_ context.Context, id int64, input DashboardRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.dashboards[id]; !ok {
		return ErrNotFound
	}
	input.AutoID = id
	s.dashboards[id] = input
	return nil
}

func (s *MemStore) DeleteDashboard(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.dashboards[id]; !ok {
		return ErrNotFound
	}
	delete(s.dashboards, id)
	return nil
}

// --- Sources ---

func (s *MemStore) ListSourceRows(_ context.Context, teamID string) ([]SourceRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.sources[teamID]
	out := make([]SourceRow, 0, len(m))
	for _, src := range m {
		out = append(out, src)
	}
	return out, nil
}

func (s *MemStore) GetSourceRow(_ context.Context, teamID, sourceID string) (*SourceRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.sources[teamID]
	src, ok := m[sourceID]
	if !ok {
		return nil, nil
	}
	return &src, nil
}

func (s *MemStore) CreateSource(_ context.Context, teamID string, input SourceRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sources[teamID] == nil {
		s.sources[teamID] = make(map[string]SourceRow)
	}
	s.sources[teamID][input.ID] = input
	return nil
}

func (s *MemStore) UpdateSource(_ context.Context, teamID, sourceID string, input SourceRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.sources[teamID]
	if _, ok := m[sourceID]; !ok {
		return ErrNotFound
	}
	input.ID = sourceID
	m[sourceID] = input
	return nil
}

func (s *MemStore) DeleteSource(_ context.Context, teamID, sourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.sources[teamID]
	if _, ok := m[sourceID]; !ok {
		return ErrNotFound
	}
	delete(m, sourceID)
	return nil
}

// --- Overrides ---

func (s *MemStore) GetProductOverrideRows(_ context.Context, productID string) ([]OverrideRepoRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overrides[productID], nil
}

func (s *MemStore) ReplaceProductOverrides(_ context.Context, productID string, overrides []OverrideRepoRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides[productID] = overrides
	return nil
}

// --- Subscriptions ---

func (s *MemStore) ListSubscriptionRows(_ context.Context) ([]SubscriptionRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SubscriptionRow, 0, len(s.subscriptions))
	for _, sub := range s.subscriptions {
		out = append(out, sub)
	}
	return out, nil
}

func (s *MemStore) GetSubscriptionRow(_ context.Context, id int64) (*SubscriptionRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subscriptions[id]
	if !ok {
		return nil, nil
	}
	return &sub, nil
}

func (s *MemStore) CreateSubscription(_ context.Context, input SubscriptionRow) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscriptionSeq++
	input.ID = s.subscriptionSeq
	now := time.Now().Format(time.RFC3339)
	input.CreatedAt = now
	input.UpdatedAt = now
	for i := range input.Elements {
		s.elementSeq++
		input.Elements[i].ID = s.elementSeq
	}
	s.subscriptions[s.subscriptionSeq] = input
	return s.subscriptionSeq, nil
}

func (s *MemStore) UpdateSubscription(_ context.Context, id int64, input SubscriptionRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.subscriptions[id]
	if !ok {
		return ErrNotFound
	}
	input.ID = id
	input.CreatedAt = existing.CreatedAt
	input.UpdatedAt = time.Now().Format(time.RFC3339)
	for i := range input.Elements {
		s.elementSeq++
		input.Elements[i].ID = s.elementSeq
	}
	s.subscriptions[id] = input
	return nil
}

func (s *MemStore) UpdateSubscriptionLastRunAt(_ context.Context, id int64, t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subscriptions[id]
	if !ok {
		return ErrNotFound
	}
	ts := t.Format(time.RFC3339)
	sub.LastRunAt = &ts
	s.subscriptions[id] = sub
	return nil
}

func (s *MemStore) DeleteSubscription(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subscriptions[id]; !ok {
		return ErrNotFound
	}
	delete(s.subscriptions, id)
	return nil
}

// --- Alerts ---

func (s *MemStore) ListAlertRows(_ context.Context) ([]AlertRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AlertRow, 0, len(s.alerts))
	for _, a := range s.alerts {
		out = append(out, a)
	}
	return out, nil
}

func (s *MemStore) GetAlertRow(_ context.Context, id int64) (*AlertRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.alerts[id]
	if !ok {
		return nil, nil
	}
	return &a, nil
}

func (s *MemStore) CreateAlert(_ context.Context, input AlertRow) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alertSeq++
	input.ID = s.alertSeq
	now := time.Now().Format(time.RFC3339)
	input.CreatedAt = now
	input.UpdatedAt = now
	s.alerts[s.alertSeq] = input
	return s.alertSeq, nil
}

func (s *MemStore) UpdateAlert(_ context.Context, id int64, input AlertRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.alerts[id]
	if !ok {
		return ErrNotFound
	}
	input.ID = id
	input.CreatedAt = existing.CreatedAt
	input.UpdatedAt = time.Now().Format(time.RFC3339)
	s.alerts[id] = input
	return nil
}

func (s *MemStore) DeleteAlert(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.alerts[id]; !ok {
		return ErrNotFound
	}
	delete(s.alerts, id)
	return nil
}

// --- Canned Queries ---

func (s *MemStore) ListCannedQueryRows(_ context.Context) ([]CannedQueryRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CannedQueryRow, 0, len(s.cannedQueries))
	for _, q := range s.cannedQueries {
		out = append(out, q)
	}
	return out, nil
}

func (s *MemStore) GetCannedQueryRow(_ context.Context, id int64) (*CannedQueryRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.cannedQueries[id]
	if !ok {
		return nil, nil
	}
	return &q, nil
}

func (s *MemStore) CreateCannedQuery(_ context.Context, input CannedQueryRow) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cannedQuerySeq++
	input.ID = s.cannedQuerySeq
	now := time.Now().Format(time.RFC3339)
	input.CreatedAt = now
	input.UpdatedAt = now
	s.cannedQueries[s.cannedQuerySeq] = input
	return s.cannedQuerySeq, nil
}

func (s *MemStore) UpdateCannedQuery(_ context.Context, id int64, input CannedQueryRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.cannedQueries[id]
	if !ok {
		return ErrNotFound
	}
	input.ID = id
	input.CreatedAt = existing.CreatedAt
	input.UpdatedAt = time.Now().Format(time.RFC3339)
	s.cannedQueries[id] = input
	return nil
}

func (s *MemStore) DeleteCannedQuery(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cannedQueries[id]; !ok {
		return ErrNotFound
	}
	delete(s.cannedQueries, id)
	return nil
}

// --- Canned Scripts ---

func (s *MemStore) ListCannedScriptRows(_ context.Context) ([]CannedScriptRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CannedScriptRow, 0, len(s.cannedScripts))
	for _, sc := range s.cannedScripts {
		out = append(out, sc)
	}
	return out, nil
}

func (s *MemStore) GetCannedScriptRow(_ context.Context, id int64) (*CannedScriptRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc, ok := s.cannedScripts[id]
	if !ok {
		return nil, nil
	}
	return &sc, nil
}

func (s *MemStore) CreateCannedScript(_ context.Context, input CannedScriptRow) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cannedScriptSeq++
	input.ID = s.cannedScriptSeq
	now := time.Now().Format(time.RFC3339)
	input.CreatedAt = now
	input.UpdatedAt = now
	s.cannedScripts[s.cannedScriptSeq] = input
	return s.cannedScriptSeq, nil
}

func (s *MemStore) UpdateCannedScript(_ context.Context, id int64, input CannedScriptRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.cannedScripts[id]
	if !ok {
		return ErrNotFound
	}
	input.ID = id
	input.CreatedAt = existing.CreatedAt
	input.UpdatedAt = time.Now().Format(time.RFC3339)
	s.cannedScripts[id] = input
	return nil
}

func (s *MemStore) DeleteCannedScript(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cannedScripts[id]; !ok {
		return ErrNotFound
	}
	delete(s.cannedScripts, id)
	return nil
}

// --- JSON Schemas ---

func (s *MemStore) ListJsonSchemaRows(_ context.Context) ([]JsonSchemaRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JsonSchemaRow, 0, len(s.jsonSchemas))
	for _, js := range s.jsonSchemas {
		out = append(out, js)
	}
	return out, nil
}

func (s *MemStore) GetJsonSchemaRow(_ context.Context, id int64) (*JsonSchemaRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	js, ok := s.jsonSchemas[id]
	if !ok {
		return nil, nil
	}
	return &js, nil
}

func (s *MemStore) CreateJsonSchema(_ context.Context, input JsonSchemaRow) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jsonSchemaSeq++
	input.ID = s.jsonSchemaSeq
	now := time.Now().Format(time.RFC3339)
	input.CreatedAt = now
	input.UpdatedAt = now
	if input.Version == 0 {
		input.Version = 1
	}
	s.jsonSchemas[s.jsonSchemaSeq] = input
	return s.jsonSchemaSeq, nil
}

func (s *MemStore) UpdateJsonSchema(_ context.Context, id int64, input JsonSchemaRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.jsonSchemas[id]
	if !ok {
		return ErrNotFound
	}
	input.ID = id
	input.CreatedAt = existing.CreatedAt
	input.UpdatedAt = time.Now().Format(time.RFC3339)
	input.Version = existing.Version + 1
	s.jsonSchemas[id] = input
	return nil
}

func (s *MemStore) DeleteJsonSchema(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jsonSchemas[id]; !ok {
		return ErrNotFound
	}
	delete(s.jsonSchemas, id)
	return nil
}

func (s *MemStore) GetJsonSchemaByName(_ context.Context, name string) (*JsonSchemaRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, js := range s.jsonSchemas {
		if js.Name == name {
			return &js, nil
		}
	}
	return nil, nil
}

// --- Stored Procedures ---

func (s *MemStore) ListAllStoredProcedureRows(_ context.Context) ([]StoredProcedureRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []StoredProcedureRow
	for _, r := range s.storedProcedures {
		out = append(out, r)
	}
	return out, nil
}

func (s *MemStore) ListStoredProcedureRows(_ context.Context, workflowID int64) ([]StoredProcedureRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []StoredProcedureRow
	for _, r := range s.storedProcedures {
		if r.WorkflowID == workflowID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *MemStore) GetStoredProcedureRow(_ context.Context, id int64) (*StoredProcedureRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.storedProcedures[id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (s *MemStore) CreateStoredProcedure(_ context.Context, input StoredProcedureRow) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sp := range s.storedProcedures {
		if sp.Name == input.Name && sp.WorkflowID == input.WorkflowID {
			return 0, ErrDuplicateName
		}
	}
	s.storedProcedureSeq++
	input.ID = s.storedProcedureSeq
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	input.CreatedAt = now
	input.UpdatedAt = now
	s.storedProcedures[input.ID] = input
	return input.ID, nil
}

func (s *MemStore) UpdateStoredProcedure(_ context.Context, id int64, input StoredProcedureRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.storedProcedures[id]
	if !ok {
		return ErrNotFound
	}
	for oid, sp := range s.storedProcedures {
		if sp.Name == input.Name && sp.WorkflowID == existing.WorkflowID && oid != id {
			return ErrDuplicateName
		}
	}
	input.ID = id
	input.CreatedAt = existing.CreatedAt
	input.UpdatedAt = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	s.storedProcedures[id] = input
	return nil
}

func (s *MemStore) DeleteStoredProcedure(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.storedProcedures[id]; !ok {
		return ErrNotFound
	}
	delete(s.storedProcedures, id)
	return nil
}

// --- Workflows ---

func (s *MemStore) ListWorkflowRows(_ context.Context) ([]WorkflowRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]WorkflowRow, 0, len(s.workflows))
	for _, w := range s.workflows {
		out = append(out, w)
	}
	return out, nil
}

func (s *MemStore) GetWorkflowRow(_ context.Context, id int64) (*WorkflowRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workflows[id]
	if !ok {
		return nil, nil
	}
	return &w, nil
}

func (s *MemStore) GetWorkflowByName(_ context.Context, name string) (*WorkflowRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.workflows {
		if w.Name == name && !w.Archived {
			return &w, nil
		}
	}
	return nil, nil
}

func (s *MemStore) CreateWorkflow(_ context.Context, input WorkflowRow) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.workflows {
		if w.Name == input.Name {
			return 0, ErrDuplicateName
		}
	}
	s.workflowSeq++
	input.ID = s.workflowSeq
	now := time.Now().Format(time.RFC3339)
	input.CreatedAt = now
	input.UpdatedAt = now
	for i := range input.Queries {
		s.workflowQuerySeq++
		input.Queries[i].ID = s.workflowQuerySeq
	}
	s.workflows[s.workflowSeq] = input
	return s.workflowSeq, nil
}

func (s *MemStore) UpdateWorkflow(_ context.Context, id int64, input WorkflowRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.workflows[id]
	if !ok {
		return ErrNotFound
	}
	input.ID = id
	input.CreatedAt = existing.CreatedAt
	input.UpdatedAt = time.Now().Format(time.RFC3339)
	for i := range input.Queries {
		s.workflowQuerySeq++
		input.Queries[i].ID = s.workflowQuerySeq
	}
	s.workflows[id] = input
	return nil
}

func (s *MemStore) SaveWorkflowDraft(_ context.Context, id int64, input WorkflowRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workflows[id]
	if !ok {
		return ErrNotFound
	}
	w.draft = &input
	s.workflows[id] = w
	return nil
}

func (s *MemStore) PublishWorkflow(_ context.Context, id int64, publishNote string) (*WorkflowRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workflows[id]
	if !ok {
		return nil, ErrNotFound
	}
	pub := w
	if w.draft != nil {
		d := *w.draft
		d.ID = w.ID
		d.CreatedAt = w.CreatedAt
		d.UpdatedAt = w.UpdatedAt
		d.Version = w.Version + 1
		d.draft = nil
		pub = d
	} else {
		pub.Version++
		pub.draft = nil
	}
	s.workflows[id] = pub
	ver := WorkflowVersionRow{
		WorkflowID:   id,
		Version:      pub.Version,
		Name:         pub.Name,
		Description:  pub.Description,
		InputScript:  pub.InputScript,
		OutputScript: pub.OutputScript,
		TestInput:    pub.TestInput,
		Tests:        pub.Tests,
		Queries:      pub.Queries,
		PublishNote:  publishNote,
		PublishedAt:  pub.UpdatedAt,
	}
	s.workflowVersions[id] = append(s.workflowVersions[id], ver)
	return &pub, nil
}

func (s *MemStore) ListWorkflowVersions(_ context.Context, workflowID int64) ([]WorkflowVersionRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	versions := s.workflowVersions[workflowID]
	out := make([]WorkflowVersionRow, len(versions))
	copy(out, versions)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (s *MemStore) GetWorkflowVersion(_ context.Context, workflowID int64, version int) (*WorkflowVersionRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.workflowVersions[workflowID] {
		if v.Version == version {
			return &v, nil
		}
	}
	return nil, nil
}

func (s *MemStore) SetProductionVersion(_ context.Context, workflowID int64, version int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	versions := s.workflowVersions[workflowID]
	found := false
	for i := range versions {
		versions[i].Production = versions[i].Version == version
		if versions[i].Version == version {
			found = true
		}
	}
	if !found {
		return ErrNotFound
	}
	s.workflowVersions[workflowID] = versions
	return nil
}

func (s *MemStore) GetWorkflowByNameAndVersion(_ context.Context, name string, version int) (*WorkflowRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for wfID, w := range s.workflows {
		if w.Name == name {
			for _, v := range s.workflowVersions[wfID] {
				if v.Version == version {
					return &WorkflowRow{
						ID: wfID, Name: v.Name, Description: v.Description,
						InputScript: v.InputScript, OutputScript: v.OutputScript,
						TestInput: v.TestInput, Version: v.Version, Queries: v.Queries,
					}, nil
				}
			}
			return nil, nil
		}
	}
	return nil, nil
}

func (s *MemStore) GetProductionWorkflowByName(_ context.Context, name string) (*WorkflowRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for wfID, w := range s.workflows {
		if w.Name == name {
			for _, v := range s.workflowVersions[wfID] {
				if v.Production {
					return &WorkflowRow{
						ID: wfID, Name: v.Name, Description: v.Description,
						InputScript: v.InputScript, OutputScript: v.OutputScript,
						TestInput: v.TestInput, Version: v.Version, Queries: v.Queries,
					}, nil
				}
			}
			return nil, nil
		}
	}
	return nil, nil
}

func (s *MemStore) ArchiveWorkflow(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workflows[id]
	if !ok {
		return ErrNotFound
	}
	// Clear production flag on all versions
	versions := s.workflowVersions[id]
	for i := range versions {
		versions[i].Production = false
	}
	s.workflowVersions[id] = versions
	// Set archived
	w.Archived = true
	s.workflows[id] = w
	return nil
}

func (s *MemStore) UnarchiveWorkflow(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workflows[id]
	if !ok {
		return ErrNotFound
	}
	w.Archived = false
	s.workflows[id] = w
	return nil
}

func (s *MemStore) DeleteWorkflow(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.workflows[id]; !ok {
		return ErrNotFound
	}
	delete(s.workflows, id)
	return nil
}

// --- Lookups ---

func (s *MemStore) GetOrgForProduct(_ context.Context, productID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for orgID, links := range s.orgProductLinks {
		for _, l := range links {
			if l.ID == productID {
				return orgID, nil
			}
		}
	}
	return "", fmt.Errorf("no org found for product %q", productID)
}

func (s *MemStore) ListAllTeamProductLinks(_ context.Context) ([]TeamProductLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []TeamProductLink
	for productID, links := range s.productTeamLinks {
		for _, l := range links {
			out = append(out, TeamProductLink{TeamID: l.ID, ProductID: productID})
		}
	}
	return out, nil
}

// --- Auth ---

func (s *MemStore) ValidateAPIKey(_ context.Context, keyHash string) (*APIKeyInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, h := range s.apiKeyHashes {
		if h == keyHash {
			k := s.apiKeys[id]
			return &k, nil
		}
	}
	return nil, nil
}

func (s *MemStore) ListAPIKeys(_ context.Context) ([]APIKeyInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]APIKeyInfo, 0, len(s.apiKeys))
	for _, k := range s.apiKeys {
		out = append(out, k)
	}
	return out, nil
}

func (s *MemStore) CreateAPIKey(_ context.Context, name, keyHash string, isAdmin bool, scopedUser string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiKeySeq++
	s.apiKeys[s.apiKeySeq] = APIKeyInfo{
		ID:         s.apiKeySeq,
		Name:       name,
		IsAdmin:    isAdmin,
		ScopedUser: scopedUser,
	}
	s.apiKeyHashes[s.apiKeySeq] = keyHash
	return s.apiKeySeq, nil
}

func (s *MemStore) DeleteAPIKey(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.apiKeys[id]; !ok {
		return ErrNotFound
	}
	delete(s.apiKeys, id)
	return nil
}
