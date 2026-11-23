import type { Dashboard, Diagram, Repo, TeamConfig, WorkitemProject } from '../api'

interface DashboardTypeParamSpec {
  configKeys: string[]
  build: (config: Record<string, string>) => Record<string, unknown>
  omitRepos?: boolean
}

function splitKeys(value: string | undefined): string[] {
  return (value || '').split(',').map(v => v.trim()).filter(Boolean)
}

// Extra request params per dashboard type, built from the team config entries
// in configKeys. All listed config entries are mandatory.
export const DASHBOARD_TYPE_PARAMS: Record<string, DashboardTypeParamSpec> = {
  aisdlc: {
    configKeys: ['rfe_jira_project', 'strat_jira_project', 'rfe_station_id', 'strategy_station_id'],
    build: config => {
      const lists: Array<[string, string]> = [
        ['rfe_jira_project', 'rfe_jira_projects'],
        ['strat_jira_project', 'strat_jira_projects'],
        ['rfe_station_id', 'rfe_station_ids'],
        ['strategy_station_id', 'strategy_station_ids'],
      ]
      const params: Record<string, unknown> = {}
      for (const [configKey, param] of lists) {
        const values = splitKeys(config[configKey])
        if (values.length > 0) params[param] = values
      }
      return params
    },
    omitRepos: true,
  },
}

export interface TypeParams {
  params: Record<string, unknown>
  missing: string[]
}

export function hasTypeParams(dashboardType: string): boolean {
  return Object.prototype.hasOwnProperty.call(DASHBOARD_TYPE_PARAMS, dashboardType)
}

// Dashboard types whose workflows don't use the team's repos get no
// owner/name in the request and no repository filter.
export function usesRepoFilter(dashboardType: string | undefined): boolean {
  return !dashboardType || !hasTypeParams(dashboardType) || !DASHBOARD_TYPE_PARAMS[dashboardType].omitRepos
}

export function resolveTypeParams(dashboardType: string, teamConfig: TeamConfig): TypeParams {
  if (!hasTypeParams(dashboardType)) return { params: {}, missing: [] }
  const spec = DASHBOARD_TYPE_PARAMS[dashboardType]
  const config: Record<string, string> = {}
  const missing: string[] = []
  for (const key of spec.configKeys) {
    const value = (teamConfig[key] || '').trim()
    if (splitKeys(value).length > 0) config[key] = value
    else missing.push(key)
  }
  return { params: spec.build(config), missing }
}

// Adds dashboard-type params and per-diagram params to the standard request
// body. Standard keys already in the body are never overridden.
export function buildDiagramBody(
  body: Record<string, unknown>,
  typeParams: Record<string, unknown>,
  diagram: Pick<Diagram, 'params'>,
): Record<string, unknown> {
  const diagramParams = diagram.params && typeof diagram.params === 'object' && !Array.isArray(diagram.params)
    ? diagram.params
    : {}
  const merged: Record<string, unknown> = { ...body }
  for (const [key, value] of Object.entries({ ...typeParams, ...diagramParams })) {
    if (!(key in merged)) merged[key] = value
  }
  return merged
}

export function buildBody(
  dashboard: Dashboard,
  repos: Repo[],
  selectedRepos: string[],
  projects: WorkitemProject[],
  selectedProjects: string[],
  timeRange: string,
  customFrom: string,
  customTo: string,
): Record<string, unknown> {
  const body: Record<string, unknown> = {}

  if (dashboard.type === 'issuecycletime') {
    body.owner = repos.map(r => r.owner)
    body.name = repos.map(r => r.name)
    body.projects = selectedProjects.length > 0
      ? projects.filter(p => selectedProjects.includes(p.id))
      : projects
  } else if (usesRepoFilter(dashboard.type)) {
    const active = selectedRepos.length > 0
      ? repos.filter(r => selectedRepos.includes(`${r.owner}/${r.name}`))
      : repos
    body.owner = active.map(r => r.owner)
    body.name = active.map(r => r.name)
  }

  const now = new Date()
  const today = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()))

  if (timeRange === 'custom') {
    if (customFrom) body.from = Math.floor(new Date(customFrom + 'T00:00:00Z').getTime() / 1000)
    if (customTo) body.to = Math.floor(new Date(customTo + 'T00:00:00Z').getTime() / 1000)
  } else {
    const days = timeRange === 'days60' ? 60 : timeRange === 'days90' ? 90 : 30
    const from = new Date(today)
    from.setUTCDate(from.getUTCDate() - days)
    body.from = Math.floor(from.getTime() / 1000)
    body.to = Math.floor(today.getTime() / 1000)
  }

  if (dashboard.connection_id) body.connectionid = dashboard.connection_id
  if (dashboard.jira_project) body.jiraproject = dashboard.jira_project

  return body
}
