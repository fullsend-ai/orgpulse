package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Row types map directly to database tables (snake_case JSON for REST API).

type OrgRow struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AuthGroup string `json:"auth_group,omitempty"`
}

type ProductRow struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContactEmail string `json:"contact_email,omitempty"`
	AuthGroup    string `json:"auth_group,omitempty"`
}

type TeamRow struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContactEmail  string `json:"contact_email,omitempty"`
	AuthGroup     string `json:"auth_group,omitempty"`
	FetchURL      string `json:"fetch_url,omitempty"`
	FetchCheckURL string `json:"fetch_check_url,omitempty"`
}

type RelEntry struct {
	ID        string `json:"id"`
	SortOrder int    `json:"sort_order"`
}

type TeamProductLink struct {
	TeamID    string `json:"team_id"`
	ProductID string `json:"product_id"`
}

type DashboardRow struct {
	AutoID         int64        `json:"auto_id,omitempty"`
	OwnerType      string       `json:"owner_type"`
	OwnerID        string       `json:"owner_id"`
	DashboardID    string       `json:"dashboard_id"`
	Type           string       `json:"type"`
	Title          string       `json:"title"`
	Description    string       `json:"description,omitempty"`
	ChatURL        string       `json:"chat_url,omitempty"`
	AIAnalyticsURL string       `json:"ai_analytics_url,omitempty"`
	DefaultSource  string       `json:"default_source,omitempty"`
	ConnectionID   string       `json:"connection_id,omitempty"`
	JiraProject    string       `json:"jira_project,omitempty"`
	Icon           string       `json:"icon,omitempty"`
	Category       string       `json:"category,omitempty"`
	Diagrams       []DiagramRow `json:"diagrams,omitempty"`
}

type DiagramRow struct {
	AutoID           int64           `json:"auto_id,omitempty"`
	Position         int             `json:"position"`
	Type             string          `json:"type"`
	Endpoint         string          `json:"endpoint"`
	Span             *int            `json:"span,omitempty"`
	Title            string          `json:"title,omitempty"`
	AIAnalytics      bool            `json:"ai_analytics"`
	Description      string          `json:"description,omitempty"`
	DocumentationURL string          `json:"documentation_url,omitempty"`
	Params           json.RawMessage `json:"params,omitempty"`
}

type SourceRow struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Config   json.RawMessage   `json:"config,omitempty"`
	Repos    []RepoRow         `json:"repos,omitempty"`
	Projects []WorkitemProjRow `json:"projects,omitempty"`
}

type RepoRow struct {
	ID                     string   `json:"id"`
	Type                   string   `json:"type"`
	Owner                  string   `json:"owner"`
	Name                   string   `json:"name"`
	ExcludedDashboardTypes []string `json:"excluded_dashboard_types,omitempty"`
	Whitelist              []string `json:"whitelist,omitempty"`
}

type WorkitemProjRow struct {
	ID          string `json:"id"`
	ProjectName string `json:"project_name"`
	JiraProject string `json:"jira_project,omitempty"`
	BoardID     string `json:"board_id,omitempty"`
	Type        string `json:"type"`
}

type OverrideRepoRow struct {
	ID    string   `json:"id"`
	Type  string   `json:"type"`
	Owner string   `json:"owner"`
	Name  string   `json:"name"`
	Teams []string `json:"teams"`
}

type SubscriptionRow struct {
	ID             int64                    `json:"id,omitempty"`
	Email          string                   `json:"email"`
	ProductID      string                   `json:"product_id,omitempty"`
	TeamID         string                   `json:"team_id,omitempty"`
	AIAnalyticsURL string                   `json:"ai_analytics_url,omitempty"`
	IntervalDays   *int                     `json:"interval_days,omitempty"`
	LastRunAt      *string                  `json:"last_run_at,omitempty"`
	CreatedAt      string                   `json:"created_at,omitempty"`
	UpdatedAt      string                   `json:"updated_at,omitempty"`
	Elements       []SubscriptionElementRow `json:"elements,omitempty"`
}

type SubscriptionElementRow struct {
	ID        int64           `json:"id,omitempty"`
	SortOrder int             `json:"sort_order"`
	URL       string          `json:"url"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type AlertRow struct {
	ID             int64           `json:"id,omitempty"`
	Email          string          `json:"email"`
	ProductID      string          `json:"product_id,omitempty"`
	TeamID         string          `json:"team_id,omitempty"`
	URL            string          `json:"url"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	AIAnalyticsURL string          `json:"ai_analytics_url"`
	Rule           string          `json:"rule"`
	CreatedAt      string          `json:"created_at,omitempty"`
	UpdatedAt      string          `json:"updated_at,omitempty"`
}

type CannedQueryRow struct {
	ID          int64           `json:"id,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Category    string          `json:"category"`
	SQLText     string          `json:"sql_text"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Version     int             `json:"version"`
	CreatedAt   string          `json:"created_at,omitempty"`
	UpdatedAt   string          `json:"updated_at,omitempty"`
}

type CannedScriptRow struct {
	ID          int64  `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category"`
	JSText      string `json:"js_text"`
	Version     int    `json:"version"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type JsonSchemaRow struct {
	ID          int64  `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	JsonSchema  string `json:"json_schema"`
	Version     int    `json:"version"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type StoredProcedureRow struct {
	ID          int64  `json:"id,omitempty"`
	WorkflowID  int64  `json:"workflow_id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SQLText     string `json:"sql_text"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type WorkflowQueryRow struct {
	ID        int64  `json:"id,omitempty"`
	Stage     int    `json:"stage"`
	SortOrder int    `json:"sort_order"`
	Label     string `json:"label"`
	SQLText   string `json:"sql_text"`
}

type WorkflowTestCase struct {
	Name         string `json:"name"`
	Input        string `json:"input"`
	AssertScript string `json:"assert_script"`
	SchemaID     int64  `json:"schema_id,omitempty"`
}

type WorkflowRow struct {
	ID                int64              `json:"id,omitempty"`
	Name              string             `json:"name"`
	Description       string             `json:"description,omitempty"`
	InputScript       string             `json:"input_script"`
	OutputScript      string             `json:"output_script"`
	TestInput         string             `json:"test_input,omitempty"`
	Tests             []WorkflowTestCase `json:"tests,omitempty"`
	Version           int                `json:"version"`
	HasDraft          bool               `json:"has_draft,omitempty"`
	ProductionVersion int                `json:"production_version,omitempty"`
	Archived          bool               `json:"archived,omitempty"`
	CreatedAt         string             `json:"created_at,omitempty"`
	UpdatedAt         string             `json:"updated_at,omitempty"`
	Queries           []WorkflowQueryRow `json:"queries,omitempty"`

	draft *WorkflowRow // used by MemStore only
}

type WorkflowVersionRow struct {
	ID           int64              `json:"id,omitempty"`
	WorkflowID   int64              `json:"workflow_id"`
	Version      int                `json:"version"`
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	InputScript  string             `json:"input_script"`
	OutputScript string             `json:"output_script"`
	TestInput    string             `json:"test_input,omitempty"`
	Tests        []WorkflowTestCase `json:"tests,omitempty"`
	Queries      []WorkflowQueryRow `json:"queries,omitempty"`
	PublishNote  string             `json:"publish_note,omitempty"`
	Production   bool               `json:"production"`
	PublishedAt  string             `json:"published_at,omitempty"`
}

// --------------- Orgs ---------------

func (s *MySQLStore) ListOrgRows(ctx context.Context) ([]OrgRow, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, COALESCE(auth_group, '') FROM orgs ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrgRow
	for rows.Next() {
		var r OrgRow
		if err := rows.Scan(&r.ID, &r.Name, &r.AuthGroup); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MySQLStore) GetOrgRow(ctx context.Context, id string) (*OrgRow, error) {
	var r OrgRow
	err := s.db.QueryRowContext(ctx, "SELECT id, name, COALESCE(auth_group, '') FROM orgs WHERE id=?", id).Scan(&r.ID, &r.Name, &r.AuthGroup)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

func (s *MySQLStore) CreateOrg(ctx context.Context, input OrgRow) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO orgs (id, name, auth_group) VALUES (?, ?, NULLIF(?, ''))", input.ID, input.Name, input.AuthGroup)
	return translateDuplicateEntry(err)
}

func (s *MySQLStore) UpdateOrg(ctx context.Context, id string, input OrgRow) error {
	res, err := s.db.ExecContext(ctx, "UPDATE orgs SET name=?, auth_group=NULLIF(?, '') WHERE id=?", input.Name, input.AuthGroup, id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

func (s *MySQLStore) DeleteOrg(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM orgs WHERE id=?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// --------------- Products ---------------

func (s *MySQLStore) ListProductRows(ctx context.Context) ([]ProductRow, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, COALESCE(contact_email, ''), COALESCE(auth_group, '') FROM products ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProductRow
	for rows.Next() {
		var r ProductRow
		if err := rows.Scan(&r.ID, &r.Name, &r.ContactEmail, &r.AuthGroup); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MySQLStore) GetProductRow(ctx context.Context, id string) (*ProductRow, error) {
	var r ProductRow
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, COALESCE(contact_email, ''), COALESCE(auth_group, '') FROM products WHERE id=?", id).
		Scan(&r.ID, &r.Name, &r.ContactEmail, &r.AuthGroup)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

func (s *MySQLStore) CreateProduct(ctx context.Context, input ProductRow) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO products (id, name, contact_email, auth_group) VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''))",
		input.ID, input.Name, input.ContactEmail, input.AuthGroup)
	return translateDuplicateEntry(err)
}

func (s *MySQLStore) UpdateProduct(ctx context.Context, id string, input ProductRow) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE products SET name=?, contact_email=NULLIF(?, ''), auth_group=NULLIF(?, '') WHERE id=?",
		input.Name, input.ContactEmail, input.AuthGroup, id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

func (s *MySQLStore) DeleteProduct(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM products WHERE id=?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// --------------- Teams ---------------

func (s *MySQLStore) ListTeamRows(ctx context.Context) ([]TeamRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(contact_email, ''), COALESCE(auth_group, ''),
		        COALESCE(fetch_url, ''), COALESCE(fetch_check_url, '')
		 FROM teams ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TeamRow
	for rows.Next() {
		var r TeamRow
		if err := rows.Scan(&r.ID, &r.Name, &r.ContactEmail, &r.AuthGroup, &r.FetchURL, &r.FetchCheckURL); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MySQLStore) GetTeamRow(ctx context.Context, id string) (*TeamRow, error) {
	var r TeamRow
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(contact_email, ''), COALESCE(auth_group, ''),
		        COALESCE(fetch_url, ''), COALESCE(fetch_check_url, '')
		 FROM teams WHERE id=?`, id).
		Scan(&r.ID, &r.Name, &r.ContactEmail, &r.AuthGroup, &r.FetchURL, &r.FetchCheckURL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

func (s *MySQLStore) CreateTeam(ctx context.Context, input TeamRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO teams (id, name, contact_email, auth_group, fetch_url, fetch_check_url)
		 VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''))`,
		input.ID, input.Name, input.ContactEmail, input.AuthGroup, input.FetchURL, input.FetchCheckURL)
	return translateDuplicateEntry(err)
}

func (s *MySQLStore) UpdateTeam(ctx context.Context, id string, input TeamRow) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE teams SET name=?, contact_email=NULLIF(?, ''), auth_group=NULLIF(?, ''),
		        fetch_url=NULLIF(?, ''), fetch_check_url=NULLIF(?, '')
		 WHERE id=?`,
		input.Name, input.ContactEmail, input.AuthGroup, input.FetchURL, input.FetchCheckURL, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists int
		err = s.db.QueryRowContext(ctx, "SELECT 1 FROM teams WHERE id=?", id).Scan(&exists)
		if err != nil {
			return ErrNotFound
		}
	}
	return nil
}

func (s *MySQLStore) DeleteTeam(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM teams WHERE id=?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// --------------- Org-Product links ---------------

func (s *MySQLStore) GetOrgProductLinks(ctx context.Context, orgID string) ([]RelEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT product_id, sort_order FROM org_products WHERE org_id=? ORDER BY sort_order", orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RelEntry
	for rows.Next() {
		var e RelEntry
		if err := rows.Scan(&e.ID, &e.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *MySQLStore) ReplaceOrgProductLinks(ctx context.Context, orgID string, entries []RelEntry) error {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM org_products WHERE org_id=?", orgID); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO org_products (org_id, product_id, sort_order) VALUES (?, ?, ?)",
			orgID, e.ID, e.SortOrder); err != nil {
			return err
		}
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

// --------------- Product-Team links ---------------

func (s *MySQLStore) GetProductTeamLinks(ctx context.Context, productID string) ([]RelEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT team_id, sort_order FROM product_teams WHERE product_id=? ORDER BY sort_order", productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RelEntry
	for rows.Next() {
		var e RelEntry
		if err := rows.Scan(&e.ID, &e.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *MySQLStore) ReplaceProductTeamLinks(ctx context.Context, productID string, entries []RelEntry) error {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM product_teams WHERE product_id=?", productID); err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO product_teams (product_id, team_id, sort_order) VALUES (?, ?, ?)",
			productID, e.ID, e.SortOrder); err != nil {
			return err
		}
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

func (s *MySQLStore) ListAllTeamProductLinks(ctx context.Context) ([]TeamProductLink, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT team_id, product_id FROM product_teams")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TeamProductLink
	for rows.Next() {
		var e TeamProductLink
		if err := rows.Scan(&e.TeamID, &e.ProductID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// --------------- Team product labels ---------------

func (s *MySQLStore) GetTeamProductLabels(ctx context.Context, teamID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT product_name FROM team_product_labels WHERE team_id=? ORDER BY product_name", teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (s *MySQLStore) ReplaceTeamProductLabels(ctx context.Context, teamID string, labels []string) error {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM team_product_labels WHERE team_id=?", teamID); err != nil {
		return err
	}
	for _, label := range labels {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO team_product_labels (team_id, product_name) VALUES (?, ?)",
			teamID, label); err != nil {
			return err
		}
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

// --------------- Entity config ---------------

func (s *MySQLStore) GetEntityConfigKV(ctx context.Context, entityType, entityID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT config_key, COALESCE(config_value, '') FROM entity_config WHERE entity_type=? AND entity_id=?",
		entityType, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = s.decodeConfigValue(entityType, entityID, k, v)
	}
	return m, rows.Err()
}

func (s *MySQLStore) ReplaceEntityConfig(ctx context.Context, entityType, entityID string, config map[string]string) error {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}

	if _, err := tx.ExecContext(ctx,
		"DELETE FROM entity_config WHERE entity_type=? AND entity_id=?",
		entityType, entityID); err != nil {
		return err
	}
	for k, v := range config {
		v, err := s.encodeConfigValue(k, v)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO entity_config (entity_type, entity_id, config_key, config_value) VALUES (?, ?, ?, ?)",
			entityType, entityID, k, v); err != nil {
			return err
		}
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

func (s *MySQLStore) SetEntityConfigKey(ctx context.Context, entityType, entityID, key, value string) error {
	value, err := s.encodeConfigValue(key, value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO entity_config (entity_type, entity_id, config_key, config_value)
		 VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE config_value = VALUES(config_value)`,
		entityType, entityID, key, value)
	return err
}

func (s *MySQLStore) SetEntityConfigKeys(ctx context.Context, entityType, entityID string, values map[string]string) error {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}
	for key, value := range values {
		value, err := s.encodeConfigValue(key, value)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO entity_config (entity_type, entity_id, config_key, config_value)
			 VALUES (?, ?, ?, ?)
			 ON DUPLICATE KEY UPDATE config_value = VALUES(config_value)`,
			entityType, entityID, key, value); err != nil {
			return err
		}
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

func (s *MySQLStore) DeleteEntityConfigKey(ctx context.Context, entityType, entityID, key string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM entity_config WHERE entity_type=? AND entity_id=? AND config_key=?",
		entityType, entityID, key)
	return err
}

func (s *MySQLStore) DeleteEntityConfig(ctx context.Context, entityType, entityID string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM entity_config WHERE entity_type=? AND entity_id=?",
		entityType, entityID)
	return err
}

// --------------- Dashboards ---------------

func (s *MySQLStore) ListDashboardRows(ctx context.Context, ownerType, ownerID string) ([]DashboardRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_type, owner_id, dashboard_id, type, title,
		        COALESCE(description, ''), COALESCE(chat_url, ''),
		        COALESCE(ai_analytics_url, ''), COALESCE(default_source, ''),
		        COALESCE(connection_id, ''), COALESCE(jira_project, ''),
		        COALESCE(icon, ''), COALESCE(category, '')
		 FROM dashboards WHERE owner_type=? AND owner_id=? ORDER BY id`,
		ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DashboardRow
	for rows.Next() {
		var r DashboardRow
		if err := rows.Scan(&r.AutoID, &r.OwnerType, &r.OwnerID, &r.DashboardID,
			&r.Type, &r.Title, &r.Description, &r.ChatURL, &r.AIAnalyticsURL,
			&r.DefaultSource, &r.ConnectionID, &r.JiraProject,
			&r.Icon, &r.Category); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		if err := s.loadDashboardChildren(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *MySQLStore) GetDashboardRow(ctx context.Context, id int64) (*DashboardRow, error) {
	var r DashboardRow
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner_type, owner_id, dashboard_id, type, title,
		        COALESCE(description, ''), COALESCE(chat_url, ''),
		        COALESCE(ai_analytics_url, ''), COALESCE(default_source, ''),
		        COALESCE(connection_id, ''), COALESCE(jira_project, ''),
		        COALESCE(icon, ''), COALESCE(category, '')
		 FROM dashboards WHERE id=?`, id).
		Scan(&r.AutoID, &r.OwnerType, &r.OwnerID, &r.DashboardID,
			&r.Type, &r.Title, &r.Description, &r.ChatURL, &r.AIAnalyticsURL,
			&r.DefaultSource, &r.ConnectionID, &r.JiraProject,
			&r.Icon, &r.Category)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := s.loadDashboardChildren(ctx, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *MySQLStore) loadDashboardChildren(ctx context.Context, d *DashboardRow) error {
	diagRows, err := s.db.QueryContext(ctx,
		`SELECT id, position, type, endpoint, span, COALESCE(title, ''),
		        ai_analytics, COALESCE(description, ''), COALESCE(documentation_url, ''), params
		 FROM diagrams WHERE dashboard_id=? ORDER BY position`, d.AutoID)
	if err != nil {
		return err
	}
	defer diagRows.Close()
	for diagRows.Next() {
		var dr DiagramRow
		var spanN sql.NullInt64
		var params []byte
		if err := diagRows.Scan(&dr.AutoID, &dr.Position, &dr.Type, &dr.Endpoint,
			&spanN, &dr.Title, &dr.AIAnalytics, &dr.Description, &dr.DocumentationURL, &params); err != nil {
			return err
		}
		dr.Params = diagramParamsFromDB(params)
		if spanN.Valid {
			v := int(spanN.Int64)
			dr.Span = &v
		}
		d.Diagrams = append(d.Diagrams, dr)
	}
	return diagRows.Err()
}

func (s *MySQLStore) CreateDashboard(ctx context.Context, input DashboardRow) (int64, error) {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return 0, err
	}
	if owned {
		defer rollbackTx(tx)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO dashboards (owner_type, owner_id, dashboard_id, type, title,
		        description, chat_url, ai_analytics_url, default_source,
		        connection_id, jira_project, icon, category)
		 VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''),
		        NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''))`,
		input.OwnerType, input.OwnerID, input.DashboardID, input.Type, input.Title,
		input.Description, input.ChatURL, input.AIAnalyticsURL,
		input.DefaultSource, input.ConnectionID, input.JiraProject,
		input.Icon, input.Category)
	if err != nil {
		return 0, err
	}
	autoID, _ := res.LastInsertId()

	if err := insertDashboardChildren(ctx, tx, autoID, input); err != nil {
		return 0, err
	}
	if owned {
		return autoID, tx.Commit()
	}
	return autoID, nil
}

func (s *MySQLStore) UpdateDashboard(ctx context.Context, id int64, input DashboardRow) error {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE dashboards SET dashboard_id=?, type=?, title=?,
		        description=NULLIF(?, ''), chat_url=NULLIF(?, ''),
		        ai_analytics_url=NULLIF(?, ''), default_source=NULLIF(?, ''),
		        connection_id=NULLIF(?, ''), jira_project=NULLIF(?, ''),
		        icon=NULLIF(?, ''), category=NULLIF(?, '')
		 WHERE id=?`,
		input.DashboardID, input.Type, input.Title,
		input.Description, input.ChatURL, input.AIAnalyticsURL,
		input.DefaultSource, input.ConnectionID, input.JiraProject,
		input.Icon, input.Category, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM dashboards WHERE id=?", id).Scan(&exists); err != nil {
			return ErrNotFound
		}
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM diagrams WHERE dashboard_id=?", id); err != nil {
		return err
	}
	if err := insertDashboardChildren(ctx, tx, id, input); err != nil {
		return err
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

func insertDashboardChildren(ctx context.Context, tx *sql.Tx, dashID int64, input DashboardRow) error {
	for _, d := range input.Diagrams {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO diagrams (dashboard_id, position, type, endpoint, span, title, ai_analytics, description, documentation_url, params)
			 VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), NULLIF(?, ''), ?)`,
			dashID, d.Position, d.Type, d.Endpoint, d.Span, d.Title, d.AIAnalytics, d.Description, d.DocumentationURL,
			diagramParamsToDB(d.Params)); err != nil {
			return err
		}
	}
	return nil
}

func diagramParamsToDB(p json.RawMessage) any {
	if len(p) == 0 || string(p) == "null" {
		return nil
	}
	return string(p)
}

func diagramParamsFromDB(b []byte) json.RawMessage {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return json.RawMessage(b)
}

func (s *MySQLStore) DeleteDashboard(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM dashboards WHERE id=?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// --------------- Sources ---------------

func (s *MySQLStore) ListSourceRows(ctx context.Context, teamID string) ([]SourceRow, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, type, config FROM sources WHERE team_id=? ORDER BY id", teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SourceRow
	for rows.Next() {
		var r SourceRow
		var cfgBytes []byte
		if err := rows.Scan(&r.ID, &r.Type, &cfgBytes); err != nil {
			return nil, err
		}
		if len(cfgBytes) > 0 && string(cfgBytes) != "null" {
			r.Config = json.RawMessage(cfgBytes)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		if err := s.loadSourceChildren(ctx, teamID, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *MySQLStore) GetSourceRow(ctx context.Context, teamID, sourceID string) (*SourceRow, error) {
	var r SourceRow
	var cfgBytes []byte
	err := s.db.QueryRowContext(ctx,
		"SELECT id, type, config FROM sources WHERE team_id=? AND id=?",
		teamID, sourceID).Scan(&r.ID, &r.Type, &cfgBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(cfgBytes) > 0 && string(cfgBytes) != "null" {
		r.Config = json.RawMessage(cfgBytes)
	}
	if err := s.loadSourceChildren(ctx, teamID, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *MySQLStore) loadSourceChildren(ctx context.Context, teamID string, src *SourceRow) error {
	repoRows, err := s.db.QueryContext(ctx,
		"SELECT id, type, owner, name FROM repos WHERE team_id=? AND source_id=? ORDER BY id",
		teamID, src.ID)
	if err != nil {
		return err
	}
	defer repoRows.Close()
	for repoRows.Next() {
		var r RepoRow
		if err := repoRows.Scan(&r.ID, &r.Type, &r.Owner, &r.Name); err != nil {
			return err
		}
		excl, err := s.getRepoExcludedTypes(ctx, teamID, src.ID, r.ID)
		if err != nil {
			return err
		}
		r.ExcludedDashboardTypes = excl
		wl, err := s.getRepoWhitelist(ctx, teamID, src.ID, r.ID)
		if err != nil {
			return err
		}
		r.Whitelist = wl
		src.Repos = append(src.Repos, r)
	}
	if err := repoRows.Err(); err != nil {
		return err
	}

	projRows, err := s.db.QueryContext(ctx,
		`SELECT id, project_name, COALESCE(jira_project, ''), COALESCE(board_id, ''), type
		 FROM workitem_projects WHERE team_id=? AND source_id=? ORDER BY id`,
		teamID, src.ID)
	if err != nil {
		return err
	}
	defer projRows.Close()
	for projRows.Next() {
		var p WorkitemProjRow
		if err := projRows.Scan(&p.ID, &p.ProjectName, &p.JiraProject, &p.BoardID, &p.Type); err != nil {
			return err
		}
		src.Projects = append(src.Projects, p)
	}
	return projRows.Err()
}

func (s *MySQLStore) getRepoExcludedTypes(ctx context.Context, teamID, sourceID, repoID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT dashboard_type FROM repo_excluded_dashboard_types WHERE team_id=? AND source_id=? AND repo_id=?",
		teamID, sourceID, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *MySQLStore) getRepoWhitelist(ctx context.Context, teamID, sourceID, repoID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT username FROM repo_whitelist WHERE team_id=? AND source_id=? AND repo_id=?",
		teamID, sourceID, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *MySQLStore) CreateSource(ctx context.Context, teamID string, input SourceRow) error {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}

	var cfgVal any
	if len(input.Config) > 0 {
		cfgVal = []byte(input.Config)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO sources (id, team_id, type, config) VALUES (?, ?, ?, ?)",
		input.ID, teamID, input.Type, cfgVal); err != nil {
		return err
	}
	if err := insertSourceChildren(ctx, tx, teamID, input); err != nil {
		return err
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

func (s *MySQLStore) UpdateSource(ctx context.Context, teamID, sourceID string, input SourceRow) error {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}

	var exists bool
	if err := tx.QueryRowContext(ctx,
		"SELECT 1 FROM sources WHERE team_id=? AND id=?", teamID, sourceID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	var cfgVal any
	if len(input.Config) > 0 {
		cfgVal = []byte(input.Config)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE sources SET type=?, config=? WHERE team_id=? AND id=?",
		input.Type, cfgVal, teamID, sourceID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM repos WHERE team_id=? AND source_id=?", teamID, sourceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM workitem_projects WHERE team_id=? AND source_id=?", teamID, sourceID); err != nil {
		return err
	}
	input.ID = sourceID
	if err := insertSourceChildren(ctx, tx, teamID, input); err != nil {
		return err
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

func insertSourceChildren(ctx context.Context, tx *sql.Tx, teamID string, src SourceRow) error {
	for _, r := range src.Repos {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO repos (id, source_id, team_id, type, owner, name) VALUES (?, ?, ?, ?, ?, ?)",
			r.ID, src.ID, teamID, r.Type, r.Owner, r.Name); err != nil {
			return err
		}
		for _, dt := range r.ExcludedDashboardTypes {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO repo_excluded_dashboard_types (team_id, source_id, repo_id, dashboard_type) VALUES (?, ?, ?, ?)",
				teamID, src.ID, r.ID, dt); err != nil {
				return err
			}
		}
		for _, u := range r.Whitelist {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO repo_whitelist (team_id, source_id, repo_id, username) VALUES (?, ?, ?, ?)",
				teamID, src.ID, r.ID, u); err != nil {
				return err
			}
		}
	}
	for _, p := range src.Projects {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workitem_projects (id, source_id, team_id, project_name, jira_project, board_id, type)
			 VALUES (?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?)`,
			p.ID, src.ID, teamID, p.ProjectName, p.JiraProject, p.BoardID, p.Type); err != nil {
			return err
		}
	}
	return nil
}

func (s *MySQLStore) DeleteSource(ctx context.Context, teamID, sourceID string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM sources WHERE team_id=? AND id=?", teamID, sourceID)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// --------------- Product overrides ---------------

func (s *MySQLStore) GetProductOverrideRows(ctx context.Context, productID string) ([]OverrideRepoRow, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, repo_id, type, owner, name FROM product_override_repos WHERE product_id=? ORDER BY id",
		productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type entry struct {
		autoID int64
		repo   OverrideRepoRow
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.autoID, &e.repo.ID, &e.repo.Type, &e.repo.Owner, &e.repo.Name); err != nil {
			return nil, err
		}
		e.repo.Teams = []string{}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, e := range entries {
		teamRows, err := s.db.QueryContext(ctx,
			"SELECT team_id FROM product_override_repo_teams WHERE override_repo_id=? ORDER BY team_id",
			e.autoID)
		if err != nil {
			return nil, err
		}
		for teamRows.Next() {
			var t string
			if err := teamRows.Scan(&t); err != nil {
				defer teamRows.Close()
				return nil, err
			}
			entries[i].repo.Teams = append(entries[i].repo.Teams, t)
		}
		defer teamRows.Close()
		if err := teamRows.Err(); err != nil {
			return nil, err
		}
	}

	out := make([]OverrideRepoRow, len(entries))
	for i, e := range entries {
		out[i] = e.repo
	}
	return out, nil
}

func (s *MySQLStore) ReplaceProductOverrides(ctx context.Context, productID string, overrides []OverrideRepoRow) error {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM product_override_repos WHERE product_id=?", productID); err != nil {
		return err
	}
	for _, o := range overrides {
		res, err := tx.ExecContext(ctx,
			"INSERT INTO product_override_repos (product_id, repo_id, type, owner, name) VALUES (?, ?, ?, ?, ?)",
			productID, o.ID, o.Type, o.Owner, o.Name)
		if err != nil {
			return err
		}
		autoID, _ := res.LastInsertId()
		for _, t := range o.Teams {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO product_override_repo_teams (override_repo_id, team_id) VALUES (?, ?)",
				autoID, t); err != nil {
				return err
			}
		}
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

// --------------- bulk operations ---------------

func (s *MySQLStore) ClearSeeds(ctx context.Context) error {
	tables := []string{
		"workflow_versions",
		"workflow_queries",
		"workflows",
		"stored_procedures",
		"json_schemas",
		"canned_scripts",
		"canned_queries",
	}
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil { // #nosec -- admin-authored DELETE text
			return fmt.Errorf("clearing %s: %w", t, err)
		}
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

func (s *MySQLStore) ClearWorkflows(ctx context.Context) error {
	tables := []string{
		"workflow_versions",
		"workflow_queries",
		"workflows",
		"stored_procedures",
	}
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil { // #nosec -- admin-authored DELETE text
			return fmt.Errorf("clearing %s: %w", t, err)
		}
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

func (s *MySQLStore) ClearConfig(ctx context.Context) error {
	tables := []string{
		"alerts",
		"subscriptions",
		"canned_queries",
		"canned_scripts",
		"json_schemas",
		"workflows",
		"entity_config",
		"org_products",
		"product_teams",
		"team_product_labels",
		"dashboards",
		"sources",
		"product_override_repos",
		"orgs",
		"products",
		"teams",
	}
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil { // #nosec -- admin-authored DELETE text
			return fmt.Errorf("clearing %s: %w", t, err)
		}
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

// --------------- Subscriptions ---------------

func (s *MySQLStore) ListSubscriptionRows(ctx context.Context) ([]SubscriptionRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, email, COALESCE(product_id, ''), COALESCE(team_id, ''),
		        COALESCE(ai_analytics_url, ''), interval_days, last_run_at, created_at, updated_at
		 FROM subscriptions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []SubscriptionRow
	idxMap := make(map[int64]int)
	for rows.Next() {
		var r SubscriptionRow
		var intervalDays sql.NullInt64
		var lastRun, createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Email, &r.ProductID, &r.TeamID,
			&r.AIAnalyticsURL, &intervalDays, &lastRun, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if intervalDays.Valid {
			v := int(intervalDays.Int64)
			r.IntervalDays = &v
		}
		if lastRun.Valid {
			s := lastRun.Time.Format("2006-01-02T15:04:05Z")
			r.LastRunAt = &s
		}
		if createdAt.Valid {
			r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
		}
		if updatedAt.Valid {
			r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		idxMap[r.ID] = len(subs)
		subs = append(subs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(subs) > 0 {
		elRows, err := s.db.QueryContext(ctx,
			"SELECT id, subscription_id, sort_order, url, payload FROM subscription_elements ORDER BY subscription_id, sort_order")
		if err != nil {
			return nil, err
		}
		defer elRows.Close()
		for elRows.Next() {
			var el SubscriptionElementRow
			var subID int64
			var payload []byte
			if err := elRows.Scan(&el.ID, &subID, &el.SortOrder, &el.URL, &payload); err != nil {
				return nil, err
			}
			if len(payload) > 0 && string(payload) != "null" {
				el.Payload = json.RawMessage(payload)
			}
			if idx, ok := idxMap[subID]; ok {
				subs[idx].Elements = append(subs[idx].Elements, el)
			}
		}
		if err := elRows.Err(); err != nil {
			return nil, err
		}
	}

	return subs, nil
}

func (s *MySQLStore) GetSubscriptionRow(ctx context.Context, id int64) (*SubscriptionRow, error) {
	var r SubscriptionRow
	var intervalDays sql.NullInt64
	var lastRun, createdAt, updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, COALESCE(product_id, ''), COALESCE(team_id, ''),
		        COALESCE(ai_analytics_url, ''), interval_days, last_run_at, created_at, updated_at
		 FROM subscriptions WHERE id=?`, id).
		Scan(&r.ID, &r.Email, &r.ProductID, &r.TeamID,
			&r.AIAnalyticsURL, &intervalDays, &lastRun, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if intervalDays.Valid {
		v := int(intervalDays.Int64)
		r.IntervalDays = &v
	}
	if lastRun.Valid {
		ts := lastRun.Time.Format("2006-01-02T15:04:05Z")
		r.LastRunAt = &ts
	}
	if createdAt.Valid {
		r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
	}
	if updatedAt.Valid {
		r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
	}

	elRows, err := s.db.QueryContext(ctx,
		"SELECT id, sort_order, url, payload FROM subscription_elements WHERE subscription_id=? ORDER BY sort_order",
		id)
	if err != nil {
		return nil, err
	}
	defer elRows.Close()
	for elRows.Next() {
		var el SubscriptionElementRow
		var payload []byte
		if err := elRows.Scan(&el.ID, &el.SortOrder, &el.URL, &payload); err != nil {
			return nil, err
		}
		if len(payload) > 0 && string(payload) != "null" {
			el.Payload = json.RawMessage(payload)
		}
		r.Elements = append(r.Elements, el)
	}
	if err := elRows.Err(); err != nil {
		return nil, err
	}

	return &r, nil
}

func (s *MySQLStore) CreateSubscription(ctx context.Context, input SubscriptionRow) (int64, error) {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return 0, err
	}
	if owned {
		defer rollbackTx(tx)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO subscriptions (email, product_id, team_id, ai_analytics_url, interval_days)
		 VALUES (?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?)`,
		input.Email, input.ProductID, input.TeamID, input.AIAnalyticsURL, input.IntervalDays)
	if err != nil {
		return 0, err
	}
	autoID, _ := res.LastInsertId()

	if err := insertSubscriptionElements(ctx, tx, autoID, input.Elements); err != nil {
		return 0, err
	}
	if owned {
		return autoID, tx.Commit()
	}
	return autoID, nil
}

func (s *MySQLStore) UpdateSubscription(ctx context.Context, id int64, input SubscriptionRow) error {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE subscriptions SET email=?, product_id=NULLIF(?, ''),
		        team_id=NULLIF(?, ''), ai_analytics_url=NULLIF(?, ''), interval_days=?
		 WHERE id=?`,
		input.Email, input.ProductID, input.TeamID, input.AIAnalyticsURL, input.IntervalDays, id)
	if err != nil {
		return err
	}
	if err := checkAffected(res); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM subscription_elements WHERE subscription_id=?", id); err != nil {
		return err
	}
	if err := insertSubscriptionElements(ctx, tx, id, input.Elements); err != nil {
		return err
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

func (s *MySQLStore) UpdateSubscriptionLastRunAt(ctx context.Context, id int64, t time.Time) error {
	res, err := s.db.ExecContext(ctx, "UPDATE subscriptions SET last_run_at=? WHERE id=?", t, id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

func (s *MySQLStore) DeleteSubscription(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM subscriptions WHERE id=?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// --------------- Alerts ---------------

func (s *MySQLStore) ListAlertRows(ctx context.Context) ([]AlertRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, email, COALESCE(product_id, ''), COALESCE(team_id, ''),
		        url, payload, ai_analytics_url, rule, created_at, updated_at
		 FROM alerts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRow
	for rows.Next() {
		var r AlertRow
		var payload []byte
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Email, &r.ProductID, &r.TeamID,
			&r.URL, &payload, &r.AIAnalyticsURL, &r.Rule, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if len(payload) > 0 && string(payload) != "null" {
			r.Payload = json.RawMessage(payload)
		}
		if createdAt.Valid {
			r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
		}
		if updatedAt.Valid {
			r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MySQLStore) GetAlertRow(ctx context.Context, id int64) (*AlertRow, error) {
	var r AlertRow
	var payload []byte
	var createdAt, updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, COALESCE(product_id, ''), COALESCE(team_id, ''),
		        url, payload, ai_analytics_url, rule, created_at, updated_at
		 FROM alerts WHERE id=?`, id).
		Scan(&r.ID, &r.Email, &r.ProductID, &r.TeamID,
			&r.URL, &payload, &r.AIAnalyticsURL, &r.Rule, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(payload) > 0 && string(payload) != "null" {
		r.Payload = json.RawMessage(payload)
	}
	if createdAt.Valid {
		r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
	}
	if updatedAt.Valid {
		r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
	}
	return &r, nil
}

func (s *MySQLStore) CreateAlert(ctx context.Context, input AlertRow) (int64, error) {
	var payload any
	if len(input.Payload) > 0 {
		payload = []byte(input.Payload)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO alerts (email, product_id, team_id, url, payload, ai_analytics_url, rule)
		 VALUES (?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?)`,
		input.Email, input.ProductID, input.TeamID, input.URL, payload, input.AIAnalyticsURL, input.Rule)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *MySQLStore) UpdateAlert(ctx context.Context, id int64, input AlertRow) error {
	var payload any
	if len(input.Payload) > 0 {
		payload = []byte(input.Payload)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE alerts SET email=?, product_id=NULLIF(?, ''), team_id=NULLIF(?, ''),
		        url=?, payload=?, ai_analytics_url=?, rule=?
		 WHERE id=?`,
		input.Email, input.ProductID, input.TeamID, input.URL, payload, input.AIAnalyticsURL, input.Rule, id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

func (s *MySQLStore) DeleteAlert(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM alerts WHERE id=?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

func (s *MySQLStore) GetOrgForProduct(ctx context.Context, productID string) (string, error) {
	var orgID string
	err := s.db.QueryRowContext(ctx, "SELECT org_id FROM org_products WHERE product_id=? LIMIT 1", productID).Scan(&orgID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return orgID, err
}

func insertSubscriptionElements(ctx context.Context, tx *sql.Tx, subID int64, elements []SubscriptionElementRow) error {
	for _, el := range elements {
		var payload any
		if len(el.Payload) > 0 {
			payload = []byte(el.Payload)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO subscription_elements (subscription_id, sort_order, url, payload) VALUES (?, ?, ?, ?)",
			subID, el.SortOrder, el.URL, payload); err != nil {
			return err
		}
	}
	return nil
}

// --------------- Canned Queries ---------------

func (s *MySQLStore) ListCannedQueryRows(ctx context.Context) ([]CannedQueryRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(description, ''), category, sql_text,
		        parameters, version, created_at, updated_at
		 FROM canned_queries ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CannedQueryRow
	for rows.Next() {
		var r CannedQueryRow
		var params []byte
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Category, &r.SQLText,
			&params, &r.Version, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if len(params) > 0 && string(params) != "null" {
			r.Parameters = json.RawMessage(params)
		}
		if createdAt.Valid {
			r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
		}
		if updatedAt.Valid {
			r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MySQLStore) GetCannedQueryRow(ctx context.Context, id int64) (*CannedQueryRow, error) {
	var r CannedQueryRow
	var params []byte
	var createdAt, updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(description, ''), category, sql_text,
		        parameters, version, created_at, updated_at
		 FROM canned_queries WHERE id=?`, id).
		Scan(&r.ID, &r.Name, &r.Description, &r.Category, &r.SQLText,
			&params, &r.Version, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(params) > 0 && string(params) != "null" {
		r.Parameters = json.RawMessage(params)
	}
	if createdAt.Valid {
		r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
	}
	if updatedAt.Valid {
		r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
	}
	return &r, nil
}

func (s *MySQLStore) CreateCannedQuery(ctx context.Context, input CannedQueryRow) (int64, error) {
	var params any
	if len(input.Parameters) > 0 {
		params = []byte(input.Parameters)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO canned_queries (name, description, category, sql_text, parameters, version)
		 VALUES (?, NULLIF(?, ''), ?, ?, ?, ?)`,
		input.Name, input.Description, input.Category, input.SQLText, params, input.Version)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *MySQLStore) UpdateCannedQuery(ctx context.Context, id int64, input CannedQueryRow) error {
	var params any
	if len(input.Parameters) > 0 {
		params = []byte(input.Parameters)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE canned_queries SET name=?, description=NULLIF(?, ''), category=?,
		        sql_text=?, parameters=?, version=?
		 WHERE id=?`,
		input.Name, input.Description, input.Category, input.SQLText, params, input.Version, id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

func (s *MySQLStore) DeleteCannedQuery(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM canned_queries WHERE id=?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// --------------- Canned Scripts ---------------

func (s *MySQLStore) ListCannedScriptRows(ctx context.Context) ([]CannedScriptRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(description, ''), category, js_text,
		        version, created_at, updated_at
		 FROM canned_scripts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CannedScriptRow
	for rows.Next() {
		var r CannedScriptRow
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Category, &r.JSText,
			&r.Version, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if createdAt.Valid {
			r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
		}
		if updatedAt.Valid {
			r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MySQLStore) GetCannedScriptRow(ctx context.Context, id int64) (*CannedScriptRow, error) {
	var r CannedScriptRow
	var createdAt, updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(description, ''), category, js_text,
		        version, created_at, updated_at
		 FROM canned_scripts WHERE id=?`, id).
		Scan(&r.ID, &r.Name, &r.Description, &r.Category, &r.JSText,
			&r.Version, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if createdAt.Valid {
		r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
	}
	if updatedAt.Valid {
		r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
	}
	return &r, nil
}

func (s *MySQLStore) CreateCannedScript(ctx context.Context, input CannedScriptRow) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO canned_scripts (name, description, category, js_text, version)
		 VALUES (?, NULLIF(?, ''), ?, ?, ?)`,
		input.Name, input.Description, input.Category, input.JSText, input.Version)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *MySQLStore) UpdateCannedScript(ctx context.Context, id int64, input CannedScriptRow) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE canned_scripts SET name=?, description=NULLIF(?, ''), category=?,
		        js_text=?, version=?
		 WHERE id=?`,
		input.Name, input.Description, input.Category, input.JSText, input.Version, id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

func (s *MySQLStore) DeleteCannedScript(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM canned_scripts WHERE id=?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// --------------- JSON Schemas ---------------

func (s *MySQLStore) ListJsonSchemaRows(ctx context.Context) ([]JsonSchemaRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(description, ''), json_schema,
		        version, created_at, updated_at
		 FROM json_schemas ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JsonSchemaRow
	for rows.Next() {
		var r JsonSchemaRow
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.JsonSchema,
			&r.Version, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if createdAt.Valid {
			r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
		}
		if updatedAt.Valid {
			r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MySQLStore) GetJsonSchemaRow(ctx context.Context, id int64) (*JsonSchemaRow, error) {
	var r JsonSchemaRow
	var createdAt, updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(description, ''), json_schema,
		        version, created_at, updated_at
		 FROM json_schemas WHERE id=?`, id).
		Scan(&r.ID, &r.Name, &r.Description, &r.JsonSchema,
			&r.Version, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if createdAt.Valid {
		r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
	}
	if updatedAt.Valid {
		r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
	}
	return &r, nil
}

func (s *MySQLStore) CreateJsonSchema(ctx context.Context, input JsonSchemaRow) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO json_schemas (name, description, json_schema)
		 VALUES (?, ?, ?)`,
		input.Name, input.Description, input.JsonSchema)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *MySQLStore) UpdateJsonSchema(ctx context.Context, id int64, input JsonSchemaRow) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE json_schemas SET name=?, description=?, json_schema=?, version=version+1
		 WHERE id=?`,
		input.Name, input.Description, input.JsonSchema, id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

func (s *MySQLStore) DeleteJsonSchema(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM json_schemas WHERE id=?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

func (s *MySQLStore) GetJsonSchemaByName(ctx context.Context, name string) (*JsonSchemaRow, error) {
	var r JsonSchemaRow
	var createdAt, updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(description, ''), json_schema,
		        version, created_at, updated_at
		 FROM json_schemas WHERE name=?`, name).
		Scan(&r.ID, &r.Name, &r.Description, &r.JsonSchema,
			&r.Version, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if createdAt.Valid {
		r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
	}
	if updatedAt.Valid {
		r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
	}
	return &r, nil
}

// --------------- Stored Procedures ---------------

func (s *MySQLStore) ListAllStoredProcedureRows(ctx context.Context) ([]StoredProcedureRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(workflow_id,0), name, COALESCE(description, ''), sql_text, created_at, updated_at
		 FROM stored_procedures ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredProcedureRow
	for rows.Next() {
		var r StoredProcedureRow
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.WorkflowID, &r.Name, &r.Description, &r.SQLText,
			&createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if createdAt.Valid {
			r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
		}
		if updatedAt.Valid {
			r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MySQLStore) ListStoredProcedureRows(ctx context.Context, workflowID int64) ([]StoredProcedureRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(workflow_id,0), name, COALESCE(description, ''), sql_text, created_at, updated_at
		 FROM stored_procedures WHERE workflow_id=? ORDER BY name`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredProcedureRow
	for rows.Next() {
		var r StoredProcedureRow
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.WorkflowID, &r.Name, &r.Description, &r.SQLText,
			&createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if createdAt.Valid {
			r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
		}
		if updatedAt.Valid {
			r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MySQLStore) GetStoredProcedureRow(ctx context.Context, id int64) (*StoredProcedureRow, error) {
	var r StoredProcedureRow
	var createdAt, updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, COALESCE(workflow_id,0), name, COALESCE(description, ''), sql_text, created_at, updated_at
		 FROM stored_procedures WHERE id=?`, id).
		Scan(&r.ID, &r.WorkflowID, &r.Name, &r.Description, &r.SQLText,
			&createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if createdAt.Valid {
		r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
	}
	if updatedAt.Valid {
		r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
	}
	return &r, nil
}

func (s *MySQLStore) CreateStoredProcedure(ctx context.Context, input StoredProcedureRow) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO stored_procedures (workflow_id, name, description, sql_text)
		 VALUES (?, ?, NULLIF(?, ''), ?)`,
		input.WorkflowID, input.Name, input.Description, input.SQLText)
	if err != nil {
		if isDuplicateEntry(err) {
			return 0, ErrDuplicateName
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (s *MySQLStore) UpdateStoredProcedure(ctx context.Context, id int64, input StoredProcedureRow) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE stored_procedures SET name=?, description=NULLIF(?, ''), sql_text=?
		 WHERE id=?`,
		input.Name, input.Description, input.SQLText, id)
	if err != nil {
		if isDuplicateEntry(err) {
			return ErrDuplicateName
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists bool
		if err := s.db.QueryRowContext(ctx, "SELECT 1 FROM stored_procedures WHERE id=?", id).Scan(&exists); err != nil {
			return ErrNotFound
		}
	}
	return nil
}

func (s *MySQLStore) DeleteStoredProcedure(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM stored_procedures WHERE id=?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// --------------- Workflows ---------------

func (s *MySQLStore) ListWorkflowRows(ctx context.Context) ([]WorkflowRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT w.id, w.name, COALESCE(w.description, ''), w.input_script, w.output_script,
		        w.test_input, COALESCE(w.tests, ''), w.version, w.draft IS NOT NULL,
		        COALESCE((SELECT version FROM workflow_versions WHERE workflow_id = w.id AND production = TRUE LIMIT 1), 0),
		        w.archived, w.created_at, w.updated_at
		 FROM workflows w ORDER BY w.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkflowRow
	for rows.Next() {
		var r WorkflowRow
		var createdAt, updatedAt sql.NullTime
		var testsStr string
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.InputScript, &r.OutputScript,
			&r.TestInput, &testsStr, &r.Version, &r.HasDraft, &r.ProductionVersion, &r.Archived, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if testsStr != "" {
			if err := json.Unmarshal([]byte(testsStr), &r.Tests); err != nil {
				return nil, fmt.Errorf("unmarshal tests: %w", err)
			}
		}
		if createdAt.Valid {
			r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
		}
		if updatedAt.Valid {
			r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.loadWorkflowQueries(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *MySQLStore) GetWorkflowRow(ctx context.Context, id int64) (*WorkflowRow, error) {
	var r WorkflowRow
	var createdAt, updatedAt sql.NullTime
	var draftStr, testsStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT w.id, w.name, COALESCE(w.description, ''), w.input_script, w.output_script,
		        w.test_input, COALESCE(w.tests, ''), w.version, COALESCE(w.draft, ''),
		        COALESCE((SELECT version FROM workflow_versions WHERE workflow_id = w.id AND production = TRUE LIMIT 1), 0),
		        w.archived, w.created_at, w.updated_at
		 FROM workflows w WHERE w.id=?`, id).
		Scan(&r.ID, &r.Name, &r.Description, &r.InputScript, &r.OutputScript,
			&r.TestInput, &testsStr, &r.Version, &draftStr, &r.ProductionVersion, &r.Archived, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if testsStr != "" {
		if err := json.Unmarshal([]byte(testsStr), &r.Tests); err != nil {
			return nil, fmt.Errorf("unmarshal tests: %w", err)
		}
	}
	if createdAt.Valid {
		r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
	}
	if updatedAt.Valid {
		r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
	}
	if err := s.loadWorkflowQueries(ctx, &r); err != nil {
		return nil, err
	}
	if draftStr != "" {
		r.HasDraft = true
		var draft WorkflowRow
		if err := json.Unmarshal([]byte(draftStr), &draft); err == nil {
			r.Name = draft.Name
			r.Description = draft.Description
			r.InputScript = draft.InputScript
			r.OutputScript = draft.OutputScript
			r.TestInput = draft.TestInput
			r.Tests = draft.Tests
			if draft.Queries != nil {
				r.Queries = draft.Queries
			}
		}
	}
	return &r, nil
}

func (s *MySQLStore) GetWorkflowByName(ctx context.Context, name string) (*WorkflowRow, error) {
	var r WorkflowRow
	var createdAt, updatedAt sql.NullTime
	var testsStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(description, ''), input_script, output_script,
		        test_input, COALESCE(tests, ''), version, created_at, updated_at
		 FROM workflows WHERE name=? AND version > 0 AND archived = FALSE`, name).
		Scan(&r.ID, &r.Name, &r.Description, &r.InputScript, &r.OutputScript,
			&r.TestInput, &testsStr, &r.Version, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if testsStr != "" {
		if err := json.Unmarshal([]byte(testsStr), &r.Tests); err != nil {
			return nil, fmt.Errorf("unmarshal tests: %w", err)
		}
	}
	if createdAt.Valid {
		r.CreatedAt = createdAt.Time.Format("2006-01-02T15:04:05Z")
	}
	if updatedAt.Valid {
		r.UpdatedAt = updatedAt.Time.Format("2006-01-02T15:04:05Z")
	}
	if err := s.loadWorkflowQueries(ctx, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *MySQLStore) loadWorkflowQueries(ctx context.Context, w *WorkflowRow) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, stage, sort_order, label, sql_text
		 FROM workflow_queries WHERE workflow_id=? ORDER BY stage, sort_order`, w.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var q WorkflowQueryRow
		if err := rows.Scan(&q.ID, &q.Stage, &q.SortOrder, &q.Label, &q.SQLText); err != nil {
			return err
		}
		w.Queries = append(w.Queries, q)
	}
	return rows.Err()
}

func (s *MySQLStore) CreateWorkflow(ctx context.Context, input WorkflowRow) (int64, error) {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return 0, err
	}
	if owned {
		defer rollbackTx(tx)
	}

	testInput := input.TestInput
	if testInput == "" {
		testInput = "{}"
	}
	var testsJSON any
	if len(input.Tests) > 0 {
		b, err := json.Marshal(input.Tests)
		if err != nil {
			return 0, err
		}
		testsJSON = string(b)
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO workflows (name, description, input_script, output_script, test_input, tests, version)
		 VALUES (?, NULLIF(?, ''), ?, ?, ?, ?, ?)`,
		input.Name, input.Description, input.InputScript, input.OutputScript, testInput, testsJSON, input.Version)
	if err != nil {
		if isDuplicateEntry(err) {
			return 0, ErrDuplicateName
		}
		return 0, err
	}
	autoID, _ := res.LastInsertId()

	if err := insertWorkflowQueries(ctx, tx, autoID, input.Queries); err != nil {
		return 0, err
	}
	if owned {
		return autoID, tx.Commit()
	}
	return autoID, nil
}

func (s *MySQLStore) UpdateWorkflow(ctx context.Context, id int64, input WorkflowRow) error {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}

	testInput := input.TestInput
	if testInput == "" {
		testInput = "{}"
	}
	var testsJSON any
	if len(input.Tests) > 0 {
		b, err := json.Marshal(input.Tests)
		if err != nil {
			return err
		}
		testsJSON = string(b)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE workflows SET name=?, description=NULLIF(?, ''),
		        input_script=?, output_script=?, test_input=?, tests=?, version=?
		 WHERE id=?`,
		input.Name, input.Description, input.InputScript, input.OutputScript, testInput, testsJSON, input.Version, id)
	if err != nil {
		if isDuplicateEntry(err) {
			return ErrDuplicateName
		}
		return err
	}
	if err := checkAffected(res); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM workflow_queries WHERE workflow_id=?", id); err != nil {
		return err
	}
	if err := insertWorkflowQueries(ctx, tx, id, input.Queries); err != nil {
		return err
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

func insertWorkflowQueries(ctx context.Context, tx *sql.Tx, workflowID int64, queries []WorkflowQueryRow) error {
	for _, q := range queries {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workflow_queries (workflow_id, stage, sort_order, label, sql_text)
			 VALUES (?, ?, ?, ?, ?)`,
			workflowID, q.Stage, q.SortOrder, q.Label, q.SQLText); err != nil {
			return err
		}
	}
	return nil
}

func (s *MySQLStore) SaveWorkflowDraft(ctx context.Context, id int64, input WorkflowRow) error {
	draftJSON, err := json.Marshal(input)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, "UPDATE workflows SET draft=? WHERE id=?", draftJSON, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists bool
		if err := s.db.QueryRowContext(ctx, "SELECT 1 FROM workflows WHERE id=?", id).Scan(&exists); err != nil {
			return ErrNotFound
		}
	}
	return nil
}

func (s *MySQLStore) PublishWorkflow(ctx context.Context, id int64, publishNote string) (*WorkflowRow, error) {
	var curName, curDesc, curInput, curOutput, curTestInput, curTestsStr, draftStr string
	var curVersion int
	err := s.db.QueryRowContext(ctx,
		`SELECT name, COALESCE(description,''), input_script, output_script,
		        test_input, COALESCE(tests,''), version, COALESCE(draft,'')
		 FROM workflows WHERE id=?`, id).
		Scan(&curName, &curDesc, &curInput, &curOutput, &curTestInput, &curTestsStr, &curVersion, &draftStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	pubName, pubDesc, pubInput, pubOutput, pubTestInput := curName, curDesc, curInput, curOutput, curTestInput
	var pubQueries []WorkflowQueryRow
	var pubTests []WorkflowTestCase
	hasDraft := draftStr != ""

	if curTestsStr != "" {
		if err := json.Unmarshal([]byte(curTestsStr), &pubTests); err != nil {
			return nil, fmt.Errorf("unmarshal tests: %w", err)
		}
	}

	if hasDraft {
		var draft WorkflowRow
		if err := json.Unmarshal([]byte(draftStr), &draft); err != nil {
			return nil, fmt.Errorf("invalid draft: %w", err)
		}
		pubName = draft.Name
		pubDesc = draft.Description
		pubInput = draft.InputScript
		pubOutput = draft.OutputScript
		pubTestInput = draft.TestInput
		pubTests = draft.Tests
		pubQueries = draft.Queries
	} else {
		var tmpWf WorkflowRow
		tmpWf.ID = id
		if err := s.loadWorkflowQueries(ctx, &tmpWf); err != nil {
			return nil, err
		}
		pubQueries = tmpWf.Queries
	}
	if pubTestInput == "" {
		pubTestInput = "{}"
	}
	newVersion := curVersion + 1

	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer rollbackTx(tx)
	}

	queriesJSON, err := json.Marshal(pubQueries)
	if err != nil {
		return nil, err
	}
	var testsJSON any
	if len(pubTests) > 0 {
		b, err := json.Marshal(pubTests)
		if err != nil {
			return nil, err
		}
		testsJSON = string(b)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workflow_versions
		 (workflow_id, version, name, description, input_script, output_script, test_input, queries, tests, publish_note)
		 VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, NULLIF(?, ''))`,
		id, newVersion, pubName, pubDesc, pubInput, pubOutput, pubTestInput, queriesJSON, testsJSON, publishNote); err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx,
		`UPDATE workflows SET name=?, description=NULLIF(?, ''),
		        input_script=?, output_script=?, test_input=?, tests=?,
		        version=?, draft=NULL
		 WHERE id=?`,
		pubName, pubDesc, pubInput, pubOutput, pubTestInput, testsJSON, newVersion, id)
	if err != nil {
		return nil, err
	}
	if err := checkAffected(res); err != nil {
		return nil, err
	}

	if hasDraft {
		if _, err := tx.ExecContext(ctx, "DELETE FROM workflow_queries WHERE workflow_id=?", id); err != nil {
			return nil, err
		}
		if err := insertWorkflowQueries(ctx, tx, id, pubQueries); err != nil {
			return nil, err
		}
	}

	if owned {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
	}
	return s.GetWorkflowRow(ctx, id)
}

func (s *MySQLStore) DeleteWorkflow(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM workflows WHERE id=?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

func (s *MySQLStore) ListWorkflowVersions(ctx context.Context, workflowID int64) ([]WorkflowVersionRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, workflow_id, version, name, COALESCE(description, ''),
		        input_script, output_script, test_input, queries,
		        COALESCE(tests, ''), COALESCE(publish_note, ''), production, published_at
		 FROM workflow_versions WHERE workflow_id=? ORDER BY version DESC`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkflowVersionRow
	for rows.Next() {
		var r WorkflowVersionRow
		var queriesBytes []byte
		var testsStr string
		var publishedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.WorkflowID, &r.Version, &r.Name, &r.Description,
			&r.InputScript, &r.OutputScript, &r.TestInput, &queriesBytes,
			&testsStr, &r.PublishNote, &r.Production, &publishedAt); err != nil {
			return nil, err
		}
		if len(queriesBytes) > 0 {
			if err := json.Unmarshal(queriesBytes, &r.Queries); err != nil {
				return nil, fmt.Errorf("unmarshal queries: %w", err)
			}
		}
		if testsStr != "" {
			if err := json.Unmarshal([]byte(testsStr), &r.Tests); err != nil {
				return nil, fmt.Errorf("unmarshal tests: %w", err)
			}
		}
		if publishedAt.Valid {
			r.PublishedAt = publishedAt.Time.Format("2006-01-02T15:04:05Z")
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *MySQLStore) GetWorkflowVersion(ctx context.Context, workflowID int64, version int) (*WorkflowVersionRow, error) {
	var r WorkflowVersionRow
	var queriesBytes []byte
	var testsStr string
	var publishedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, workflow_id, version, name, COALESCE(description, ''),
		        input_script, output_script, test_input, queries,
		        COALESCE(tests, ''), COALESCE(publish_note, ''), production, published_at
		 FROM workflow_versions WHERE workflow_id=? AND version=?`, workflowID, version).
		Scan(&r.ID, &r.WorkflowID, &r.Version, &r.Name, &r.Description,
			&r.InputScript, &r.OutputScript, &r.TestInput, &queriesBytes,
			&testsStr, &r.PublishNote, &r.Production, &publishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(queriesBytes) > 0 {
		if err := json.Unmarshal(queriesBytes, &r.Queries); err != nil {
			return nil, fmt.Errorf("unmarshal queries: %w", err)
		}
	}
	if testsStr != "" {
		if err := json.Unmarshal([]byte(testsStr), &r.Tests); err != nil {
			return nil, fmt.Errorf("unmarshal tests: %w", err)
		}
	}
	if publishedAt.Valid {
		r.PublishedAt = publishedAt.Time.Format("2006-01-02T15:04:05Z")
	}
	return &r, nil
}

func (s *MySQLStore) SetProductionVersion(ctx context.Context, workflowID int64, version int) error {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE workflow_versions SET production=FALSE WHERE workflow_id=?", workflowID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		"UPDATE workflow_versions SET production=TRUE WHERE workflow_id=? AND version=?", workflowID, version)
	if err != nil {
		return err
	}
	if err := checkAffected(res); err != nil {
		return err
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

func (s *MySQLStore) GetWorkflowByNameAndVersion(ctx context.Context, name string, version int) (*WorkflowRow, error) {
	var wfID int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM workflows WHERE name=?", name).Scan(&wfID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ver, err := s.GetWorkflowVersion(ctx, wfID, version)
	if err != nil {
		return nil, err
	}
	if ver == nil {
		return nil, nil
	}
	return &WorkflowRow{
		ID:           wfID,
		Name:         ver.Name,
		Description:  ver.Description,
		InputScript:  ver.InputScript,
		OutputScript: ver.OutputScript,
		TestInput:    ver.TestInput,
		Version:      ver.Version,
		Queries:      ver.Queries,
	}, nil
}

func (s *MySQLStore) GetProductionWorkflowByName(ctx context.Context, name string) (*WorkflowRow, error) {
	var wfID int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM workflows WHERE name=?", name).Scan(&wfID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r WorkflowVersionRow
	var queriesBytes []byte
	var publishedAt sql.NullTime
	err = s.db.QueryRowContext(ctx,
		`SELECT id, workflow_id, version, name, COALESCE(description, ''),
		        input_script, output_script, test_input, queries, published_at
		 FROM workflow_versions WHERE workflow_id=? AND production=TRUE`, wfID).
		Scan(&r.ID, &r.WorkflowID, &r.Version, &r.Name, &r.Description,
			&r.InputScript, &r.OutputScript, &r.TestInput, &queriesBytes, &publishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(queriesBytes) > 0 {
		if err := json.Unmarshal(queriesBytes, &r.Queries); err != nil {
			return nil, fmt.Errorf("unmarshal queries: %w", err)
		}
	}
	return &WorkflowRow{
		ID:           wfID,
		Name:         r.Name,
		Description:  r.Description,
		InputScript:  r.InputScript,
		OutputScript: r.OutputScript,
		TestInput:    r.TestInput,
		Version:      r.Version,
		Queries:      r.Queries,
	}, nil
}

func (s *MySQLStore) ArchiveWorkflow(ctx context.Context, id int64) error {
	tx, owned, err := s.beginOrReuse(ctx)
	if err != nil {
		return err
	}
	if owned {
		defer rollbackTx(tx)
	}
	// Clear production flag on all versions
	if _, err := tx.ExecContext(ctx,
		"UPDATE workflow_versions SET production=FALSE WHERE workflow_id=?", id); err != nil {
		return err
	}
	// Set archived
	res, err := tx.ExecContext(ctx,
		"UPDATE workflows SET archived=TRUE WHERE id=?", id)
	if err != nil {
		return err
	}
	if err := checkAffected(res); err != nil {
		return err
	}
	if owned {
		return tx.Commit()
	}
	return nil
}

func (s *MySQLStore) UnarchiveWorkflow(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE workflows SET archived=FALSE WHERE id=?", id)
	if err != nil {
		return err
	}
	return checkAffected(res)
}

// --------------- helpers ---------------

var ErrNotFound = sql.ErrNoRows
var ErrDuplicateName = errors.New("duplicate name")

func isDuplicateEntry(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

func translateDuplicateEntry(err error) error {
	if isDuplicateEntry(err) {
		return ErrDuplicateName
	}
	return err
}

func rollbackTx(tx *sql.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		slog.Error("transaction rollback", "err", err)
	}
}

func checkAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
