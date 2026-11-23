package devlake

type Config struct {
	APIURL                string
	APIKey                string
	CronConfig            string
	AllowPrivateEndpoints bool
	TLSSkipVerify         bool
}

type Blueprint struct {
	ID              int64        `json:"id,omitempty"`
	Name            string       `json:"name"`
	Mode            string       `json:"mode"`
	Enable          bool         `json:"enable"`
	CronConfig      string       `json:"cronConfig"`
	IsManual        bool         `json:"isManual"`
	Labels          []string     `json:"labels"`
	Connections     []Connection `json:"connections"`
	SkipFailedTasks bool         `json:"skipOnFail"`
	TimeAfter       string       `json:"timeAfter"`
}

type Connection struct {
	PluginName   string  `json:"pluginName"`
	ConnectionID int     `json:"connectionId"`
	Scopes       []Scope `json:"scopes"`
}

type Scope struct {
	ScopeID string `json:"scopeId"`
}

type ScopeKey struct {
	PluginName   string
	ConnectionID int
	ScopeID      string
}

type SyncResult struct {
	Created  []string `json:"created"`
	Updated  []string `json:"updated"`
	Deleted  []string `json:"deleted"`
	Skipped  []string `json:"skipped"`
	Errors   []string `json:"errors"`
	Resolved []string `json:"resolved,omitempty"`
	Failed   []string `json:"failed,omitempty"`
}

type TriggerResult struct {
	Triggered []string `json:"triggered"`
	Errors    []string `json:"errors"`
}

type ConnectionEntry struct {
	Plugin       string `json:"plugin"`
	Token        string `json:"token"`
	Endpoint     string `json:"endpoint"`
	Username     string `json:"username,omitempty"`
	Organization string `json:"organization,omitempty"`
	Service      string `json:"service,omitempty"`
	CITool       string `json:"ci_tool,omitempty"`
	ExpiryDate   string `json:"expiry_date"`
}

type TokenStatus struct {
	Plugin     string `json:"plugin"`
	ConnID     int    `json:"connection_id"`
	ExpiryDate string `json:"expiry_date"`
}

type ConnectionResult struct {
	Plugin       string `json:"plugin"`
	ConnectionID int    `json:"connection_id"`
}

type CleanupResult struct {
	DeletedBlueprints  []string `json:"deleted_blueprints"`
	DeletedScopes      []string `json:"deleted_scopes"`
	DeletedConnections []string `json:"deleted_connections"`
	Migrated           []string `json:"migrated"`
	Errors             []string `json:"errors"`
}

type ExistingScope struct {
	FullName       string `json:"full_name"`
	ConnectionName string `json:"connection_name"`
	ConnectionID   int    `json:"connection_id"`
}

type BrowseResult struct {
	FullName string `json:"full_name"`
	Owner    string `json:"owner"`
	Name     string `json:"name"`
	Plugin   string `json:"plugin"`
	BoardID  string `json:"board_id,omitempty"`
}

type blueprintListResponse struct {
	Blueprints []Blueprint `json:"blueprints"`
	Count      int64       `json:"count"`
}

type connectionInfo struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	Endpoint           string `json:"endpoint"`
	Organization       string `json:"organization"`
	Service            string `json:"service"`
	CITool             string `json:"ciTool,omitempty"`
	GitHubOrganization string `json:"githubOrganization,omitempty"`
	QuayOrganization   string `json:"quayOrganization,omitempty"`
}

type AvailableCodecovBlueprint struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	ConnectionID int      `json:"connection_id"`
	Scopes       []string `json:"scopes"`
}

type AvailableCodecov struct {
	Connections []connectionInfo            `json:"connections"`
	Blueprints  []AvailableCodecovBlueprint `json:"blueprints"`
}

type AttachCodecovRequest struct {
	ConnectionID int   `json:"connection_id"`
	BlueprintID  int64 `json:"blueprint_id"`
}

type scopeDetail struct {
	Scope map[string]any `json:"scope"`
}

type scopeListResponse struct {
	Scopes []scopeDetail `json:"scopes"`
	Count  int64         `json:"count"`
}
