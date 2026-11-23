package mcpserver

import (
	"encoding/json"
	"fmt"
	"net/url"
)

type toolHandler func(c *apiClient, args map[string]any) (any, error)

func toolHandlers() map[string]toolHandler {
	return map[string]toolHandler{
		"orgs":                handleOrgs,
		"products":            handleProducts,
		"teams":               handleTeams,
		"dashboards":          handleDashboards,
		"sources":             handleSources,
		"subscriptions":       handleSubscriptions,
		"alerts":              handleAlerts,
		"canned_queries":      handleCannedQueries,
		"canned_scripts":      handleCannedScripts,
		"stored_procedures":   handleStoredProcedures,
		"workflows":           handleWorkflows,
		"json_schemas":        handleJsonSchemas,
		"api_keys":            handleAPIKeys,
		"org_product_links":   handleOrgProductLinks,
		"product_team_links":  handleProductTeamLinks,
		"team_product_labels": handleTeamProductLabels,
		"entity_config":       handleEntityConfig,
		"product_overrides":   handleProductOverrides,
	}
}

func handleOrgs(c *apiClient, args map[string]any) (any, error) {
	return handleCRUD(c, args, "/api/v1/orgs", "string")
}

func handleProducts(c *apiClient, args map[string]any) (any, error) {
	return handleCRUD(c, args, "/api/v1/products", "string")
}

func handleTeams(c *apiClient, args map[string]any) (any, error) {
	return handleCRUD(c, args, "/api/v1/teams", "string")
}

func handleDashboards(c *apiClient, args map[string]any) (any, error) {
	op, _ := args["operation"].(string)
	switch op {
	case "list":
		ownerType, err := extractStringArg(args, "owner_type")
		if err != nil {
			return nil, err
		}
		ownerID, err := extractStringArg(args, "owner_id")
		if err != nil {
			return nil, err
		}
		return c.get(fmt.Sprintf("/api/v1/dashboards?owner_type=%s&owner_id=%s",
			url.QueryEscape(ownerType), url.QueryEscape(ownerID)))
	default:
		return handleCRUD(c, args, "/api/v1/dashboards", "integer")
	}
}

func handleSources(c *apiClient, args map[string]any) (any, error) {
	teamID, err := extractStringArg(args, "team_id")
	if err != nil {
		return nil, err
	}
	op, _ := args["operation"].(string)
	basePath := fmt.Sprintf("/api/v1/teams/%s/sources", url.PathEscape(teamID))
	switch op {
	case "list":
		return c.get(basePath)
	case "get":
		sourceID, err := extractStringArg(args, "source_id")
		if err != nil {
			return nil, err
		}
		return c.get(fmt.Sprintf("%s/%s", basePath, url.PathEscape(sourceID)))
	case "create":
		data := args["data"]
		return c.post(basePath, data)
	case "update":
		sourceID, err := extractStringArg(args, "source_id")
		if err != nil {
			return nil, err
		}
		data := args["data"]
		return c.put(fmt.Sprintf("%s/%s", basePath, url.PathEscape(sourceID)), data)
	case "delete":
		sourceID, err := extractStringArg(args, "source_id")
		if err != nil {
			return nil, err
		}
		return c.delete(fmt.Sprintf("%s/%s", basePath, url.PathEscape(sourceID)))
	default:
		return nil, fmt.Errorf("unknown operation %q", op)
	}
}

func handleSubscriptions(c *apiClient, args map[string]any) (any, error) {
	return handleCRUD(c, args, "/api/v1/subscriptions", "integer")
}

func handleAlerts(c *apiClient, args map[string]any) (any, error) {
	return handleCRUD(c, args, "/api/v1/alerts", "integer")
}

func handleCannedQueries(c *apiClient, args map[string]any) (any, error) {
	return handleCRUD(c, args, "/api/v1/canned-queries", "integer")
}

func handleCannedScripts(c *apiClient, args map[string]any) (any, error) {
	return handleCRUD(c, args, "/api/v1/canned-scripts", "integer")
}

func handleStoredProcedures(c *apiClient, args map[string]any) (any, error) {
	op, _ := args["operation"].(string)
	switch op {
	case "list":
		wfID, err := extractIntArg(args, "workflow_id")
		if err != nil {
			return nil, fmt.Errorf("workflow_id is required for list")
		}
		return c.get(fmt.Sprintf("/api/v1/stored-procedures?workflow_id=%s", wfID))
	case "get", "update", "delete":
		return handleCRUD(c, args, "/api/v1/stored-procedures", "integer")
	case "create":
		data := args["data"]
		return c.post("/api/v1/stored-procedures", data)
	case "deploy":
		id, err := extractIntArg(args, "id")
		if err != nil {
			return nil, err
		}
		return c.post(fmt.Sprintf("/api/v1/stored-procedures/%s/deploy", id), nil)
	case "execute_sql":
		sqlStr, err := extractStringArg(args, "sql")
		if err != nil {
			return nil, err
		}
		return c.post("/api/v1/stored-procedures/execute-sql", map[string]any{"sql": sqlStr})
	default:
		return nil, fmt.Errorf("unknown operation %q", op)
	}
}

func handleWorkflows(c *apiClient, args map[string]any) (any, error) {
	op, _ := args["operation"].(string)
	switch op {
	case "list", "get", "create", "update", "delete":
		return handleCRUD(c, args, "/api/v1/workflows", "integer")
	case "save_draft":
		id, err := extractIntArg(args, "id")
		if err != nil {
			return nil, err
		}
		data := args["data"]
		return c.put(fmt.Sprintf("/api/v1/workflows/%s/draft", id), data)
	case "publish":
		id, err := extractIntArg(args, "id")
		if err != nil {
			return nil, err
		}
		body := map[string]any{}
		if note, ok := args["publish_note"].(string); ok {
			body["publish_note"] = note
		}
		return c.post(fmt.Sprintf("/api/v1/workflows/%s/publish", id), body)
	case "list_versions":
		id, err := extractIntArg(args, "id")
		if err != nil {
			return nil, err
		}
		return c.get(fmt.Sprintf("/api/v1/workflows/%s/versions", id))
	case "get_version":
		id, err := extractIntArg(args, "id")
		if err != nil {
			return nil, err
		}
		version, err := extractIntArg(args, "version")
		if err != nil {
			return nil, err
		}
		return c.get(fmt.Sprintf("/api/v1/workflows/%s/versions/%s", id, version))
	case "set_production":
		id, err := extractIntArg(args, "id")
		if err != nil {
			return nil, err
		}
		version, err := extractIntArg(args, "version")
		if err != nil {
			return nil, err
		}
		return c.post(fmt.Sprintf("/api/v1/workflows/%s/versions/%s/production", id, version), nil)
	case "test":
		id, err := extractIntArg(args, "id")
		if err != nil {
			return nil, err
		}
		input := args["test_input"]
		if input == nil {
			input = map[string]any{}
		}
		return c.post(fmt.Sprintf("/api/v1/workflows/%s/test", id), input)
	case "archive":
		id, err := extractIntArg(args, "id")
		if err != nil {
			return nil, err
		}
		return c.post(fmt.Sprintf("/api/v1/workflows/%s/archive", id), nil)
	case "unarchive":
		id, err := extractIntArg(args, "id")
		if err != nil {
			return nil, err
		}
		return c.post(fmt.Sprintf("/api/v1/workflows/%s/unarchive", id), nil)
	case "lock":
		id, err := extractIntArg(args, "id")
		if err != nil {
			return nil, err
		}
		return c.post(fmt.Sprintf("/api/v1/workflows/%s/lock", id), map[string]any{"session_id": c.sessionID})
	case "unlock":
		id, err := extractIntArg(args, "id")
		if err != nil {
			return nil, err
		}
		return c.do("DELETE", fmt.Sprintf("/api/v1/workflows/%s/lock", id), map[string]any{"session_id": c.sessionID})
	default:
		return nil, fmt.Errorf("unknown operation %q", op)
	}
}

func handleJsonSchemas(c *apiClient, args map[string]any) (any, error) {
	return handleCRUD(c, args, "/api/v1/json-schemas", "integer")
}

func handleAPIKeys(c *apiClient, args map[string]any) (any, error) {
	op, _ := args["operation"].(string)
	switch op {
	case "list":
		return c.get("/api/v1/keys")
	case "create":
		body := map[string]any{}
		if name, ok := args["name"].(string); ok {
			body["name"] = name
		}
		if isAdmin, ok := args["is_admin"].(bool); ok {
			body["is_admin"] = isAdmin
		}
		return c.post("/api/v1/keys", body)
	case "delete":
		id, err := extractIntArg(args, "id")
		if err != nil {
			return nil, err
		}
		return c.delete(fmt.Sprintf("/api/v1/keys/%s", id))
	default:
		return nil, fmt.Errorf("unknown operation %q, expected one of: list, create, delete", op)
	}
}

func handleOrgProductLinks(c *apiClient, args map[string]any) (any, error) {
	orgID, err := extractStringArg(args, "org_id")
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/v1/orgs/%s/products", url.PathEscape(orgID))
	op, _ := args["operation"].(string)
	switch op {
	case "get":
		return c.get(path)
	case "replace":
		entries := args["entries"]
		return c.put(path, entries)
	default:
		return nil, fmt.Errorf("unknown operation %q, expected get or replace", op)
	}
}

func handleProductTeamLinks(c *apiClient, args map[string]any) (any, error) {
	productID, err := extractStringArg(args, "product_id")
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/v1/products/%s/teams", url.PathEscape(productID))
	op, _ := args["operation"].(string)
	switch op {
	case "get":
		return c.get(path)
	case "replace":
		entries := args["entries"]
		return c.put(path, entries)
	default:
		return nil, fmt.Errorf("unknown operation %q, expected get or replace", op)
	}
}

func handleTeamProductLabels(c *apiClient, args map[string]any) (any, error) {
	teamID, err := extractStringArg(args, "team_id")
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/v1/teams/%s/product-labels", url.PathEscape(teamID))
	op, _ := args["operation"].(string)
	switch op {
	case "get":
		return c.get(path)
	case "replace":
		labels := args["labels"]
		return c.put(path, labels)
	default:
		return nil, fmt.Errorf("unknown operation %q, expected get or replace", op)
	}
}

func handleEntityConfig(c *apiClient, args map[string]any) (any, error) {
	entityType, err := extractStringArg(args, "entity_type")
	if err != nil {
		return nil, err
	}
	entityID, err := extractStringArg(args, "entity_id")
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/v1/config/%s/%s", url.PathEscape(entityType), url.PathEscape(entityID))
	op, _ := args["operation"].(string)
	switch op {
	case "get":
		return c.get(path)
	case "replace":
		config := args["config"]
		return c.put(path, config)
	case "delete":
		return c.delete(path)
	default:
		return nil, fmt.Errorf("unknown operation %q, expected get, replace, or delete", op)
	}
}

func handleProductOverrides(c *apiClient, args map[string]any) (any, error) {
	productID, err := extractStringArg(args, "product_id")
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/v1/products/%s/overrides", url.PathEscape(productID))
	op, _ := args["operation"].(string)
	switch op {
	case "get":
		return c.get(path)
	case "replace":
		overrides := args["overrides"]
		return c.put(path, overrides)
	default:
		return nil, fmt.Errorf("unknown operation %q, expected get or replace", op)
	}
}

func formatResult(result any, err error) toolResult {
	if err != nil {
		return toolResult{
			Content: []toolContent{{Type: "text", Text: fmt.Sprintf("Error: %s", err)}},
			IsError: true,
		}
	}
	var text string
	switch v := result.(type) {
	case json.RawMessage:
		var buf json.RawMessage
		if json.Unmarshal(v, &buf) == nil {
			pretty, err := json.MarshalIndent(json.RawMessage(v), "", "  ")
			if err == nil {
				text = string(pretty)
			} else {
				text = string(v)
			}
		} else {
			text = string(v)
		}
	default:
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return toolResult{
				Content: []toolContent{{Type: "text", Text: fmt.Sprintf("Error: %s", err)}},
				IsError: true,
			}
		}
		text = string(data)
	}
	return toolResult{
		Content: []toolContent{{Type: "text", Text: text}},
	}
}
