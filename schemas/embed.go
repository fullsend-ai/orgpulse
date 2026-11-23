package schemas

import _ "embed"

//go:embed config-keys.json
var ConfigKeysJSON string

//go:embed dashboard-templates.json
var DashboardTemplatesJSON string

//go:embed alert-test-response.json
var AlertTestResponseJSON string
