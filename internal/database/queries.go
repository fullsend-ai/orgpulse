package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
)

type sourceKey struct {
	teamID   string
	sourceID string
}

type repoKey struct {
	teamID   string
	sourceID string
	repoID   string
}

type dashRow struct {
	autoID  int
	ownerID string
	dash    Dashboard
}

func (s *MySQLStore) GetOrgs(ctx context.Context) ([]Org, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name FROM orgs ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []Org
	for rows.Next() {
		var o Org
		if err := rows.Scan(&o.ID, &o.Name); err != nil {
			return nil, err
		}
		o.Products = []string{}
		orgs = append(orgs, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	productMap, err := s.loadOrgProducts(ctx)
	if err != nil {
		return nil, err
	}

	configMap, err := s.loadEntityConfigs(ctx, "org")
	if err != nil {
		return nil, err
	}

	dashMap, err := s.loadDashboards(ctx, "org")
	if err != nil {
		return nil, err
	}

	for i := range orgs {
		if products, ok := productMap[orgs[i].ID]; ok {
			orgs[i].Products = products
		}
		if cfg, ok := configMap[orgs[i].ID]; ok {
			m := make(map[string]any, len(cfg))
			for k, v := range cfg {
				m[k] = v
			}
			orgs[i].Config = m
		}
		if dashes, ok := dashMap[orgs[i].ID]; ok {
			orgs[i].Dashboards = dashes
		}
	}

	return orgs, nil
}

func (s *MySQLStore) GetProducts(ctx context.Context) ([]Product, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, COALESCE(contact_email, '') FROM products ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.ContactEmail); err != nil {
			return nil, err
		}
		p.Teams = []string{}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	teamMap, err := s.loadProductTeams(ctx)
	if err != nil {
		return nil, err
	}

	configMap, err := s.loadEntityConfigs(ctx, "product")
	if err != nil {
		return nil, err
	}

	overrideMap, err := s.loadProductOverrides(ctx)
	if err != nil {
		return nil, err
	}

	dashMap, err := s.loadDashboards(ctx, "product")
	if err != nil {
		return nil, err
	}

	for i := range products {
		if teams, ok := teamMap[products[i].ID]; ok {
			products[i].Teams = teams
		}
		products[i].Config = buildProductConfig(
			configMap[products[i].ID],
			overrideMap[products[i].ID],
		)
		if dashes, ok := dashMap[products[i].ID]; ok {
			products[i].Dashboards = dashes
		}
	}

	return products, nil
}

func (s *MySQLStore) GetTeams(ctx context.Context) ([]Team, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(contact_email, ''),
		        COALESCE(fetch_url, ''), COALESCE(fetch_check_url, '')
		 FROM teams ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []Team
	for rows.Next() {
		var t Team
		if err := rows.Scan(&t.ID, &t.Name, &t.ContactEmail, &t.FetchURL, &t.FetchCheckURL); err != nil {
			return nil, err
		}
		teams = append(teams, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	productLabelMap, err := s.loadTeamProductLabels(ctx)
	if err != nil {
		return nil, err
	}

	sourceMap, err := s.loadSources(ctx)
	if err != nil {
		return nil, err
	}

	dashMap, err := s.loadDashboards(ctx, "team")
	if err != nil {
		return nil, err
	}

	for i := range teams {
		if labels, ok := productLabelMap[teams[i].ID]; ok {
			teams[i].Product = labels
		}
		if sources, ok := sourceMap[teams[i].ID]; ok {
			teams[i].Sources = sources
		}
		if dashes, ok := dashMap[teams[i].ID]; ok {
			teams[i].Dashboards = dashes
		}
	}

	return teams, nil
}

func (s *MySQLStore) loadOrgProducts(ctx context.Context) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT org_id, product_id FROM org_products ORDER BY org_id, sort_order")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string][]string)
	for rows.Next() {
		var orgID, productID string
		if err := rows.Scan(&orgID, &productID); err != nil {
			return nil, err
		}
		m[orgID] = append(m[orgID], productID)
	}
	return m, rows.Err()
}

func (s *MySQLStore) loadProductTeams(ctx context.Context) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT product_id, team_id FROM product_teams ORDER BY product_id, sort_order")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string][]string)
	for rows.Next() {
		var productID, teamID string
		if err := rows.Scan(&productID, &teamID); err != nil {
			return nil, err
		}
		m[productID] = append(m[productID], teamID)
	}
	return m, rows.Err()
}

func (s *MySQLStore) loadEntityConfigs(ctx context.Context, entityType string) (map[string]map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT entity_id, config_key, COALESCE(config_value, '') FROM entity_config WHERE entity_type=?",
		entityType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]map[string]string)
	for rows.Next() {
		var entityID, key, value string
		if err := rows.Scan(&entityID, &key, &value); err != nil {
			return nil, err
		}
		if m[entityID] == nil {
			m[entityID] = make(map[string]string)
		}
		m[entityID][key] = s.decodeConfigValue(entityType, entityID, key, value)
	}
	return m, rows.Err()
}

func (s *MySQLStore) loadProductOverrides(ctx context.Context) (map[string][]OverrideRepo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, product_id, repo_id, type, owner, name
		 FROM product_override_repos ORDER BY product_id, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type overrideEntry struct {
		autoID    int
		productID string
		repo      OverrideRepo
	}
	var entries []overrideEntry
	autoIDs := make(map[int]int)

	for rows.Next() {
		var e overrideEntry
		if err := rows.Scan(&e.autoID, &e.productID, &e.repo.ID, &e.repo.Type, &e.repo.Owner, &e.repo.Name); err != nil {
			return nil, err
		}
		e.repo.Teams = []string{}
		autoIDs[e.autoID] = len(entries)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	teamRows, err := s.db.QueryContext(ctx,
		"SELECT override_repo_id, team_id FROM product_override_repo_teams ORDER BY override_repo_id, team_id")
	if err != nil {
		return nil, err
	}
	defer teamRows.Close()

	for teamRows.Next() {
		var overrideID int
		var teamID string
		if err := teamRows.Scan(&overrideID, &teamID); err != nil {
			return nil, err
		}
		if idx, ok := autoIDs[overrideID]; ok {
			entries[idx].repo.Teams = append(entries[idx].repo.Teams, teamID)
		}
	}
	if err := teamRows.Err(); err != nil {
		return nil, err
	}

	m := make(map[string][]OverrideRepo)
	for _, e := range entries {
		m[e.productID] = append(m[e.productID], e.repo)
	}
	return m, nil
}

func (s *MySQLStore) loadTeamProductLabels(ctx context.Context) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT team_id, product_name FROM team_product_labels ORDER BY team_id, product_name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string][]string)
	for rows.Next() {
		var teamID, productName string
		if err := rows.Scan(&teamID, &productName); err != nil {
			return nil, err
		}
		m[teamID] = append(m[teamID], productName)
	}
	return m, rows.Err()
}

func (s *MySQLStore) loadSources(ctx context.Context) (map[string][]Source, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, team_id, type, config FROM sources ORDER BY team_id, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	srcIdx := make(map[sourceKey]int)
	sourcesByTeam := make(map[string][]Source)

	for rows.Next() {
		var teamID string
		var src Source
		var cfgBytes []byte
		if err := rows.Scan(&src.ID, &teamID, &src.Type, &cfgBytes); err != nil {
			return nil, err
		}
		if len(cfgBytes) > 0 && string(cfgBytes) != "null" {
			src.Config = json.RawMessage(cfgBytes)
		}
		key := sourceKey{teamID, src.ID}
		srcIdx[key] = len(sourcesByTeam[teamID])
		sourcesByTeam[teamID] = append(sourcesByTeam[teamID], src)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := s.loadRepos(ctx, sourcesByTeam, srcIdx); err != nil {
		return nil, err
	}
	if err := s.loadWorkitemProjects(ctx, sourcesByTeam, srcIdx); err != nil {
		return nil, err
	}

	return sourcesByTeam, nil
}

func (s *MySQLStore) loadRepos(ctx context.Context, sourcesByTeam map[string][]Source, srcIdx map[sourceKey]int) error {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, source_id, team_id, type, owner, name FROM repos ORDER BY team_id, source_id, id")
	if err != nil {
		return err
	}
	defer rows.Close()

	rpIdx := make(map[repoKey]int)

	for rows.Next() {
		var r Repo
		var srcID, teamID string
		if err := rows.Scan(&r.ID, &srcID, &teamID, &r.Type, &r.Owner, &r.Name); err != nil {
			return err
		}
		sk := sourceKey{teamID, srcID}
		if si, ok := srcIdx[sk]; ok {
			rk := repoKey{teamID, srcID, r.ID}
			rpIdx[rk] = len(sourcesByTeam[teamID][si].Repos)
			sourcesByTeam[teamID][si].Repos = append(sourcesByTeam[teamID][si].Repos, r)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	excludedRows, err := s.db.QueryContext(ctx,
		"SELECT team_id, source_id, repo_id, dashboard_type FROM repo_excluded_dashboard_types ORDER BY team_id, source_id, repo_id")
	if err != nil {
		return err
	}
	defer excludedRows.Close()

	for excludedRows.Next() {
		var teamID, srcID, repoID, dashType string
		if err := excludedRows.Scan(&teamID, &srcID, &repoID, &dashType); err != nil {
			return err
		}
		rk := repoKey{teamID, srcID, repoID}
		if ri, ok := rpIdx[rk]; ok {
			sk := sourceKey{teamID, srcID}
			if si, ok := srcIdx[sk]; ok {
				sourcesByTeam[teamID][si].Repos[ri].ExcludedDashboardTypes =
					append(sourcesByTeam[teamID][si].Repos[ri].ExcludedDashboardTypes, dashType)
			}
		}
	}
	if err := excludedRows.Err(); err != nil {
		return err
	}

	wlRows, err := s.db.QueryContext(ctx,
		"SELECT team_id, source_id, repo_id, username FROM repo_whitelist ORDER BY team_id, source_id, repo_id, username")
	if err != nil {
		return err
	}
	defer wlRows.Close()

	for wlRows.Next() {
		var teamID, srcID, repoID, username string
		if err := wlRows.Scan(&teamID, &srcID, &repoID, &username); err != nil {
			return err
		}
		rk := repoKey{teamID, srcID, repoID}
		if ri, ok := rpIdx[rk]; ok {
			sk := sourceKey{teamID, srcID}
			if si, ok := srcIdx[sk]; ok {
				sourcesByTeam[teamID][si].Repos[ri].Whitelist =
					append(sourcesByTeam[teamID][si].Repos[ri].Whitelist, username)
			}
		}
	}
	return wlRows.Err()
}

func (s *MySQLStore) loadWorkitemProjects(ctx context.Context, sourcesByTeam map[string][]Source, srcIdx map[sourceKey]int) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, source_id, team_id, project_name,
		        COALESCE(jira_project, ''), COALESCE(board_id, ''), type
		 FROM workitem_projects ORDER BY team_id, source_id, id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var wp WorkitemProject
		var srcID, teamID string
		if err := rows.Scan(&wp.ID, &srcID, &teamID, &wp.ProjectName, &wp.JiraProject, &wp.BoardID, &wp.Type); err != nil {
			return err
		}
		sk := sourceKey{teamID, srcID}
		if si, ok := srcIdx[sk]; ok {
			sourcesByTeam[teamID][si].Projects = append(sourcesByTeam[teamID][si].Projects, wp)
		}
	}
	return rows.Err()
}

func (s *MySQLStore) loadDashboards(ctx context.Context, ownerType string) (map[string][]Dashboard, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, dashboard_id, type, title,
		        COALESCE(description, ''), COALESCE(chat_url, ''),
		        COALESCE(ai_analytics_url, ''), COALESCE(default_source, ''),
		        COALESCE(connection_id, ''), COALESCE(jira_project, '')
		 FROM dashboards WHERE owner_type=? ORDER BY owner_id, id`,
		ownerType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dashes []dashRow
	autoIDs := make(map[int]int)

	for rows.Next() {
		var r dashRow
		if err := rows.Scan(
			&r.autoID, &r.ownerID, &r.dash.ID, &r.dash.Type, &r.dash.Title,
			&r.dash.Description, &r.dash.ChatURL, &r.dash.AIAnalyticsURL,
			&r.dash.DefaultSource, &r.dash.ConnectionID, &r.dash.JiraProject,
		); err != nil {
			return nil, err
		}
		autoIDs[r.autoID] = len(dashes)
		dashes = append(dashes, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(dashes) > 0 {
		if err := s.loadDiagrams(ctx, dashes, autoIDs); err != nil {
			return nil, err
		}
	}

	m := make(map[string][]Dashboard)
	for _, r := range dashes {
		m[r.ownerID] = append(m[r.ownerID], r.dash)
	}
	return m, nil
}

func (s *MySQLStore) loadDiagrams(ctx context.Context, dashes []dashRow, autoIDs map[int]int) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT dashboard_id, position, type, endpoint,
		        span, COALESCE(title, ''), ai_analytics, COALESCE(description, ''),
		        COALESCE(documentation_url, ''), params
		 FROM diagrams ORDER BY dashboard_id, position`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var dashID int
		var d Diagram
		var position int
		var span sql.NullInt64
		var params []byte
		if err := rows.Scan(&dashID, &position, &d.Type, &d.Endpoint, &span, &d.Title, &d.AIAnalytics, &d.Description, &d.DocumentationURL, &params); err != nil {
			return err
		}
		d.Params = diagramParamsFromDB(params)
		d.Position = &position
		if span.Valid {
			v := int(span.Int64)
			d.Span = &v
		}
		if idx, ok := autoIDs[dashID]; ok {
			dashes[idx].dash.Diagrams = append(dashes[idx].dash.Diagrams, d)
		}
	}
	return rows.Err()
}

func buildProductConfig(configKV map[string]string, overrides []OverrideRepo) *ProductConfig {
	if len(configKV) == 0 && len(overrides) == 0 {
		return nil
	}

	cfg := &ProductConfig{}
	hasValue := false

	if v, ok := configKV["cycleTimeSlowHours"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.CycleTimeSlowHours = &f
			hasValue = true
		}
	}
	if v, ok := configKV["cycleTimeCriticalHours"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.CycleTimeCriticalHours = &f
			hasValue = true
		}
	}
	if v, ok := configKV["analyticsprompt"]; ok && v != "" {
		cfg.AnalyticsPrompt = v
		hasValue = true
	}

	if len(overrides) > 0 {
		cfg.Overrides = &ConfigOverrides{
			Sources: &SourceOverrides{
				Repos: overrides,
			},
		}
		hasValue = true
	}

	if !hasValue {
		return nil
	}
	return cfg
}
