package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/internal/devlake"
)

type importAction struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Status      string `json:"status,omitempty"`
}

type repoDuplicate struct {
	Owner         string   `json:"owner"`
	Name          string   `json:"name"`
	RepoType      string   `json:"repo_type"`
	ExistingTeams []string `json:"existing_teams"`
}

type importRequiredConnection struct {
	Plugin          string `json:"plugin"`
	DefaultEndpoint string `json:"default_endpoint"`
	NeedsUsername   bool   `json:"needs_username"`
}

type importPreviewResponse struct {
	Team                importPreviewTeam          `json:"team"`
	Actions             []importAction             `json:"actions"`
	Duplicates          []repoDuplicate            `json:"duplicates"`
	RequiredConnections []importRequiredConnection `json:"required_connections"`
	Warnings            []string                   `json:"warnings"`
	Errors              []string                   `json:"errors"`
	Valid               bool                       `json:"valid"`
}

type importPreviewTeam struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContactEmail string `json:"contact_email,omitempty"`
	Exists       bool   `json:"exists"`
}

type importExecuteRequest struct {
	Team        json.RawMessage           `json:"team"`
	ProductID   string                    `json:"product_id,omitempty"`
	Connections []devlake.ConnectionEntry `json:"connections"`
}

type importExecuteResponse struct {
	TeamID     string              `json:"team_id"`
	TeamName   string              `json:"team_name"`
	Timestamp  string              `json:"timestamp"`
	Actions    []importAction      `json:"actions"`
	Duplicates []repoDuplicate     `json:"duplicates"`
	SyncResult *devlake.SyncResult `json:"sync_result,omitempty"`
	RevertInfo revertInfo          `json:"revert_info"`
}

type revertInfo struct {
	TeamID         string   `json:"team_id"`
	SourcesCreated []string `json:"sources_created"`
	ProductLabels  []string `json:"product_labels"`
	ConfigKeys     []string `json:"config_keys"`
}

// decodeImportTeam accepts both the legacy import format (contactemail) and
// the REST API format (contact_email).
func decodeImportTeam(data []byte) (database.Team, error) {
	var team database.Team
	if err := json.Unmarshal(data, &team); err != nil {
		return team, err
	}
	var email struct {
		ContactEmail       *string `json:"contact_email"`
		LegacyContactEmail *string `json:"contactemail"`
	}
	if err := json.Unmarshal(data, &email); err != nil {
		return team, err
	}
	if email.ContactEmail != nil {
		team.ContactEmail = *email.ContactEmail
	} else if email.LegacyContactEmail != nil {
		team.ContactEmail = *email.LegacyContactEmail
	}
	return team, nil
}

func importTeamPreviewHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		team, err := decodeImportTeam(body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}

		if ident, ok := identityFromContext(r.Context()); ok && !ident.IsAdmin && ident.Email != "" {
			team.ContactEmail = ident.Email
		}

		resp := buildImportPreview(r.Context(), store, team)
		writeJSON(w, http.StatusOK, resp)
	}
}

func buildImportPreview(ctx context.Context, store database.ConfigStore, team database.Team) importPreviewResponse {
	resp := importPreviewResponse{
		Team: importPreviewTeam{
			ID:           team.ID,
			Name:         team.Name,
			ContactEmail: team.ContactEmail,
		},
		Actions:             []importAction{},
		Duplicates:          []repoDuplicate{},
		RequiredConnections: []importRequiredConnection{},
		Warnings:            []string{},
		Errors:              []string{},
	}

	if team.ID == "" {
		resp.Errors = append(resp.Errors, "team id is required")
	}
	if team.Name == "" {
		resp.Errors = append(resp.Errors, "team name is required")
	}
	if team.ContactEmail != "" && !isValidEmail(team.ContactEmail) {
		resp.Errors = append(resp.Errors, "contact_email must be a valid email address")
	}
	if len(resp.Errors) > 0 {
		return resp
	}

	existing, err := store.GetTeamRow(ctx, team.ID)
	if err != nil {
		resp.Errors = append(resp.Errors, "failed to check existing team: "+err.Error())
		return resp
	}
	if existing != nil {
		resp.Team.Exists = true
		resp.Errors = append(resp.Errors, fmt.Sprintf("team %q already exists", team.ID))
		return resp
	}

	resp.Actions = append(resp.Actions, importAction{
		Type:        "create_team",
		Description: fmt.Sprintf("Create team %q (%s)", team.Name, team.ID),
	})

	if team.ContactEmail != "" {
		resp.Actions = append(resp.Actions, importAction{
			Type:        "set_contact_email",
			Description: fmt.Sprintf("Set contact email to %s", team.ContactEmail),
		})
	}

	for _, label := range team.Product {
		resp.Actions = append(resp.Actions, importAction{
			Type:        "add_product_label",
			Description: fmt.Sprintf("Add product label %q", label),
		})
	}

	importPreviewSources(ctx, store, team, &resp)

	// Warn about legacy fields that are ignored.
	var ignored []string
	if team.FetchURL != "" {
		ignored = append(ignored, "fetchurl")
	}
	if team.FetchCheckURL != "" {
		ignored = append(ignored, "fetchcheckurl")
	}
	if importHasBlueprintIDs(team.Dashboards) {
		ignored = append(ignored, "blueprintid/blueprintids")
	}
	if len(team.Dashboards) > 0 {
		ignored = append(ignored, "dashboards")
	}
	if len(ignored) > 0 {
		resp.Warnings = append(resp.Warnings,
			fmt.Sprintf("Ignored legacy fields: %s (not used in new system)", strings.Join(ignored, ", ")))
	}

	if len(resp.Duplicates) > 0 {
		resp.Warnings = append(resp.Warnings,
			fmt.Sprintf("%d repository/repositories already exist in other teams (will still be imported)", len(resp.Duplicates)))
	}

	resp.Valid = len(resp.Errors) == 0
	return resp
}

func importPreviewSources(ctx context.Context, store database.ConfigStore, team database.Team, resp *importPreviewResponse) {
	repoIndex := buildRepoIndex(ctx, store)
	neededPlugins := make(map[string]bool)

	for _, src := range team.Sources {
		resp.Actions = append(resp.Actions, importAction{
			Type:        "create_source",
			Description: fmt.Sprintf("Create source %q (type: %s)", src.ID, src.Type),
		})

		for _, repo := range src.Repos {
			resp.Actions = append(resp.Actions, importAction{
				Type:        "add_repo",
				Description: fmt.Sprintf("Add %s repo %s/%s", repo.Type, repo.Owner, repo.Name),
			})

			plugin := strings.ToLower(repo.Type)
			if plugin != "" {
				neededPlugins[plugin] = true
			}

			key := importRepoKey(repo.Type, repo.Owner, repo.Name)
			if teams, ok := repoIndex[key]; ok {
				resp.Duplicates = append(resp.Duplicates, repoDuplicate{
					Owner:         repo.Owner,
					Name:          repo.Name,
					RepoType:      repo.Type,
					ExistingTeams: teams,
				})
			}
		}

		for _, proj := range src.Projects {
			label := proj.JiraProject
			if label == "" {
				label = proj.BoardID
			}
			desc := fmt.Sprintf("Add %s project %s", proj.Type, label)
			if proj.BoardID != "" && proj.JiraProject != "" {
				desc += fmt.Sprintf(" (Board: %s)", proj.BoardID)
			}
			if proj.ScopeConfig != nil {
				desc += " with scope config"
			}
			resp.Actions = append(resp.Actions, importAction{
				Type:        "add_project",
				Description: desc,
			})

			plugin := strings.ToLower(proj.Type)
			if plugin != "" {
				neededPlugins[plugin] = true
			}
		}
	}

	for plugin := range neededPlugins {
		rc := importRequiredConnection{Plugin: plugin}
		switch plugin {
		case "github":
			rc.DefaultEndpoint = "https://api.github.com"
		case "gitlab":
			rc.DefaultEndpoint = "https://gitlab.com"
		case "jira":
			rc.NeedsUsername = true
		}
		resp.RequiredConnections = append(resp.RequiredConnections, rc)
		resp.Actions = append(resp.Actions, importAction{
			Type:        "create_connection",
			Description: fmt.Sprintf("Create %s connection (token required)", plugin),
		})
	}

	if len(team.Sources) > 0 {
		resp.Actions = append(resp.Actions, importAction{
			Type:        "devlake_sync",
			Description: "Sync repos and scopes with DevLake",
		})
	}
}

func validateImportJiraProjects(ctx context.Context, devlakeSvc *devlake.Service, team *database.Team, connections []devlake.ConnectionEntry) error {
	var jiraConn *devlake.ConnectionEntry
	for i := range connections {
		if connections[i].Plugin == "jira" {
			jiraConn = &connections[i]
			break
		}
	}

	for si := range team.Sources {
		for pi := range team.Sources[si].Projects {
			project := &team.Sources[si].Projects[pi]
			if project.Type != "jira" {
				continue
			}
			project.JiraProject = strings.TrimSpace(project.JiraProject)
			project.BoardID = strings.TrimSpace(project.BoardID)
			if project.JiraProject != "" {
				continue
			}
			if project.BoardID == "" {
				return fmt.Errorf("jira project key or board ID is required for source %s", team.Sources[si].ID)
			}
			if devlakeSvc == nil || !devlakeSvc.Enabled() || jiraConn == nil {
				return fmt.Errorf("jira project key is missing for board %s; provide a Jira connection so the key can be resolved", project.BoardID)
			}

			meta, err := devlakeSvc.ProxyJiraBoardMeta(ctx, *jiraConn, project.BoardID)
			if err != nil {
				return fmt.Errorf("jira board %s could not be resolved: %w", project.BoardID, err)
			}
			project.JiraProject = strings.TrimSpace(meta.ProjectKey)
			if project.JiraProject == "" {
				return fmt.Errorf("jira board %s has no single project key", project.BoardID)
			}
		}
	}
	return nil
}

func importHasBlueprintIDs(dashboards []database.Dashboard) bool {
	for _, d := range dashboards {
		if d.BlueprintID != "" || len(d.BlueprintIDs) > 0 {
			return true
		}
	}
	return false
}

func buildRepoIndex(ctx context.Context, store database.ConfigStore) map[string][]string {
	index := make(map[string][]string)

	teams, err := store.ListTeamRows(ctx)
	if err != nil {
		return index
	}

	for _, team := range teams {
		sources, err := store.ListSourceRows(ctx, team.ID)
		if err != nil {
			continue
		}
		for _, src := range sources {
			for _, repo := range src.Repos {
				key := importRepoKey(repo.Type, repo.Owner, repo.Name)
				index[key] = append(index[key], team.ID)
			}
		}
	}
	return index
}

func importRepoKey(repoType, owner, name string) string {
	return strings.ToLower(repoType + ":" + owner + "/" + name)
}

func importTeamExecuteHandler(store database.ConfigStore, devlakeSvc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req importExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		team, err := decodeImportTeam(req.Team)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid team JSON: "+err.Error())
			return
		}
		if ident, ok := identityFromContext(r.Context()); ok && !ident.IsAdmin && ident.Email != "" {
			team.ContactEmail = ident.Email
		}

		preview := buildImportPreview(r.Context(), store, team)
		if !preview.Valid {
			writeError(w, http.StatusBadRequest, strings.Join(preview.Errors, "; "))
			return
		}
		if err := validateImportJiraProjects(r.Context(), devlakeSvc, &team, req.Connections); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		user := "unknown"
		if ident, ok := identityFromContext(r.Context()); ok {
			user = ident.Username
		}

		slog.Info("import-team: starting",
			"team", team.ID, "team_name", team.Name, "user", user,
			"connections", len(req.Connections))

		// Detach from the request context so DevLake sync completes even if the
		// client disconnects. Values (request ID, identity) are preserved.
		ctx := context.WithoutCancel(r.Context())
		resp := importExecuteResponse{
			TeamID:     team.ID,
			TeamName:   team.Name,
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			Actions:    []importAction{},
			Duplicates: preview.Duplicates,
			RevertInfo: revertInfo{
				TeamID:         team.ID,
				SourcesCreated: []string{},
				ProductLabels:  []string{},
				ConfigKeys:     []string{},
			},
		}

		// Step 1: Create team (no fetchurl/fetchcheckurl — legacy fields).
		teamRow := database.TeamRow{
			ID:           team.ID,
			Name:         team.Name,
			ContactEmail: team.ContactEmail,
		}
		if err := store.CreateTeam(ctx, teamRow); err != nil {
			resp.Actions = append(resp.Actions, importAction{
				Type: "create_team", Status: "error",
				Description: fmt.Sprintf("Create team %q: %v", team.Name, err),
			})
			slog.Error("import-team: failed to create team", "team", team.ID, "err", err)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("create team: %v", err))
			return
		}
		resp.Actions = append(resp.Actions, importAction{
			Type: "create_team", Status: "success",
			Description: fmt.Sprintf("Created team %q (%s)", team.Name, team.ID),
		})

		// Step 2: Link team to product (same as wizard).
		if req.ProductID != "" {
			existing, err := store.GetProductTeamLinks(ctx, req.ProductID)
			if err != nil {
				slog.Error("import-team: failed to get product team links", "product", req.ProductID, "err", err)
				resp.Actions = append(resp.Actions, importAction{
					Type: "link_product", Status: "error",
					Description: fmt.Sprintf("Link to product %q: %v", req.ProductID, err),
				})
			} else {
				existing = append(existing, database.RelEntry{ID: team.ID, SortOrder: len(existing)})
				if err := store.ReplaceProductTeamLinks(ctx, req.ProductID, existing); err != nil {
					slog.Error("import-team: failed to link team to product", "product", req.ProductID, "team", team.ID, "err", err)
					resp.Actions = append(resp.Actions, importAction{
						Type: "link_product", Status: "error",
						Description: fmt.Sprintf("Link to product %q: %v", req.ProductID, err),
					})
				} else {
					slog.Info("import-team: linked team to product", "product", req.ProductID, "team", team.ID)
					resp.Actions = append(resp.Actions, importAction{
						Type: "link_product", Status: "success",
						Description: fmt.Sprintf("Linked team to product %q", req.ProductID),
					})
				}
			}
		}

		// Step 3: Create DevLake connections (same as wizard).
		if len(req.Connections) > 0 {
			if err := onboardCreateConnections(ctx, devlakeSvc, team.ID, req.Connections); err != nil {
				slog.Error("import-team: failed to create connections", "team", team.ID, "err", err)
				resp.Actions = append(resp.Actions, importAction{
					Type: "create_connection", Status: "error",
					Description: fmt.Sprintf("Create connections: %v", err),
				})
			} else {
				for _, c := range req.Connections {
					resp.Actions = append(resp.Actions, importAction{
						Type: "create_connection", Status: "success",
						Description: fmt.Sprintf("Created %s connection", c.Plugin),
					})
				}
			}
		}

		// Step 4: Product labels.
		if len(team.Product) > 0 {
			if err := store.ReplaceTeamProductLabels(ctx, team.ID, team.Product); err != nil {
				slog.Error("import-team: failed to set product labels", "team", team.ID, "err", err)
				resp.Actions = append(resp.Actions, importAction{
					Type: "add_product_label", Status: "error",
					Description: fmt.Sprintf("Set product labels: %v", err),
				})
			} else {
				resp.Actions = append(resp.Actions, importAction{
					Type: "add_product_label", Status: "success",
					Description: fmt.Sprintf("Added %d product label(s)", len(team.Product)),
				})
				resp.RevertInfo.ProductLabels = team.Product
			}
		}

		// Step 5: Sources with repos and projects.
		for _, src := range team.Sources {
			srcRow := importConvertSource(src)
			if err := store.CreateSource(ctx, team.ID, srcRow); err != nil {
				slog.Error("import-team: failed to create source",
					"team", team.ID, "source", src.ID, "err", err)
				resp.Actions = append(resp.Actions, importAction{
					Type: "create_source", Status: "error",
					Description: fmt.Sprintf("Create source %q: %v", src.ID, err),
				})
			} else {
				repoCount := len(src.Repos)
				projCount := len(src.Projects)
				desc := fmt.Sprintf("Created source %q with %d repo(s), %d project(s)", src.ID, repoCount, projCount)
				resp.Actions = append(resp.Actions, importAction{
					Type: "create_source", Status: "success",
					Description: desc,
				})
				resp.RevertInfo.SourcesCreated = append(resp.RevertInfo.SourcesCreated, src.ID)
			}
		}

		// Step 6: DevLake sync — register repos/scopes (same as wizard).
		if devlakeSvc != nil && devlakeSvc.Enabled() && len(team.Sources) > 0 {
			slog.Info("import-team: starting DevLake sync", "team", team.ID)
			syncResult, err := devlakeSvc.SyncTeam(ctx, team.ID)
			if err != nil {
				slog.Error("import-team: sync failed", "team", team.ID, "err", err)
				resp.Actions = append(resp.Actions, importAction{
					Type: "devlake_sync", Status: "error",
					Description: fmt.Sprintf("DevLake sync: %v", err),
				})
			} else {
				slog.Info("import-team: sync complete", "team", team.ID,
					"created", syncResult.Created,
					"updated", syncResult.Updated,
					"deleted", syncResult.Deleted,
					"skipped", syncResult.Skipped,
					"errors", syncResult.Errors)
				desc := fmt.Sprintf("DevLake sync: %d created, %d updated, %d skipped",
					len(syncResult.Created), len(syncResult.Updated), len(syncResult.Skipped))
				if len(syncResult.Errors) > 0 {
					desc += fmt.Sprintf(", %d errors", len(syncResult.Errors))
				}
				resp.Actions = append(resp.Actions, importAction{
					Type: "devlake_sync", Status: "success",
					Description: desc,
				})
				resp.SyncResult = syncResult
			}
		}

		// Step 7: Apply Jira scope configs from imported projects.
		importApplyScopeConfigs(ctx, devlakeSvc, team, &resp)

		slog.Info("import-team: complete",
			"team", team.ID, "team_name", team.Name, "user", user,
			"actions", len(resp.Actions))

		writeJSON(w, http.StatusCreated, resp)
	}
}

func importApplyScopeConfigs(ctx context.Context, devlakeSvc *devlake.Service, team database.Team, resp *importExecuteResponse) {
	if devlakeSvc == nil || !devlakeSvc.Enabled() {
		return
	}
	scopeConfigs := map[string]devlake.JiraScopeConfig{}
	for _, src := range team.Sources {
		for _, proj := range src.Projects {
			if proj.ScopeConfig != nil && proj.BoardID != "" {
				scopeConfigs[proj.BoardID] = devlake.JiraScopeConfig{
					RequirementTypes: proj.ScopeConfig.RequirementTypes,
					BugTypes:         proj.ScopeConfig.BugTypes,
					StoryPointField:  proj.ScopeConfig.StoryPointField,
				}
			}
		}
	}
	if len(scopeConfigs) == 0 {
		return
	}
	if err := devlakeSvc.ApplyJiraScopeConfigs(ctx, team.ID, scopeConfigs); err != nil {
		slog.Error("import-team: scope config failed", "team", team.ID, "err", err)
		resp.Actions = append(resp.Actions, importAction{
			Type: "apply_scope_config", Status: "error",
			Description: fmt.Sprintf("Apply Jira scope configs: %v", err),
		})
	} else {
		resp.Actions = append(resp.Actions, importAction{
			Type: "apply_scope_config", Status: "success",
			Description: fmt.Sprintf("Applied %d Jira scope config(s)", len(scopeConfigs)),
		})
	}
}

func importConvertSource(s database.Source) database.SourceRow {
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
