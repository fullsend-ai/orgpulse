package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/database"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: import-config <directory>")
		os.Exit(1)
	}
	dir := os.Args[1]

	host := envOr("DASHBOARD_DB_HOST", "localhost")
	port, _ := strconv.Atoi(envOr("DASHBOARD_DB_PORT", "3306"))
	user := requireEnv("DASHBOARD_DB_USER")
	pass := requireEnv("DASHBOARD_DB_PASSWORD")
	name := requireEnv("DASHBOARD_DB_NAME")
	tlsCABundlePath := envOr("DASHBOARD_DB_TLS_CA_BUNDLE_PATH", "")

	store, err := database.NewMySQLStore(host, port, user, pass, name, tlsCABundlePath)
	if err == nil {
		_, err = store.EnableSecretEncryptionFromEnv()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "database connection failed: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	teams, err := loadAll[database.Team](filepath.Join(dir, "teams"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "loading teams: %v\n", err)
		os.Exit(1)
	}
	products, err := loadAll[database.Product](filepath.Join(dir, "products"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "loading products: %v\n", err)
		os.Exit(1)
	}
	orgs, err := loadAll[database.Org](filepath.Join(dir, "orgs"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "loading orgs: %v\n", err)
		os.Exit(1)
	}

	if err := store.RunInTransaction(ctx, func(tx *database.MySQLStore) error {
		if err := tx.Exec(ctx, "SET FOREIGN_KEY_CHECKS=0"); err != nil {
			return err
		}
		defer func() {
			if err := tx.Exec(ctx, "SET FOREIGN_KEY_CHECKS=1"); err != nil {
				fmt.Fprintf(os.Stderr, "failed to set foreign key checks: %v\n", err)
			}
		}()

		for _, t := range teams {
			if err := importTeam(ctx, tx, t); err != nil {
				return fmt.Errorf("importing team %q: %w", t.ID, err)
			}
		}
		for _, p := range products {
			if err := importProduct(ctx, tx, p); err != nil {
				return fmt.Errorf("importing product %q: %w", p.ID, err)
			}
		}
		for _, o := range orgs {
			if err := importOrg(ctx, tx, o); err != nil {
				return fmt.Errorf("importing org %q: %w", o.ID, err)
			}
		}
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("imported %d teams, %d products, %d orgs\n", len(teams), len(products), len(orgs))
}

func loadAll[T any](dir string) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []T
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := root.ReadFile(e.Name())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		var v T
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		result = append(result, v)
	}
	return result, nil
}

func importTeam(ctx context.Context, store *database.MySQLStore, t database.Team) error {
	if err := store.CreateTeam(ctx, database.TeamRow{
		ID:            t.ID,
		Name:          t.Name,
		ContactEmail:  t.ContactEmail,
		FetchURL:      t.FetchURL,
		FetchCheckURL: t.FetchCheckURL,
	}); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if len(t.Product) > 0 {
		if err := store.ReplaceTeamProductLabels(ctx, t.ID, t.Product); err != nil {
			return fmt.Errorf("product labels: %w", err)
		}
	}
	for _, s := range t.Sources {
		if err := store.CreateSource(ctx, t.ID, convertSource(s)); err != nil {
			return fmt.Errorf("source %q: %w", s.ID, err)
		}
	}
	for _, d := range t.Dashboards {
		if _, err := store.CreateDashboard(ctx, convertDashboard(d, "team", t.ID)); err != nil {
			return fmt.Errorf("dashboard %q: %w", d.ID, err)
		}
	}
	if bpCSV := collectBlueprintIDs(t.Dashboards); bpCSV != "" {
		cfg := map[string]string{"blueprintid": bpCSV}
		if err := store.ReplaceEntityConfig(ctx, "team", t.ID, cfg); err != nil {
			return fmt.Errorf("blueprint config: %w", err)
		}
	}
	return nil
}

func importProduct(ctx context.Context, store *database.MySQLStore, p database.Product) error {
	if err := store.CreateProduct(ctx, database.ProductRow{
		ID:           p.ID,
		Name:         p.Name,
		ContactEmail: p.ContactEmail,
	}); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if len(p.Teams) > 0 {
		entries := make([]database.RelEntry, len(p.Teams))
		for i, id := range p.Teams {
			entries[i] = database.RelEntry{ID: id, SortOrder: i}
		}
		if err := store.ReplaceProductTeamLinks(ctx, p.ID, entries); err != nil {
			return fmt.Errorf("team links: %w", err)
		}
	}
	if p.Config != nil {
		cfg := configToMap(p.Config)
		if len(cfg) > 0 {
			if err := store.ReplaceEntityConfig(ctx, "product", p.ID, cfg); err != nil {
				return fmt.Errorf("config: %w", err)
			}
		}
		if p.Config.Overrides != nil && p.Config.Overrides.Sources != nil {
			overrides := mergeOverrideRepos(p.Config.Overrides.Sources.Repos)
			if err := store.ReplaceProductOverrides(ctx, p.ID, overrides); err != nil {
				return fmt.Errorf("overrides: %w", err)
			}
		}
	}
	for _, d := range p.Dashboards {
		if _, err := store.CreateDashboard(ctx, convertDashboard(d, "product", p.ID)); err != nil {
			return fmt.Errorf("dashboard %q: %w", d.ID, err)
		}
	}
	return nil
}

func importOrg(ctx context.Context, store *database.MySQLStore, o database.Org) error {
	if err := store.CreateOrg(ctx, database.OrgRow{
		ID:   o.ID,
		Name: o.Name,
	}); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if len(o.Products) > 0 {
		entries := make([]database.RelEntry, len(o.Products))
		for i, id := range o.Products {
			entries[i] = database.RelEntry{ID: id, SortOrder: i}
		}
		if err := store.ReplaceOrgProductLinks(ctx, o.ID, entries); err != nil {
			return fmt.Errorf("product links: %w", err)
		}
	}
	for _, d := range o.Dashboards {
		if _, err := store.CreateDashboard(ctx, convertDashboard(d, "org", o.ID)); err != nil {
			return fmt.Errorf("dashboard %q: %w", d.ID, err)
		}
	}
	return nil
}

func convertDashboard(d database.Dashboard, ownerType, ownerID string) database.DashboardRow {
	diagrams := make([]database.DiagramRow, len(d.Diagrams))
	for i, dg := range d.Diagrams {
		pos := 0
		if dg.Position != nil {
			pos = *dg.Position
		}
		diagrams[i] = database.DiagramRow{
			Position:    pos,
			Type:        dg.Type,
			Endpoint:    dg.Endpoint,
			Span:        dg.Span,
			Title:       dg.Title,
			AIAnalytics: dg.AIAnalytics,
			Description: dg.Description,
			Params:      dg.Params,
		}
	}

	return database.DashboardRow{
		OwnerType:      ownerType,
		OwnerID:        ownerID,
		DashboardID:    d.ID,
		Type:           d.Type,
		Title:          d.Title,
		Description:    d.Description,
		ChatURL:        d.ChatURL,
		AIAnalyticsURL: d.AIAnalyticsURL,
		DefaultSource:  d.DefaultSource,
		ConnectionID:   d.ConnectionID,
		JiraProject:    d.JiraProject,
		Diagrams:       diagrams,
	}
}

func convertSource(s database.Source) database.SourceRow {
	repos := make([]database.RepoRow, len(s.Repos))
	for i, r := range s.Repos {
		repos[i] = database.RepoRow(r)
	}
	projects := make([]database.WorkitemProjRow, len(s.Projects))
	for i, p := range s.Projects {
		projects[i] = database.WorkitemProjRow{
			ID:          p.ID,
			ProjectName: p.ProjectName,
			JiraProject: p.JiraProject,
			BoardID:     p.BoardID,
			Type:        p.Type,
		}
	}
	return database.SourceRow{
		ID:       s.ID,
		Type:     s.Type,
		Config:   s.Config,
		Repos:    repos,
		Projects: projects,
	}
}

func mergeOverrideRepos(repos []database.OverrideRepo) []database.OverrideRepoRow {
	seen := make(map[string]int)
	var merged []database.OverrideRepoRow
	for _, r := range repos {
		if idx, ok := seen[r.ID]; ok {
			for _, t := range r.Teams {
				found := false
				for _, et := range merged[idx].Teams {
					if et == t {
						found = true
						break
					}
				}
				if !found {
					merged[idx].Teams = append(merged[idx].Teams, t)
				}
			}
		} else {
			seen[r.ID] = len(merged)
			merged = append(merged, database.OverrideRepoRow(r))
		}
	}
	return merged
}

func configToMap(c *database.ProductConfig) map[string]string {
	m := make(map[string]string)
	if c.CycleTimeSlowHours != nil {
		m["cycleTimeSlowHours"] = strconv.FormatFloat(*c.CycleTimeSlowHours, 'f', -1, 64)
	}
	if c.CycleTimeCriticalHours != nil {
		m["cycleTimeCriticalHours"] = strconv.FormatFloat(*c.CycleTimeCriticalHours, 'f', -1, 64)
	}
	if c.AnalyticsPrompt != "" {
		m["analyticsprompt"] = c.AnalyticsPrompt
	}
	return m
}

func collectBlueprintIDs(dashboards []database.Dashboard) string {
	seen := make(map[string]bool)
	var ids []string
	for _, d := range dashboards {
		if d.BlueprintID != "" {
			for _, part := range strings.Split(d.BlueprintID, ",") {
				part = strings.TrimSpace(part)
				if part != "" && !seen[part] {
					seen[part] = true
					ids = append(ids, part)
				}
			}
		}
		for _, bid := range d.BlueprintIDs {
			bid = strings.TrimSpace(bid)
			if bid != "" && !seen[bid] {
				seen[bid] = true
				ids = append(ids, bid)
			}
		}
	}
	return strings.Join(ids, ",")
}

func requireEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fmt.Fprintf(os.Stderr, "required environment variable %s is not set\n", k)
		os.Exit(1)
	}
	return v
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
