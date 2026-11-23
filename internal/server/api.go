package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/authz"
	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/internal/devlake"
	"github.com/fullsend-ai/orgpulse/internal/logging"
	"github.com/fullsend-ai/orgpulse/internal/netguard"
)

func sanitizeLog(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", ""), "\r", "")
}

type apiOptions struct {
	outbound outboundConfig
	// trustedReportURLs are the only subscription and alert URLs a non-admin
	// may store; see reportURLAllowed.
	trustedReportURLs []string
}

func registerAPI(mux *http.ServeMux, store database.ConfigStore, cfg AuthConfig, workflowExec *WorkflowExecutor, devlakeSvc *devlake.Service, devlakeDB *sql.DB, az *Authorizer, opts apiOptions) {
	auth := requireAuth(store, cfg, az)
	admin := requireAdmin(store, cfg, az)
	lockMgr := NewWorkflowLockManager(30 * time.Second)

	mux.HandleFunc("GET /api/v1/auth/me", authMeHandler(store, cfg, az))

	mux.Handle("GET /api/v1/orgs", auth(listRows(store.ListOrgRows, func(g authz.Grant, row database.OrgRow) bool {
		return g.CanReadOrg(row.ID)
	})))
	mux.Handle("POST /api/v1/orgs", auth(orgCreateHandler(store)))
	mux.Handle("GET /api/v1/orgs/{id}", auth(getRow(store.GetOrgRow, func(g authz.Grant, id string) bool {
		return g.CanReadOrg(id)
	})))
	mux.Handle("PUT /api/v1/orgs/{id}", auth(orgUpdateHandler(store)))
	mux.Handle("DELETE /api/v1/orgs/{id}", admin(crudDelete(store.DeleteOrg)))

	mux.Handle("GET /api/v1/products", auth(listRows(store.ListProductRows, func(g authz.Grant, row database.ProductRow) bool {
		return g.CanReadProduct(row.ID)
	})))
	mux.Handle("POST /api/v1/products", auth(productCreateHandler(store)))
	mux.Handle("GET /api/v1/products/{id}", auth(getRow(store.GetProductRow, func(g authz.Grant, id string) bool {
		return g.CanReadProduct(id)
	})))
	mux.Handle("PUT /api/v1/products/{id}", auth(productUpdateHandler(store)))
	mux.Handle("DELETE /api/v1/products/{id}", admin(crudDelete(store.DeleteProduct)))

	mux.Handle("GET /api/v1/teams", auth(listRows(store.ListTeamRows, func(g authz.Grant, row database.TeamRow) bool {
		return g.CanReadTeam(row.ID)
	})))
	mux.Handle("POST /api/v1/teams", admin(teamCreateHandler(store)))
	mux.Handle("GET /api/v1/teams/{id}", auth(getRow(store.GetTeamRow, func(g authz.Grant, id string) bool {
		return g.CanReadTeam(id)
	})))
	mux.Handle("PUT /api/v1/teams/{id}", auth(teamUpdateHandler(store)))
	mux.Handle("DELETE /api/v1/teams/{id}", admin(teamDeleteHandler(store, devlakeSvc)))

	mux.Handle("GET /api/v1/orgs/{id}/products", auth(orgProductsGetHandler(store)))
	mux.Handle("PUT /api/v1/orgs/{id}/products", auth(relPutScoped(store.ReplaceOrgProductLinks, store.GetOrgProductLinks, func(g authz.Grant, id string) bool {
		return g.CanUpdateOrg(id)
	}, authz.Grant.CanUpdateProduct)))
	mux.Handle("GET /api/v1/products/{id}/teams", auth(productTeamsGetHandler(store)))
	mux.Handle("PUT /api/v1/products/{id}/teams", auth(relPutScoped(store.ReplaceProductTeamLinks, store.GetProductTeamLinks, func(g authz.Grant, id string) bool {
		return g.CanUpdateProduct(id)
	}, authz.Grant.CanUpdateTeam)))
	mux.Handle("GET /api/v1/team-product-links", auth(teamProductLinksHandler(store)))
	mux.Handle("GET /api/v1/teams/{id}/product-labels", auth(labelGet(store.GetTeamProductLabels)))
	mux.Handle("PUT /api/v1/teams/{id}/product-labels", admin(labelPut(store.ReplaceTeamProductLabels)))

	mux.Handle("GET /api/v1/config/{type}/{id}", auth(configGetHandler(store)))
	mux.Handle("PUT /api/v1/config/{type}/{id}", auth(configPutTeamAwareHandler(store)))
	mux.Handle("DELETE /api/v1/config/{type}/{id}", admin(configDeleteHandler(store)))

	mux.Handle("GET /api/v1/dashboards", auth(dashboardListHandler(store)))
	mux.Handle("GET /api/v1/data-freshness", auth(dataFreshnessHandler(store, devlakeSvc, devlakeDB)))
	mux.Handle("POST /api/v1/dashboards", auth(dashboardCreateTeamAwareHandler(store)))
	mux.Handle("GET /api/v1/dashboards/{id}", auth(dashboardGetHandler(store)))
	mux.Handle("PUT /api/v1/dashboards/{id}", auth(dashboardUpdateTeamAwareHandler(store)))
	mux.Handle("DELETE /api/v1/dashboards/{id}", auth(dashboardDeleteTeamAwareHandler(store)))

	mux.Handle("GET /api/v1/teams/{teamId}/sync-status", auth(syncStatusHandler(store)))
	mux.Handle("GET /api/v1/teams/{teamId}/sources", auth(sourceListHandler(store)))
	mux.Handle("POST /api/v1/teams/{teamId}/sources", admin(sourceCreateHandler(store)))
	mux.Handle("GET /api/v1/teams/{teamId}/sources/{sourceId}", auth(sourceGetHandler(store)))
	mux.Handle("PUT /api/v1/teams/{teamId}/sources/{sourceId}", admin(sourceUpdateHandler(store)))
	mux.Handle("DELETE /api/v1/teams/{teamId}/sources/{sourceId}", admin(sourceDeleteHandler(store)))

	mux.Handle("GET /api/v1/products/{id}/overrides", auth(overridesGetHandler(store)))
	mux.Handle("PUT /api/v1/products/{id}/overrides", admin(overridesPutHandler(store)))

	mux.Handle("GET /api/v1/subscriptions", auth(subscriptionListHandler(store)))
	mux.Handle("POST /api/v1/subscriptions", auth(subscriptionCreateHandler(store, opts.trustedReportURLs)))
	mux.Handle("GET /api/v1/subscriptions/{id}", auth(subscriptionGetHandler(store)))
	mux.Handle("PUT /api/v1/subscriptions/{id}", auth(subscriptionUpdateHandler(store, opts.trustedReportURLs)))
	mux.Handle("DELETE /api/v1/subscriptions/{id}", auth(subscriptionDeleteHandler(store)))

	mux.Handle("GET /api/v1/alerts", auth(alertListHandler(store)))
	mux.Handle("POST /api/v1/alerts", auth(alertCreateHandler(store, opts.trustedReportURLs)))
	mux.Handle("GET /api/v1/alerts/{id}", auth(alertGetHandler(store)))
	mux.Handle("PUT /api/v1/alerts/{id}", auth(alertUpdateHandler(store, opts.trustedReportURLs)))
	mux.Handle("DELETE /api/v1/alerts/{id}", auth(alertDeleteHandler(store)))

	mux.Handle("GET /api/v1/canned-queries", auth(cannedQueryListHandler(store)))
	mux.Handle("POST /api/v1/canned-queries", admin(cannedQueryCreateHandler(store)))
	mux.Handle("GET /api/v1/canned-queries/{id}", auth(cannedQueryGetHandler(store)))
	mux.Handle("PUT /api/v1/canned-queries/{id}", admin(cannedQueryUpdateHandler(store)))
	mux.Handle("DELETE /api/v1/canned-queries/{id}", admin(cannedQueryDeleteHandler(store)))

	mux.Handle("GET /api/v1/canned-scripts", auth(cannedScriptListHandler(store)))
	mux.Handle("POST /api/v1/canned-scripts", admin(cannedScriptCreateHandler(store)))
	mux.Handle("GET /api/v1/canned-scripts/{id}", auth(cannedScriptGetHandler(store)))
	mux.Handle("PUT /api/v1/canned-scripts/{id}", admin(cannedScriptUpdateHandler(store)))
	mux.Handle("DELETE /api/v1/canned-scripts/{id}", admin(cannedScriptDeleteHandler(store)))

	mux.Handle("GET /api/v1/json-schemas", auth(jsonSchemaListHandler(store)))
	mux.Handle("POST /api/v1/json-schemas", admin(jsonSchemaCreateHandler(store)))
	mux.Handle("GET /api/v1/json-schemas/{id}", auth(jsonSchemaGetHandler(store)))
	mux.Handle("PUT /api/v1/json-schemas/{id}", admin(jsonSchemaUpdateHandler(store)))
	mux.Handle("DELETE /api/v1/json-schemas/{id}", admin(jsonSchemaDeleteHandler(store)))

	mux.Handle("GET /api/v1/stored-procedures", auth(storedProcListHandler(store)))
	mux.Handle("POST /api/v1/stored-procedures", admin(storedProcCreateHandler(store)))
	mux.Handle("GET /api/v1/stored-procedures/{id}", auth(storedProcGetHandler(store)))
	mux.Handle("PUT /api/v1/stored-procedures/{id}", admin(storedProcUpdateHandler(store)))
	mux.Handle("DELETE /api/v1/stored-procedures/{id}", admin(storedProcDeleteHandler(store)))
	if workflowExec != nil {
		mux.Handle("POST /api/v1/stored-procedures/{id}/deploy", admin(storedProcDeployHandler(store, workflowExec)))
		mux.Handle("POST /api/v1/stored-procedures/execute-sql", admin(storedProcExecuteSQLHandler(workflowExec)))
	}

	mux.Handle("GET /api/v1/workflows", auth(workflowListHandler(store, lockMgr)))
	mux.Handle("POST /api/v1/workflows", admin(workflowCreateHandler(store)))
	mux.Handle("GET /api/v1/workflows/{id}", auth(workflowGetHandler(store)))
	mux.Handle("PUT /api/v1/workflows/{id}", admin(workflowUpdateHandler(store)))
	mux.Handle("DELETE /api/v1/workflows/{id}", admin(workflowDeleteHandler(store)))
	mux.Handle("PUT /api/v1/workflows/{id}/draft", admin(workflowDraftHandler(store)))
	mux.Handle("POST /api/v1/workflows/{id}/publish", admin(workflowPublishHandler(store)))
	mux.Handle("GET /api/v1/workflows/{id}/versions", auth(workflowVersionsListHandler(store)))
	mux.Handle("GET /api/v1/workflows/{id}/versions/{version}", auth(workflowVersionGetHandler(store)))
	mux.Handle("POST /api/v1/workflows/{id}/versions/{version}/production", admin(workflowSetProductionHandler(store)))
	mux.Handle("POST /api/v1/workflows/{id}/archive", admin(workflowArchiveHandler(store)))
	mux.Handle("POST /api/v1/workflows/{id}/unarchive", admin(workflowUnarchiveHandler(store)))
	mux.Handle("GET /api/v1/workflows/{id}/export", admin(workflowExportHandler(store)))
	mux.Handle("POST /api/v1/workflows/import", admin(workflowImportHandler(store)))
	mux.Handle("POST /api/v1/workflows/{id}/lock", admin(workflowLockHandler(lockMgr)))
	mux.Handle("DELETE /api/v1/workflows/{id}/lock", admin(workflowUnlockHandler(lockMgr)))
	mux.Handle("POST /api/v1/workflows/{id}/force-unlock", admin(workflowForceUnlockHandler(lockMgr)))

	if workflowExec != nil {
		mux.Handle("POST /api/v1/workflows/{id}/test", admin(workflowTestHandler(store, workflowExec)))
		mux.Handle("POST /api/v1/workflows/{id}/run-tests", admin(workflowRunTestsHandler(store, workflowExec)))
		mux.Handle("POST /api/v1/workflows/run-stage", admin(workflowRunStageHandler(workflowExec)))
		mux.Handle("GET /workflows/staging/{name}", auth(workflowLiveHandler(store, workflowExec, az)))
		mux.Handle("POST /workflows/staging/{name}", auth(workflowLiveHandler(store, workflowExec, az)))
		mux.Handle("GET /workflows/staging/{name}/{version}", auth(workflowLiveVersionHandler(store, workflowExec, az)))
		mux.Handle("POST /workflows/staging/{name}/{version}", auth(workflowLiveVersionHandler(store, workflowExec, az)))
	}

	mux.Handle("GET /api/v1/keys", admin(keysListHandler(store)))
	mux.Handle("POST /api/v1/keys", admin(keysCreateHandler(store)))
	mux.Handle("DELETE /api/v1/keys/{id}", admin(keysDeleteHandler(store)))

	mux.Handle("GET /auth/vscode-token", admin(vsCodeTokenConfirmHandler()))
	mux.Handle("POST /auth/vscode-token", admin(vsCodeTokenHandler(store)))

	if devlakeSvc != nil && devlakeSvc.Enabled() {
		mux.Handle("POST /api/v1/devlake/teams/{teamId}/sync", admin(devlakeSyncHandler(devlakeSvc)))
		mux.Handle("POST /api/v1/devlake/teams/{teamId}/trigger", admin(devlakeTriggerHandler(devlakeSvc)))
		mux.Handle("POST /api/v1/devlake/teams/{teamId}/connections", auth(devlakeCreateConnectionHandler(store, devlakeSvc)))
		mux.Handle("GET /api/v1/devlake/teams/{teamId}/codecov/available", admin(devlakeAvailableCodecovHandler(store, devlakeSvc)))
		mux.Handle("POST /api/v1/devlake/teams/{teamId}/codecov/attach", admin(devlakeAttachCodecovHandler(store, devlakeSvc)))
		mux.Handle("PUT /api/v1/devlake/teams/{teamId}/connections/{plugin}", auth(devlakeUpdateConnectionHandler(store, devlakeSvc)))
		mux.Handle("GET /api/v1/devlake/teams/{teamId}/token-status", auth(devlakeTokenStatusHandler(devlakeSvc)))
		mux.Handle("GET /api/v1/devlake/teams/{teamId}/existing-scopes/{plugin}", auth(devlakeExistingScopesHandler(devlakeSvc)))
		mux.Handle("GET /api/v1/devlake/teams/{teamId}/browse-remote/{plugin}", auth(devlakeBrowseRemoteHandler(devlakeSvc)))
		mux.Handle("GET /api/v1/devlake/teams/{teamId}/browse-remote-count/{plugin}", auth(devlakeBrowseRemoteCountHandler(devlakeSvc)))
		mux.Handle("POST /api/v1/devlake/test-connection", auth(devlakeTestConnectionHandler(devlakeSvc)))
		mux.Handle("POST /api/v1/devlake/proxy-browse", auth(devlakeProxyBrowseHandler(devlakeSvc)))
		mux.Handle("POST /api/v1/devlake/proxy-lookup", auth(devlakeProxyLookupHandler(devlakeSvc)))
		mux.Handle("POST /api/v1/devlake/proxy-jira-meta", auth(devlakeProxyJiraMetaHandler(devlakeSvc)))
		mux.Handle("POST /api/v1/devlake/proxy-jira-project", auth(devlakeProxyJiraProjectHandler(devlakeSvc)))
		mux.Handle("POST /api/v1/devlake/proxy-jira-board-meta", auth(devlakeProxyJiraBoardMetaHandler(devlakeSvc)))
		mux.Handle("GET /api/v1/devlake/teams/{teamId}/jira-board-meta/{boardId}", auth(devlakeTeamJiraBoardMetaHandler(store, devlakeSvc)))
	}

	mux.Handle("GET /api/v1/my-issues", auth(myIssuesHandler(store, opts.outbound)))
	mux.Handle("GET /api/v1/my-contributions", auth(myContributionsHandler(store, opts.outbound)))
	mux.Handle("GET /api/v1/my-links", auth(myLinksHandler(store, opts.outbound)))
	mux.Handle("GET /api/v1/my-issues/transitions", auth(myIssueTransitionsHandler(store, opts.outbound)))
	mux.Handle("POST /api/v1/my-issues/transition", auth(myIssueTransitionHandler(store, opts.outbound)))

	mux.Handle("GET /api/v1/personal-tokens", auth(personalTokensGetHandler(store)))
	mux.Handle("PUT /api/v1/personal-tokens/{plugin}", auth(personalTokenPutHandler(store, opts.outbound)))
	mux.Handle("DELETE /api/v1/personal-tokens/{plugin}", auth(personalTokenDeleteHandler(store)))

	mux.Handle("POST /api/v1/onboard", auth(onboardHandler(store, devlakeSvc)))
	mux.Handle("POST /api/v1/offboard", auth(offboardHandler(store, devlakeSvc)))
	mux.Handle("POST /api/v1/onboard-source", auth(onboardSourceHandler(store, devlakeSvc)))
	mux.Handle("POST /api/v1/offboard-source", auth(offboardSourceHandler(store, devlakeSvc)))
	mux.Handle("POST /api/v1/teams/{teamId}/validate-source", auth(validateSourceHandler(store, devlakeSvc)))

	mux.Handle("POST /api/v1/admin/import-team/preview", admin(importTeamPreviewHandler(store)))
	mux.Handle("POST /api/v1/admin/import-team/execute", admin(importTeamExecuteHandler(store, devlakeSvc)))

	if devlakeDB != nil {
		mux.Handle("POST /api/v1/devlake-db/query", admin(devlakeDBQueryHandler(devlakeDB)))
		mux.Handle("GET /api/v1/devlake-db/tables", admin(devlakeDBTablesHandler(devlakeDB)))
	}
}

// --------------- Auth middleware ---------------

func requireAuth(store database.ConfigStore, cfg AuthConfig, az *Authorizer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, err := authenticateRequest(r, store, cfg)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "auth error")
				return
			}
			if !id.Known() {
				if extractAPIKey(r) != "" {
					writeError(w, http.StatusUnauthorized, "invalid API key")
					return
				}
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			ctx, err := attachGrant(withIdentity(r.Context(), id), id, az)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "auth error")
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requireAdmin(store database.ConfigStore, cfg AuthConfig, az *Authorizer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, err := authenticateRequest(r, store, cfg)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "auth error")
				return
			}
			if !id.Known() {
				if extractAPIKey(r) != "" {
					writeError(w, http.StatusUnauthorized, "invalid API key")
					return
				}
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if !id.IsAdmin {
				writeError(w, http.StatusForbidden, "admin access required")
				return
			}
			ctx, err := attachGrant(withIdentity(r.Context(), id), id, az)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "auth error")
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func isTeamManagerOrAdmin(ctx context.Context, store database.ConfigStore, ident Identity, teamID string) bool {
	if ident.IsAdmin {
		return true
	}
	if grant, ok := grantFromContext(ctx); ok {
		return grant.CanUpdateTeam(teamID)
	}
	team, err := store.GetTeamRow(ctx, teamID)
	if err != nil || team == nil {
		return false
	}
	return team.ContactEmail != "" && strings.EqualFold(team.ContactEmail, ident.Email)
}

func teamUpdateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		ident, ok := identityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if !isTeamManagerOrAdmin(r.Context(), store, ident, id) {
			writeError(w, http.StatusForbidden, "admin or team manager access required")
			return
		}
		input, err := decodeBody[database.TeamRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !ident.IsAdmin && ident.Email != "" {
			input.ContactEmail = ident.Email
		}
		if strings.TrimSpace(input.AuthGroup) == "" {
			writeError(w, http.StatusBadRequest, errAuthGroupRequired)
			return
		}
		if input.ContactEmail != "" && !isValidEmail(input.ContactEmail) {
			writeError(w, http.StatusBadRequest, "contact_email must be a valid email address")
			return
		}
		if err := store.UpdateTeam(r.Context(), id, input); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusOK, input)
	}
}

func configPutTeamAwareHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entityType := r.PathValue("type")
		entityID := r.PathValue("id")
		if !isValidEntityType(entityType) {
			writeError(w, http.StatusBadRequest, "type must be 'org', 'product', or 'team'")
			return
		}
		ident, ok := identityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if entityType == "team" {
			if !isTeamManagerOrAdmin(r.Context(), store, ident, entityID) {
				writeError(w, http.StatusForbidden, "admin or team manager access required")
				return
			}
		} else if !writable(w, r, func(g authz.Grant) bool {
			return canWriteOwner(g, entityType, entityID)
		}) {
			return
		}
		config, err := decodeBody[map[string]string](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		existing, err := store.GetEntityConfigKV(r.Context(), entityType, entityID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		for k, v := range config {
			if v == redactedSecret && isSecretConfigKey(k) {
				delete(config, k)
			}
		}
		for k, v := range existing {
			if strings.HasPrefix(k, "devlake_") {
				config[k] = v
			}
		}
		if err := store.ReplaceEntityConfig(r.Context(), entityType, entityID, config); err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusOK, redactSecretConfig(config))
	}
}

func dashboardCreateTeamAwareHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeBody[database.DashboardRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.OwnerType == "" || input.OwnerID == "" || input.DashboardID == "" || input.Type == "" || input.Title == "" {
			writeError(w, http.StatusBadRequest, "owner_type, owner_id, dashboard_id, type, and title are required")
			return
		}
		if msg := validateDiagramParams(input.Diagrams); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		ident, ok := identityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if input.OwnerType == "team" {
			if !isTeamManagerOrAdmin(r.Context(), store, ident, input.OwnerID) {
				writeError(w, http.StatusForbidden, "admin or team manager access required")
				return
			}
		} else if !writable(w, r, func(g authz.Grant) bool {
			return canWriteOwner(g, input.OwnerType, input.OwnerID)
		}) {
			return
		}
		autoID, err := store.CreateDashboard(r.Context(), input)
		if err != nil {
			if errors.Is(err, database.ErrDuplicateName) {
				writeError(w, http.StatusConflict, "dashboard already exists for this owner")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.AutoID = autoID
		writeJSON(w, http.StatusCreated, input)
	}
}

func dashboardUpdateTeamAwareHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid dashboard id")
			return
		}
		ident, ok := identityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		existing, err := store.GetDashboardRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if existing.OwnerType == "team" {
			if !isTeamManagerOrAdmin(r.Context(), store, ident, existing.OwnerID) {
				writeError(w, http.StatusForbidden, "admin or team manager access required")
				return
			}
		} else if !writable(w, r, func(g authz.Grant) bool {
			return canWriteOwner(g, existing.OwnerType, existing.OwnerID)
		}) {
			return
		}
		input, err := decodeBody[database.DashboardRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if msg := validateDiagramParams(input.Diagrams); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		if err := store.UpdateDashboard(r.Context(), id, input); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.AutoID = id
		writeJSON(w, http.StatusOK, input)
	}
}

func validateDiagramParams(diagrams []database.DiagramRow) string {
	for _, d := range diagrams {
		p := bytes.TrimSpace(d.Params)
		if len(p) == 0 || string(p) == "null" {
			continue
		}
		if p[0] != '{' {
			return fmt.Sprintf("diagram at position %d: params must be a JSON object", d.Position)
		}
	}
	return ""
}

func dashboardDeleteTeamAwareHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid dashboard id")
			return
		}
		ident, ok := identityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		existing, err := store.GetDashboardRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if existing.OwnerType == "team" {
			if !isTeamManagerOrAdmin(r.Context(), store, ident, existing.OwnerID) {
				writeError(w, http.StatusForbidden, "admin or team manager access required")
				return
			}
		} else if !writable(w, r, func(g authz.Grant) bool {
			return canWriteOwner(g, existing.OwnerType, existing.OwnerID)
		}) {
			return
		}
		if err := store.DeleteDashboard(r.Context(), id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func authMeHandler(store database.ConfigStore, cfg AuthConfig, az *Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := authenticateRequest(r, store, cfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "auth error")
			return
		}
		authType := "anonymous"
		switch {
		case cfg.DevAdminOverride:
			authType = "dev_override"
		case cfg.TestMode:
			authType = "test_mode"
		case id.IsAPIKey:
			authType = "api_key"
		case id.Known():
			authType = "forwarded_identity"
		}

		roles := id.Roles
		if len(roles) == 0 {
			if id.Known() {
				roles = []string{"contributor"}
			} else {
				roles = []string{"anonymous"}
			}
		}

		grant := authz.Grant{}
		if id.Known() {
			ctx, grantErr := attachGrant(r.Context(), id, az)
			if grantErr != nil {
				writeError(w, http.StatusInternalServerError, "auth error")
				return
			}
			grant, _ = grantFromContext(ctx)
		}

		resp := map[string]any{
			"authenticated": id.Known(),
			"username":      id.Username,
			"email":         id.Email,
			"is_admin":      id.IsAdmin,
			"auth_type":     authType,
			"roles":         roles,
			"grant":         grant.Summary(),
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func extractBearerToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func extractAPIKey(r *http.Request) string {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}
	return extractBearerToken(r)
}

// --------------- Generic CRUD handlers ---------------

func crudList[T any](listFn func(context.Context) ([]T, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := listFn(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if items == nil {
			items = []T{}
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func listRows[T any](listFn func(context.Context) ([]T, error), keep func(authz.Grant, T) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := listFn(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if grant, ok := grantFromContext(r.Context()); ok {
			filtered := make([]T, 0, len(items))
			for _, item := range items {
				if keep(grant, item) {
					filtered = append(filtered, item)
				}
			}
			items = filtered
		}
		if items == nil {
			items = []T{}
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func getRow[T any](getFn func(context.Context, string) (*T, error), allow func(authz.Grant, string) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !visible(w, r, func(g authz.Grant) bool { return allow(g, id) }) {
			return
		}
		item, err := getFn(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if item == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func orgCreateHandler(store database.ConfigStore) http.HandlerFunc {
	next := crudCreate(store.CreateOrg, func(row database.OrgRow) string { return row.ID })
	return func(w http.ResponseWriter, r *http.Request) {
		if !writable(w, r, func(g authz.Grant) bool { return g.CanCreateOrg() }) {
			return
		}
		next.ServeHTTP(w, r)
	}
}

func orgUpdateHandler(store database.ConfigStore) http.HandlerFunc {
	next := crudUpdate(store.UpdateOrg)
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !writable(w, r, func(g authz.Grant) bool { return g.CanUpdateOrg(id) }) {
			return
		}
		next.ServeHTTP(w, r)
	}
}

func crudGet[T any](getFn func(context.Context, string) (*T, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		item, err := getFn(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if item == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

func crudCreate[T any](createFn func(context.Context, T) error, idFn func(T) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeBody[T](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := createFn(r.Context(), input); err != nil {
			if errors.Is(err, database.ErrDuplicateName) {
				writeError(w, http.StatusConflict, fmt.Sprintf("id %q already exists", idFn(input)))
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusCreated, input)
	}
}

func productCreateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !writable(w, r, func(g authz.Grant) bool { return g.CanCreateProduct() }) {
			return
		}
		input, err := decodeBody[database.ProductRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.ContactEmail != "" && !isValidEmail(input.ContactEmail) {
			writeError(w, http.StatusBadRequest, "contact_email must be a valid email address")
			return
		}
		if err := store.CreateProduct(r.Context(), input); err != nil {
			if errors.Is(err, database.ErrDuplicateName) {
				writeError(w, http.StatusConflict, fmt.Sprintf("id %q already exists", input.ID))
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusCreated, input)
	}
}

func productUpdateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		input, err := decodeBody[database.ProductRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.ContactEmail != "" && !isValidEmail(input.ContactEmail) {
			writeError(w, http.StatusBadRequest, "contact_email must be a valid email address")
			return
		}
		if !productWritable(w, r, store, id, input.AuthGroup) {
			return
		}
		if err := store.UpdateProduct(r.Context(), id, input); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusOK, input)
	}
}

// errAuthGroupRequired is returned when a team create/update omits auth_group.
const errAuthGroupRequired = "auth_group is required"

func teamCreateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeBody[database.TeamRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(input.AuthGroup) == "" {
			writeError(w, http.StatusBadRequest, errAuthGroupRequired)
			return
		}
		if input.ContactEmail != "" && !isValidEmail(input.ContactEmail) {
			writeError(w, http.StatusBadRequest, "contact_email must be a valid email address")
			return
		}
		if err := store.CreateTeam(r.Context(), input); err != nil {
			if errors.Is(err, database.ErrDuplicateName) {
				writeError(w, http.StatusConflict, fmt.Sprintf("id %q already exists", input.ID))
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusCreated, input)
	}
}

func crudUpdate[T any](updateFn func(context.Context, string, T) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		input, err := decodeBody[T](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := updateFn(r.Context(), id, input); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusOK, input)
	}
}

func crudDelete(deleteFn func(context.Context, string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := deleteFn(r.Context(), id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --------------- Relationship handlers ---------------

func relGet(getFn func(context.Context, string) ([]database.RelEntry, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		entries, err := getFn(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if entries == nil {
			entries = []database.RelEntry{}
		}
		writeJSON(w, http.StatusOK, entries)
	}
}

func relPut(putFn func(context.Context, string, []database.RelEntry) error, getFn func(context.Context, string) ([]database.RelEntry, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		entries, err := decodeBody[[]database.RelEntry](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		slog.Info("relPut: saving", "id", id, "entries", len(entries))
		if err := putFn(r.Context(), id, entries); err != nil {
			slog.Error("relPut: save failed", "id", id, "err", err)
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		stored, err := getFn(r.Context(), id)
		if err != nil {
			slog.Error("relPut: read-back failed", "id", id, "err", err)
			writeJSON(w, http.StatusOK, entries)
			return
		}
		slog.Info("relPut: verified", "id", id, "stored", len(stored))
		if stored == nil {
			stored = []database.RelEntry{}
		}
		writeJSON(w, http.StatusOK, stored)
	}
}

// relPutScoped guards a parent's child list. Linking a child grants access
// to it through the parent, so every newly added child must already be
// updatable by the caller; removing or reordering children needs only the
// parent permission.
func relPutScoped(putFn func(context.Context, string, []database.RelEntry) error, getFn func(context.Context, string) ([]database.RelEntry, error), allow, allowChild func(authz.Grant, string) bool) http.HandlerFunc {
	next := relPut(putFn, getFn)
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !writable(w, r, func(g authz.Grant) bool { return allow(g, id) }) {
			return
		}
		if grant, ok := grantFromContext(r.Context()); ok && !grant.Bypass {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var entries []database.RelEntry
			if err := json.Unmarshal(body, &entries); err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid JSON: %v", err))
				return
			}
			existing, err := getFn(r.Context(), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "database error")
				return
			}
			linked := make(map[string]bool, len(existing))
			for _, e := range existing {
				linked[e.ID] = true
			}
			for _, e := range entries {
				if !linked[e.ID] && !allowChild(grant, e.ID) {
					writeError(w, http.StatusForbidden, fmt.Sprintf("forbidden: cannot link %q", e.ID))
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	}
}

func orgProductsGetHandler(store database.ConfigStore) http.HandlerFunc {
	return scopedRelGet(store.GetOrgProductLinks, func(g authz.Grant, id string) bool {
		return g.CanReadOrg(id)
	}, func(g authz.Grant, entry database.RelEntry) bool {
		return g.CanReadProduct(entry.ID)
	})
}

func productTeamsGetHandler(store database.ConfigStore) http.HandlerFunc {
	return scopedRelGet(store.GetProductTeamLinks, func(g authz.Grant, id string) bool {
		return g.CanReadProduct(id)
	}, func(g authz.Grant, entry database.RelEntry) bool {
		return g.CanReadTeam(entry.ID)
	})
}

func scopedRelGet(getFn func(context.Context, string) ([]database.RelEntry, error), parent func(authz.Grant, string) bool, child func(authz.Grant, database.RelEntry) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !visible(w, r, func(g authz.Grant) bool { return parent(g, id) }) {
			return
		}
		entries, err := getFn(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if grant, ok := grantFromContext(r.Context()); ok && !grant.Bypass && !grant.Operator {
			filtered := make([]database.RelEntry, 0, len(entries))
			for _, entry := range entries {
				if child(grant, entry) {
					filtered = append(filtered, entry)
				}
			}
			entries = filtered
		}
		if entries == nil {
			entries = []database.RelEntry{}
		}
		writeJSON(w, http.StatusOK, entries)
	}
}

func teamProductLinksHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		links, err := store.ListAllTeamProductLinks(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if grant, ok := grantFromContext(r.Context()); ok && !grant.Bypass && !grant.Operator {
			filtered := make([]database.TeamProductLink, 0, len(links))
			for _, link := range links {
				if grant.CanReadTeam(link.TeamID) && grant.CanReadProduct(link.ProductID) {
					filtered = append(filtered, link)
				}
			}
			links = filtered
		}
		if links == nil {
			links = []database.TeamProductLink{}
		}
		writeJSON(w, http.StatusOK, links)
	}
}

func productWritable(w http.ResponseWriter, r *http.Request, store database.ConfigStore, id, authGroup string) bool {
	grant, ok := grantFromContext(r.Context())
	if !ok || grant.Bypass {
		return true
	}
	if !grant.CanUpdateProduct(id) {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	existing, err := store.GetProductRow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return false
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "not found")
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(existing.AuthGroup), strings.TrimSpace(authGroup)) && !grant.CanChangeProductGroup(id) {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func labelGet(getFn func(context.Context, string) ([]string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !visible(w, r, func(g authz.Grant) bool { return g.CanReadTeam(id) }) {
			return
		}
		labels, err := getFn(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if labels == nil {
			labels = []string{}
		}
		writeJSON(w, http.StatusOK, labels)
	}
}

func labelPut(putFn func(context.Context, string, []string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		labels, err := decodeBody[[]string](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := putFn(r.Context(), id, labels); err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusOK, labels)
	}
}

// --------------- Config handlers ---------------

func isValidEntityType(s string) bool {
	return s == "org" || s == "product" || s == "team"
}

func configGetHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entityType := r.PathValue("type")
		entityID := r.PathValue("id")
		if !isValidEntityType(entityType) {
			writeError(w, http.StatusBadRequest, "type must be 'org', 'product', or 'team'")
			return
		}
		if !visible(w, r, func(g authz.Grant) bool { return canReadOwner(g, entityType, entityID) }) {
			return
		}
		config, err := store.GetEntityConfigKV(r.Context(), entityType, entityID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusOK, redactSecretConfig(config))
	}
}

// redactedSecret replaces stored integration tokens in config responses.
// The frontend only checks that a token is present; devlake_* keys are
// preserved server-side on PUT, so the placeholder is never written back.
const redactedSecret = "********"

func isSecretConfigKey(key string) bool {
	return strings.HasPrefix(key, "devlake_") && strings.HasSuffix(key, "_token")
}

func redactSecretConfig(config map[string]string) map[string]string {
	if config == nil {
		return nil
	}
	out := make(map[string]string, len(config))
	for k, v := range config {
		if v != "" && isSecretConfigKey(k) {
			v = redactedSecret
		}
		out[k] = v
	}
	return out
}

func configPutHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entityType := r.PathValue("type")
		entityID := r.PathValue("id")
		if !isValidEntityType(entityType) {
			writeError(w, http.StatusBadRequest, "type must be 'org', 'product', or 'team'")
			return
		}
		config, err := decodeBody[map[string]string](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		existing, err := store.GetEntityConfigKV(r.Context(), entityType, entityID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		for k, v := range config {
			if v == redactedSecret && isSecretConfigKey(k) {
				delete(config, k)
			}
		}
		for k, v := range existing {
			if strings.HasPrefix(k, "devlake_") {
				config[k] = v
			}
		}
		if err := store.ReplaceEntityConfig(r.Context(), entityType, entityID, config); err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusOK, redactSecretConfig(config))
	}
}

func configDeleteHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entityType := r.PathValue("type")
		entityID := r.PathValue("id")
		if !isValidEntityType(entityType) {
			writeError(w, http.StatusBadRequest, "type must be 'org', 'product', or 'team'")
			return
		}
		if err := store.DeleteEntityConfig(r.Context(), entityType, entityID); err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --------------- Personal token handlers ---------------

var personalTokenPlugins = []string{"github", "gitlab", "jira"}

func personalTokensGetHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ident, ok := identityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		cfg, err := store.GetEntityConfigKV(r.Context(), "user", ident.Email)
		if err != nil {
			cfg = map[string]string{}
		}

		type tokenEntry struct {
			Plugin     string `json:"plugin"`
			Endpoint   string `json:"endpoint"`
			ExpiryDate string `json:"expiry_date"`
			HasToken   bool   `json:"has_token"`
		}
		var tokens []tokenEntry
		for _, p := range personalTokenPlugins {
			tok := cfg["personal_"+p+"_token"]
			if tok == "" {
				continue
			}
			tokens = append(tokens, tokenEntry{
				Plugin:     p,
				Endpoint:   cfg["personal_"+p+"_endpoint"],
				ExpiryDate: cfg["personal_"+p+"_expiry"],
				HasToken:   true,
			})
		}
		if tokens == nil {
			tokens = []tokenEntry{}
		}
		writeJSON(w, http.StatusOK, tokens)
	}
}

func personalTokenPutHandler(store database.ConfigStore, outbound outboundConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ident, ok := identityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		plugin := r.PathValue("plugin")
		validPlugin := false
		for _, p := range personalTokenPlugins {
			if p == plugin {
				validPlugin = true
				break
			}
		}
		if !validPlugin {
			writeError(w, http.StatusBadRequest, "plugin must be github, gitlab, or jira")
			return
		}

		var req struct {
			Endpoint   string `json:"endpoint"`
			Token      string `json:"token"`
			ExpiryDate string `json:"expiry_date"`
			Username   string `json:"username"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.Endpoint == "" || req.Token == "" {
			writeError(w, http.StatusBadRequest, "endpoint and token are required")
			return
		}
		if err := netguard.ValidateURL(req.Endpoint, outbound.AllowPrivate); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		ctx := r.Context()
		prefix := "personal_" + plugin + "_"
		if err := store.SetEntityConfigKey(ctx, "user", ident.Email, prefix+"token", req.Token); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save token")
			return
		}
		_ = store.SetEntityConfigKey(ctx, "user", ident.Email, prefix+"endpoint", req.Endpoint)
		_ = store.SetEntityConfigKey(ctx, "user", ident.Email, prefix+"expiry", req.ExpiryDate)
		if plugin == "jira" {
			_ = store.SetEntityConfigKey(ctx, "user", ident.Email, prefix+"username", req.Username)
		}
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
	}
}

func personalTokenDeleteHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ident, ok := identityFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		plugin := r.PathValue("plugin")
		ctx := r.Context()
		prefix := "personal_" + plugin + "_"
		for _, suffix := range []string{"token", "endpoint", "expiry", "username"} {
			_ = store.DeleteEntityConfigKey(ctx, "user", ident.Email, prefix+suffix)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --------------- Dashboard handlers ---------------

type dataFreshnessResponse struct {
	LastCollectedAt *time.Time `json:"last_collected_at,omitempty"`
}

func dataFreshnessHandler(store database.ConfigStore, devlakeSvc *devlake.Service, devlakeDB *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerType := r.URL.Query().Get("owner_type")
		ownerID := r.URL.Query().Get("owner_id")
		if ownerType == "" || ownerID == "" {
			writeError(w, http.StatusBadRequest, "owner_type and owner_id query parameters are required")
			return
		}
		if !visible(w, r, func(g authz.Grant) bool { return canReadOwner(g, ownerType, ownerID) }) {
			return
		}
		if devlakeSvc == nil || devlakeDB == nil || !devlakeSvc.Enabled() {
			writeJSON(w, http.StatusOK, dataFreshnessResponse{})
			return
		}

		teamIDs, err := freshnessTeamIDs(r.Context(), store, ownerType, ownerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve dashboard teams")
			return
		}
		blueprintIDs := make([]int64, 0)
		seen := make(map[int64]bool)
		for _, teamID := range teamIDs {
			blueprints, listErr := devlakeSvc.Client().ListBlueprints(r.Context(), "team:"+teamID)
			if listErr != nil {
				writeError(w, http.StatusBadGateway, "failed to list DevLake blueprints")
				return
			}
			for _, blueprint := range blueprints {
				if blueprint.ID > 0 && !seen[blueprint.ID] {
					seen[blueprint.ID] = true
					blueprintIDs = append(blueprintIDs, blueprint.ID)
				}
			}
		}
		if len(blueprintIDs) == 0 {
			writeJSON(w, http.StatusOK, dataFreshnessResponse{})
			return
		}

		placeholders := make([]string, len(blueprintIDs))
		args := make([]any, len(blueprintIDs))
		for i, id := range blueprintIDs {
			placeholders[i] = "?"
			args[i] = id
		}
		query := `SELECT MAX(finished_at) FROM _devlake_pipelines
			WHERE blueprint_id IN (` + strings.Join(placeholders, ",") + `)
			AND status IN ('TASK_COMPLETED', 'TASK_PARTIAL')`
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var lastCollected sql.NullTime
		if err := devlakeDB.QueryRowContext(ctx, query, args...).Scan(&lastCollected); err != nil {
			writeError(w, http.StatusBadGateway, "failed to query DevLake pipeline history")
			return
		}
		response := dataFreshnessResponse{}
		if lastCollected.Valid {
			t := lastCollected.Time.UTC()
			response.LastCollectedAt = &t
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func freshnessTeamIDs(ctx context.Context, store database.ConfigStore, ownerType, ownerID string) ([]string, error) {
	switch ownerType {
	case "team":
		return []string{ownerID}, nil
	case "product":
		links, err := store.GetProductTeamLinks(ctx, ownerID)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(links))
		for _, link := range links {
			ids = append(ids, link.ID)
		}
		return ids, nil
	case "org":
		products, err := store.GetOrgProductLinks(ctx, ownerID)
		if err != nil {
			return nil, err
		}
		seen := make(map[string]bool)
		var ids []string
		for _, product := range products {
			teams, err := store.GetProductTeamLinks(ctx, product.ID)
			if err != nil {
				return nil, err
			}
			for _, team := range teams {
				if !seen[team.ID] {
					seen[team.ID] = true
					ids = append(ids, team.ID)
				}
			}
		}
		return ids, nil
	default:
		return nil, fmt.Errorf("unsupported owner type %q", ownerType)
	}
}

func dashboardListHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerType := r.URL.Query().Get("owner_type")
		ownerID := r.URL.Query().Get("owner_id")
		if ownerType == "" || ownerID == "" {
			writeError(w, http.StatusBadRequest, "owner_type and owner_id query parameters are required")
			return
		}
		if !visible(w, r, func(g authz.Grant) bool { return canReadOwner(g, ownerType, ownerID) }) {
			return
		}
		rows, err := store.ListDashboardRows(r.Context(), ownerType, ownerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if rows == nil {
			rows = []database.DashboardRow{}
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

func dashboardGetHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid dashboard id")
			return
		}
		row, err := store.GetDashboardRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if row == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if !visible(w, r, func(g authz.Grant) bool { return canReadOwner(g, row.OwnerType, row.OwnerID) }) {
			return
		}
		writeJSON(w, http.StatusOK, row)
	}
}

// --------------- Source handlers ---------------

type sourceResponse struct {
	database.SourceRow
	JiraConnectionID string `json:"jira_connection_id,omitempty"`
}

func sourceListHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		if !visible(w, r, func(g authz.Grant) bool { return g.CanReadTeam(teamID) }) {
			return
		}
		rows, err := store.ListSourceRows(r.Context(), teamID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		cfg, err := store.GetEntityConfigKV(r.Context(), "team", teamID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		response := make([]sourceResponse, 0, len(rows))
		for _, row := range rows {
			source := sourceResponse{SourceRow: row}
			for _, project := range row.Projects {
				if project.Type == "jira" {
					source.JiraConnectionID = cfg["devlake_jira_connection_id"]
					break
				}
			}
			response = append(response, source)
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func syncStatusHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		if !visible(w, r, func(g authz.Grant) bool { return g.CanReadTeam(teamID) }) {
			return
		}
		cfg, err := store.GetEntityConfigKV(r.Context(), "team", teamID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		resp := map[string]any{
			"last_sync_time":   cfg["last_sync_time"],
			"last_sync_result": nil,
		}
		if raw, ok := cfg["last_sync_result"]; ok && raw != "" {
			var result json.RawMessage
			if json.Unmarshal([]byte(raw), &result) == nil {
				resp["last_sync_result"] = result
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func sourceCreateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		input, err := decodeBody[database.SourceRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.ID == "" || input.Type == "" {
			writeError(w, http.StatusBadRequest, "id and type are required")
			return
		}
		if err := store.CreateSource(r.Context(), teamID, input); err != nil {
			if errors.Is(err, database.ErrDuplicateName) {
				writeError(w, http.StatusConflict, fmt.Sprintf("source %q already exists for team %q", input.ID, teamID))
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusCreated, input)
	}
}

func sourceGetHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		sourceID := r.PathValue("sourceId")
		if !visible(w, r, func(g authz.Grant) bool { return g.CanReadTeam(teamID) }) {
			return
		}
		row, err := store.GetSourceRow(r.Context(), teamID, sourceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if row == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, row)
	}
}

func sourceUpdateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		sourceID := r.PathValue("sourceId")
		input, err := decodeBody[database.SourceRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := store.UpdateSource(r.Context(), teamID, sourceID, input); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.ID = sourceID
		writeJSON(w, http.StatusOK, input)
	}
}

func sourceDeleteHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		sourceID := r.PathValue("sourceId")
		if err := store.DeleteSource(r.Context(), teamID, sourceID); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --------------- Override handlers ---------------

func overridesGetHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !visible(w, r, func(g authz.Grant) bool { return g.CanAggregateProduct(id) }) {
			return
		}
		rows, err := store.GetProductOverrideRows(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if rows == nil {
			rows = []database.OverrideRepoRow{}
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

func overridesPutHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		overrides, err := decodeBody[[]database.OverrideRepoRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := store.ReplaceProductOverrides(r.Context(), id, overrides); err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusOK, overrides)
	}
}

func teamDeleteHandler(store database.ConfigStore, devlakeSvc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		if devlakeSvc != nil && devlakeSvc.Enabled() {
			cleanup, err := devlakeSvc.CleanupTeam(r.Context(), id)
			if err != nil {
				slog.Error("DevLake cleanup failed during team delete", "team", sanitizeLog(id), "err", err) // #nosec G706
			} else {
				slog.Info("DevLake cleanup completed",
					"team", sanitizeLog(id), // #nosec G706
					"deleted", cleanup.DeletedBlueprints,
					"migrated", cleanup.Migrated,
					"errors", cleanup.Errors) // #nosec G706
			}
		}

		if err := store.DeleteTeam(r.Context(), id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --------------- Onboard handler ---------------

type onboardRequest struct {
	Team             database.TeamRow                   `json:"team"`
	ProductID        string                             `json:"product_id,omitempty"`
	Connections      []devlake.ConnectionEntry          `json:"connections"`
	Sources          []database.SourceRow               `json:"sources"`
	Dashboards       []database.DashboardRow            `json:"dashboards"`
	Config           map[string]string                  `json:"config"`
	JiraScopeConfigs map[string]devlake.JiraScopeConfig `json:"jira_scope_configs,omitempty"`
}

func onboardHandler(store database.ConfigStore, devlakeSvc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req onboardRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if msg := validateOnboardRequest(req); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		if ident, ok := identityFromContext(r.Context()); ok && !ident.IsAdmin && ident.Email != "" {
			req.Team.ContactEmail = ident.Email
		}
		if grant, ok := grantFromContext(r.Context()); ok {
			if ident, idOK := identityFromContext(r.Context()); idOK && !ident.IsAdmin && !grant.CanOnboard(req.ProductID) {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
		}
		if req.Team.ContactEmail != "" && !isValidEmail(req.Team.ContactEmail) {
			writeError(w, http.StatusBadRequest, "contact_email must be a valid email address")
			return
		}
		if err := validateOnboardJiraProjects(r.Context(), devlakeSvc, req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		user := "unknown"
		if ident, ok := identityFromContext(r.Context()); ok {
			user = ident.Username
		}

		slog.Info("onboard: starting",
			"team", req.Team.ID,
			"user", user,
			"connections", len(req.Connections),
			"sources", len(req.Sources),
			"dashboards", len(req.Dashboards),
			"config_keys", len(req.Config))

		ctx := r.Context()

		if err := store.CreateTeam(ctx, req.Team); err != nil {
			slog.Error("onboard: failed to create team", "team", req.Team.ID, "user", user, "err", err)
			if errors.Is(err, database.ErrDuplicateName) {
				writeError(w, http.StatusConflict, fmt.Sprintf("team id %q already exists", req.Team.ID))
				return
			}
			writeDetailedError(w, r, "create team failed", err)
			return
		}
		slog.Info("onboard: team created", "team", req.Team.ID)

		if req.ProductID != "" {
			existing, err := store.GetProductTeamLinks(ctx, req.ProductID)
			if err != nil {
				slog.Error("onboard: failed to get product team links", "product", req.ProductID, "err", err)
			} else {
				existing = append(existing, database.RelEntry{ID: req.Team.ID, SortOrder: len(existing)})
				if err := store.ReplaceProductTeamLinks(ctx, req.ProductID, existing); err != nil {
					slog.Error("onboard: failed to link team to product", "product", req.ProductID, "team", req.Team.ID, "err", err)
				} else {
					slog.Info("onboard: linked team to product", "product", req.ProductID, "team", req.Team.ID)
				}
			}
		}

		if err := onboardCreateConnections(ctx, devlakeSvc, req.Team.ID, req.Connections); err != nil {
			rollbackOnboardTeam(ctx, store, devlakeSvc, req.Team.ID, req.ProductID)
			writeDetailedError(w, r, "create connections failed", err)
			return
		}

		onboardSaveConfigSourcesDashboards(ctx, store, &req)

		slog.Info("onboard: complete", "team", req.Team.ID, "user", user)

		resp := map[string]any{"team_id": req.Team.ID, "status": "created"}
		if connIDs := onboardConnectionIDs(ctx, devlakeSvc, req.Team.ID, req.Connections); len(connIDs) > 0 {
			resp["connection_ids"] = connIDs
		}

		writeJSON(w, http.StatusCreated, resp)

		if devlakeSvc != nil && devlakeSvc.Enabled() {
			bgCtx := context.WithoutCancel(ctx)
			go func() {
				onboardSyncAndScopeConfigs(bgCtx, devlakeSvc, req)
			}()
		}
	}
}

func validateOnboardJiraProjects(ctx context.Context, devlakeSvc *devlake.Service, req onboardRequest) error {
	if devlakeSvc == nil || !devlakeSvc.Enabled() {
		for _, src := range req.Sources {
			for _, project := range src.Projects {
				if project.Type == "jira" && strings.TrimSpace(project.JiraProject) == "" {
					return fmt.Errorf("jira project key is required when the Jira board lookup is unavailable")
				}
			}
		}
		return nil
	}
	var jiraConn *devlake.ConnectionEntry
	for i := range req.Connections {
		if req.Connections[i].Plugin == "jira" {
			jiraConn = &req.Connections[i]
			break
		}
	}
	if jiraConn == nil {
		return nil
	}
	for si := range req.Sources {
		for pi := range req.Sources[si].Projects {
			project := &req.Sources[si].Projects[pi]
			if project.Type != "jira" {
				continue
			}
			key := strings.TrimSpace(project.JiraProject)
			boardID := strings.TrimSpace(project.BoardID)
			if key == "" && boardID == "" {
				return fmt.Errorf("jira project key or board ID is required")
			}
			if key == "" && boardID != "" {
				meta, err := devlakeSvc.ProxyJiraBoardMeta(ctx, *jiraConn, boardID)
				if err != nil {
					return fmt.Errorf("jira board %s could not be validated: %w", boardID, err)
				}
				key = meta.ProjectKey
				project.JiraProject = key
			}
			verified, err := devlakeSvc.ProxyJiraProject(ctx, *jiraConn, key)
			if err != nil {
				return fmt.Errorf("jira project %s could not be validated: %w", key, err)
			}
			if !strings.EqualFold(verified.Key, key) {
				return fmt.Errorf("jira project key %s did not resolve to %s", key, verified.Key)
			}
		}
	}
	return nil
}

func validateOnboardRequest(req onboardRequest) string {
	if req.Team.ID == "" || req.Team.Name == "" {
		return "team id and name are required"
	}
	if strings.TrimSpace(req.Team.AuthGroup) == "" {
		return errAuthGroupRequired
	}
	connPlugins := make(map[string]bool)
	var codecovConn *devlake.ConnectionEntry
	for _, c := range req.Connections {
		if c.Plugin == devlake.PluginTestRegistry {
			if msg := validateConnectionEntryFields(c); msg != "" {
				return msg
			}
		} else if c.Plugin == "" || c.Token == "" || c.Endpoint == "" {
			return "each connection requires plugin, token, and endpoint"
		}
		if c.Plugin == "codecov" && strings.TrimSpace(c.Organization) == "" {
			return "organization is required for codecov connections"
		}
		if c.ExpiryDate == "" || (c.ExpiryDate != "never" && !isValidDate(c.ExpiryDate)) {
			return "valid expiry_date is required for " + c.Plugin
		}
		if c.Plugin == "jira" && c.Username == "" {
			return "username is required for jira connections"
		}
		connPlugins[c.Plugin] = true
		if c.Plugin == "codecov" {
			codecovConn = &c
		}
	}
	for _, src := range req.Sources {
		if msg := validateOnboardSource(src, connPlugins, codecovConn); msg != "" {
			return msg
		}
	}
	return ""
}

func validateOnboardSource(src database.SourceRow, connPlugins map[string]bool, codecovConn *devlake.ConnectionEntry) string {
	switch src.Type {
	case "gitrepos":
		for _, repo := range src.Repos {
			plugin := strings.ToLower(repo.Type)
			if plugin != "" && !connPlugins[plugin] &&
				(codecovConn == nil || !devlake.CodecovMatchesRepo(codecovConn.Service, codecovConn.Organization, repo)) {
				return fmt.Sprintf("source references %s repos but no %s connection was provided", plugin, plugin)
			}
		}
	case "workitems":
		for _, proj := range src.Projects {
			if proj.Type == "jira" && !connPlugins["jira"] {
				return "source references jira projects but no jira connection was provided"
			}
		}
	case devlake.SourceTypeTestSuites:
		for _, proj := range src.Projects {
			if proj.Type != devlake.PluginTestRegistry || strings.TrimSpace(proj.ProjectName) == "" {
				return "testsuites sources accept only testregistry scopes with a project_name"
			}
		}
		if len(src.Projects) > 0 && !connPlugins[devlake.PluginTestRegistry] {
			return "source references testregistry scopes but no testregistry connection was provided"
		}
	}
	return ""
}

func onboardCreateConnections(ctx context.Context, devlakeSvc *devlake.Service, teamID string, conns []devlake.ConnectionEntry) error {
	if devlakeSvc == nil || !devlakeSvc.Enabled() {
		return nil
	}
	for _, conn := range conns {
		connID, err := devlakeSvc.CreateTeamConnection(ctx, teamID, conn)
		if err != nil {
			slog.Error("onboard: failed to create connection", "team", teamID, "plugin", conn.Plugin, "err", err)
			return fmt.Errorf("create %s connection: %w", conn.Plugin, err)
		}
		slog.Info("onboard: connection created", "team", teamID, "plugin", conn.Plugin, "connection_id", connID)
	}
	return nil
}

// rollbackOnboardTeam removes the local records created before a DevLake
// connection failure. Without this rollback, a failed onboarding attempt
// leaves a team visible in My Teams even though creation reported an error.
func rollbackOnboardTeam(ctx context.Context, store database.ConfigStore, devlakeSvc *devlake.Service, teamID, productID string) {
	if devlakeSvc != nil && devlakeSvc.Enabled() {
		if _, err := devlakeSvc.CleanupTeam(ctx, teamID); err != nil {
			slog.Warn("onboard: DevLake cleanup after connection failure was incomplete",
				"team", teamID, "err", err)
		}
	}

	if productID != "" {
		if existing, err := store.GetProductTeamLinks(ctx, productID); err != nil {
			slog.Warn("onboard: failed to read product links during rollback",
				"product", productID, "team", teamID, "err", err)
		} else {
			remaining := make([]database.RelEntry, 0, len(existing))
			for _, entry := range existing {
				if entry.ID != teamID {
					remaining = append(remaining, entry)
				}
			}
			if err := store.ReplaceProductTeamLinks(ctx, productID, remaining); err != nil {
				slog.Warn("onboard: failed to remove product link during rollback",
					"product", productID, "team", teamID, "err", err)
			}
		}
	}

	if err := store.DeleteEntityConfig(ctx, "team", teamID); err != nil {
		slog.Warn("onboard: failed to remove team config during rollback",
			"team", teamID, "err", err)
	}
	if err := store.DeleteTeam(ctx, teamID); err != nil {
		slog.Warn("onboard: failed to remove team during rollback",
			"team", teamID, "err", err)
	} else {
		slog.Info("onboard: rolled back team after connection failure", "team", teamID)
	}
}

func onboardSaveConfigSourcesDashboards(ctx context.Context, store database.ConfigStore, req *onboardRequest) {
	teamID := req.Team.ID
	if len(req.Config) > 0 {
		if err := store.ReplaceEntityConfig(ctx, "team", teamID, req.Config); err != nil {
			slog.Error("onboard: failed to set config", "team", teamID, "err", err)
		} else {
			slog.Info("onboard: config saved", "team", teamID, "keys", len(req.Config))
		}
	}
	for _, src := range req.Sources {
		if err := store.CreateSource(ctx, teamID, src); err != nil {
			slog.Error("onboard: failed to create source", "team", teamID, "source", src.ID, "err", err)
		} else {
			slog.Info("onboard: source created", "team", teamID, "source", src.ID, "type", src.Type)
		}
	}
	for i := range req.Dashboards {
		req.Dashboards[i].OwnerType = "team"
		req.Dashboards[i].OwnerID = teamID
		if _, err := store.CreateDashboard(ctx, req.Dashboards[i]); err != nil {
			slog.Error("onboard: failed to create dashboard", "team", teamID, "dashboard", req.Dashboards[i].DashboardID, "err", err)
		} else {
			slog.Info("onboard: dashboard created", "team", teamID, "dashboard", req.Dashboards[i].DashboardID)
		}
	}
}

func onboardSyncAndScopeConfigs(ctx context.Context, devlakeSvc *devlake.Service, req onboardRequest) *devlake.SyncResult {
	if devlakeSvc == nil || !devlakeSvc.Enabled() {
		return nil
	}
	teamID := req.Team.ID
	var syncResult *devlake.SyncResult
	if len(req.Sources) > 0 {
		slog.Info("onboard: starting DevLake sync", "team", teamID)
		var err error
		syncResult, err = devlakeSvc.SyncTeam(ctx, teamID)
		if err != nil {
			slog.Error("onboard: sync failed", "team", teamID, "err", err)
		} else {
			slog.Info("onboard: sync complete", "team", teamID,
				"created", syncResult.Created,
				"updated", syncResult.Updated,
				"deleted", syncResult.Deleted,
				"skipped", syncResult.Skipped,
				"errors", syncResult.Errors)
		}
	}
	if len(req.JiraScopeConfigs) > 0 {
		slog.Info("onboard: applying Jira scope configs", "team", teamID, "configs", len(req.JiraScopeConfigs))
		if err := devlakeSvc.ApplyJiraScopeConfigs(ctx, teamID, req.JiraScopeConfigs); err != nil {
			slog.Error("onboard: scope config failed", "team", teamID, "err", err)
		}
	}
	return syncResult
}

func onboardConnectionIDs(ctx context.Context, devlakeSvc *devlake.Service, teamID string, conns []devlake.ConnectionEntry) map[string]int {
	if devlakeSvc == nil || !devlakeSvc.Enabled() {
		return nil
	}
	connIDs := make(map[string]int)
	for _, conn := range conns {
		if cid, err := devlakeSvc.TeamConnectionID(ctx, teamID, conn.Plugin); err == nil && cid != 0 {
			connIDs[conn.Plugin] = cid
		}
	}
	return connIDs
}

// --------------- Offboard handler ---------------

type offboardRequest struct {
	TeamID string `json:"team_id"`
}

func offboardHandler(store database.ConfigStore, devlakeSvc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req offboardRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.TeamID == "" {
			writeError(w, http.StatusBadRequest, "team_id is required")
			return
		}

		ident, ok := identityFromContext(r.Context())
		if !ok || !ident.Known() {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		team, err := store.GetTeamRow(r.Context(), req.TeamID)
		if err != nil {
			slog.Error("offboard: failed to look up team", "team", req.TeamID, "err", err)
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if team == nil {
			writeError(w, http.StatusNotFound, "team not found")
			return
		}

		if !ident.IsAdmin {
			writeError(w, http.StatusForbidden, "admin access required")
			return
		}

		user := ident.Username
		slog.Info("offboard: starting",
			"team", req.TeamID, "team_name", team.Name,
			"user", user, "user_email", ident.Email)

		ctx := r.Context()
		resp := map[string]any{"team_id": req.TeamID, "status": "deleted"}

		// Phase 1: DevLake cleanup (blueprints + migration).
		// This runs first so that a DevLake failure aborts before we
		// delete local data, keeping the two stores consistent enough
		// for a manual retry.
		if devlakeSvc != nil && devlakeSvc.Enabled() {
			slog.Info("offboard: phase 1 — DevLake cleanup", "team", req.TeamID)
			cleanup, err := devlakeSvc.CleanupTeam(ctx, req.TeamID)
			if err != nil {
				slog.Error("offboard: DevLake cleanup failed — aborting delete",
					"team", req.TeamID, "err", err)
				writeError(w, http.StatusBadGateway,
					fmt.Sprintf("DevLake cleanup failed, team was NOT deleted: %v", err))
				return
			}
			slog.Info("offboard: DevLake cleanup completed",
				"team", req.TeamID,
				"deleted_blueprints", cleanup.DeletedBlueprints,
				"migrated", cleanup.Migrated,
				"cleanup_errors", cleanup.Errors)
			resp["cleanup"] = cleanup
		}

		// Phase 2: delete local dashboard DB resources.
		// Each step is logged individually so partial failures are
		// traceable for manual repair.
		slog.Info("offboard: phase 2 — deleting local resources", "team", req.TeamID)

		sources, err := store.ListSourceRows(ctx, req.TeamID)
		if err != nil {
			slog.Error("offboard: failed to list sources", "team", req.TeamID, "err", err)
		}
		for _, src := range sources {
			if err := store.DeleteSource(ctx, req.TeamID, src.ID); err != nil {
				slog.Error("offboard: failed to delete source",
					"team", req.TeamID, "source", src.ID, "type", src.Type, "err", err)
			} else {
				slog.Info("offboard: deleted source",
					"team", req.TeamID, "source", src.ID, "type", src.Type)
			}
		}

		dashboards, err := store.ListDashboardRows(ctx, "team", req.TeamID)
		if err != nil {
			slog.Error("offboard: failed to list dashboards", "team", req.TeamID, "err", err)
		}
		for _, d := range dashboards {
			if err := store.DeleteDashboard(ctx, d.AutoID); err != nil {
				slog.Error("offboard: failed to delete dashboard",
					"team", req.TeamID, "dashboard_id", d.DashboardID,
					"auto_id", d.AutoID, "err", err)
			} else {
				slog.Info("offboard: deleted dashboard",
					"team", req.TeamID, "dashboard_id", d.DashboardID, "auto_id", d.AutoID)
			}
		}

		if err := store.DeleteEntityConfig(ctx, "team", req.TeamID); err != nil {
			slog.Error("offboard: failed to delete entity config", "team", req.TeamID, "err", err)
		} else {
			slog.Info("offboard: deleted entity config", "team", req.TeamID)
		}

		if err := store.DeleteTeam(ctx, req.TeamID); err != nil {
			slog.Error("offboard: failed to delete team row",
				"team", req.TeamID, "team_name", team.Name, "err", err)
			writeError(w, http.StatusInternalServerError,
				fmt.Sprintf("delete team row failed (DevLake already cleaned up): %v", err))
			return
		}
		slog.Info("offboard: deleted team row", "team", req.TeamID, "team_name", team.Name)

		slog.Info("offboard: complete",
			"team", req.TeamID, "team_name", team.Name,
			"user", user,
			"sources_deleted", len(sources),
			"dashboards_deleted", len(dashboards))
		writeJSON(w, http.StatusOK, resp)
	}
}

// --------------- Source offboard handler ---------------

type onboardSourceRequest struct {
	TeamID   string `json:"team_id"`
	SourceID string `json:"source_id"`
	Items    []struct {
		Type        string `json:"type"`
		Owner       string `json:"owner,omitempty"`
		Name        string `json:"name,omitempty"`
		ProjectName string `json:"project_name,omitempty"`
		JiraProject string `json:"jira_project,omitempty"`
		BoardID     string `json:"board_id,omitempty"`
	} `json:"items"`
	JiraScopeConfigs map[string]devlake.JiraScopeConfig `json:"jira_scope_configs,omitempty"`
}

type offboardSourceRequest struct {
	TeamID   string `json:"team_id"`
	SourceID string `json:"source_id"`
	ItemID   string `json:"item_id"`
}

func onboardSourceHandler(store database.ConfigStore, devlakeSvc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req onboardSourceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.TeamID == "" || req.SourceID == "" || len(req.Items) == 0 {
			writeError(w, http.StatusBadRequest, "team_id, source_id, and items are required")
			return
		}

		ident, ok := identityFromContext(r.Context())
		if !ok || !ident.Known() {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		team, err := store.GetTeamRow(r.Context(), req.TeamID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if team == nil {
			writeError(w, http.StatusNotFound, "team not found")
			return
		}

		if !ident.IsAdmin {
			if err := requireTeamOwnerOrAdmin(r, store, req.TeamID); err != nil {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
		}

		ctx := r.Context()

		if msg := validateTestRegistryItems(ctx, &req, devlakeSvc); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}

		src, err := store.GetSourceRow(ctx, req.TeamID, req.SourceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if src == nil {
			newSrc := database.SourceRow{
				ID:   req.SourceID,
				Type: req.SourceID,
			}
			if err := store.CreateSource(ctx, req.TeamID, newSrc); err != nil {
				slog.Error("onboard-source: failed to create source",
					"team", req.TeamID, "source", req.SourceID, "err", err)
				writeError(w, http.StatusInternalServerError, "failed to create source")
				return
			}
			slog.Info("onboard-source: auto-created source",
				"team", req.TeamID, "source", req.SourceID)
			src = &newSrc
		}

		updated := *src
		validationWarnings, validErr := validateAndAppendItems(ctx, req, src, &updated, devlakeSvc)
		if validErr != "" {
			writeError(w, http.StatusBadRequest, validErr)
			return
		}

		if err := store.UpdateSource(ctx, req.TeamID, req.SourceID, updated); err != nil {
			slog.Error("onboard-source: failed to update source",
				"team", req.TeamID, "source", req.SourceID, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to update source")
			return
		}

		slog.Info("onboard-source: added items",
			"team", req.TeamID, "source", req.SourceID,
			"count", len(req.Items), "user", ident.Username)

		resp := map[string]any{
			"team_id":   req.TeamID,
			"source_id": req.SourceID,
			"added":     len(req.Items),
		}
		if len(validationWarnings) > 0 {
			resp["validation_warnings"] = validationWarnings
		}

		writeJSON(w, http.StatusOK, resp)

		if devlakeSvc != nil && devlakeSvc.Enabled() {
			hasJiraItems := false
			for _, item := range req.Items {
				if item.Type == "jira" {
					hasJiraItems = true
					break
				}
			}
			bgCtx := context.WithoutCancel(ctx)
			go func() {
				if hasJiraItems {
					if err := devlakeSvc.EnsureJiraConnection(bgCtx, req.TeamID); err != nil {
						slog.Error("onboard-source: failed to ensure jira connection",
							"team", req.TeamID, "err", err)
					}
				}
				syncResult, err := devlakeSvc.SyncTeam(bgCtx, req.TeamID)
				if err != nil {
					slog.Error("onboard-source: DevLake sync failed",
						"team", req.TeamID, "err", err)
				} else {
					slog.Info("onboard-source: DevLake sync complete",
						"team", req.TeamID,
						"created", len(syncResult.Created),
						"updated", len(syncResult.Updated))
				}
				if len(req.JiraScopeConfigs) > 0 {
					if err := devlakeSvc.ApplyJiraScopeConfigs(bgCtx, req.TeamID, req.JiraScopeConfigs); err != nil {
						slog.Error("onboard-source: scope config failed",
							"team", req.TeamID, "err", err)
					}
				}
			}()
		}
	}
}

func validateAndAppendItems(ctx context.Context, req onboardSourceRequest, src *database.SourceRow, updated *database.SourceRow, devlakeSvc *devlake.Service) (warnings []string, validationErr string) {
	slog.Info("validateAndAppendItems: starting",
		"team", req.TeamID, "source", req.SourceID, "sourceType", src.Type, "itemCount", len(req.Items))
	for i, item := range req.Items {
		slog.Info("validateAndAppendItems: processing item",
			"index", i, "type", item.Type, "owner", item.Owner, "name", item.Name,
			"projectName", item.ProjectName, "jiraProject", item.JiraProject,
			"boardID", item.BoardID, "sourceType", src.Type)
		if devlakeSvc != nil && devlakeSvc.Enabled() && src.Type == "gitrepos" {
			owner, name := item.Owner, item.Name
			slog.Info("validateAndAppendItems: validation params",
				"team", req.TeamID, "plugin", item.Type, "owner", owner, "name", name)
			if owner != "" {
				lookup := devlakeSvc.LookupRepoForTeam(ctx, req.TeamID, item.Type, owner, name)
				slog.Info("validateAndAppendItems: lookup result",
					"team", req.TeamID, "plugin", item.Type,
					"valid", lookup.Valid, "error", lookup.Error, "warning", lookup.Warning,
					"canonicalName", lookup.CanonicalName)
				if !lookup.Valid && lookup.Error != "" {
					slog.Warn("onboard-source: validation failed",
						"team", req.TeamID, "plugin", item.Type, "owner", owner, "name", name, "err", lookup.Error)
					return nil, lookup.Error
				}
				if lookup.Warning != "" {
					warnings = append(warnings, fmt.Sprintf("%s/%s: %s", owner, name, lookup.Warning))
				}
				if lookup.CanonicalName != "" {
					parts := strings.SplitN(lookup.CanonicalName, "/", 2)
					if len(parts) == 2 {
						item.Owner = parts[0]
						item.Name = parts[1]
					}
				}
			} else {
				slog.Info("validateAndAppendItems: owner is empty, skipping validation lookup",
					"team", req.TeamID, "plugin", item.Type, "name", name)
			}
		}

		if src.Type != "gitrepos" && item.Type == "jira" {
			item.BoardID = strings.TrimSpace(item.BoardID)
			item.JiraProject = strings.TrimSpace(item.JiraProject)
			if item.BoardID != "" && item.JiraProject == "" {
				if devlakeSvc == nil || !devlakeSvc.Enabled() {
					return nil, "Jira project key is missing for this board. Use Browse or configure the team's Jira connection so the key can be resolved."
				}

				meta, err := devlakeSvc.TeamJiraBoardMeta(ctx, req.TeamID, item.BoardID)
				if err != nil {
					slog.Warn("validateAndAppendItems: failed to resolve Jira project key",
						"team", req.TeamID, "boardID", item.BoardID, "err", err)
					return nil, fmt.Sprintf("could not determine the Jira project key for board %s: %v", item.BoardID, err)
				}
				if meta == nil {
					return nil, fmt.Sprintf("could not determine the Jira project key for board %s", item.BoardID)
				}
				item.JiraProject = strings.TrimSpace(meta.ProjectKey)
				if item.JiraProject == "" {
					return nil, fmt.Sprintf("Jira board %s has no single project key; provide a board with a project key", item.BoardID)
				}
				slog.Info("validateAndAppendItems: resolved Jira project key from board",
					"team", req.TeamID, "boardID", item.BoardID, "jiraProject", item.JiraProject)
			}
		}

		id := fmt.Sprintf("%s-%d-%s", item.Type, time.Now().UnixMilli(), randomHex(4))
		if src.Type == devlake.SourceTypeTestSuites {
			name := strings.TrimSpace(item.ProjectName)
			if item.Type != devlake.PluginTestRegistry || name == "" {
				return nil, "testsuites items require type \"testregistry\" and a project_name"
			}
			if hasTestRegistryScope(updated.Projects, name) {
				warnings = append(warnings, fmt.Sprintf("%s is already configured for this team", name))
				continue
			}
			updated.Projects = append(updated.Projects, database.WorkitemProjRow{
				ID:          id,
				Type:        devlake.PluginTestRegistry,
				ProjectName: name,
			})
			continue
		}
		if src.Type == "gitrepos" {
			if item.Owner == "" || item.Name == "" {
				slog.Warn("validateAndAppendItems: skipping repo with empty owner or name",
					"owner", item.Owner, "name", item.Name)
				continue
			}
			updated.Repos = append(updated.Repos, database.RepoRow{
				ID:    id,
				Type:  item.Type,
				Owner: item.Owner,
				Name:  item.Name,
			})
			slog.Info("validateAndAppendItems: added repo",
				"id", id, "type", item.Type, "owner", item.Owner, "name", item.Name)
		} else {
			if item.BoardID == "" {
				slog.Warn("validateAndAppendItems: skipping jira project with empty board_id",
					"type", item.Type, "jiraProject", item.JiraProject)
				continue
			}
			updated.Projects = append(updated.Projects, database.WorkitemProjRow{
				ID:          id,
				Type:        item.Type,
				ProjectName: item.ProjectName,
				JiraProject: item.JiraProject,
				BoardID:     item.BoardID,
			})
			slog.Info("validateAndAppendItems: added workitem project",
				"id", id, "type", item.Type, "projectName", item.ProjectName,
				"jiraProject", item.JiraProject, "boardID", item.BoardID)
		}
	}
	return warnings, ""
}

func hasTestRegistryScope(projects []database.WorkitemProjRow, fullName string) bool {
	for _, p := range projects {
		if p.Type == devlake.PluginTestRegistry && strings.EqualFold(p.ProjectName, fullName) {
			return true
		}
	}
	return false
}

// validateTestRegistryItems keeps testregistry items in the testsuites
// source and resolves their canonical scope names against the team's
// connection before they are stored.
func validateTestRegistryItems(ctx context.Context, req *onboardSourceRequest, devlakeSvc *devlake.Service) string {
	if req.SourceID != devlake.SourceTypeTestSuites {
		for _, item := range req.Items {
			if item.Type == devlake.PluginTestRegistry {
				return "testregistry items must be added to the testsuites source"
			}
		}
		return ""
	}
	if devlakeSvc == nil || !devlakeSvc.Enabled() {
		return ""
	}
	names := make([]string, len(req.Items))
	for i, item := range req.Items {
		names[i] = item.ProjectName
	}
	canonical, err := devlakeSvc.ValidateTestRegistryScopes(ctx, req.TeamID, names)
	if err != nil {
		return err.Error()
	}
	for i := range req.Items {
		req.Items[i].ProjectName = canonical[i]
	}
	return ""
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func validateSourceHandler(store database.ConfigStore, devlakeSvc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		if teamID == "" {
			writeError(w, http.StatusBadRequest, "team ID is required")
			return
		}
		if err := requireTeamOwnerOrAdmin(r, store, teamID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}

		var req struct {
			Plugin string `json:"plugin"`
			Owner  string `json:"owner"`
			Name   string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.Plugin == "" || req.Owner == "" {
			writeError(w, http.StatusBadRequest, "plugin and owner are required")
			return
		}
		if req.Plugin != "jira" && req.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required for git repos")
			return
		}

		sources, err := store.ListSourceRows(r.Context(), teamID)
		if err == nil {
			fullName := req.Owner + "/" + req.Name
			if req.Plugin == "jira" {
				fullName = req.Owner
			}
			for _, src := range sources {
				for _, repo := range src.Repos {
					existing := repo.Owner + "/" + repo.Name
					if strings.EqualFold(existing, fullName) && strings.EqualFold(repo.Type, req.Plugin) {
						writeJSON(w, http.StatusOK, map[string]any{
							"valid": false,
							"error": fmt.Sprintf("%s is already configured for this team", fullName),
						})
						return
					}
				}
				for _, proj := range src.Projects {
					if strings.EqualFold(proj.JiraProject, req.Owner) {
						writeJSON(w, http.StatusOK, map[string]any{
							"valid": false,
							"error": fmt.Sprintf("%s is already configured for this team", req.Owner),
						})
						return
					}
				}
			}
		}

		if devlakeSvc == nil || !devlakeSvc.Enabled() {
			writeJSON(w, http.StatusOK, map[string]any{"valid": true})
			return
		}

		result := devlakeSvc.LookupRepoForTeam(r.Context(), teamID, req.Plugin, req.Owner, req.Name)
		resp := map[string]any{"valid": result.Valid}
		if result.CanonicalName != "" {
			resp["canonical_name"] = result.CanonicalName
		}
		if result.Warning != "" {
			resp["warning"] = result.Warning
		}
		if result.Error != "" {
			resp["error"] = result.Error
		}
		if result.Archived {
			resp["archived"] = true
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func offboardSourceHandler(store database.ConfigStore, devlakeSvc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req offboardSourceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.TeamID == "" || req.SourceID == "" || req.ItemID == "" {
			writeError(w, http.StatusBadRequest, "team_id, source_id, and item_id are required")
			return
		}

		ident, ok := identityFromContext(r.Context())
		if !ok || !ident.Known() {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}

		team, err := store.GetTeamRow(r.Context(), req.TeamID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if team == nil {
			writeError(w, http.StatusNotFound, "team not found")
			return
		}

		if !ident.IsAdmin {
			if err := requireTeamOwnerOrAdmin(r, store, req.TeamID); err != nil {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
		}

		ctx := r.Context()

		if devlakeSvc != nil && devlakeSvc.Enabled() {
			running, err := devlakeSvc.HasRunningPipelines(ctx)
			if err != nil {
				slog.Error("offboard-source: failed to check running pipelines",
					"team", req.TeamID, "err", err)
			} else if running {
				writeError(w, http.StatusConflict,
					"Cannot remove source while a data collection pipeline is running. "+
						"Please wait for the current collection to finish and try again.")
				return
			}
		}

		empty, statusCode, errMsg := offboardRemoveItem(ctx, store, req, ident.Username)
		if errMsg != "" {
			writeError(w, statusCode, errMsg)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"team_id":        req.TeamID,
			"source_id":      req.SourceID,
			"item_id":        req.ItemID,
			"source_deleted": empty,
		})

		if devlakeSvc != nil && devlakeSvc.Enabled() {
			bgCtx := context.WithoutCancel(ctx)
			go func() {
				syncResult, err := devlakeSvc.SyncTeam(bgCtx, req.TeamID)
				if err != nil {
					slog.Error("offboard-source: DevLake sync failed",
						"team", req.TeamID, "err", err)
				} else {
					slog.Info("offboard-source: DevLake sync complete",
						"team", req.TeamID,
						"created", len(syncResult.Created),
						"updated", len(syncResult.Updated),
						"deleted", len(syncResult.Deleted))
				}

				deletedScopes, scopeErrors := devlakeSvc.CleanupOrphanedScopes(bgCtx, req.TeamID)
				if len(deletedScopes) > 0 {
					slog.Info("offboard-source: cleaned up orphaned scopes",
						"team", req.TeamID, "deleted", deletedScopes)
				}
				if len(scopeErrors) > 0 {
					slog.Error("offboard-source: scope cleanup errors",
						"team", req.TeamID, "errors", scopeErrors)
				}
			}()
		}
	}
}

func offboardRemoveItem(ctx context.Context, store database.ConfigStore, req offboardSourceRequest, username string) (empty bool, status int, errMsg string) {
	src, err := store.GetSourceRow(ctx, req.TeamID, req.SourceID)
	if err != nil {
		return false, http.StatusInternalServerError, "database error"
	}
	if src == nil {
		return false, http.StatusNotFound, "source not found"
	}

	var found bool
	updated := *src
	if src.Type == "gitrepos" {
		var kept []database.RepoRow
		for _, repo := range src.Repos {
			if repo.ID == req.ItemID {
				found = true
				continue
			}
			kept = append(kept, repo)
		}
		updated.Repos = kept
	} else {
		var kept []database.WorkitemProjRow
		for _, proj := range src.Projects {
			if proj.ID == req.ItemID {
				found = true
				continue
			}
			kept = append(kept, proj)
		}
		updated.Projects = kept
	}

	if !found {
		return false, http.StatusNotFound, "item not found in source"
	}

	slog.Info("offboard-source: removing item",
		"team", req.TeamID, "source", req.SourceID,
		"item", req.ItemID, "user", username)

	empty = len(updated.Repos) == 0 && len(updated.Projects) == 0
	if empty {
		if err := store.DeleteSource(ctx, req.TeamID, req.SourceID); err != nil {
			slog.Error("offboard-source: failed to delete empty source",
				"team", req.TeamID, "source", req.SourceID, "err", err)
			return false, http.StatusInternalServerError, "failed to delete source"
		}
		slog.Info("offboard-source: deleted empty source",
			"team", req.TeamID, "source", req.SourceID)
	} else {
		if err := store.UpdateSource(ctx, req.TeamID, req.SourceID, updated); err != nil {
			slog.Error("offboard-source: failed to update source",
				"team", req.TeamID, "source", req.SourceID, "err", err)
			return false, http.StatusInternalServerError, "failed to update source"
		}
		slog.Info("offboard-source: updated source",
			"team", req.TeamID, "source", req.SourceID,
			"remaining_repos", len(updated.Repos),
			"remaining_projects", len(updated.Projects))
	}
	return empty, 0, ""
}

// --------------- DevLake handlers ---------------

func devlakeSyncHandler(svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		if teamID == "" {
			writeError(w, http.StatusBadRequest, "teamId is required")
			return
		}
		if !writable(w, r, func(g authz.Grant) bool { return g.CanUpdateTeam(teamID) }) {
			return
		}
		result, err := svc.SyncTeam(r.Context(), teamID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("sync failed: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func devlakeTriggerHandler(svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		if teamID == "" {
			writeError(w, http.StatusBadRequest, "teamId is required")
			return
		}
		if !writable(w, r, func(g authz.Grant) bool { return g.CanUpdateTeam(teamID) }) {
			return
		}
		result, err := svc.TriggerTeam(r.Context(), teamID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("trigger failed: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func devlakeCreateConnectionHandler(store database.ConfigStore, svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		if teamID == "" {
			writeError(w, http.StatusBadRequest, "teamId is required")
			return
		}
		if err := requireTeamOwnerOrAdmin(r, store, teamID); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		entry, err := decodeBody[devlake.ConnectionEntry](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if msg := validateConnectionEntryFields(entry); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		if entry.Plugin == "codecov" && strings.TrimSpace(entry.Organization) == "" {
			writeError(w, http.StatusBadRequest, "organization is required for codecov connections")
			return
		}
		if entry.Plugin == devlake.PluginTestRegistry && entry.Token == "" && entry.ExpiryDate == "" {
			entry.ExpiryDate = "never"
		}
		if entry.ExpiryDate == "" {
			writeError(w, http.StatusBadRequest, "expiry_date is required")
			return
		}
		if entry.ExpiryDate != "never" && !isValidDate(entry.ExpiryDate) {
			writeError(w, http.StatusBadRequest, "expiry_date must be in YYYY-MM-DD format or \"never\"")
			return
		}
		connID, err := svc.CreateTeamConnection(r.Context(), teamID, entry)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, devlake.ErrInvalidConnection) {
				status = http.StatusBadRequest
			}
			writeError(w, status, fmt.Sprintf("create connection: %v", err))
			return
		}
		writeJSON(w, http.StatusCreated, devlake.ConnectionResult{
			Plugin:       entry.Plugin,
			ConnectionID: connID,
		})
	}
}

// validateConnectionEntryFields checks the fields every connection needs.
// TestRegistry has no endpoint and only Openshift CI uses a token; its
// CI-tool-specific fields are validated by the devlake service.
func validateConnectionEntryFields(entry devlake.ConnectionEntry) string {
	if entry.Plugin == devlake.PluginTestRegistry {
		if strings.TrimSpace(entry.CITool) == "" || strings.TrimSpace(entry.Organization) == "" {
			return "ci_tool and organization are required for testregistry connections"
		}
		return ""
	}
	if entry.Plugin == "" || entry.Token == "" || entry.Endpoint == "" {
		return "plugin, token, and endpoint are required"
	}
	return ""
}

func devlakeAvailableCodecovHandler(store database.ConfigStore, svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := requireTeamOwnerOrAdmin(r, store, r.PathValue("teamId")); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		available, err := svc.AvailableCodecov(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("list Codecov resources: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, available)
	}
}

func devlakeAttachCodecovHandler(store database.ConfigStore, svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		if err := requireTeamOwnerOrAdmin(r, store, teamID); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		req, err := decodeBody[devlake.AttachCodecovRequest](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := svc.AttachCodecov(r.Context(), teamID, req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "attached"})
	}
}

func devlakeUpdateConnectionHandler(store database.ConfigStore, svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		plugin := r.PathValue("plugin")
		if teamID == "" || plugin == "" {
			writeError(w, http.StatusBadRequest, "teamId and plugin are required")
			return
		}
		if err := requireTeamOwnerOrAdmin(r, store, teamID); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		entry, err := decodeBody[devlake.ConnectionEntry](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		entry.Plugin = plugin
		if entry.Token == "" {
			writeError(w, http.StatusBadRequest, "token is required")
			return
		}
		if entry.ExpiryDate == "" {
			writeError(w, http.StatusBadRequest, "expiry_date is required")
			return
		}
		if entry.ExpiryDate != "never" && !isValidDate(entry.ExpiryDate) {
			writeError(w, http.StatusBadRequest, "expiry_date must be in YYYY-MM-DD format or \"never\"")
			return
		}
		if err := svc.UpdateTeamConnection(r.Context(), teamID, entry); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, devlake.ErrInvalidConnection) {
				status = http.StatusBadRequest
			}
			writeError(w, status, fmt.Sprintf("update connection: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}

func devlakeTokenStatusHandler(svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		if teamID == "" {
			writeError(w, http.StatusBadRequest, "teamId is required")
			return
		}
		if !visible(w, r, func(g authz.Grant) bool { return g.CanReadTeam(teamID) }) {
			return
		}
		statuses, err := svc.GetTokenStatus(r.Context(), teamID)
		if err != nil {
			writeDetailedError(w, r, "get token status failed", err)
			return
		}
		if statuses == nil {
			statuses = []devlake.TokenStatus{}
		}
		writeJSON(w, http.StatusOK, statuses)
	}
}

func devlakeExistingScopesHandler(svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		plugin := r.PathValue("plugin")
		if plugin == "" {
			writeError(w, http.StatusBadRequest, "plugin is required")
			return
		}
		if !visible(w, r, func(g authz.Grant) bool { return g.CanReadTeam(teamID) }) {
			return
		}
		scopes, err := svc.ListExistingScopes(r.Context(), plugin, teamID)
		if err != nil {
			writeDetailedError(w, r, "list scopes failed", err)
			return
		}
		if scopes == nil {
			scopes = []devlake.ExistingScope{}
		}
		writeJSON(w, http.StatusOK, scopes)
	}
}

func devlakeBrowseRemoteHandler(svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		plugin := r.PathValue("plugin")
		search := r.URL.Query().Get("search")
		if plugin == "" {
			writeError(w, http.StatusBadRequest, "plugin is required")
			return
		}
		if !visible(w, r, func(g authz.Grant) bool { return g.CanReadTeam(teamID) }) {
			return
		}
		results, err := svc.BrowseRemoteScopes(r.Context(), teamID, plugin, search)
		if err != nil {
			slog.Error("browse-remote failed", "team", teamID, "plugin", plugin, "err", err)
			writeDetailedError(w, r, "browse scopes failed", err)
			return
		}
		if results == nil {
			results = []devlake.BrowseResult{}
		}
		writeJSON(w, http.StatusOK, results)
	}
}

func devlakeBrowseRemoteCountHandler(svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		plugin := r.PathValue("plugin")
		if plugin == "" {
			writeError(w, http.StatusBadRequest, "plugin is required")
			return
		}
		if !visible(w, r, func(g authz.Grant) bool { return g.CanReadTeam(teamID) }) {
			return
		}
		count, err := svc.BrowseRemoteScopeCount(r.Context(), teamID, plugin)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]int{"count": 0})
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"count": count})
	}
}

func devlakeTestConnectionHandler(svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var entry devlake.ConnectionEntry
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if msg := validateConnectionEntryFields(entry); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		if entry.Plugin == "jira" && entry.Username == "" {
			writeError(w, http.StatusBadRequest, "username is required for Jira connections")
			return
		}
		if entry.Plugin == "codecov" && strings.TrimSpace(entry.Organization) == "" {
			writeError(w, http.StatusBadRequest, "organization is required for Codecov connections")
			return
		}
		result, err := svc.TestConnection(r.Context(), entry)
		if err != nil {
			slog.Warn("test-connection failed", "plugin", entry.Plugin, "endpoint", entry.Endpoint, "err", err)
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": err.Error()})
			return
		}
		resp := map[string]any{"success": true, "message": "Connection successful"}
		if result != nil && result.ExpiryDate != "" {
			resp["expiry_date"] = result.ExpiryDate
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func devlakeProxyLookupHandler(svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			devlake.ConnectionEntry
			Owner string `json:"owner"`
			Name  string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		entry := req.ConnectionEntry
		if entry.Plugin == "" || entry.Token == "" || entry.Endpoint == "" {
			writeError(w, http.StatusBadRequest, "plugin, token, and endpoint are required")
			return
		}
		if strings.TrimSpace(req.Owner) == "" || strings.TrimSpace(req.Name) == "" {
			writeError(w, http.StatusBadRequest, "owner and name are required")
			return
		}
		if entry.Plugin != "github" && entry.Plugin != "gitlab" {
			writeError(w, http.StatusBadRequest, "lookup is only supported for github and gitlab")
			return
		}
		result := svc.ProxyLookup(r.Context(), entry, req.Owner, req.Name)
		resp := map[string]any{"valid": result.Valid}
		if result.CanonicalName != "" {
			resp["canonical_name"] = result.CanonicalName
		}
		if result.Warning != "" {
			resp["warning"] = result.Warning
		}
		if result.Error != "" {
			resp["error"] = result.Error
		}
		if result.Archived {
			resp["archived"] = true
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func devlakeProxyBrowseHandler(svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			devlake.ConnectionEntry
			Query string `json:"query,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		entry := req.ConnectionEntry
		if msg := validateConnectionEntryFields(entry); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		if entry.Plugin == "jira" && entry.Username == "" {
			writeError(w, http.StatusBadRequest, "username is required for Jira connections")
			return
		}
		results, err := svc.ProxyBrowse(r.Context(), entry, req.Query)
		if err != nil {
			slog.Error("proxy-browse failed", "plugin", entry.Plugin, "endpoint", entry.Endpoint, "err", err)
			writeError(w, http.StatusBadGateway, fmt.Sprintf("browse: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, results)
	}
}

func devlakeProxyJiraMetaHandler(svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			devlake.ConnectionEntry
			ProjectKey string `json:"project_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.Plugin != "jira" || req.Token == "" || req.Endpoint == "" || req.Username == "" {
			writeError(w, http.StatusBadRequest, "plugin (jira), token, endpoint, and username are required")
			return
		}
		if req.ProjectKey == "" {
			writeError(w, http.StatusBadRequest, "project_key is required")
			return
		}
		meta, err := svc.ProxyJiraMeta(r.Context(), req.ConnectionEntry, req.ProjectKey)
		if err != nil {
			slog.Error("proxy-jira-meta failed", "project", req.ProjectKey, "err", err)
			writeError(w, http.StatusBadGateway, fmt.Sprintf("jira meta: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, meta)
	}
}

func devlakeProxyJiraProjectHandler(svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			devlake.ConnectionEntry
			ProjectKey string `json:"project_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.Plugin != "jira" || req.Token == "" || req.Endpoint == "" || req.Username == "" {
			writeError(w, http.StatusBadRequest, "plugin (jira), token, endpoint, and username are required")
			return
		}
		if strings.TrimSpace(req.ProjectKey) == "" {
			writeError(w, http.StatusBadRequest, "project_key is required")
			return
		}
		project, err := svc.ProxyJiraProject(r.Context(), req.ConnectionEntry, strings.TrimSpace(req.ProjectKey))
		if err != nil {
			status := http.StatusBadGateway
			message := fmt.Sprintf("Unable to validate Jira project %s", strings.TrimSpace(req.ProjectKey))
			errText := err.Error()
			if strings.Contains(errText, "HTTP 404") {
				status = http.StatusNotFound
				message = fmt.Sprintf("Jira project %s was not found or you do not have access to it", strings.TrimSpace(req.ProjectKey))
			} else if strings.Contains(errText, "HTTP 401") || strings.Contains(errText, "HTTP 403") {
				status = http.StatusForbidden
				message = fmt.Sprintf("You do not have access to Jira project %s", strings.TrimSpace(req.ProjectKey))
			}
			slog.Warn("jira project validation failed", "project", req.ProjectKey, "err", err)
			writeError(w, status, message)
			return
		}
		writeJSON(w, http.StatusOK, project)
	}
}

func devlakeProxyJiraBoardMetaHandler(svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			devlake.ConnectionEntry
			BoardID string `json:"board_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.Plugin != "jira" || req.Token == "" || req.Endpoint == "" || req.Username == "" {
			writeError(w, http.StatusBadRequest, "plugin (jira), token, endpoint, and username are required")
			return
		}
		if req.BoardID == "" {
			writeError(w, http.StatusBadRequest, "board_id is required")
			return
		}
		result, err := svc.ProxyJiraBoardMeta(r.Context(), req.ConnectionEntry, req.BoardID)
		if err != nil {
			slog.Error("proxy-jira-board-meta failed", "board", req.BoardID, "err", err)
			writeError(w, http.StatusBadGateway, fmt.Sprintf("jira board meta: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func devlakeTeamJiraBoardMetaHandler(store database.ConfigStore, svc *devlake.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		teamID := r.PathValue("teamId")
		boardID := r.PathValue("boardId")
		if teamID == "" || boardID == "" {
			writeError(w, http.StatusBadRequest, "teamId and boardId are required")
			return
		}

		ident, ok := identityFromContext(r.Context())
		if !ok || !ident.Known() {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		team, err := store.GetTeamRow(r.Context(), teamID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if team == nil {
			writeError(w, http.StatusNotFound, "team not found")
			return
		}
		if !ident.IsAdmin {
			if err := requireTeamOwnerOrAdmin(r, store, teamID); err != nil {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
		}

		result, err := svc.TeamJiraBoardMeta(r.Context(), teamID, boardID)
		if err != nil {
			slog.Error("jira-board-meta failed", "team", teamID, "board", boardID, "err", err)
			writeError(w, http.StatusBadGateway, fmt.Sprintf("jira board meta: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func isValidDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func isValidEmail(email string) bool {
	if strings.TrimSpace(email) != email || email == "" {
		return false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}
	if addr.Address != email {
		return false
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || strings.HasPrefix(parts[0], "`") ||
		strings.HasSuffix(parts[0], "`") {
		return false
	}
	return strings.Contains(parts[1], ".") &&
		!strings.HasPrefix(parts[1], ".") && !strings.HasSuffix(parts[1], ".")
}

// --------------- Subscription handlers ---------------

func subscriptionListHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.ListSubscriptionRows(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if rows == nil {
			rows = []database.SubscriptionRow{}
		}
		id, ok := identityFromContext(r.Context())
		if ok && !id.IsAdmin {
			var filtered []database.SubscriptionRow
			for _, row := range rows {
				if strings.EqualFold(row.Email, id.Email) {
					filtered = append(filtered, row)
				}
			}
			rows = filtered
			if rows == nil {
				rows = []database.SubscriptionRow{}
			}
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

// trustedReportURLs lists what a non-admin may store as a subscription or
// alert URL: the workflow endpoints of this dashboard and of the workflow
// execution service, and the configured AI analytics endpoint. The report
// scheduler posts to these URLs and mails the responses, so anything else
// would let a user read internal services.
func trustedReportURLs(cfg FrontendConfig, workflowExecURL string) []string {
	var out []string
	for _, base := range []string{cfg.WorkflowBaseURL, workflowExecURL} {
		if base = strings.TrimRight(strings.TrimSpace(base), "/"); base != "" {
			out = append(out, base+"/workflows/")
		}
	}
	if u := strings.TrimSpace(cfg.AIAnalyticsURL); u != "" {
		out = append(out, u)
	}
	return out
}

// reportURLAllowed reports whether raw equals a trusted URL or lies under a
// trusted prefix ending in "/". Paths must already be clean so "../" cannot
// climb out of the prefix.
func reportURLAllowed(raw string, trusted []string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Opaque != "" || u.Host == "" {
		return false
	}
	if u.Path != "" && path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") {
		return false
	}
	for _, t := range trusted {
		tu, err := url.Parse(t)
		if err != nil {
			continue
		}
		if !strings.EqualFold(u.Scheme, tu.Scheme) || !strings.EqualFold(u.Host, tu.Host) {
			continue
		}
		if strings.HasSuffix(tu.Path, "/") {
			if strings.HasPrefix(u.Path, tu.Path) && len(u.Path) > len(tu.Path) {
				return true
			}
		} else if u.Path == tu.Path && u.RawQuery == tu.RawQuery {
			return true
		}
	}
	return false
}

func checkReportURLs(ident Identity, trusted []string, urls ...string) error {
	if ident.IsAdmin {
		return nil
	}
	for _, u := range urls {
		if u != "" && !reportURLAllowed(u, trusted) {
			return fmt.Errorf("url %q is not allowed: use a workflow endpoint of this dashboard", u)
		}
	}
	return nil
}

func subscriptionURLs(row database.SubscriptionRow) []string {
	urls := []string{row.AIAnalyticsURL}
	for _, el := range row.Elements {
		urls = append(urls, el.URL)
	}
	return urls
}

func subscriptionCreateHandler(store database.ConfigStore, trusted []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeBody[database.SubscriptionRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		id, _ := identityFromContext(r.Context())
		if err := checkReportURLs(id, trusted, subscriptionURLs(input)...); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		input.Email = strings.TrimSpace(input.Email)
		if input.Email == "" {
			input.Email = id.Email
		} else if !id.IsAdmin && !strings.EqualFold(input.Email, id.Email) {
			writeError(w, http.StatusForbidden, "cannot create subscription for another user")
			return
		}
		if input.Email == "" || !isValidEmail(input.Email) {
			writeError(w, http.StatusBadRequest, "valid email is required")
			return
		}
		autoID, err := store.CreateSubscription(r.Context(), input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.ID = autoID
		writeJSON(w, http.StatusCreated, input)
	}
}

func subscriptionGetHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid subscription id")
			return
		}
		row, err := store.GetSubscriptionRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if row == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		ident, _ := identityFromContext(r.Context())
		if !ident.IsAdmin && !strings.EqualFold(row.Email, ident.Email) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeJSON(w, http.StatusOK, row)
	}
}

func subscriptionUpdateHandler(store database.ConfigStore, trusted []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid subscription id")
			return
		}
		existing, err := store.GetSubscriptionRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		ident, _ := identityFromContext(r.Context())
		if !ident.IsAdmin && !strings.EqualFold(existing.Email, ident.Email) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		input, err := decodeBody[database.SubscriptionRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := checkReportURLs(ident, trusted, subscriptionURLs(input)...); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		input.Email = strings.TrimSpace(input.Email)
		if input.Email == "" {
			input.Email = existing.Email
		} else if !ident.IsAdmin && !strings.EqualFold(input.Email, ident.Email) {
			writeError(w, http.StatusForbidden, "cannot transfer subscription to another user")
			return
		}
		if !isValidEmail(input.Email) {
			writeError(w, http.StatusBadRequest, "valid email is required")
			return
		}
		if err := store.UpdateSubscription(r.Context(), id, input); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.ID = id
		writeJSON(w, http.StatusOK, input)
	}
}

func subscriptionDeleteHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid subscription id")
			return
		}
		existing, err := store.GetSubscriptionRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		ident, _ := identityFromContext(r.Context())
		if !ident.IsAdmin && !strings.EqualFold(existing.Email, ident.Email) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if err := store.DeleteSubscription(r.Context(), id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --------------- Alert handlers ---------------

func alertListHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.ListAlertRows(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if rows == nil {
			rows = []database.AlertRow{}
		}
		id, ok := identityFromContext(r.Context())
		if ok && !id.IsAdmin {
			var filtered []database.AlertRow
			for _, row := range rows {
				if strings.EqualFold(row.Email, id.Email) {
					filtered = append(filtered, row)
				}
			}
			rows = filtered
			if rows == nil {
				rows = []database.AlertRow{}
			}
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

func alertCreateHandler(store database.ConfigStore, trusted []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeBody[database.AlertRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		id, _ := identityFromContext(r.Context())
		if err := checkReportURLs(id, trusted, input.URL, input.AIAnalyticsURL); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		input.Email = strings.TrimSpace(input.Email)
		if input.Email == "" {
			input.Email = id.Email
		} else if !id.IsAdmin && !strings.EqualFold(input.Email, id.Email) {
			writeError(w, http.StatusForbidden, "cannot create alert for another user")
			return
		}
		if input.Email == "" || !isValidEmail(input.Email) || input.URL == "" || input.Rule == "" {
			writeError(w, http.StatusBadRequest, "valid email, url, and rule are required")
			return
		}
		autoID, err := store.CreateAlert(r.Context(), input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.ID = autoID
		writeJSON(w, http.StatusCreated, input)
	}
}

func alertGetHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid alert id")
			return
		}
		row, err := store.GetAlertRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if row == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		ident, _ := identityFromContext(r.Context())
		if !ident.IsAdmin && !strings.EqualFold(row.Email, ident.Email) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeJSON(w, http.StatusOK, row)
	}
}

func alertUpdateHandler(store database.ConfigStore, trusted []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid alert id")
			return
		}
		existing, err := store.GetAlertRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		ident, _ := identityFromContext(r.Context())
		if !ident.IsAdmin && !strings.EqualFold(existing.Email, ident.Email) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		input, err := decodeBody[database.AlertRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := checkReportURLs(ident, trusted, input.URL, input.AIAnalyticsURL); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		input.Email = strings.TrimSpace(input.Email)
		if input.Email == "" {
			input.Email = existing.Email
		} else if !ident.IsAdmin && !strings.EqualFold(input.Email, ident.Email) {
			writeError(w, http.StatusForbidden, "cannot transfer alert to another user")
			return
		}
		if !isValidEmail(input.Email) || input.URL == "" || input.Rule == "" {
			writeError(w, http.StatusBadRequest, "valid email, url, and rule are required")
			return
		}
		if err := store.UpdateAlert(r.Context(), id, input); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.ID = id
		writeJSON(w, http.StatusOK, input)
	}
}

func alertDeleteHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid alert id")
			return
		}
		existing, err := store.GetAlertRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		ident, _ := identityFromContext(r.Context())
		if !ident.IsAdmin && !strings.EqualFold(existing.Email, ident.Email) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if err := store.DeleteAlert(r.Context(), id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --------------- Canned query handlers ---------------

func cannedQueryListHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.ListCannedQueryRows(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if rows == nil {
			rows = []database.CannedQueryRow{}
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

func cannedQueryCreateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeBody[database.CannedQueryRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.Name == "" || input.SQLText == "" {
			writeError(w, http.StatusBadRequest, "name and sql_text are required")
			return
		}
		autoID, err := store.CreateCannedQuery(r.Context(), input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.ID = autoID
		writeJSON(w, http.StatusCreated, input)
	}
}

func cannedQueryGetHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid query id")
			return
		}
		row, err := store.GetCannedQueryRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if row == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, row)
	}
}

func cannedQueryUpdateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid query id")
			return
		}
		input, err := decodeBody[database.CannedQueryRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := store.UpdateCannedQuery(r.Context(), id, input); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.ID = id
		writeJSON(w, http.StatusOK, input)
	}
}

func cannedQueryDeleteHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid query id")
			return
		}
		if err := store.DeleteCannedQuery(r.Context(), id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --------------- Canned script handlers ---------------

func cannedScriptListHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.ListCannedScriptRows(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if rows == nil {
			rows = []database.CannedScriptRow{}
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

func cannedScriptCreateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeBody[database.CannedScriptRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.Name == "" || input.JSText == "" {
			writeError(w, http.StatusBadRequest, "name and js_text are required")
			return
		}
		autoID, err := store.CreateCannedScript(r.Context(), input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.ID = autoID
		writeJSON(w, http.StatusCreated, input)
	}
}

func cannedScriptGetHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid script id")
			return
		}
		row, err := store.GetCannedScriptRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if row == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, row)
	}
}

func cannedScriptUpdateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid script id")
			return
		}
		input, err := decodeBody[database.CannedScriptRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := store.UpdateCannedScript(r.Context(), id, input); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.ID = id
		writeJSON(w, http.StatusOK, input)
	}
}

func cannedScriptDeleteHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid script id")
			return
		}
		if err := store.DeleteCannedScript(r.Context(), id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --------------- JSON Schema handlers ---------------

func jsonSchemaListHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.ListJsonSchemaRows(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if rows == nil {
			rows = []database.JsonSchemaRow{}
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

func jsonSchemaCreateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeBody[database.JsonSchemaRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.Name == "" || input.JsonSchema == "" {
			writeError(w, http.StatusBadRequest, "name and json_schema are required")
			return
		}
		autoID, err := store.CreateJsonSchema(r.Context(), input)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.ID = autoID
		writeJSON(w, http.StatusCreated, input)
	}
}

func jsonSchemaGetHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid schema id")
			return
		}
		row, err := store.GetJsonSchemaRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if row == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, row)
	}
}

func jsonSchemaUpdateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid schema id")
			return
		}
		input, err := decodeBody[database.JsonSchemaRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := store.UpdateJsonSchema(r.Context(), id, input); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.ID = id
		writeJSON(w, http.StatusOK, input)
	}
}

func jsonSchemaDeleteHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid schema id")
			return
		}
		if err := store.DeleteJsonSchema(r.Context(), id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --------------- Stored Procedure handlers ---------------

func storedProcListHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("workflow_id")
		if raw == "" {
			rows, err := store.ListAllStoredProcedureRows(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "database error")
				return
			}
			if rows == nil {
				rows = []database.StoredProcedureRow{}
			}
			writeJSON(w, http.StatusOK, rows)
			return
		}
		wfID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || wfID <= 0 {
			writeError(w, http.StatusBadRequest, "invalid workflow_id")
			return
		}
		rows, err := store.ListStoredProcedureRows(r.Context(), wfID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if rows == nil {
			rows = []database.StoredProcedureRow{}
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

func storedProcCreateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeBody[database.StoredProcedureRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.Name == "" || input.SQLText == "" {
			writeError(w, http.StatusBadRequest, "name and sql_text are required")
			return
		}
		if input.WorkflowID <= 0 {
			writeError(w, http.StatusBadRequest, "workflow_id is required")
			return
		}
		id, err := store.CreateStoredProcedure(r.Context(), input)
		if err != nil {
			if errors.Is(err, database.ErrDuplicateName) {
				writeError(w, http.StatusConflict, "a stored procedure named '"+input.Name+"' already exists")
				return
			}
			writeDetailedError(w, r, "create stored procedure failed", err)
			return
		}
		input.ID = id
		writeJSON(w, http.StatusCreated, input)
	}
}

func storedProcGetHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		row, err := store.GetStoredProcedureRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if row == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, row)
	}
}

func storedProcUpdateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		input, err := decodeBody[database.StoredProcedureRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.Name == "" || input.SQLText == "" {
			writeError(w, http.StatusBadRequest, "name and sql_text are required")
			return
		}
		if err := store.UpdateStoredProcedure(r.Context(), id, input); err != nil {
			switch {
			case errors.Is(err, database.ErrDuplicateName):
				writeError(w, http.StatusConflict, "a stored procedure named '"+input.Name+"' already exists")
			case errors.Is(err, database.ErrNotFound):
				writeError(w, http.StatusNotFound, "not found")
			default:
				writeDetailedError(w, r, "update stored procedure failed", err)
			}
			return
		}
		input.ID = id
		writeJSON(w, http.StatusOK, input)
	}
}

func storedProcDeleteHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		if err := store.DeleteStoredProcedure(r.Context(), id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
			} else {
				writeDetailedError(w, r, "delete stored procedure failed", err)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func storedProcDeployHandler(store database.ConfigStore, exec *WorkflowExecutor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		row, err := store.GetStoredProcedureRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if row == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if _, err := exec.dataDB.ExecContext(r.Context(), row.SQLText); err != nil { // #nosec -- admin-authored CREATE PROCEDURE text
			writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func storedProcExecuteSQLHandler(exec *WorkflowExecutor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeBody[struct {
			SQL    string `json:"sql"`
			Params []any  `json:"params"`
		}](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.SQL == "" {
			writeError(w, http.StatusBadRequest, "sql is required")
			return
		}
		rows, err := exec.queryRows(r.Context(), req.SQL, req.Params)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"output": rows})
	}
}

// --------------- DevLake DB handlers ---------------

func devlakeDBQueryHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeBody[struct {
			SQL    string `json:"sql"`
			Params []any  `json:"params"`
		}](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.SQL == "" {
			writeError(w, http.StatusBadRequest, "sql is required")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		rows, err := db.QueryContext(ctx, req.SQL, req.Params...)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
			return
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
			return
		}

		var results []map[string]any
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
				return
			}
			row := make(map[string]any, len(cols))
			for i, col := range cols {
				switch vt := vals[i].(type) {
				case []byte:
					var parsed any
					if err := json.Unmarshal(vt, &parsed); err == nil {
						row[col] = parsed
					} else {
						row[col] = string(vt)
					}
				default:
					row[col] = vt
				}
			}
			results = append(results, row)
		}
		if results == nil {
			results = []map[string]any{}
		}
		if err := rows.Err(); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"columns": cols,
			"rows":    results,
			"count":   len(results),
		})
	}
}

func devlakeDBTablesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		rows, err := db.QueryContext(ctx, "SHOW TABLES")
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
			return
		}
		if rows.Err() != nil {
			writeJSON(w, http.StatusOK, map[string]any{"error": rows.Err().Error()})
			return
		}
		defer rows.Close()

		var tables []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
				return
			}
			tables = append(tables, name)
		}
		if tables == nil {
			tables = []string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"tables": tables})
	}
}

// --------------- Workflow handlers ---------------

type workflowListEntry struct {
	database.WorkflowRow
	Locked          bool   `json:"locked"`
	LockedSessionID string `json:"locked_session,omitempty"`
}

func workflowListHandler(store database.ConfigStore, lockMgr *WorkflowLockManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.ListWorkflowRows(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if rows == nil {
			rows = []database.WorkflowRow{}
		}
		activeLocks := lockMgr.ActiveLocks()
		entries := make([]workflowListEntry, len(rows))
		for i, row := range rows {
			entries[i].WorkflowRow = row
			if sid, ok := activeLocks[row.ID]; ok {
				entries[i].Locked = true
				entries[i].LockedSessionID = sid
			}
		}
		writeJSON(w, http.StatusOK, entries)
	}
}

func workflowCreateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeBody[database.WorkflowRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		autoID, err := store.CreateWorkflow(r.Context(), input)
		if err != nil {
			if errors.Is(err, database.ErrDuplicateName) {
				writeError(w, http.StatusConflict, fmt.Sprintf("workflow %q already exists", input.Name))
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.ID = autoID
		writeJSON(w, http.StatusCreated, input)
	}
}

func workflowGetHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		row, err := store.GetWorkflowRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if row == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, row)
	}
}

func workflowUpdateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		input, err := decodeBody[database.WorkflowRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := store.UpdateWorkflow(r.Context(), id, input); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			if errors.Is(err, database.ErrDuplicateName) {
				writeError(w, http.StatusConflict, fmt.Sprintf("workflow %q already exists", input.Name))
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		input.ID = id
		writeJSON(w, http.StatusOK, input)
	}
}

func workflowDeleteHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		row, err := store.GetWorkflowRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if row == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if !row.Archived {
			writeError(w, http.StatusConflict, "workflow must be archived before it can be deleted")
			return
		}
		if err := store.DeleteWorkflow(r.Context(), id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func workflowDraftHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		input, err := decodeBody[database.WorkflowRow](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := store.SaveWorkflowDraft(r.Context(), id, input); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "draft: workflow not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "draft saved"})
	}
}

func workflowPublishHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		var body struct {
			PublishNote string `json:"publish_note"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		wf, err := store.PublishWorkflow(r.Context(), id, body.PublishNote)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "publish: workflow not found")
				return
			}
			writeDetailedError(w, r, "publish workflow failed", err)
			return
		}
		writeJSON(w, http.StatusOK, wf)
	}
}

func workflowVersionsListHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		versions, err := store.ListWorkflowVersions(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if versions == nil {
			versions = []database.WorkflowVersionRow{}
		}
		writeJSON(w, http.StatusOK, versions)
	}
}

func workflowVersionGetHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		version, err := strconv.Atoi(r.PathValue("version"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid version number")
			return
		}
		ver, err := store.GetWorkflowVersion(r.Context(), id, version)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if ver == nil {
			writeError(w, http.StatusNotFound, "version not found")
			return
		}
		writeJSON(w, http.StatusOK, ver)
	}
}

func workflowSetProductionHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		version, err := strconv.Atoi(r.PathValue("version"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid version number")
			return
		}
		if err := store.SetProductionVersion(r.Context(), id, version); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "version not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "production set", "version": version})
	}
}

func workflowArchiveHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		if err := store.ArchiveWorkflow(r.Context(), id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func workflowUnarchiveHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		if err := store.UnarchiveWorkflow(r.Context(), id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func workflowLockHandler(lockMgr *WorkflowLockManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		var body struct {
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SessionID == "" {
			writeError(w, http.StatusBadRequest, "session_id is required")
			return
		}
		if lockMgr.Acquire(id, body.SessionID) {
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		} else {
			_, holder := lockMgr.IsLocked(id)
			writeJSON(w, http.StatusConflict, map[string]any{"error": "locked", "locked_session": holder})
		}
	}
}

func workflowUnlockHandler(lockMgr *WorkflowLockManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		var body struct {
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SessionID == "" {
			writeError(w, http.StatusBadRequest, "session_id is required")
			return
		}
		lockMgr.Release(id, body.SessionID)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func workflowForceUnlockHandler(lockMgr *WorkflowLockManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		lockMgr.ForceRelease(id)
		slog.Info("workflow force-unlocked", "id", id) //nolint:gosec
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func workflowTestHandler(store database.ConfigStore, exec *WorkflowExecutor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		wf, err := store.GetWorkflowRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if wf == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		input, err := decodeBody[map[string]any](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		result := exec.Execute(r.Context(), wf, input)
		writeJSON(w, http.StatusOK, result)
	}
}

func workflowRunTestsHandler(store database.ConfigStore, exec *WorkflowExecutor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		wf, err := store.GetWorkflowRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if wf == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if len(wf.Tests) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"results": []TestCaseResult{}})
			return
		}

		// Pre-fetch any referenced JSON schemas.
		schemas := make(map[int64]string)
		for _, tc := range wf.Tests {
			if tc.SchemaID > 0 {
				if _, ok := schemas[tc.SchemaID]; !ok {
					row, err := store.GetJsonSchemaRow(r.Context(), tc.SchemaID)
					if err != nil {
						writeError(w, http.StatusInternalServerError, "database error")
						return
					}
					if row != nil {
						schemas[tc.SchemaID] = row.JsonSchema
					}
				}
			}
		}

		results := exec.RunTests(r.Context(), wf, schemas)
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	}
}

type runStageRequest struct {
	Type    string         `json:"type"`
	Script  string         `json:"script"`
	Globals map[string]any `json:"globals"`
	Params  []any          `json:"params"`
	Scope   map[string]any `json:"scope"`
}

func workflowRunStageHandler(exec *WorkflowExecutor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := decodeBody[runStageRequest](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Script == "" {
			writeError(w, http.StatusBadRequest, "script is required")
			return
		}
		switch req.Type {
		case "js":
			jsRes, err := runJS(req.Script, req.Globals)
			if err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"error": err.Error(), "logs": jsRes.logs})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"output": jsRes.output, "logs": jsRes.logs})
		case "sql":
			sqlText := req.Script
			params := req.Params
			if req.Scope != nil {
				sqlText, params = resolveNamedParams(sqlText, req.Scope)
			}
			rows, err := exec.queryRows(r.Context(), sqlText, params)
			if err != nil {
				writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"output": rows})
		default:
			writeError(w, http.StatusBadRequest, "type must be 'js' or 'sql'")
		}
	}
}

func workflowLiveHandler(store database.ConfigStore, exec *WorkflowExecutor, az *Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		wf, err := store.GetWorkflowByName(r.Context(), name)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if wf == nil {
			writeError(w, http.StatusNotFound, fmt.Sprintf("workflow %q not found", name))
			return
		}

		input := make(map[string]any)
		if r.Method == http.MethodGet {
			for k, v := range r.URL.Query() {
				if len(v) == 1 {
					input[k] = v[0]
				} else {
					input[k] = v
				}
			}
		} else {
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
		}

		ident, _ := identityFromContext(r.Context())
		if err := authorizeMetricInput(r.Context(), az, ident, input); err != nil {
			writeMetricError(w, err)
			return
		}

		result := exec.Execute(r.Context(), wf, input)
		if result.Error != "" {
			writeDetailedError(w, r, "execution error", errors.New(result.Error))
			return
		}
		if result.Error != "" {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": result.Error})
			return
		}
		writeJSON(w, http.StatusOK, result.Output)
	}
}

func workflowLiveVersionHandler(store database.ConfigStore, exec *WorkflowExecutor, az *Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		versionStr := r.PathValue("version")

		var wf *database.WorkflowRow
		var err error

		if versionStr == "production" {
			wf, err = store.GetProductionWorkflowByName(r.Context(), name)
		} else {
			version, parseErr := strconv.Atoi(versionStr)
			if parseErr != nil {
				writeError(w, http.StatusBadRequest, "invalid version")
				return
			}
			wf, err = store.GetWorkflowByNameAndVersion(r.Context(), name, version)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if wf == nil {
			writeError(w, http.StatusNotFound, fmt.Sprintf("workflow %q version %s not found", name, versionStr))
			return
		}

		input := make(map[string]any)
		if r.Method == http.MethodGet {
			for k, v := range r.URL.Query() {
				if len(v) == 1 {
					input[k] = v[0]
				} else {
					input[k] = v
				}
			}
		} else {
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
		}

		ident, _ := identityFromContext(r.Context())
		if err := authorizeMetricInput(r.Context(), az, ident, input); err != nil {
			writeMetricError(w, err)
			return
		}

		result := exec.Execute(r.Context(), wf, input)
		if result.Error != "" {
			writeDetailedError(w, r, "execution error", errors.New(result.Error))
			return
		}
		if result.Error != "" {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": result.Error})
			return
		}
		writeJSON(w, http.StatusOK, result.Output)
	}
}

// --------------- Workflow export/import ---------------

type WorkflowBundle struct {
	FormatVersion    int                           `json:"format_version"`
	ExportedAt       string                        `json:"exported_at"`
	Workflow         database.WorkflowRow          `json:"workflow"`
	StoredProcedures []database.StoredProcedureRow `json:"stored_procedures"`
	JsonSchemas      []database.JsonSchemaRow      `json:"json_schemas"`
	Versions         []database.WorkflowVersionRow `json:"versions"`
}

func workflowExportHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid workflow id")
			return
		}
		wf, err := store.GetWorkflowRow(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if wf == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		procs, err := store.ListStoredProcedureRows(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		schemaIDs := make(map[int64]bool)
		for _, tc := range wf.Tests {
			if tc.SchemaID > 0 {
				schemaIDs[tc.SchemaID] = true
			}
		}

		var schemas []database.JsonSchemaRow
		for sid := range schemaIDs {
			row, err := store.GetJsonSchemaRow(r.Context(), sid)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "database error")
				return
			}
			if row != nil {
				schemas = append(schemas, *row)
			}
		}

		versions, err := store.ListWorkflowVersions(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		for i := range procs {
			procs[i].ID = 0
			procs[i].WorkflowID = 0
			procs[i].CreatedAt = ""
			procs[i].UpdatedAt = ""
		}
		for i := range schemas {
			schemas[i].ID = 0
			schemas[i].CreatedAt = ""
			schemas[i].UpdatedAt = ""
		}
		for i := range versions {
			versions[i].ID = 0
			versions[i].WorkflowID = 0
		}

		exportWf := *wf
		exportWf.ID = 0
		exportWf.CreatedAt = ""
		exportWf.UpdatedAt = ""
		for i := range exportWf.Queries {
			exportWf.Queries[i].ID = 0
		}

		bundle := WorkflowBundle{
			FormatVersion:    1,
			ExportedAt:       time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			Workflow:         exportWf,
			StoredProcedures: procs,
			JsonSchemas:      schemas,
			Versions:         versions,
		}
		if bundle.StoredProcedures == nil {
			bundle.StoredProcedures = []database.StoredProcedureRow{}
		}
		if bundle.JsonSchemas == nil {
			bundle.JsonSchemas = []database.JsonSchemaRow{}
		}
		if bundle.Versions == nil {
			bundle.Versions = []database.WorkflowVersionRow{}
		}

		w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=%q", wf.Name+".json"))
		writeJSON(w, http.StatusOK, bundle)
	}
}

func workflowImportHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bundle, err := decodeBody[WorkflowBundle](r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if bundle.FormatVersion != 1 {
			writeError(w, http.StatusBadRequest, "unsupported format_version")
			return
		}
		if bundle.Workflow.Name == "" || bundle.Workflow.InputScript == "" || bundle.Workflow.OutputScript == "" {
			writeError(w, http.StatusBadRequest, "workflow name, input_script, and output_script are required")
			return
		}

		ctx := r.Context()

		schemaMap := make(map[string]int64)
		for _, schema := range bundle.JsonSchemas {
			if schema.Name == "" {
				continue
			}
			existing, err := store.GetJsonSchemaByName(ctx, schema.Name)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "database error")
				return
			}
			if existing != nil {
				schemaMap[schema.Name] = existing.ID
				continue
			}
			schema.ID = 0
			newID, err := store.CreateJsonSchema(ctx, schema)
			if err != nil {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("create schema %q: %v", schema.Name, err))
				return
			}
			schemaMap[schema.Name] = newID
		}

		oldSchemaIDToName := make(map[int64]string)
		for _, schema := range bundle.JsonSchemas {
			if schema.ID > 0 {
				oldSchemaIDToName[schema.ID] = schema.Name
			}
		}

		for i, tc := range bundle.Workflow.Tests {
			if tc.SchemaID > 0 {
				if name, ok := oldSchemaIDToName[tc.SchemaID]; ok {
					if newID, ok := schemaMap[name]; ok {
						bundle.Workflow.Tests[i].SchemaID = newID
					}
				}
			}
		}

		bundle.Workflow.ID = 0
		bundle.Workflow.CreatedAt = ""
		bundle.Workflow.UpdatedAt = ""
		for i := range bundle.Workflow.Queries {
			bundle.Workflow.Queries[i].ID = 0
		}

		wfID, err := store.CreateWorkflow(ctx, bundle.Workflow)
		if err != nil {
			if errors.Is(err, database.ErrDuplicateName) {
				writeError(w, http.StatusConflict, fmt.Sprintf("workflow %q already exists", bundle.Workflow.Name))
				return
			}
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("create workflow: %v", err))
			return
		}

		var procErrors []string
		for _, proc := range bundle.StoredProcedures {
			proc.ID = 0
			proc.WorkflowID = wfID
			proc.CreatedAt = ""
			proc.UpdatedAt = ""
			if _, err := store.CreateStoredProcedure(ctx, proc); err != nil {
				if errors.Is(err, database.ErrDuplicateName) {
					procErrors = append(procErrors, fmt.Sprintf("stored procedure %q already exists, skipped", proc.Name))
					continue
				}
				procErrors = append(procErrors, fmt.Sprintf("stored procedure %q: %v", proc.Name, err))
			}
		}

		resp := map[string]any{
			"workflow_id":   wfID,
			"workflow_name": bundle.Workflow.Name,
			"imported": map[string]int{
				"stored_procedures": len(bundle.StoredProcedures) - len(procErrors),
				"json_schemas":      len(schemaMap),
			},
		}
		if len(procErrors) > 0 {
			resp["warnings"] = procErrors
		}
		writeJSON(w, http.StatusCreated, resp)
	}
}

// --------------- API key handlers ---------------

func keysListHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keys, err := store.ListAPIKeys(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if keys == nil {
			keys = []database.APIKeyInfo{}
		}
		writeJSON(w, http.StatusOK, keys)
	}
}

func keysCreateHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if input.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}

		ident, _ := identityFromContext(r.Context())
		scopedUser := ident.Email
		if scopedUser == "" {
			scopedUser = ident.Username
		}

		rawKey, err := generateAPIKey()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to generate key")
			return
		}

		id, err := store.CreateAPIKey(r.Context(), input.Name, database.HashAPIKey(rawKey), true, scopedUser)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"id":          id,
			"name":        input.Name,
			"key":         rawKey,
			"is_admin":    true,
			"scoped_user": scopedUser,
		})
	}
}

var vsCodeStateRe = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

// vsCodeCallback validates the extension's loopback callback URL and optional
// state, returning the callback port.
func vsCodeCallback(callback, state string) (int, string) {
	if callback == "" {
		return 0, "callback parameter is required"
	}
	cbURL, err := url.Parse(callback)
	if err != nil || (cbURL.Hostname() != "localhost" && cbURL.Hostname() != "127.0.0.1") {
		return 0, "callback must be a localhost URL"
	}
	if cbURL.Scheme != "http" {
		return 0, "callback must be an http:// localhost URL"
	}
	cbPort, err := strconv.Atoi(cbURL.Port())
	if err != nil || cbPort < 1 || cbPort > 65535 {
		return 0, "callback must include a valid port"
	}
	if state != "" && !vsCodeStateRe.MatchString(state) {
		return 0, "invalid state parameter"
	}
	return cbPort, ""
}

var vsCodeConfirmTmpl = template.Must(template.New("vscode-confirm").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Authorize VS Code</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; max-width: 480px; margin: 64px auto; padding: 0 24px; color: #151515; }
button { font-size: 16px; padding: 8px 20px; cursor: pointer; }
</style>
</head>
<body>
<h2>Authorize VS Code</h2>
<p>The LFM Workflow Editor on this computer is requesting an API key for <strong>{{.User}}</strong>.</p>
<p>Only continue if you just started the login from VS Code.</p>
<form method="POST" action="/auth/vscode-token">
<input type="hidden" name="callback" value="{{.Callback}}">
<input type="hidden" name="state" value="{{.State}}">
<button type="submit">Authorize</button>
</form>
</body>
</html>
`))

// vsCodeTokenConfirmHandler shows a confirmation page; the key is only minted
// by the POST it submits, so a cross-site link or image cannot create one.
func vsCodeTokenConfirmHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		callback := r.URL.Query().Get("callback")
		state := r.URL.Query().Get("state")
		if _, msg := vsCodeCallback(callback, state); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		ident, _ := identityFromContext(r.Context())
		user := ident.Email
		if user == "" {
			user = ident.Username
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
		if err := vsCodeConfirmTmpl.Execute(w, map[string]string{"User": user, "Callback": callback, "State": state}); err != nil {
			logging.FromContext(r.Context()).Error("vscode token: render confirmation", "err", err)
		}
	}
}

func vsCodeTokenHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		callback := r.FormValue("callback")
		state := r.FormValue("state")
		cbPort, msg := vsCodeCallback(callback, state)
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}

		ident, _ := identityFromContext(r.Context())
		scopedUser := ident.Email
		if scopedUser == "" {
			scopedUser = ident.Username
		}

		rawKey, err := generateAPIKey()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to generate key")
			return
		}

		keyName := fmt.Sprintf("vscode-%s", scopedUser)
		_, err = store.CreateAPIKey(r.Context(), keyName, database.HashAPIKey(rawKey), true, scopedUser)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback?token=%s", cbPort, url.QueryEscape(rawKey))
		if state != "" {
			redirectURL += "&state=" + url.QueryEscape(state)
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, redirectURL, http.StatusSeeOther) //nolint:gosec // validated localhost redirect
	}
}

func keysDeleteHandler(store database.ConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := pathInt64(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid key id")
			return
		}
		if err := store.DeleteAPIKey(r.Context(), id); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --------------- Helpers ---------------

func requireTeamOwnerOrAdmin(r *http.Request, store database.ConfigStore, teamID string) error {
	ident, ok := identityFromContext(r.Context())
	if !ok || !ident.Known() {
		return fmt.Errorf("authentication required")
	}
	if ident.IsAdmin {
		return nil
	}
	if grant, granted := grantFromContext(r.Context()); granted {
		if grant.CanUpdateTeam(teamID) {
			return nil
		}
		return fmt.Errorf("forbidden")
	}
	team, err := store.GetTeamRow(r.Context(), teamID)
	if err != nil {
		return fmt.Errorf("database error")
	}
	if team == nil {
		return fmt.Errorf("team not found")
	}
	if team.ContactEmail == "" || !strings.EqualFold(team.ContactEmail, ident.Email) {
		return fmt.Errorf("only the team manager or an admin can modify connections")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("api response encode failed", "err", err)
	}
}

// writeDetailedError reports a failure whose detail can expose internals
// such as SQL, driver or DevLake API errors. Admins get the detail; other
// callers get msg and the request ID, and the detail goes to the log.
func writeDetailedError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	const status = http.StatusInternalServerError
	reqID := logging.RequestIDFromContext(r.Context())
	logging.FromContext(r.Context()).Error(msg, "err", err, "path", r.URL.Path, "request_id", reqID)
	if ident, _ := identityFromContext(r.Context()); ident.IsAdmin {
		writeError(w, status, fmt.Sprintf("%s: %v", msg, err))
		return
	}
	if reqID != "" {
		msg += " (request id " + reqID + ")"
	}
	writeError(w, status, msg)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	if status == http.StatusNotFound {
		slog.Warn("API not found", "msg", msg, "status", status) // #nosec G706
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Error("api response encode failed", "err", err)
	}
}

func decodeBody[T any](r *http.Request) (T, error) {
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		return v, fmt.Errorf("invalid JSON: %w", err)
	}
	return v, nil
}

func pathInt64(r *http.Request, name string) (int64, error) { //nolint:unparam
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

func generateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
