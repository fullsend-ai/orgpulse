package mcpserver

import (
	"fmt"
	"net/url"
)

func allTools() []toolDefinition {
	return []toolDefinition{
		orgsTool(),
		productsTool(),
		teamsTool(),
		dashboardsTool(),
		sourcesTool(),
		subscriptionsTool(),
		alertsTool(),
		cannedQueriesTool(),
		cannedScriptsTool(),
		storedProceduresTool(),
		workflowsTool(),
		jsonSchemasTool(),
		apiKeysTool(),
		orgProductLinksTool(),
		productTeamLinksTool(),
		teamProductLabelsTool(),
		entityConfigTool(),
		productOverridesTool(),
	}
}

func crudSchema(entity string, idType string, fields []schemaField) map[string]any {
	createProps := map[string]any{}
	createRequired := []string{}
	for _, f := range fields {
		createProps[f.name] = f.schema
		if f.required {
			createRequired = append(createRequired, f.name)
		}
	}

	idSchema := map[string]any{"type": "string", "description": fmt.Sprintf("The %s ID", entity)}
	if idType == "integer" {
		idSchema = map[string]any{"type": "integer", "description": fmt.Sprintf("The %s numeric ID", entity)}
	}

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "get", "create", "update", "delete"},
				"description": "The operation to perform",
			},
			"id":   idSchema,
			"data": map[string]any{"type": "object", "properties": createProps, "required": createRequired},
		},
		"required": []string{"operation"},
	}
}

type schemaField struct {
	name     string
	schema   map[string]any
	required bool
}

func stringField(name, desc string, required bool) schemaField {
	return schemaField{name: name, schema: map[string]any{"type": "string", "description": desc}, required: required}
}

func intField(name, desc string, required bool) schemaField {
	return schemaField{name: name, schema: map[string]any{"type": "integer", "description": desc}, required: required}
}

func handleCRUD(c *apiClient, args map[string]any, basePath string, idType string) (any, error) {
	op, _ := args["operation"].(string)
	switch op {
	case "list":
		return c.get(basePath)
	case "get":
		id, err := extractID(args, idType)
		if err != nil {
			return nil, err
		}
		return c.get(fmt.Sprintf("%s/%s", basePath, id))
	case "create":
		data := args["data"]
		return c.post(basePath, data)
	case "update":
		id, err := extractID(args, idType)
		if err != nil {
			return nil, err
		}
		data := args["data"]
		return c.put(fmt.Sprintf("%s/%s", basePath, id), data)
	case "delete":
		id, err := extractID(args, idType)
		if err != nil {
			return nil, err
		}
		return c.delete(fmt.Sprintf("%s/%s", basePath, id))
	default:
		return nil, fmt.Errorf("unknown operation %q, expected one of: list, get, create, update, delete", op)
	}
}

func extractID(args map[string]any, idType string) (string, error) {
	raw, ok := args["id"]
	if !ok {
		return "", fmt.Errorf("id is required for this operation")
	}
	if idType == "integer" {
		switch v := raw.(type) {
		case float64:
			return fmt.Sprintf("%d", int64(v)), nil
		case string:
			return v, nil
		default:
			return "", fmt.Errorf("id must be a number")
		}
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("id must be a string")
	}
	return url.PathEscape(s), nil
}

func extractStringArg(args map[string]any, key string) (string, error) {
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return s, nil
}

func extractIntArg(args map[string]any, key string) (string, error) {
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	switch v := raw.(type) {
	case float64:
		return fmt.Sprintf("%d", int64(v)), nil
	case string:
		return v, nil
	default:
		return "", fmt.Errorf("%s must be a number", key)
	}
}

// --- Tool definitions ---

func orgsTool() toolDefinition {
	return toolDefinition{
		Name:        "orgs",
		Description: "Manage organizations. Operations: list all orgs, get/create/update/delete a single org.",
		InputSchema: crudSchema("org", "string", []schemaField{
			stringField("id", "Unique org identifier", true),
			stringField("name", "Display name", true),
		}),
	}
}

func productsTool() toolDefinition {
	return toolDefinition{
		Name:        "products",
		Description: "Manage products. Operations: list all products, get/create/update/delete a single product.",
		InputSchema: crudSchema("product", "string", []schemaField{
			stringField("id", "Unique product identifier", true),
			stringField("name", "Display name", true),
			stringField("contact_email", "Contact email", false),
		}),
	}
}

func teamsTool() toolDefinition {
	return toolDefinition{
		Name:        "teams",
		Description: "Manage teams. Operations: list all teams, get/create/update/delete a single team.",
		InputSchema: crudSchema("team", "string", []schemaField{
			stringField("id", "Unique team identifier", true),
			stringField("name", "Display name", true),
			stringField("contact_email", "Contact email", false),
			stringField("fetch_url", "Data fetch URL", false),
			stringField("fetch_check_url", "Data fetch check URL", false),
		}),
	}
}

func dashboardsTool() toolDefinition {
	return toolDefinition{
		Name:        "dashboards",
		Description: "Manage dashboards. For list, provide owner_type (org/product/team) and owner_id. For get/update/delete, provide the numeric id.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation": map[string]any{
					"type":        "string",
					"enum":        []string{"list", "get", "create", "update", "delete"},
					"description": "The operation to perform",
				},
				"id":         map[string]any{"type": "integer", "description": "Dashboard auto-ID (for get/update/delete)"},
				"owner_type": map[string]any{"type": "string", "enum": []string{"org", "product", "team"}, "description": "Owner entity type (for list)"},
				"owner_id":   map[string]any{"type": "string", "description": "Owner entity ID (for list)"},
				"data": map[string]any{
					"type":        "object",
					"description": "Dashboard data for create/update",
				},
			},
			"required": []string{"operation"},
		},
	}
}

func sourcesTool() toolDefinition {
	return toolDefinition{
		Name:        "sources",
		Description: "Manage data sources for a team. Requires team_id for all operations. For get/update/delete, also provide source_id.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation": map[string]any{
					"type":        "string",
					"enum":        []string{"list", "get", "create", "update", "delete"},
					"description": "The operation to perform",
				},
				"team_id":   map[string]any{"type": "string", "description": "The team ID (required for all operations)"},
				"source_id": map[string]any{"type": "string", "description": "The source ID (for get/update/delete)"},
				"data": map[string]any{
					"type":        "object",
					"description": "Source data for create/update",
				},
			},
			"required": []string{"operation", "team_id"},
		},
	}
}

func subscriptionsTool() toolDefinition {
	return toolDefinition{
		Name:        "subscriptions",
		Description: "Manage report subscriptions. Operations: list all subscriptions, get/create/update/delete a single subscription.",
		InputSchema: crudSchema("subscription", "integer", []schemaField{
			stringField("email", "Recipient email", true),
			stringField("product_id", "Product ID", false),
			stringField("team_id", "Team ID", false),
			stringField("ai_analytics_url", "AI analytics URL", false),
			intField("interval_days", "Report interval in days", false),
		}),
	}
}

func alertsTool() toolDefinition {
	return toolDefinition{
		Name:        "alerts",
		Description: "Manage alert definitions. Operations: list all alerts, get/create/update/delete a single alert.",
		InputSchema: crudSchema("alert", "integer", []schemaField{
			stringField("email", "Recipient email", true),
			stringField("url", "Metrics endpoint URL", true),
			stringField("ai_analytics_url", "AI analytics URL", true),
			stringField("rule", "JS rule expression", true),
			stringField("product_id", "Product ID", false),
			stringField("team_id", "Team ID", false),
		}),
	}
}

func cannedQueriesTool() toolDefinition {
	return toolDefinition{
		Name:        "canned_queries",
		Description: "Manage reusable SQL query templates. Operations: list all, get/create/update/delete a single canned query.",
		InputSchema: crudSchema("canned query", "integer", []schemaField{
			stringField("name", "Query name", true),
			stringField("category", "Category", true),
			stringField("sql_text", "SQL query text", true),
			stringField("description", "Description", false),
			intField("version", "Version number", false),
		}),
	}
}

func cannedScriptsTool() toolDefinition {
	return toolDefinition{
		Name:        "canned_scripts",
		Description: "Manage reusable JavaScript fragments. Operations: list all, get/create/update/delete a single canned script.",
		InputSchema: crudSchema("canned script", "integer", []schemaField{
			stringField("name", "Script name", true),
			stringField("category", "Category", true),
			stringField("js_text", "JavaScript code", true),
			stringField("description", "Description", false),
			intField("version", "Version number", false),
		}),
	}
}

func storedProceduresTool() toolDefinition {
	return toolDefinition{
		Name: "stored_procedures",
		Description: "Manage SQL stored procedures scoped to a workflow. Operations: list (requires workflow_id), get, create, update, delete (standard CRUD); " +
			"deploy (execute the procedure's SQL on the data database); " +
			"execute_sql (run arbitrary SQL for testing, e.g. CALL my_proc()).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation": map[string]any{
					"type":        "string",
					"enum":        []string{"list", "get", "create", "update", "delete", "deploy", "execute_sql"},
					"description": "The operation to perform",
				},
				"id":          map[string]any{"type": "integer", "description": "Procedure ID"},
				"workflow_id": map[string]any{"type": "integer", "description": "Workflow ID (required for list)"},
				"sql":         map[string]any{"type": "string", "description": "SQL statement to execute (for execute_sql)"},
				"data":        map[string]any{"type": "object", "description": "Procedure data for create/update (workflow_id, name, description, sql_text)"},
			},
			"required": []string{"operation"},
		},
	}
}

func workflowsTool() toolDefinition {
	return toolDefinition{
		Name: "workflows",
		Description: "Manage workflows with draft/publish lifecycle. Operations: list, get, create, update, delete (standard CRUD); " +
			"save_draft (save work-in-progress); publish (create a new version with optional note); " +
			"list_versions (show version history); get_version (get a specific version); " +
			"set_production (mark a version as the production version); test (execute workflow with test input); " +
			"archive (disable workflow, remove production version); unarchive (re-enable workflow without setting production); " +
			"lock (acquire editor lock); unlock (release editor lock).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation": map[string]any{
					"type":        "string",
					"enum":        []string{"list", "get", "create", "update", "delete", "save_draft", "publish", "list_versions", "get_version", "set_production", "test", "archive", "unarchive", "lock", "unlock"},
					"description": "The operation to perform",
				},
				"id":           map[string]any{"type": "integer", "description": "Workflow ID"},
				"version":      map[string]any{"type": "integer", "description": "Version number (for get_version, set_production)"},
				"publish_note": map[string]any{"type": "string", "description": "Note describing changes (for publish)"},
				"test_input":   map[string]any{"type": "object", "description": "Input data for test execution"},
				"data":         map[string]any{"type": "object", "description": "Workflow data for create/update/save_draft"},
			},
			"required": []string{"operation"},
		},
	}
}

func jsonSchemasTool() toolDefinition {
	return toolDefinition{
		Name:        "json_schemas",
		Description: "Manage JSON Schema catalog for workflow test assertions. Operations: list all, get/create/update/delete a single schema.",
		InputSchema: crudSchema("json schema", "integer", []schemaField{
			stringField("name", "Schema name (unique)", true),
			stringField("json_schema", "JSON Schema document", true),
			stringField("description", "Description", false),
		}),
	}
}

func apiKeysTool() toolDefinition {
	return toolDefinition{
		Name:        "api_keys",
		Description: "Manage API keys (requires an admin API key). Operations: list all keys, create a new key, delete a key.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation": map[string]any{
					"type":        "string",
					"enum":        []string{"list", "create", "delete"},
					"description": "The operation to perform",
				},
				"id":       map[string]any{"type": "integer", "description": "Key ID (for delete)"},
				"name":     map[string]any{"type": "string", "description": "Name for the new key (for create)"},
				"is_admin": map[string]any{"type": "boolean", "description": "Whether the key has admin privileges (for create)"},
			},
			"required": []string{"operation"},
		},
	}
}

func orgProductLinksTool() toolDefinition {
	return toolDefinition{
		Name:        "org_product_links",
		Description: "Manage which products belong to an organization. Operations: get (list linked products), replace (set the full list).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation": map[string]any{"type": "string", "enum": []string{"get", "replace"}, "description": "The operation to perform"},
				"org_id":    map[string]any{"type": "string", "description": "Organization ID"},
				"entries": map[string]any{
					"type":        "array",
					"description": "Array of {id, sort_order} entries (for replace)",
					"items":       map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "sort_order": map[string]any{"type": "integer"}}},
				},
			},
			"required": []string{"operation", "org_id"},
		},
	}
}

func productTeamLinksTool() toolDefinition {
	return toolDefinition{
		Name:        "product_team_links",
		Description: "Manage which teams belong to a product. Operations: get (list linked teams), replace (set the full list).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation":  map[string]any{"type": "string", "enum": []string{"get", "replace"}, "description": "The operation to perform"},
				"product_id": map[string]any{"type": "string", "description": "Product ID"},
				"entries": map[string]any{
					"type":        "array",
					"description": "Array of {id, sort_order} entries (for replace)",
					"items":       map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "sort_order": map[string]any{"type": "integer"}}},
				},
			},
			"required": []string{"operation", "product_id"},
		},
	}
}

func teamProductLabelsTool() toolDefinition {
	return toolDefinition{
		Name:        "team_product_labels",
		Description: "Manage display product labels for a team. Operations: get (list labels), replace (set the full list).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation": map[string]any{"type": "string", "enum": []string{"get", "replace"}, "description": "The operation to perform"},
				"team_id":   map[string]any{"type": "string", "description": "Team ID"},
				"labels": map[string]any{
					"type":        "array",
					"description": "Array of product label strings (for replace)",
					"items":       map[string]any{"type": "string"},
				},
			},
			"required": []string{"operation", "team_id"},
		},
	}
}

func entityConfigTool() toolDefinition {
	return toolDefinition{
		Name:        "entity_config",
		Description: "Manage key-value configuration for orgs, products, or teams. Operations: get, replace, delete.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation":   map[string]any{"type": "string", "enum": []string{"get", "replace", "delete"}, "description": "The operation to perform"},
				"entity_type": map[string]any{"type": "string", "enum": []string{"org", "product", "team"}, "description": "Entity type"},
				"entity_id":   map[string]any{"type": "string", "description": "Entity ID"},
				"config": map[string]any{
					"type":                 "object",
					"description":          "Key-value config map (for replace)",
					"additionalProperties": map[string]any{"type": "string"},
				},
			},
			"required": []string{"operation", "entity_type", "entity_id"},
		},
	}
}

func productOverridesTool() toolDefinition {
	return toolDefinition{
		Name:        "product_overrides",
		Description: "Manage repository override mappings for a product. Operations: get (list overrides), replace (set the full list).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"operation":  map[string]any{"type": "string", "enum": []string{"get", "replace"}, "description": "The operation to perform"},
				"product_id": map[string]any{"type": "string", "description": "Product ID"},
				"overrides": map[string]any{
					"type":        "array",
					"description": "Array of override repo objects (for replace)",
					"items":       map[string]any{"type": "object"},
				},
			},
			"required": []string{"operation", "product_id"},
		},
	}
}
