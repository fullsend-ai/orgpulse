// Package integration exercises the report/alert scheduler and the REST API
// end-to-end against an in-memory store and mocked external HTTP endpoints.
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/database"
	"github.com/fullsend-ai/orgpulse/internal/report"
	"github.com/fullsend-ai/orgpulse/internal/server"
)

func TestReportAndAlerts(t *testing.T) {
	ctx := t.Context()
	store := database.NewMemStore()

	var elementResponse atomic.Value
	elementResponse.Store([]byte(`{"atomicMetricsValue": 200, "type": "bar", "label": "Test", "data": {"labels":["A"], "datasets":[{"label":"S","data":[1]}]}}`))

	elementSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(elementResponse.Load().([]byte)); err != nil {
			t.Errorf("write element response: %v", err)
		}
	}))
	defer elementSrv.Close()

	aiAnalyticsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("<p>Analysis</p>")); err != nil {
			t.Errorf("write analysis: %v", err)
		}
	}))
	defer aiAnalyticsSrv.Close()

	aiSummarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("<p>Summary</p>")); err != nil {
			t.Errorf("write summary: %v", err)
		}
	}))
	defer aiSummarySrv.Close()

	if err := store.CreateOrg(ctx, database.OrgRow{ID: "test-org", Name: "Test Org"}); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := store.CreateProduct(ctx, database.ProductRow{ID: "test-product", Name: "Test Product"}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	if err := store.CreateTeam(ctx, database.TeamRow{ID: "test-team", Name: "Test Team"}); err != nil {
		t.Fatalf("create team: %v", err)
	}
	if err := store.ReplaceOrgProductLinks(ctx, "test-org", []database.RelEntry{{ID: "test-product", SortOrder: 0}}); err != nil {
		t.Fatalf("link org->product: %v", err)
	}
	if err := store.ReplaceProductTeamLinks(ctx, "test-product", []database.RelEntry{{ID: "test-team", SortOrder: 0}}); err != nil {
		t.Fatalf("link product->team: %v", err)
	}

	intervalDays := 1
	periodicID, err := store.CreateSubscription(ctx, database.SubscriptionRow{
		Email:          "test@example.com",
		ProductID:      "test-product",
		TeamID:         "test-team",
		AIAnalyticsURL: aiAnalyticsSrv.URL,
		IntervalDays:   &intervalDays,
		Elements: []database.SubscriptionElementRow{
			{SortOrder: 0, URL: elementSrv.URL, Payload: json.RawMessage(`{}`)},
		},
	})
	if err != nil {
		t.Fatalf("create periodic subscription: %v", err)
	}

	var mu sync.Mutex
	var capturedEmails [][]byte
	mockSendMail := func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		mu.Lock()
		capturedEmails = append(capturedEmails, msg)
		mu.Unlock()
		return nil
	}

	svc := report.NewService(store, &http.Client{Timeout: 10 * time.Second}, report.Config{
		AIAnalyticsURL: aiAnalyticsSrv.URL,
		AISummaryURL:   aiSummarySrv.URL,
		SMTPHost:       "localhost",
		SMTPPort:       25,
		SMTPFrom:       "test@example.com",
		SendMailFunc:   mockSendMail,
	})

	t.Log("=== Test: SendReport (periodic) ===")

	err = svc.SendReport(ctx, periodicID)
	check(t, "SendReport succeeds", err == nil, fmt.Sprintf("err=%v", err))
	check(t, "email captured", len(capturedEmails) > 0, "no email captured")
	if len(capturedEmails) > 0 {
		check(t, "email contains analysis", strings.Contains(string(capturedEmails[0]), "<p>Analysis</p>"), "")
		check(t, "email contains summary", strings.Contains(string(capturedEmails[0]), "<p>Summary</p>"), "")
	}

	sub, err := store.GetSubscriptionRow(ctx, periodicID)
	check(t, "last_run_at updated", err == nil && sub != nil && sub.LastRunAt != nil, fmt.Sprintf("sub=%+v err=%v", sub, err))

	t.Log("=== Test: Alert triggered (rule: data.atomicMetricsValue > 100) ===")

	capturedEmails = nil
	_, err = store.CreateAlert(ctx, database.AlertRow{
		Email:          "alert@example.com",
		ProductID:      "test-product",
		TeamID:         "test-team",
		URL:            elementSrv.URL,
		Payload:        json.RawMessage(`{}`),
		AIAnalyticsURL: aiAnalyticsSrv.URL,
		Rule:           "data.atomicMetricsValue > 100",
	})
	if err != nil {
		t.Fatalf("create alert: %v", err)
	}

	scheduler := report.NewScheduler(svc, report.SchedulerConfig{
		SubscriptionInterval: time.Hour,
		AlertInterval:        time.Hour,
	})
	scheduler.Tick(ctx)

	mu.Lock()
	alertEmails := len(capturedEmails)
	mu.Unlock()
	// Tick sends periodic report (1 email) + triggered alert (1 email) = 2 emails
	// But the periodic sub was already run, so last_run_at is recent — it won't be due again.
	// So only the alert email should be captured.
	check(t, "alert email sent", alertEmails >= 1, fmt.Sprintf("emails=%d", alertEmails))
	if alertEmails >= 1 {
		lastEmail := string(capturedEmails[alertEmails-1])
		check(t, "alert email has analysis", strings.Contains(lastEmail, "<p>Analysis</p>"), "")
		check(t, "alert email to correct address", strings.Contains(lastEmail, "alert@example.com"), "")
	}

	t.Log("=== Test: Alert NOT triggered (rule: data.atomicMetricsValue > 300) ===")

	capturedEmails = nil
	_, err = store.CreateAlert(ctx, database.AlertRow{
		Email:          "noalert@example.com",
		ProductID:      "test-product",
		TeamID:         "test-team",
		URL:            elementSrv.URL,
		Payload:        json.RawMessage(`{}`),
		AIAnalyticsURL: aiAnalyticsSrv.URL,
		Rule:           "data.atomicMetricsValue > 300",
	})
	if err != nil {
		t.Fatalf("create non-triggered alert: %v", err)
	}

	scheduler.Tick(ctx)

	mu.Lock()
	hasNoAlertEmail := true
	for _, e := range capturedEmails {
		if strings.Contains(string(e), "noalert@example.com") {
			hasNoAlertEmail = false
		}
	}
	mu.Unlock()
	check(t, "no email for non-triggered alert", hasNoAlertEmail, "got email for noalert@example.com")

	t.Log("=== Test: Alert with object atomicMetricsValue ===")

	capturedEmails = nil
	elementResponse.Store([]byte(`{"atomicMetricsValue": {"cycleTime": 200, "count": 5}}`))

	_, err = store.CreateAlert(ctx, database.AlertRow{
		Email:          "object@example.com",
		ProductID:      "test-product",
		TeamID:         "test-team",
		URL:            elementSrv.URL,
		Payload:        json.RawMessage(`{}`),
		AIAnalyticsURL: aiAnalyticsSrv.URL,
		Rule:           "data.atomicMetricsValue.cycleTime > 168",
	})
	if err != nil {
		t.Fatalf("create object alert: %v", err)
	}

	scheduler.Tick(ctx)

	mu.Lock()
	hasObjectEmail := false
	for _, e := range capturedEmails {
		if strings.Contains(string(e), "object@example.com") {
			hasObjectEmail = true
		}
	}
	mu.Unlock()
	check(t, "object rule triggered email", hasObjectEmail, "no email for object@example.com")
}

// ---------- REST API tests ----------

func TestRESTAPI(t *testing.T) {
	ctx := t.Context()
	store := database.NewMemStore()

	apiKey := "test-api-key-12345"
	apiKeyHash := database.HashAPIKey(apiKey)
	_, err := store.CreateAPIKey(ctx, "test-key", apiKeyHash, false, "")
	if err != nil {
		t.Errorf("create API key: %v", err)
	}

	adminKey := "admin-api-key-99999"
	adminKeyHash := database.HashAPIKey(adminKey)
	_, err = store.CreateAPIKey(ctx, "admin-key", adminKeyHash, true, "")
	if err != nil {
		t.Errorf("create admin API key: %v", err)
	}

	srv := server.New(0, false, fstest.MapFS{}, nil, store, nil, server.FrontendConfig{}, nil, server.AIAssistConfig{}, nil, nil, "")
	handler := srv.Handler()

	// --- Auth middleware ---

	t.Log("=== Test: Auth middleware ===")

	status, _ := doRequest(handler, "GET", "/api/v1/orgs", nil, "")
	check(t, "no api key → 401", status == 401, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "GET", "/api/v1/orgs", nil, "bogus-key")
	check(t, "invalid api key → 401", status == 401, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "GET", "/api/v1/orgs", nil, apiKey)
	check(t, "valid api key → 200", status == 200, fmt.Sprintf("status=%d", status))

	// Non-admin cannot mutate global orgs
	status, _ = doRequest(handler, "POST", "/api/v1/orgs",
		jsonBody(t, database.OrgRow{ID: "org-1", Name: "Org One"}), apiKey)
	check(t, "create org with non-admin key → 403", status == 403, fmt.Sprintf("status=%d", status))

	// --- Orgs CRUD (admin) ---

	t.Log("=== Test: Orgs CRUD ===")

	status, body := doRequest(handler, "POST", "/api/v1/orgs",
		jsonBody(t, database.OrgRow{ID: "org-1", Name: "Org One"}), adminKey)
	check(t, "create org → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))

	status, body = doRequest(handler, "GET", "/api/v1/orgs", nil, adminKey)
	check(t, "list orgs → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "list orgs contains org-1", strings.Contains(body, `"org-1"`), body)

	status, _ = doRequest(handler, "GET", "/api/v1/orgs/org-1", nil, apiKey)
	check(t, "unscoped api key cannot read org → 404", status == 404, fmt.Sprintf("status=%d", status))

	status, body = doRequest(handler, "GET", "/api/v1/orgs/org-1", nil, adminKey)
	check(t, "get org → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "get org has name", strings.Contains(body, `"Org One"`), body)

	status, _ = doRequest(handler, "PUT", "/api/v1/orgs/org-1",
		jsonBody(t, database.OrgRow{Name: "Org Updated"}), adminKey)
	check(t, "update org → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, body = doRequest(handler, "GET", "/api/v1/orgs/org-1", nil, adminKey)
	check(t, "get updated org → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "get updated org has name", strings.Contains(body, `"Org Updated"`), body)

	status, _ = doRequest(handler, "DELETE", "/api/v1/orgs/org-1", nil, adminKey)
	check(t, "delete org → 204", status == 204, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "GET", "/api/v1/orgs/org-1", nil, apiKey)
	check(t, "get deleted org → 404", status == 404, fmt.Sprintf("status=%d", status))

	// --- Products CRUD ---

	t.Log("=== Test: Products CRUD ===")

	status, _ = doRequest(handler, "POST", "/api/v1/products",
		jsonBody(t, database.ProductRow{ID: "prod-1", Name: "Product One"}), adminKey)
	check(t, "create product → 201", status == 201, fmt.Sprintf("status=%d", status))

	status, body = doRequest(handler, "GET", "/api/v1/products/prod-1", nil, adminKey)
	check(t, "get product → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "get product has name", strings.Contains(body, `"Product One"`), body)

	status, _ = doRequest(handler, "DELETE", "/api/v1/products/prod-1", nil, adminKey)
	check(t, "delete product → 204", status == 204, fmt.Sprintf("status=%d", status))

	// --- Teams CRUD ---

	t.Log("=== Test: Teams CRUD ===")

	status, _ = doRequest(handler, "POST", "/api/v1/teams",
		jsonBody(t, database.TeamRow{ID: "team-1", Name: "Team One", AuthGroup: "team-one-group"}), adminKey)
	check(t, "create team → 201", status == 201, fmt.Sprintf("status=%d", status))

	status, body = doRequest(handler, "GET", "/api/v1/teams/team-1", nil, adminKey)
	check(t, "get team → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "get team has name", strings.Contains(body, `"Team One"`), body)

	status, _ = doRequest(handler, "DELETE", "/api/v1/teams/team-1", nil, adminKey)
	check(t, "delete team → 204", status == 204, fmt.Sprintf("status=%d", status))

	// --- Relationships ---

	t.Log("=== Test: Relationships ===")

	if err := store.CreateOrg(ctx, database.OrgRow{ID: "rel-org", Name: "Rel Org"}); err != nil {
		t.Errorf("create org: %v", err)
	}
	if err := store.CreateProduct(ctx, database.ProductRow{ID: "rel-prod", Name: "Rel Prod"}); err != nil {
		t.Errorf("create product: %v", err)
	}
	if err := store.CreateTeam(ctx, database.TeamRow{ID: "rel-team", Name: "Rel Team"}); err != nil {
		t.Errorf("create team: %v", err)
	}

	status, _ = doRequest(handler, "PUT", "/api/v1/orgs/rel-org/products",
		jsonBody(t, []database.RelEntry{{ID: "rel-prod", SortOrder: 0}}), adminKey)
	check(t, "set org-product link → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, body = doRequest(handler, "GET", "/api/v1/orgs/rel-org/products", nil, adminKey)
	check(t, "get org-product links → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "link contains rel-prod", strings.Contains(body, `"rel-prod"`), body)

	status, _ = doRequest(handler, "PUT", "/api/v1/products/rel-prod/teams",
		jsonBody(t, []database.RelEntry{{ID: "rel-team", SortOrder: 0}}), adminKey)
	check(t, "set product-team link → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, body = doRequest(handler, "GET", "/api/v1/products/rel-prod/teams", nil, adminKey)
	check(t, "get product-team links → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "link contains rel-team", strings.Contains(body, `"rel-team"`), body)

	status, _ = doRequest(handler, "PUT", "/api/v1/teams/rel-team/product-labels",
		jsonBody(t, []string{"label-a", "label-b"}), adminKey)
	check(t, "set team product labels → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, body = doRequest(handler, "GET", "/api/v1/teams/rel-team/product-labels", nil, adminKey)
	check(t, "get team product labels → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "labels contain label-a", strings.Contains(body, `"label-a"`), body)

	// --- Entity config ---

	t.Log("=== Test: Entity config ===")

	status, _ = doRequest(handler, "PUT", "/api/v1/config/product/rel-prod",
		jsonBody(t, map[string]string{"maxAge": "30"}), adminKey)
	check(t, "put config → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, body = doRequest(handler, "GET", "/api/v1/config/product/rel-prod", nil, adminKey)
	check(t, "get config → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "config has maxAge", strings.Contains(body, `"maxAge"`), body)

	status, _ = doRequest(handler, "DELETE", "/api/v1/config/product/rel-prod", nil, adminKey)
	check(t, "delete config → 204", status == 204, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "GET", "/api/v1/config/badtype/id", nil, apiKey)
	check(t, "invalid entity type → 400", status == 400, fmt.Sprintf("status=%d", status))

	// --- Subscriptions CRUD ---

	t.Log("=== Test: Subscriptions CRUD ===")

	status, body = doRequest(handler, "POST", "/api/v1/subscriptions",
		jsonBody(t, map[string]any{
			"email":            "sub@example.com",
			"product_id":       "rel-prod",
			"ai_analytics_url": "http://example.com/ai",
			"elements":         []map[string]any{{"sort_order": 0, "url": "http://example.com", "payload": map[string]any{}}},
		}), adminKey)
	check(t, "create subscription → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))

	var createdSub struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &createdSub); err != nil {
		t.Errorf("unmarshal subscription: %v", err)
	}
	check(t, "subscription has auto ID", createdSub.ID > 0, fmt.Sprintf("id=%d", createdSub.ID))

	subPath := fmt.Sprintf("/api/v1/subscriptions/%d", createdSub.ID)

	status, body = doRequest(handler, "GET", subPath, nil, adminKey)
	check(t, "get subscription → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "subscription has email", strings.Contains(body, `"sub@example.com"`), body)
	check(t, "subscription has ai_analytics_url", strings.Contains(body, `"http://example.com/ai"`), body)

	status, _ = doRequest(handler, "PUT", subPath,
		jsonBody(t, map[string]any{
			"email":      "updated@example.com",
			"product_id": "rel-prod",
		}), adminKey)
	check(t, "update subscription → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "DELETE", subPath, nil, adminKey)
	check(t, "delete subscription → 204", status == 204, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "GET", subPath, nil, adminKey)
	check(t, "get deleted subscription → 404", status == 404, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "POST", "/api/v1/subscriptions",
		jsonBody(t, map[string]string{"product_id": "rel-prod"}), adminKey)
	check(t, "create sub missing email → 400", status == 400, fmt.Sprintf("status=%d", status))

	// --- Alerts CRUD ---

	t.Log("=== Test: Alerts CRUD ===")

	status, body = doRequest(handler, "POST", "/api/v1/alerts",
		jsonBody(t, database.AlertRow{
			Email:          "crud-alert@example.com",
			ProductID:      "rel-prod",
			TeamID:         "rel-team",
			URL:            "http://example.com/metrics",
			Payload:        json.RawMessage(`{}`),
			AIAnalyticsURL: "http://example.com/ai",
			Rule:           "data.value > 100",
		}), adminKey)
	check(t, "create alert → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))

	var createdAlert struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &createdAlert); err != nil {
		t.Errorf("unmarshal alert: %v", err)
	}
	check(t, "alert has auto ID", createdAlert.ID > 0, fmt.Sprintf("id=%d", createdAlert.ID))

	alertPath := fmt.Sprintf("/api/v1/alerts/%d", createdAlert.ID)

	status, body = doRequest(handler, "GET", alertPath, nil, adminKey)
	check(t, "get alert → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "alert has rule", strings.Contains(body, `data.value`) && strings.Contains(body, `100`), body)

	status, body = doRequest(handler, "GET", "/api/v1/alerts", nil, adminKey)
	check(t, "list alerts → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "list contains alert", strings.Contains(body, `"crud-alert@example.com"`), body)

	status, _ = doRequest(handler, "PUT", alertPath,
		jsonBody(t, database.AlertRow{
			Email:          "updated-alert@example.com",
			URL:            "http://example.com/metrics",
			AIAnalyticsURL: "http://example.com/ai",
			Rule:           "data.value > 200",
		}), adminKey)
	check(t, "update alert → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "DELETE", alertPath, nil, adminKey)
	check(t, "delete alert → 204", status == 204, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "GET", alertPath, nil, adminKey)
	check(t, "get deleted alert → 404", status == 404, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "POST", "/api/v1/alerts",
		jsonBody(t, map[string]string{"email": "x@x.com"}), adminKey)
	check(t, "create alert missing fields → 400", status == 400, fmt.Sprintf("status=%d", status))

	// --- Dashboards CRUD ---

	t.Log("=== Test: Dashboards CRUD ===")

	status, body = doRequest(handler, "POST", "/api/v1/dashboards",
		jsonBody(t, map[string]any{
			"owner_type":   "product",
			"owner_id":     "rel-prod",
			"dashboard_id": "dash-1",
			"type":         "metrics",
			"title":        "Test Dashboard",
		}), adminKey)
	check(t, "create dashboard → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))

	var createdDash struct {
		AutoID int64 `json:"auto_id"`
	}
	if err := json.Unmarshal([]byte(body), &createdDash); err != nil {
		t.Errorf("unmarshal dashboard: %v", err)
	}
	check(t, "dashboard has auto ID", createdDash.AutoID > 0, fmt.Sprintf("auto_id=%d", createdDash.AutoID))

	dashPath := fmt.Sprintf("/api/v1/dashboards/%d", createdDash.AutoID)

	status, body = doRequest(handler, "GET", dashPath, nil, adminKey)
	check(t, "get dashboard → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "dashboard has title", strings.Contains(body, `"Test Dashboard"`), body)

	status, body = doRequest(handler, "GET", "/api/v1/dashboards?owner_type=product&owner_id=rel-prod", nil, adminKey)
	check(t, "list dashboards → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "list contains dashboard", strings.Contains(body, `"dash-1"`), body)

	status, _ = doRequest(handler, "DELETE", dashPath, nil, adminKey)
	check(t, "delete dashboard → 204", status == 204, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "POST", "/api/v1/dashboards",
		jsonBody(t, map[string]string{"owner_type": "product"}), adminKey)
	check(t, "create dashboard missing fields → 400", status == 400, fmt.Sprintf("status=%d", status))

	// --- Sources CRUD ---

	t.Log("=== Test: Sources CRUD ===")

	status, body = doRequest(handler, "POST", "/api/v1/teams/rel-team/sources",
		jsonBody(t, database.SourceRow{ID: "src-1", Type: "github"}), adminKey)
	check(t, "create source → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))

	status, body = doRequest(handler, "GET", "/api/v1/teams/rel-team/sources/src-1", nil, adminKey)
	check(t, "get source → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "source has type", strings.Contains(body, `"github"`), body)

	status, _ = doRequest(handler, "DELETE", "/api/v1/teams/rel-team/sources/src-1", nil, adminKey)
	check(t, "delete source → 204", status == 204, fmt.Sprintf("status=%d", status))

	// --- Overrides ---

	t.Log("=== Test: Overrides ===")

	status, _ = doRequest(handler, "PUT", "/api/v1/products/rel-prod/overrides",
		jsonBody(t, []database.OverrideRepoRow{{ID: "repo-1", Type: "github", Owner: "owner", Name: "repo", Teams: []string{"rel-team"}}}), adminKey)
	check(t, "put overrides → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, body = doRequest(handler, "GET", "/api/v1/products/rel-prod/overrides", nil, adminKey)
	check(t, "get overrides → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "overrides contain repo-1", strings.Contains(body, `"repo-1"`), body)

	// --- API keys (admin auth) ---

	t.Log("=== Test: API keys (admin auth) ===")

	status, _ = doRequest(handler, "GET", "/api/v1/keys", nil, apiKey)
	check(t, "list keys with non-admin → 403", status == 403, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "GET", "/api/v1/keys", nil, adminKey)
	check(t, "list keys with admin → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, body = doRequest(handler, "POST", "/api/v1/keys",
		jsonBody(t, map[string]any{"name": "new-key", "is_admin": false}), adminKey)
	check(t, "create key → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))
	check(t, "response contains raw key", strings.Contains(body, `"key"`), body)

	var createdKey struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &createdKey); err != nil {
		t.Errorf("unmarshal key: %v", err)
	}

	keyPath := fmt.Sprintf("/api/v1/keys/%d", createdKey.ID)
	status, _ = doRequest(handler, "DELETE", keyPath, nil, adminKey)
	check(t, "delete key → 204", status == 204, fmt.Sprintf("status=%d", status))

	// --- Canned Queries CRUD ---

	t.Log("=== Test: Canned Queries CRUD ===")

	status, body = doRequest(handler, "POST", "/api/v1/canned-queries",
		jsonBody(t, database.CannedQueryRow{
			Name:        "Test Query",
			Description: "A test canned query",
			Category:    "Testing",
			SQLText:     "SELECT COUNT(*) FROM repos WHERE owner = ?",
			Parameters:  json.RawMessage(`[{"name":"owner","type":"string","description":"Repo owner","default":"myorg"}]`),
			Version:     1,
		}), adminKey)
	check(t, "create canned query → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))

	var createdQuery struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &createdQuery); err != nil {
		t.Errorf("unmarshal canned query: %v", err)
	}
	check(t, "canned query has auto ID", createdQuery.ID > 0, fmt.Sprintf("id=%d", createdQuery.ID))

	queryPath := fmt.Sprintf("/api/v1/canned-queries/%d", createdQuery.ID)

	status, body = doRequest(handler, "GET", queryPath, nil, apiKey)
	check(t, "get canned query → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "canned query has name", strings.Contains(body, `"Test Query"`), body)
	check(t, "canned query has sql", strings.Contains(body, `SELECT COUNT`), body)
	check(t, "canned query has parameters", strings.Contains(body, `"owner"`), body)

	status, body = doRequest(handler, "GET", "/api/v1/canned-queries", nil, apiKey)
	check(t, "list canned queries → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "list contains query", strings.Contains(body, `"Test Query"`), body)

	status, _ = doRequest(handler, "PUT", queryPath,
		jsonBody(t, database.CannedQueryRow{
			Name:     "Updated Query",
			Category: "Testing",
			SQLText:  "SELECT * FROM repos",
			Version:  2,
		}), adminKey)
	check(t, "update canned query → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, body = doRequest(handler, "GET", queryPath, nil, apiKey)
	check(t, "get updated query → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "get updated query has new name", strings.Contains(body, `"Updated Query"`), body)
	check(t, "get updated query has version 2", strings.Contains(body, `"version":2`), body)

	status, _ = doRequest(handler, "DELETE", queryPath, nil, adminKey)
	check(t, "delete canned query → 204", status == 204, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "GET", queryPath, nil, apiKey)
	check(t, "get deleted canned query → 404", status == 404, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "POST", "/api/v1/canned-queries",
		jsonBody(t, map[string]string{"category": "x"}), adminKey)
	check(t, "create query missing fields → 400", status == 400, fmt.Sprintf("status=%d", status))

	// --- Canned Scripts CRUD ---

	t.Log("=== Test: Canned Scripts CRUD ===")

	status, body = doRequest(handler, "POST", "/api/v1/canned-scripts",
		jsonBody(t, database.CannedScriptRow{
			Name:        "Test Script",
			Description: "A test JS fragment",
			Category:    "Testing",
			JSText:      "return data.map(d => d.value * 2);",
			Version:     1,
		}), adminKey)
	check(t, "create canned script → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))

	var createdScript struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &createdScript); err != nil {
		t.Errorf("unmarshal canned script: %v", err)
	}
	check(t, "canned script has auto ID", createdScript.ID > 0, fmt.Sprintf("id=%d", createdScript.ID))

	scriptPath := fmt.Sprintf("/api/v1/canned-scripts/%d", createdScript.ID)

	status, body = doRequest(handler, "GET", scriptPath, nil, apiKey)
	check(t, "get canned script → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "canned script has name", strings.Contains(body, `"Test Script"`), body)
	check(t, "canned script has js", strings.Contains(body, `data.map`), body)

	status, body = doRequest(handler, "GET", "/api/v1/canned-scripts", nil, apiKey)
	check(t, "list canned scripts → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "list contains script", strings.Contains(body, `"Test Script"`), body)

	status, _ = doRequest(handler, "PUT", scriptPath,
		jsonBody(t, database.CannedScriptRow{
			Name:     "Updated Script",
			Category: "Testing",
			JSText:   "return data.filter(d => d.active);",
			Version:  2,
		}), adminKey)
	check(t, "update canned script → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, body = doRequest(handler, "GET", scriptPath, nil, apiKey)
	check(t, "get updated script has new name", strings.Contains(body, `"Updated Script"`), body)
	check(t, "get updated script has version 2", strings.Contains(body, `"version":2`), body)
	check(t, "get updated script → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "DELETE", scriptPath, nil, adminKey)
	check(t, "delete canned script → 204", status == 204, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "GET", scriptPath, nil, apiKey)
	check(t, "get deleted canned script → 404", status == 404, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "POST", "/api/v1/canned-scripts",
		jsonBody(t, map[string]string{"category": "x"}), adminKey)
	check(t, "create script missing fields → 400", status == 400, fmt.Sprintf("status=%d", status))

	// --- Workflows CRUD ---

	t.Log("=== Test: Workflows CRUD ===")

	status, body = doRequest(handler, "POST", "/api/v1/workflows",
		jsonBody(t, database.WorkflowRow{
			Name:         "test-workflow",
			Description:  "A test workflow",
			InputScript:  "({name: input.name || 'default'})",
			OutputScript: "({result: results})",
			Version:      1,
			Queries: []database.WorkflowQueryRow{
				{SortOrder: 0, Label: "count", SQLText: "SELECT COUNT(*) AS cnt FROM orgs WHERE name = :name"},
			},
		}), adminKey)
	check(t, "create workflow → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))

	var createdWorkflow struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &createdWorkflow); err != nil {
		t.Errorf("unmarshal workflow: %v", err)
	}
	check(t, "workflow has auto ID", createdWorkflow.ID > 0, fmt.Sprintf("id=%d", createdWorkflow.ID))

	workflowPath := fmt.Sprintf("/api/v1/workflows/%d", createdWorkflow.ID)

	status, body = doRequest(handler, "GET", workflowPath, nil, apiKey)
	check(t, "get workflow → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "workflow has name", strings.Contains(body, `"test-workflow"`), body)
	check(t, "workflow has input_script", strings.Contains(body, `input.name`), body)
	check(t, "workflow has queries", strings.Contains(body, `"count"`), body)
	check(t, "workflow has named param in sql", strings.Contains(body, `:name`), body)

	status, body = doRequest(handler, "GET", "/api/v1/workflows", nil, apiKey)
	check(t, "list workflows → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "list contains workflow", strings.Contains(body, `"test-workflow"`), body)

	status, _ = doRequest(handler, "PUT", workflowPath,
		jsonBody(t, database.WorkflowRow{
			Name:         "test-workflow-updated",
			InputScript:  "({x: 1})",
			OutputScript: "({y: 2})",
			Version:      2,
			Queries: []database.WorkflowQueryRow{
				{SortOrder: 0, Label: "q1", SQLText: "SELECT 1"},
				{SortOrder: 1, Label: "q2", SQLText: "SELECT 2"},
			},
		}), adminKey)
	check(t, "update workflow → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, body = doRequest(handler, "GET", workflowPath, nil, apiKey)
	check(t, "get updated workflow → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "get updated workflow has new name", strings.Contains(body, `"test-workflow-updated"`), body)
	check(t, "get updated workflow has version 2", strings.Contains(body, `"version":2`), body)
	check(t, "get updated workflow has 2 queries", strings.Contains(body, `"q2"`), body)

	// Duplicate name
	status, _ = doRequest(handler, "POST", "/api/v1/workflows",
		jsonBody(t, database.WorkflowRow{
			Name:         "test-workflow-updated",
			InputScript:  "({x: 1})",
			OutputScript: "({y: 2})",
			Version:      1,
		}), adminKey)
	check(t, "create duplicate workflow → 409", status == 409, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "POST", workflowPath+"/archive", nil, adminKey)
	check(t, "archive workflow → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "DELETE", workflowPath, nil, adminKey)
	check(t, "delete workflow → 204", status == 204, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "GET", workflowPath, nil, apiKey)
	check(t, "get deleted workflow → 404", status == 404, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "POST", "/api/v1/workflows",
		jsonBody(t, map[string]string{"description": "x"}), adminKey)
	check(t, "create workflow missing fields → 400", status == 400, fmt.Sprintf("status=%d", status))

	// --- Workflow export/import ---

	t.Log("=== Test: Workflow export/import ===")

	// Create a workflow with test cases referencing a JSON schema
	status, body = doRequest(handler, "POST", "/api/v1/json-schemas",
		jsonBody(t, database.JsonSchemaRow{
			Name:       "export-test-schema",
			JsonSchema: `{"type":"object"}`,
		}), adminKey)
	check(t, "create json schema for export → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))

	var createdSchema struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &createdSchema); err != nil {
		t.Errorf("unmarshal schema: %v", err)
	}

	status, body = doRequest(handler, "POST", "/api/v1/workflows",
		jsonBody(t, database.WorkflowRow{
			Name:         "export-test-workflow",
			Description:  "Workflow for export testing",
			InputScript:  "({x: input.x})",
			OutputScript: "({y: results})",
			TestInput:    `{"x": 1}`,
			Tests: []database.WorkflowTestCase{
				{Name: "basic", Input: `{"x":1}`, AssertScript: "true", SchemaID: createdSchema.ID},
			},
			Version: 0,
			Queries: []database.WorkflowQueryRow{
				{Stage: 0, SortOrder: 0, Label: "q1", SQLText: "SELECT 1"},
			},
		}), adminKey)
	check(t, "create workflow for export → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))

	var exportWf struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &exportWf); err != nil {
		t.Errorf("unmarshal export workflow: %v", err)
	}
	exportWfPath := fmt.Sprintf("/api/v1/workflows/%d", exportWf.ID)

	// Create a stored procedure for this workflow
	status, _ = doRequest(handler, "POST", "/api/v1/stored-procedures",
		jsonBody(t, database.StoredProcedureRow{
			WorkflowID:  exportWf.ID,
			Name:        "export_test_proc",
			Description: "Test procedure",
			SQLText:     "CREATE PROCEDURE export_test_proc() BEGIN SELECT 1; END",
		}), adminKey)
	check(t, "create stored proc for export → 201", status == 201, fmt.Sprintf("status=%d", status))

	// Export the workflow
	status, body = doRequest(handler, "GET", exportWfPath+"/export", nil, adminKey)
	check(t, "export workflow → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "export has format_version", strings.Contains(body, `"format_version":1`), body)
	check(t, "export has workflow name", strings.Contains(body, `"export-test-workflow"`), body)
	check(t, "export has stored procedures", strings.Contains(body, `"export_test_proc"`), body)
	check(t, "export has json schemas", strings.Contains(body, `"export-test-schema"`), body)
	check(t, "export has queries", strings.Contains(body, `"q1"`), body)
	check(t, "export strips workflow id", !strings.Contains(body, `"id":`+fmt.Sprintf("%d", exportWf.ID)), body)

	// Non-admin cannot export
	status, _ = doRequest(handler, "GET", exportWfPath+"/export", nil, apiKey)
	check(t, "non-admin export → 403", status == 403, fmt.Sprintf("status=%d", status))

	// Export non-existent workflow
	status, _ = doRequest(handler, "GET", "/api/v1/workflows/99999/export", nil, adminKey)
	check(t, "export non-existent → 404", status == 404, fmt.Sprintf("status=%d", status))

	// Archive and delete the original workflow before importing
	status, _ = doRequest(handler, "POST", exportWfPath+"/archive", nil, adminKey)
	check(t, "archive workflow before import → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "DELETE", exportWfPath, nil, adminKey)
	check(t, "delete workflow before import → 204", status == 204, fmt.Sprintf("status=%d", status))

	// Import the exported bundle
	status, body = doRequest(handler, "POST", "/api/v1/workflows/import",
		strings.NewReader(body), adminKey)
	check(t, "import workflow → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))
	check(t, "import response has workflow_id", strings.Contains(body, `"workflow_id"`), body)
	check(t, "import response has workflow_name", strings.Contains(body, `"export-test-workflow"`), body)

	var importResp struct {
		WorkflowID int64 `json:"workflow_id"`
	}
	if err := json.Unmarshal([]byte(body), &importResp); err != nil {
		t.Errorf("unmarshal import response: %v", err)
	}
	check(t, "imported workflow has ID", importResp.WorkflowID > 0, fmt.Sprintf("id=%d", importResp.WorkflowID))

	// Verify the imported workflow exists and has correct data
	importedPath := fmt.Sprintf("/api/v1/workflows/%d", importResp.WorkflowID)
	status, body = doRequest(handler, "GET", importedPath, nil, adminKey)
	check(t, "get imported workflow → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "imported workflow has name", strings.Contains(body, `"export-test-workflow"`), body)
	check(t, "imported workflow has queries", strings.Contains(body, `"q1"`), body)
	check(t, "imported workflow has test cases", strings.Contains(body, `"basic"`), body)

	// Import duplicate name → 409
	status, _ = doRequest(handler, "POST", "/api/v1/workflows/import",
		jsonBody(t, map[string]any{
			"format_version": 1,
			"workflow": map[string]any{
				"name":          "export-test-workflow",
				"input_script":  "({x: 1})",
				"output_script": "({y: 2})",
			},
		}), adminKey)
	check(t, "import duplicate name → 409", status == 409, fmt.Sprintf("status=%d", status))

	// Import bad format version → 400
	status, _ = doRequest(handler, "POST", "/api/v1/workflows/import",
		jsonBody(t, map[string]any{
			"format_version": 99,
			"workflow":       map[string]any{"name": "x", "input_script": "x", "output_script": "x"},
		}), adminKey)
	check(t, "import bad format version → 400", status == 400, fmt.Sprintf("status=%d", status))

	// Import missing required fields → 400
	status, _ = doRequest(handler, "POST", "/api/v1/workflows/import",
		jsonBody(t, map[string]any{
			"format_version": 1,
			"workflow":       map[string]any{"name": ""},
		}), adminKey)
	check(t, "import missing fields → 400", status == 400, fmt.Sprintf("status=%d", status))

	// Non-admin cannot import
	status, _ = doRequest(handler, "POST", "/api/v1/workflows/import",
		jsonBody(t, map[string]any{
			"format_version": 1,
			"workflow": map[string]any{
				"name":          "admin-only-wf",
				"input_script":  "({x: 1})",
				"output_script": "({y: 2})",
			},
		}), apiKey)
	check(t, "non-admin import → 403", status == 403, fmt.Sprintf("status=%d", status))

	// Clean up
	status, _ = doRequest(handler, "POST", importedPath+"/archive", nil, adminKey)
	check(t, "archive imported workflow → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "DELETE", importedPath, nil, adminKey)
	check(t, "delete imported workflow → 204", status == 204, fmt.Sprintf("status=%d", status))

	// --- Bad JSON ---

	t.Log("=== Test: Bad JSON handling ===")

	status, _ = doRequest(handler, "POST", "/api/v1/orgs", strings.NewReader("{invalid"), adminKey)
	check(t, "bad JSON → 400", status == 400, fmt.Sprintf("status=%d", status))

	// --- Health endpoint ---

	t.Log("=== Test: Health endpoint ===")

	status, _ = doRequest(handler, "GET", "/health/live", nil, "")
	check(t, "/health/live → 200", status == 200, fmt.Sprintf("status=%d", status))

	status, _ = doRequest(handler, "GET", "/health", nil, "")
	check(t, "/health → 404", status == 404, fmt.Sprintf("status=%d", status))
}

func TestRESTAPI_ForwardedIdentity(t *testing.T) {
	store := database.NewMemStore()
	srv := server.New(0, false, fstest.MapFS{}, nil, store, nil, server.FrontendConfig{
		AdminUsers: []string{"alice"},
	}, nil, server.AIAssistConfig{}, nil, nil, "")
	handler := srv.Handler()

	t.Log("=== Test: forwarded identity authenticates without an API key ===")

	status, _ := doRequestWithHeaders(handler, "GET", "/api/v1/orgs", nil, map[string]string{
		"X-Forwarded-User": "bob",
	})
	check(t, "X-Forwarded-User present, no API key → 200 for read", status == 200, fmt.Sprintf("status=%d", status))

	// Non-admin bob cannot mutate orgs
	status, _ = doRequestWithHeaders(handler, "POST", "/api/v1/orgs",
		jsonBody(t, database.OrgRow{ID: "org-bob", Name: "Bob Org"}),
		map[string]string{"X-Forwarded-User": "bob"},
	)
	check(t, "non-admin forwarded user POST /orgs → 403", status == 403, fmt.Sprintf("status=%d", status))

	// Admin alice can mutate orgs
	status, body := doRequestWithHeaders(handler, "POST", "/api/v1/orgs",
		jsonBody(t, database.OrgRow{ID: "org-alice", Name: "Alice Org"}),
		map[string]string{"X-Forwarded-User": "alice", "X-Forwarded-Email": "alice@example.com"},
	)
	check(t, "admin forwarded user POST /orgs → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))

	t.Log("=== Test: auth/me returns rich identity ===")
	status, body = doRequestWithHeaders(handler, "GET", "/api/v1/auth/me", nil, map[string]string{
		"X-Forwarded-User":  "alice",
		"X-Forwarded-Email": "alice@example.com",
	})
	check(t, "/api/v1/auth/me → 200", status == 200, fmt.Sprintf("status=%d", status))
	check(t, "auth/me is_admin true", strings.Contains(body, `"is_admin":true`), body)

	t.Log("=== Test: subscription create auto-fills email from X-Forwarded-Email ===")

	status, body = doRequestWithHeaders(handler, "POST", "/api/v1/subscriptions",
		jsonBody(t, map[string]any{"interval_days": 7}),
		map[string]string{"X-Forwarded-User": "alice", "X-Forwarded-Email": "alice@example.com"},
	)
	check(t, "create subscription without email in body → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))
	check(t, "created subscription has email from header", strings.Contains(body, `"alice@example.com"`), body)

	t.Log("=== Test: known identity with no forwarded email still requires email in body ===")

	status, body = doRequestWithHeaders(handler, "POST", "/api/v1/subscriptions",
		jsonBody(t, map[string]any{"interval_days": 7}),
		map[string]string{"X-Forwarded-User": "bob"},
	)
	check(t, "create subscription with no email anywhere → 400", status == 400, fmt.Sprintf("status=%d body=%s", status, body))

	t.Log("=== Test: alert create auto-fills email from X-Forwarded-Email ===")

	status, body = doRequestWithHeaders(handler, "POST", "/api/v1/alerts",
		jsonBody(t, map[string]any{"url": "https://example.com/metrics", "rule": "data.value > 1"}),
		map[string]string{"X-Forwarded-User": "alice", "X-Forwarded-Email": "alice@example.com"},
	)
	check(t, "create alert without email in body → 201", status == 201, fmt.Sprintf("status=%d body=%s", status, body))
	check(t, "created alert has email from header", strings.Contains(body, `"alice@example.com"`), body)

	t.Log("=== Test: no identity, no API key still 401s ===")

	status, _ = doRequestWithHeaders(handler, "GET", "/api/v1/orgs", nil, nil)
	check(t, "no identity, no API key → 401", status == 401, fmt.Sprintf("status=%d", status))
}

// ---------- Helpers ----------

func doRequest(handler http.Handler, method, path string, body io.Reader, apiKey string) (int, string) {
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func doRequestWithHeaders(handler http.Handler, method, path string, body io.Reader, headers map[string]string) (int, string) {
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func jsonBody(t *testing.T, v any) io.Reader {
	b, err := json.Marshal(v)
	if err != nil {
		t.Errorf("json marshal: %v", err)
	}
	return bytes.NewReader(b)
}

func check(t *testing.T, name string, ok bool, detail string) {
	t.Helper()
	if !ok {
		msg := name
		if detail != "" {
			msg += " (" + detail + ")"
		}
		t.Error(msg)
	}
}
