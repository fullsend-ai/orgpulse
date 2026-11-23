package database

import "encoding/json"

type Org struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	AuthGroup  string         `json:"authgroup,omitempty"`
	Products   []string       `json:"products"`
	Config     map[string]any `json:"config,omitempty"`
	Dashboards []Dashboard    `json:"dashboards,omitempty"`
}

type Product struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Teams        []string       `json:"teams"`
	ContactEmail string         `json:"contactemail,omitempty"`
	AuthGroup    string         `json:"authgroup,omitempty"`
	Config       *ProductConfig `json:"config,omitempty"`
	Dashboards   []Dashboard    `json:"dashboards,omitempty"`
}

type ProductConfig struct {
	CycleTimeSlowHours     *float64         `json:"cycleTimeSlowHours,omitempty"`
	CycleTimeCriticalHours *float64         `json:"cycleTimeCriticalHours,omitempty"`
	AnalyticsPrompt        string           `json:"analyticsprompt,omitempty"`
	Overrides              *ConfigOverrides `json:"overrides,omitempty"`
}

type ConfigOverrides struct {
	Sources *SourceOverrides `json:"sources,omitempty"`
}

type SourceOverrides struct {
	Repos []OverrideRepo `json:"repos,omitempty"`
}

type OverrideRepo struct {
	ID    string   `json:"id"`
	Type  string   `json:"type"`
	Owner string   `json:"owner"`
	Name  string   `json:"name"`
	Teams []string `json:"teams"`
}

type Team struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	ContactEmail  string      `json:"contactemail,omitempty"`
	AuthGroup     string      `json:"authgroup,omitempty"`
	Product       []string    `json:"product,omitempty"`
	FetchURL      string      `json:"fetchurl,omitempty"`
	FetchCheckURL string      `json:"fetchcheckurl,omitempty"`
	Sources       []Source    `json:"sources,omitempty"`
	Dashboards    []Dashboard `json:"dashboards,omitempty"`
}

type Source struct {
	ID       string            `json:"-"`
	Type     string            `json:"-"`
	Config   json.RawMessage   `json:"-"`
	Repos    []Repo            `json:"-"`
	Projects []WorkitemProject `json:"-"`
}

func (s Source) MarshalJSON() ([]byte, error) {
	type common struct {
		ID     string          `json:"id"`
		Type   string          `json:"type"`
		Config json.RawMessage `json:"config,omitempty"`
	}
	c := common{ID: s.ID, Type: s.Type, Config: s.Config}
	if s.Type == "gitrepos" {
		repos := s.Repos
		if repos == nil {
			repos = []Repo{}
		}
		return json.Marshal(struct {
			common
			Repos []Repo `json:"repos"`
		}{c, repos})
	}
	projects := s.Projects
	if projects == nil {
		projects = []WorkitemProject{}
	}
	return json.Marshal(struct {
		common
		Projects []WorkitemProject `json:"projects"`
	}{c, projects})
}

func (s *Source) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID       string            `json:"id"`
		Type     string            `json:"type"`
		Config   json.RawMessage   `json:"config,omitempty"`
		Repos    []Repo            `json:"repos,omitempty"`
		Projects []WorkitemProject `json:"projects,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.ID = raw.ID
	s.Type = raw.Type
	s.Config = raw.Config
	s.Repos = raw.Repos
	s.Projects = raw.Projects
	return nil
}

type Repo struct {
	ID                     string   `json:"id"`
	Type                   string   `json:"type"`
	Owner                  string   `json:"owner"`
	Name                   string   `json:"name"`
	ExcludedDashboardTypes []string `json:"excludedDashboardTypes,omitempty"`
	Whitelist              []string `json:"whitelist,omitempty"`
}

type WorkitemProject struct {
	ID          string `json:"id"`
	ProjectName string `json:"projectname"`
	JiraProject string `json:"jiraproject,omitempty"`
	BoardID     string `json:"boardid,omitempty"`
	Type        string `json:"type"`
	ScopeConfig *struct {
		RequirementTypes []string `json:"requirement_types,omitempty"`
		BugTypes         []string `json:"bug_types,omitempty"`
		StoryPointField  string   `json:"story_point_field,omitempty"`
	} `json:"scope_config,omitempty"`
}

type Dashboard struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	Title          string    `json:"title"`
	Description    string    `json:"description,omitempty"`
	DefaultSource  string    `json:"defaultsource,omitempty"`
	ConnectionID   string    `json:"connectionid,omitempty"`
	JiraProject    string    `json:"jiraproject,omitempty"`
	ChatURL        string    `json:"chaturl,omitempty"`
	AIAnalyticsURL string    `json:"aianalyticsurl,omitempty"`
	Icon           string    `json:"icon,omitempty"`
	Category       string    `json:"category,omitempty"`
	BlueprintID    string    `json:"blueprintid,omitempty"`
	BlueprintIDs   []string  `json:"blueprintids,omitempty"`
	Diagrams       []Diagram `json:"diagrams,omitempty"`
}

type Diagram struct {
	Position         *int            `json:"position,omitempty"`
	Type             string          `json:"type,omitempty"`
	Endpoint         string          `json:"endpoint"`
	Span             *int            `json:"span,omitempty"`
	Title            string          `json:"title,omitempty"`
	AIAnalytics      bool            `json:"aianalytics,omitempty"`
	Description      string          `json:"description,omitempty"`
	DocumentationURL string          `json:"documentation_url,omitempty"`
	Params           json.RawMessage `json:"params,omitempty"`
}
