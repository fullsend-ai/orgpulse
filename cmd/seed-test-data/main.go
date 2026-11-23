package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/fullsend-ai/orgpulse/internal/database"
)

func main() {
	host := envOr("DASHBOARD_DB_HOST", "localhost")
	port, _ := strconv.Atoi(envOr("DASHBOARD_DB_PORT", "3306"))
	user := requireEnv("DASHBOARD_DB_USER")
	pass := requireEnv("DASHBOARD_DB_PASSWORD")
	name := requireEnv("DASHBOARD_DB_NAME")
	tlsCABundlePath := envOr("DASHBOARD_DB_TLS_CA_BUNDLE_PATH", "")

	store, err := database.NewMySQLStore(host, port, user, pass, name, tlsCABundlePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database connection failed: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := store.ClearConfig(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to clear config: %v\n", err)
		os.Exit(1)
	}

	elementURL := envOr("TEST_ELEMENT_URL", "http://localhost:9999/metrics-data")

	if err := store.CreateOrg(ctx, database.OrgRow{ID: "test-org", Name: "Test Org"}); err != nil {
		fatal("create org", err)
	}
	if err := store.CreateProduct(ctx, database.ProductRow{ID: "test-product", Name: "Test Product"}); err != nil {
		fatal("create product", err)
	}
	if err := store.CreateTeam(ctx, database.TeamRow{ID: "test-team", Name: "Test Team"}); err != nil {
		fatal("create team", err)
	}

	if err := store.ReplaceOrgProductLinks(ctx, "test-org", []database.RelEntry{{ID: "test-product", SortOrder: 0}}); err != nil {
		fatal("link org->product", err)
	}
	if err := store.ReplaceProductTeamLinks(ctx, "test-product", []database.RelEntry{{ID: "test-team", SortOrder: 0}}); err != nil {
		fatal("link product->team", err)
	}

	if err := store.ReplaceEntityConfig(ctx, "product", "test-product", map[string]string{"cycleTimeSlowHours": "168"}); err != nil {
		fatal("product config", err)
	}
	if err := store.ReplaceEntityConfig(ctx, "team", "test-team", map[string]string{"cycleTimeCriticalHours": "336"}); err != nil {
		fatal("team config", err)
	}

	// Dashboards owned by test-team (used by team-dashboard-report workflow)
	span6 := 6
	db1ID, err := store.CreateDashboard(ctx, database.DashboardRow{
		OwnerType:   "team",
		OwnerID:     "test-team",
		DashboardID: "engineering-velocity",
		Type:        "engineering-velocity",
		Title:       "Engineering Velocity",
		Description: "Cycle time, throughput, and deployment frequency metrics.",
		Diagrams: []database.DiagramRow{
			{Position: 0, Type: "json", Endpoint: elementURL, Title: "Cycle Time", Span: &span6},
			{Position: 1, Type: "json", Endpoint: elementURL, Title: "Throughput", Span: &span6},
		},
	})
	if err != nil {
		fatal("create dashboard engineering-velocity", err)
	}
	db2ID, err := store.CreateDashboard(ctx, database.DashboardRow{
		OwnerType:   "team",
		OwnerID:     "test-team",
		DashboardID: "quality-overview",
		Type:        "quality-overview",
		Title:       "Quality Overview",
		Description: "Test coverage, defect rates, and code quality trends.",
		Diagrams: []database.DiagramRow{
			{Position: 0, Type: "json", Endpoint: elementURL, Title: "Test Coverage"},
			{Position: 1, Type: "json", Endpoint: elementURL, Title: "Defect Rate"},
			{Position: 2, Type: "json", Endpoint: elementURL, Title: "Code Smells"},
		},
	})
	if err != nil {
		fatal("create dashboard quality-overview", err)
	}
	db3ID, err := store.CreateDashboard(ctx, database.DashboardRow{
		OwnerType:   "team",
		OwnerID:     "test-team",
		DashboardID: "prcycletime",
		Type:        "prcycletime",
		Title:       "PR Cycle Time",
		Description: "Key performance indicators and metrics of your team's pull requests.",
		Diagrams: []database.DiagramRow{
			{Position: 0, Type: "json", Endpoint: "/workflows/production/prkeymetrics", Title: "Key Metrics", AIAnalytics: true, Description: "Throughput, median cycle time, opened PRs, and outlier count for the selected time range.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#pr-cycle-time"},
			{Position: 1, Type: "json", Endpoint: "/workflows/production/prstages", Title: "Where PRs Spend Their Time", AIAnalytics: true, Description: "Percentage of average cycle time spent in pickup, review, and integration stages.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#pr-cycle-time"},
			{Position: 2, Type: "json", Endpoint: "/workflows/production/prproductivity", Title: "PR Productivity", AIAnalytics: true, Description: "Ratio of merged PRs to total closed PRs, measuring work that reaches production.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#pr-cycle-time"},
			{Position: 3, Type: "json", Endpoint: "/workflows/production/prflow", Title: "PR Stage Flow", AIAnalytics: true, Description: "How PRs move through their lifecycle from creation to merge or close.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#pr-cycle-time"},
			{Position: 4, Type: "json", Endpoint: "/workflows/production/przscore", Title: "PR Cycle Time Z-Score", AIAnalytics: true, Description: "Z-score of PR cycle time over a 90-day baseline. Green is fast, gray is average, red is slow.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#pr-cycle-time"},
			{Position: 5, Type: "json", Endpoint: "/workflows/production/prcycletimescatter", Title: "PR Cycle Time Distribution", AIAnalytics: true, Description: "Distribution of PR cycle times across time buckets for merged and abandoned PRs.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#pr-cycle-time"},
		},
	})
	if err != nil {
		fatal("create dashboard prcycletime", err)
	}

	db4ID, err := store.CreateDashboard(ctx, database.DashboardRow{
		OwnerType:   "team",
		OwnerID:     "test-team",
		DashboardID: "issuecycletime",
		Type:        "issuecycletime",
		Title:       "Issue Cycle Time",
		Description: "Key performance indicators and metrics of your team's issues, stories, and epics.",
		Diagrams: []database.DiagramRow{
			{Position: 0, Type: "json", Endpoint: "/workflows/production/issuekeymetrics", Title: "Key Metrics", AIAnalytics: true, Description: "Total resolved, median resolution time, created, open, and story points.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#issue-cycle-time"},
			{Position: 1, Type: "json", Endpoint: "/workflows/production/issuemediancycletime", Title: "Median Resolution Time (hours)", AIAnalytics: true, Description: "Daily median issue resolution time with 7-day rolling average.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#issue-cycle-time"},
			{Position: 2, Type: "json", Endpoint: "/workflows/production/issuezscore", Title: "Issue Z-Score (resolution time)", AIAnalytics: true, Description: "Z-score of issue resolution time over a 90-day baseline.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#issue-cycle-time"},
			{Position: 3, Type: "json", Endpoint: "/workflows/production/issuestages", Title: "Issue Stage Breakdown", AIAnalytics: true, Description: "Distribution of resolved issues across workflow stages.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#issue-cycle-time"},
			{Position: 4, Type: "json", Endpoint: "/workflows/production/issuetypebreakdown", Title: "Issue Type Breakdown", AIAnalytics: true, Description: "Distribution of resolved issues by type (Bug, Story, Task, etc.).", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#issue-cycle-time"},
			{Position: 5, Type: "json", Endpoint: "/workflows/production/issuethroughput", Title: "Issue Throughput", AIAnalytics: true, Description: "Daily count of created and resolved issues over time.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#issue-cycle-time"},
		},
	})
	if err != nil {
		fatal("create dashboard issuecycletime", err)
	}

	db5ID, err := store.CreateDashboard(ctx, database.DashboardRow{
		OwnerType:   "team",
		OwnerID:     "test-team",
		DashboardID: "ftpr",
		Type:        "ftpr",
		Title:       "First Time Pass Rate",
		Description: "First time pass rate metrics for CI pipelines.",
		Diagrams: []database.DiagramRow{
			{Position: 0, Type: "json", Endpoint: "/workflows/production/ftprkeymetrics", Title: "FTPR Key Metrics", AIAnalytics: true, Description: "FTPR percentage, total merged PRs, first-time passes, and CI coverage.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#first-time-pass-rate"},
			{Position: 1, Type: "json", Endpoint: "/workflows/production/ftprovertime", Title: "FTPR Over Time", AIAnalytics: true, Description: "Weekly FTPR trend over the selected time range.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#first-time-pass-rate"},
			{Position: 2, Type: "json", Endpoint: "/workflows/production/ftprpassfail", Title: "Pass / Fail Breakdown", AIAnalytics: true, Description: "Doughnut chart showing pass vs fail distribution.", DocumentationURL: "https://github.com/fullsend-ai/orgpulse/blob/main/docs/orgpulse-user-guide.md#first-time-pass-rate"},
		},
	})
	if err != nil {
		fatal("create dashboard ftpr", err)
	}

	// Sources for test-team (used by team-details and team-dashboard-report workflows)
	if err := store.CreateSource(ctx, "test-team", database.SourceRow{
		ID:     "platform-repos",
		Type:   "gitrepos",
		Config: json.RawMessage(`{"org": "test-org", "prefix": "platform-"}`),
		Repos: []database.RepoRow{
			{ID: "platform-api", Type: "github", Owner: "test-org", Name: "platform-api"},
			{ID: "platform-ui", Type: "github", Owner: "test-org", Name: "platform-ui"},
		},
	}); err != nil {
		fatal("create source platform-repos", err)
	}
	if err := store.CreateSource(ctx, "test-team", database.SourceRow{
		ID:     "backlog",
		Type:   "workitems",
		Config: json.RawMessage(`{"project": "PLATFORM", "board": "Sprint Board"}`),
		Projects: []database.WorkitemProjRow{
			{ID: "platform-jira", ProjectName: "Platform", JiraProject: "PLATFORM", BoardID: "10", Type: "jira"},
		},
	}); err != nil {
		fatal("create source backlog", err)
	}

	intervalDays := 1
	periodicID, err := store.CreateSubscription(ctx, database.SubscriptionRow{
		Email:          "test@example.com",
		ProductID:      "test-product",
		TeamID:         "test-team",
		AIAnalyticsURL: elementURL,
		IntervalDays:   &intervalDays,
		Elements: []database.SubscriptionElementRow{
			{SortOrder: 0, URL: elementURL, Payload: json.RawMessage(`{}`)},
		},
	})
	if err != nil {
		fatal("create periodic subscription", err)
	}

	alertID, err := store.CreateAlert(ctx, database.AlertRow{
		Email:          "alert@example.com",
		ProductID:      "test-product",
		TeamID:         "test-team",
		URL:            elementURL,
		Payload:        json.RawMessage(`{}`),
		AIAnalyticsURL: elementURL,
		Rule:           "data.atomicMetricsValue > 100",
	})
	if err != nil {
		fatal("create alert", err)
	}

	seedCannedEntries(ctx, store)
	seedJsonSchemas(ctx, store)
	wfIDs := seedWorkflows(ctx, store)
	seedStoredProcedures(ctx, store, wfIDs)

	fmt.Printf("test data created:\n")
	fmt.Printf("  org:                   test-org\n")
	fmt.Printf("  product:               test-product\n")
	fmt.Printf("  team:                  test-team\n")
	fmt.Printf("  dashboard:             engineering-velocity (id=%d, 2 diagrams)\n", db1ID)
	fmt.Printf("  dashboard:             quality-overview (id=%d, 3 diagrams)\n", db2ID)
	fmt.Printf("  dashboard:             prcycletime (id=%d, 6 diagrams)\n", db3ID)
	fmt.Printf("  dashboard:             issuecycletime (id=%d, 6 diagrams)\n", db4ID)
	fmt.Printf("  dashboard:             ftpr (id=%d, 3 diagrams)\n", db5ID)
	fmt.Printf("  source:                platform-repos (gitrepos, 2 repos)\n")
	fmt.Printf("  source:                backlog (workitems)\n")
	fmt.Printf("  periodic subscription: %d\n", periodicID)
	fmt.Printf("  alert:                 %d\n", alertID)
	fmt.Printf("  element URL:           %s\n", elementURL)
}

func seedStoredProcedures(ctx context.Context, store *database.MySQLStore, wfIDs map[string]int64) {
	type spDef struct {
		workflow string
		row      database.StoredProcedureRow
	}
	procs := []spDef{
		{"config-summary", database.StoredProcedureRow{
			Name:        "get_entity_counts",
			Description: "Returns the number of rows in each core configuration table, filtered by a minimum count threshold.",
			SQLText: `DROP PROCEDURE IF EXISTS get_entity_counts;
CREATE PROCEDURE get_entity_counts(IN min_count INT)
BEGIN
  SELECT entity_type, count FROM (
    SELECT 'orgs' AS entity_type, COUNT(*) AS count FROM orgs
    UNION ALL SELECT 'products', COUNT(*) FROM products
    UNION ALL SELECT 'teams', COUNT(*) FROM teams
    UNION ALL SELECT 'dashboards', COUNT(*) FROM dashboards
    UNION ALL SELECT 'subscriptions', COUNT(*) FROM subscriptions
    UNION ALL SELECT 'alerts', COUNT(*) FROM alerts
  ) counts
  WHERE count >= min_count
  ORDER BY count DESC;
END`,
		}},
		{"team-details", database.StoredProcedureRow{
			Name:        "get_team_sources",
			Description: "Returns all data sources configured for a given team, including source type and configuration.",
			SQLText: `DROP PROCEDURE IF EXISTS get_team_sources;
CREATE PROCEDURE get_team_sources(IN tid VARCHAR(100))
BEGIN
  SELECT id, type, config
  FROM sources
  WHERE team_id = tid
  ORDER BY id;
END`,
		}},
		{"team-dashboard-report", database.StoredProcedureRow{
			Name:        "get_dashboard_summary",
			Description: "Returns dashboards and diagram counts for a given owner type and ID.",
			SQLText: `DROP PROCEDURE IF EXISTS get_dashboard_summary;
CREATE PROCEDURE get_dashboard_summary(IN owner VARCHAR(10), IN oid VARCHAR(100))
BEGIN
  SELECT d.id, d.dashboard_id, d.type, d.title,
         COUNT(g.id) AS diagram_count
  FROM dashboards d
  LEFT JOIN diagrams g ON g.dashboard_id = d.id
  WHERE d.owner_type = owner AND d.owner_id = oid
  GROUP BY d.id, d.dashboard_id, d.type, d.title
  ORDER BY d.id;
END`,
		}},
		{"team-dashboard-report", database.StoredProcedureRow{
			Name:        "get_team_sources",
			Description: "Returns all data sources configured for a given team, including source type and configuration.",
			SQLText: `DROP PROCEDURE IF EXISTS get_team_sources;
CREATE PROCEDURE get_team_sources(IN tid VARCHAR(100))
BEGIN
  SELECT id, type, config
  FROM sources
  WHERE team_id = tid
  ORDER BY id;
END`,
		}},
	}
	for _, p := range procs {
		p.row.WorkflowID = wfIDs[p.workflow]
		id, err := store.CreateStoredProcedure(ctx, p.row)
		if err != nil {
			fatal("create stored procedure "+p.row.Name, err)
		}
		fmt.Printf("  stored procedure:      %s (id=%d, workflow=%s)\n", p.row.Name, id, p.workflow)
	}
}

func seedCannedEntries(ctx context.Context, store *database.MySQLStore) {
	_, err := store.CreateCannedQuery(ctx, database.CannedQueryRow{
		Name:        "Config Entity Counts",
		Description: "Returns the number of rows in each core configuration table.",
		Category:    "Admin",
		SQLText:     "CALL get_entity_counts(0)",
		Parameters:  json.RawMessage(`[]`),
		Version:     1,
	})
	if err != nil {
		fatal("create canned query", err)
	}
	fmt.Printf("  canned query:          Config Entity Counts\n")

	_, err = store.CreateCannedScript(ctx, database.CannedScriptRow{
		Name:        "Rows to Keyed Object",
		Description: "Converts an array of {entity_type, count} rows into a single keyed object, e.g. {orgs: 5, teams: 3}.",
		Category:    "Transforms",
		JSText:      "// Converts [{entity_type: 'orgs', count: 5}, ...] to {orgs: 5, ...}\nvar obj = {};\nfor (var i = 0; i < rows.length; i++) {\n  obj[rows[i].entity_type] = rows[i].count;\n}\n(obj)",
		Version:     1,
	})
	if err != nil {
		fatal("create canned script", err)
	}
	fmt.Printf("  canned script:         Rows to Keyed Object\n")
}

func seedJsonSchemas(ctx context.Context, store *database.MySQLStore) {
	schemas := []database.JsonSchemaRow{
		{
			Name:        "config-summary-output",
			Description: "Validates the output of the config-summary workflow — expects a summary object with entity counts and filter metadata.",
			JsonSchema: `{
  "type": "object",
  "required": ["summary", "filter_applied", "threshold"],
  "properties": {
    "summary": {
      "type": "object"
    },
    "filter_applied": {
      "type": "boolean"
    },
    "threshold": {
      "type": "integer",
      "minimum": 0
    }
  },
  "additionalProperties": false
}`,
		},
		{
			Name:        "team-details-output",
			Description: "Validates the output of the team-details workflow — expects team info, source count, and sources array.",
			JsonSchema: `{
  "type": "object",
  "required": ["team", "source_count", "sources"],
  "properties": {
    "team": {
      "type": "object",
      "required": ["id", "name"],
      "properties": {
        "id": {"type": "string"},
        "name": {"type": "string"}
      }
    },
    "source_count": {
      "type": "integer",
      "minimum": 0
    },
    "sources": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["id", "type"],
        "properties": {
          "id": {"type": "string"},
          "type": {"type": "string"}
        }
      }
    }
  }
}`,
		},
		{
			Name:        "dashboard-report-output",
			Description: "Validates the output of the team-dashboard-report workflow — expects team, dashboards array with diagram counts, and sources.",
			JsonSchema: `{
  "type": "object",
  "required": ["team", "dashboards", "source_count", "sources"],
  "properties": {
    "team": {
      "type": "object",
      "required": ["id", "name"],
      "properties": {
        "id": {"type": "string"},
        "name": {"type": "string"}
      }
    },
    "dashboards": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["title", "diagram_count"],
        "properties": {
          "title": {"type": "string"},
          "diagram_count": {"type": "integer", "minimum": 0}
        }
      }
    },
    "source_count": {
      "type": "integer",
      "minimum": 0
    },
    "sources": {
      "type": "array"
    }
  }
}`,
		},
	}
	for _, s := range schemas {
		id, err := store.CreateJsonSchema(ctx, s)
		if err != nil {
			fatal("create json schema "+s.Name, err)
		}
		fmt.Printf("  json schema:           %s (id=%d)\n", s.Name, id)
	}
}

func seedWorkflows(ctx context.Context, store *database.MySQLStore) map[string]int64 {
	ids := make(map[string]int64)
	// Workflow 1: config-summary — calls get_entity_counts procedure
	wf1ID, err := store.CreateWorkflow(ctx, database.WorkflowRow{
		Name:        "config-summary",
		Description: "Returns a summary of all entity counts using the get_entity_counts stored procedure. Optionally filter by a minimum count threshold.",
		InputScript: "// Pass through the optional min_count filter (default 0)\nvar min = parseInt(input.min_count) || 0;\n({min_count: min})",
		OutputScript: `// Build summary object from procedure results
var counts = results['entity_counts'] || [];
var min = input.min_count || 0;
var summary = {};
for (var i = 0; i < counts.length; i++) {
  summary[counts[i].entity_type] = counts[i].count;
}
({summary: summary, filter_applied: min > 0, threshold: min})`,
		TestInput: `{"min_count": 0}`,
		Queries: []database.WorkflowQueryRow{
			{Stage: 0, SortOrder: 0, Label: "entity_counts", SQLText: "CALL get_entity_counts(:min_count)"},
		},
		Tests: []database.WorkflowTestCase{
			{
				Name:  "Returns summary with all entities",
				Input: `{"min_count": 0}`,
				AssertScript: `if (!output.summary) throw new Error('missing summary');
if (typeof output.summary !== 'object') throw new Error('summary should be an object');
if (output.filter_applied !== false) throw new Error('filter_applied should be false for threshold 0');`,
			},
			{
				Name:  "Applies minimum count filter",
				Input: `{"min_count": 999}`,
				AssertScript: `if (!output.summary) throw new Error('missing summary');
if (output.filter_applied !== true) throw new Error('filter_applied should be true');
if (output.threshold !== 999) throw new Error('threshold should be 999');`,
			},
		},
	})
	if err != nil {
		fatal("create workflow config-summary", err)
	}
	ids["config-summary"] = wf1ID
	fmt.Printf("  workflow:              config-summary (id=%d)\n", wf1ID)

	// Workflow 2: team-details — calls get_team_sources procedure
	wf2ID, err := store.CreateWorkflow(ctx, database.WorkflowRow{
		Name:        "team-details",
		Description: "Returns team information and configured sources. Uses get_team_sources stored procedure for the source data.",
		InputScript: "// Extract and validate team_id from request params\nvar tid = input.team_id || '';\nif (!tid) throw new Error('team_id is required');\n({team_id: tid})",
		OutputScript: `// Combine team info and sources into a single response
var info = results['team_info'] || [];
var sources = results['team_sources'] || [];
var team = info.length > 0 ? info[0] : null;
({team: team, source_count: sources.length, sources: sources})`,
		TestInput: `{"team_id": "test-team"}`,
		Queries: []database.WorkflowQueryRow{
			{Stage: 0, SortOrder: 0, Label: "team_info", SQLText: "SELECT id, name, contact_email FROM teams WHERE id = :team_id"},
			{Stage: 0, SortOrder: 1, Label: "team_sources", SQLText: "CALL get_team_sources(:team_id)"},
		},
		Tests: []database.WorkflowTestCase{
			{
				Name:  "Returns test-team details with sources",
				Input: `{"team_id": "test-team"}`,
				AssertScript: `if (!output.team) throw new Error('missing team');
if (output.team.id !== 'test-team') throw new Error('wrong team id: ' + output.team.id);
if (output.source_count < 1) throw new Error('expected at least 1 source, got ' + output.source_count);`,
			},
			{
				Name:  "Rejects missing team_id",
				Input: `{}`,
				AssertScript: `if (!error) throw new Error('expected pipeline to fail');
if (!error.includes('team_id is required')) throw new Error('wrong error: ' + error);`,
			},
		},
	})
	if err != nil {
		fatal("create workflow team-details", err)
	}
	ids["team-details"] = wf2ID
	fmt.Printf("  workflow:              team-details (id=%d)\n", wf2ID)

	// Workflow 3: team-dashboard-report — multi-step with stored procedures
	wf3ID, err := store.CreateWorkflow(ctx, database.WorkflowRow{
		Name:        "team-dashboard-report",
		Description: "Multi-step report: resolves a team, then fetches dashboards via the get_dashboard_summary procedure and sources via get_team_sources in parallel. Demonstrates sequential step execution with stored procedures.",
		InputScript: "// Validate team_id\nvar tid = input.team_id || '';\nif (!tid) throw new Error('team_id is required');\nconsole.log('Generating report for team:', tid);\n({team_id: tid})",
		OutputScript: `// Build a structured report from all steps
var team = (results['team'] || [])[0] || null;
var dashboards = results['dashboards'] || [];
var sources = results['sources'] || [];
console.log('Report complete:', dashboards.length, 'dashboards,', sources.length, 'sources');
({team: team, dashboards: dashboards, source_count: sources.length, sources: sources})`,
		TestInput: `{"team_id": "test-team"}`,
		Queries: []database.WorkflowQueryRow{
			// Step 0: get the team
			{Stage: 0, SortOrder: 0, Label: "team", SQLText: "SELECT id, name, contact_email FROM teams WHERE id = :team_id"},
			// Step 1: dashboards and sources in parallel (both depend on team existing)
			{Stage: 1, SortOrder: 0, Label: "dashboards", SQLText: "CALL get_dashboard_summary('team', :team_id)"},
			{Stage: 1, SortOrder: 1, Label: "sources", SQLText: "CALL get_team_sources(:team_id)"},
		},
		Tests: []database.WorkflowTestCase{
			{
				Name:  "Returns dashboards and sources for test-team",
				Input: `{"team_id": "test-team"}`,
				AssertScript: `if (!output.team) throw new Error('missing team');
if (output.team.id !== 'test-team') throw new Error('wrong team id');
if (!Array.isArray(output.dashboards)) throw new Error('dashboards should be an array');
if (output.dashboards.length < 1) throw new Error('expected at least 1 dashboard');
if (output.source_count < 1) throw new Error('expected at least 1 source');`,
			},
			{
				Name:  "Dashboard entries have diagram counts",
				Input: `{"team_id": "test-team"}`,
				AssertScript: `var dbs = output.dashboards || [];
if (!dbs.length) throw new Error('no dashboards');
for (var i = 0; i < dbs.length; i++) {
  if (dbs[i].diagram_count === undefined) throw new Error('dashboard ' + dbs[i].title + ' missing diagram_count');
}`,
			},
		},
	})
	if err != nil {
		fatal("create workflow team-dashboard-report", err)
	}
	ids["team-dashboard-report"] = wf3ID
	fmt.Printf("  workflow:              team-dashboard-report (id=%d)\n", wf3ID)
	return ids
}

func fatal(action string, err error) {
	fmt.Fprintf(os.Stderr, "failed to %s: %v\n", action, err)
	os.Exit(1)
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
