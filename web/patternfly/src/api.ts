export interface FrontendConfig {
  user_name: string
  user_email: string
  is_admin: boolean
  roles: string[]
  devlake_enabled: boolean
  ai_analytics_url: string
  workflow_base_url: string
  public_api_url?: string
  test_mode: boolean
}

export interface Org {
  id: string
  name: string
  auth_group?: string
  description?: string
  logo_url?: string
  products?: string[]
}

export interface Product {
  id: string
  name: string
  contact_email?: string
  auth_group?: string
  teams?: string[]
}

export interface Team {
  id: string
  name: string
  auth_group?: string
}

export interface Diagram {
  auto_id: number
  position: number
  type: string
  endpoint: string
  span?: number
  title?: string
  description?: string
  documentation_url?: string
  ai_analytics: boolean
  params?: Record<string, unknown>
}

export interface Dashboard {
  auto_id: number
  dashboard_id: string
  owner_type: 'org' | 'product' | 'team'
  owner_id: string
  type: string
  title: string
  description?: string
  connection_id?: string
  jira_project?: string
  icon?: string
  category?: string
  diagrams?: Diagram[]
}

export interface Repo {
  id: string
  type: string
  owner: string
  name: string
}

export interface WorkitemProject {
  id: string
  projectname: string
  jiraproject: string
  boardid?: string
  connectionid?: string
  type: string
}

interface SourceRow {
  id: string
  type: string
  jira_connection_id?: string
  repos?: Repo[]
  projects?: Array<{
    id: string
    project_name: string
    jira_project?: string
    board_id?: string
    type: string
  }>
}

async function fetchJson<T>(url: string): Promise<T> {
  const resp = await fetch(url)
  if (!resp.ok) {
    throw new Error(`HTTP ${resp.status}: ${await resp.text()}`)
  }
  return resp.json() as Promise<T>
}

export async function fetchConfig(): Promise<FrontendConfig> {
  return fetchJson<FrontendConfig>('/api/config')
}

export interface AuthScope {
  id: string
  aggregate: boolean
}

export interface AuthGrant {
  operator: boolean
  orgs: AuthScope[]
  products: AuthScope[]
  teams: string[]
}

export interface AuthMe {
  authenticated: boolean
  username: string
  email: string
  is_admin: boolean
  grant: AuthGrant
}

export async function fetchAuthMe(): Promise<AuthMe> {
  return fetchJson<AuthMe>('/api/v1/auth/me')
}

export async function fetchOrgs(): Promise<Org[]> {
  return fetchJson<Org[]>('/api/orgs')
}

export async function fetchProducts(): Promise<Product[]> {
  return fetchJson<Product[]>('/api/products')
}

export async function fetchTeams(): Promise<Team[]> {
  return fetchJson<Team[]>('/api/teams')
}

export async function fetchDashboards(ownerType: string, ownerId: string): Promise<Dashboard[]> {
  return fetchJson<Dashboard[]>(
    `/api/v1/dashboards?owner_type=${encodeURIComponent(ownerType)}&owner_id=${encodeURIComponent(ownerId)}`,
  )
}

export interface DataFreshness {
  last_collected_at?: string
}

export async function fetchDataFreshness(ownerType: string, ownerId: string): Promise<DataFreshness> {
  return fetchJson<DataFreshness>(
    `/api/v1/data-freshness?owner_type=${encodeURIComponent(ownerType)}&owner_id=${encodeURIComponent(ownerId)}`,
  )
}

export async function fetchDiagramData(
  endpoint: string,
  body: Record<string, unknown>,
  signal?: AbortSignal,
): Promise<unknown> {
  const resp = await fetch(endpoint, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  })
  if (!resp.ok) throw new Error(`Metric query failed: ${await resp.text()}`)
  return resp.json()
}

export interface FullTeam {
  id: string
  name: string
  contact_email?: string
  auth_group?: string
  product_id?: string
}

export interface FullProduct {
  id: string
  name: string
  contact_email?: string
  auth_group?: string
  org_id?: string
}

export interface TeamLink {
  id: string
  sort_order: number
}

export interface ConfigKey {
  key: string
  label: string
  description: string
  type: string
}

export interface DashboardTemplate {
  dashboard_id: string
  type: string
  title: string
  description?: string
  chat_url?: string
  ai_analytics_url?: string
  default_source?: string
  connection_id?: string
  jira_project?: string
  icon?: string
  category?: string
  diagrams: Array<{
    position: number
    type: string
    endpoint: string
    span?: number
    title: string
    ai_analytics: boolean
    description?: string
    params?: Record<string, unknown>
  }>
}

export interface ProviderRepo {
  full_name: string
  plugin: string
  owner: string
  name: string
  board_id?: string
}

export interface JiraIssueType {
  name: string
}

export interface JiraField {
  id: string
  name: string
  custom: boolean
}

export interface JiraProjectMeta {
  issue_types: JiraIssueType[]
  fields: JiraField[]
}

export interface JiraScopeConfig {
  requirement_types: string[]
  bug_types: string[]
  story_point_field: string
}

export interface OnboardResult {
  team_id: string
  status: string
  sync?: {
    created?: string[]
    updated?: string[]
    deleted?: string[]
    skipped?: string[]
    errors?: string[]
  }
  connection_ids?: Record<string, number>
  error?: string
}

async function fetchJsonAuth<T>(url: string, opts?: RequestInit): Promise<T> {
  const resp = await fetch(url, opts)
  if (!resp.ok) throw new Error(`HTTP ${resp.status}: ${await resp.text()}`)
  return resp.json() as Promise<T>
}

export async function fetchAllTeams(): Promise<FullTeam[]> {
  return fetchJsonAuth<FullTeam[]>('/api/v1/teams')
}

export interface TeamProductLink {
  team_id: string
  product_id: string
}

export async function fetchTeamProductLinks(): Promise<TeamProductLink[]> {
  return fetchJsonAuth<TeamProductLink[]>('/api/v1/team-product-links')
}

export async function fetchAllProducts(): Promise<FullProduct[]> {
  return fetchJsonAuth<FullProduct[]>('/api/v1/products')
}

export async function updateTeam(id: string, data: { name: string; contact_email?: string; auth_group?: string }): Promise<FullTeam> {
  return fetchJsonAuth<FullTeam>(`/api/v1/teams/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
}

export async function onboardTeam(payload: Record<string, unknown>): Promise<Record<string, unknown>> {
  return fetchJsonAuth<Record<string, unknown>>('/api/v1/onboard', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
}

export async function offboardTeam(teamId: string): Promise<Record<string, unknown>> {
  return fetchJsonAuth<Record<string, unknown>>('/api/v1/offboard', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ team_id: teamId }),
  })
}

export async function createProduct(data: { id: string; name: string; contact_email?: string; auth_group?: string }): Promise<FullProduct> {
  return fetchJsonAuth<FullProduct>('/api/v1/products', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
}

export async function updateProduct(id: string, data: { name: string; contact_email?: string; auth_group?: string }): Promise<FullProduct> {
  return fetchJsonAuth<FullProduct>(`/api/v1/products/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
}

export async function deleteProduct(id: string): Promise<void> {
  const resp = await fetch(`/api/v1/products/${encodeURIComponent(id)}`, { method: 'DELETE' })
  if (!resp.ok) throw new Error(`Failed to delete product: ${await resp.text()}`)
}

export async function fetchProductTeamLinks(productId: string): Promise<TeamLink[]> {
  return fetchJsonAuth<TeamLink[]>(`/api/v1/products/${encodeURIComponent(productId)}/teams`)
}

export async function updateProductTeamLinks(productId: string, entries: TeamLink[]): Promise<TeamLink[]> {
  return fetchJsonAuth<TeamLink[]>(`/api/v1/products/${encodeURIComponent(productId)}/teams`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(entries),
  })
}

export async function fetchOrgProductLinks(orgId: string): Promise<Array<{ id: string; sort_order: number }>> {
  return fetchJsonAuth<Array<{ id: string; sort_order: number }>>(`/api/v1/orgs/${encodeURIComponent(orgId)}/products`)
}

export async function updateOrgProductLinks(orgId: string, entries: Array<{ id: string; sort_order: number }>): Promise<Array<{ id: string; sort_order: number }>> {
  return fetchJsonAuth<Array<{ id: string; sort_order: number }>>(`/api/v1/orgs/${encodeURIComponent(orgId)}/products`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(entries),
  })
}

export async function fetchTeamSources(teamId: string): Promise<{ repos: Repo[], projects: WorkitemProject[] }> {
  const rows = await fetchJson<SourceRow[]>(
    `/api/v1/teams/${encodeURIComponent(teamId)}/sources`,
  )
  const repos: Repo[] = []
  const projects: WorkitemProject[] = []
  for (const row of rows) {
    if (row.repos) repos.push(...row.repos)
    if (row.projects) projects.push(...row.projects.map(project => ({
      id: JSON.stringify([teamId, row.id, project.id]),
      projectname: project.project_name,
      jiraproject: project.jira_project || '',
      boardid: project.board_id,
      connectionid: project.type === 'jira' ? row.jira_connection_id : undefined,
      type: project.type,
    })))
  }
  return { repos, projects }
}

export async function fetchConfigKeys(): Promise<ConfigKey[]> {
  return fetchJson<ConfigKey[]>('/api/v1/config-keys')
}

export async function fetchDashboardCatalog(): Promise<DashboardTemplate[]> {
  return fetchJson<DashboardTemplate[]>('/api/v1/dashboard-template-catalog')
}

export async function testConnection(
  plugin: string,
  endpoint: string,
  token: string,
  options?: { username?: string; organization?: string; service?: string; ci_tool?: TestRegistryCITool },
): Promise<{ success: boolean; message: string; expiry_date?: string }> {
  return fetchJsonAuth<{ success: boolean; message: string; expiry_date?: string }>(
    '/api/v1/devlake/test-connection',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ plugin, endpoint: endpoint.replace(/\/+$/, ''), token, ...options }),
    },
  )
}

export async function lookupProviderRepo(
  plugin: 'github' | 'gitlab',
  endpoint: string,
  token: string,
  owner: string,
  name: string,
): Promise<{ valid: boolean; error?: string; warning?: string; canonical_name?: string }> {
  return fetchJsonAuth('/api/v1/devlake/proxy-lookup', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      plugin,
      endpoint: endpoint.replace(/\/+$/, ''),
      token,
      owner,
      name,
    }),
  })
}

export async function fetchProviderRepos(
  plugin: 'github' | 'gitlab',
  endpoint: string,
  token: string,
): Promise<ProviderRepo[]> {
  const raw = await fetchJsonAuth<Array<{ key: string; name: string }>>(
    '/api/v1/devlake/proxy-browse',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ plugin, endpoint: endpoint.replace(/\/+$/, ''), token }),
    },
  )
  return raw.map((r) => {
    const parts = r.key.split('/')
    return {
      full_name: r.key,
      plugin,
      owner: plugin === 'gitlab' ? parts.slice(0, -1).join('/') : parts[0] ?? '',
      name: plugin === 'gitlab' ? (parts[parts.length - 1] ?? '') : parts.slice(1).join('/'),
    }
  })
}

export async function fetchTestRegistryScopes(
  ciTool: TestRegistryCITool,
  organization: string,
  token: string,
): Promise<ProviderRepo[]> {
  const raw = await fetchJsonAuth<Array<{ key: string; name: string }>>(
    '/api/v1/devlake/proxy-browse',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ plugin: 'testregistry', ci_tool: ciTool, organization, token }),
    },
  )
  return raw.map(s => ({ full_name: s.key, plugin: 'testregistry', owner: '', name: s.name }))
}

export async function fetchProviderProjects(
  endpoint: string,
  token: string,
  username: string,
  query = '',
): Promise<ProviderRepo[]> {
  const raw = await fetchJsonAuth<Array<{ key: string; name: string; board_id?: string }>>(
    '/api/v1/devlake/proxy-browse',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ plugin: 'jira', endpoint, token, username, query }),
    },
  )
  return raw.map((p) => ({
    full_name: p.board_id ? `Board ${p.board_id} - ${p.name}` : `${p.key} - ${p.name}`,
    plugin: 'jira',
    owner: p.key,
    name: p.name,
    board_id: p.board_id,
  }))
}

export async function fetchJiraMeta(
  endpoint: string,
  token: string,
  username: string,
  projectKey: string,
): Promise<JiraProjectMeta> {
  return fetchJsonAuth<JiraProjectMeta>(
    '/api/v1/devlake/proxy-jira-meta',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ plugin: 'jira', endpoint, token, username, project_key: projectKey }),
    },
  )
}

export async function fetchProxyJiraBoardMeta(
  endpoint: string,
  token: string,
  username: string,
  boardId: string,
): Promise<{ project_key: string } & JiraProjectMeta> {
  return fetchJsonAuth<{ project_key: string } & JiraProjectMeta>(
    '/api/v1/devlake/proxy-jira-board-meta',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ plugin: 'jira', endpoint, token, username, board_id: boardId }),
    },
  )
}

export interface TeamConfig {
  [key: string]: string
}

export interface SyncStatus {
  last_sync_time: string | null
  last_sync_result: {
    resolved?: string[]
    failed?: string[]
    errors?: string[]
    skipped?: string[]
  } | null
}

export interface Source {
  id: string
  type: string
  owner: string
  name: string
  full_name?: string
  board_id?: string
}

export async function fetchTeamConfig(teamId: string): Promise<TeamConfig> {
  return fetchJsonAuth<TeamConfig>(`/api/v1/config/team/${encodeURIComponent(teamId)}`)
}

export async function updateTeamConfig(teamId: string, config: TeamConfig): Promise<TeamConfig> {
  return fetchJsonAuth<TeamConfig>(`/api/v1/config/team/${encodeURIComponent(teamId)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  })
}

export async function fetchTeamDashboards(teamId: string): Promise<Dashboard[]> {
  return fetchJsonAuth<Dashboard[]>(`/api/v1/dashboards?owner_type=team&owner_id=${encodeURIComponent(teamId)}`)
}

export async function createTeamDashboard(teamId: string, template: DashboardTemplate): Promise<Dashboard> {
  return fetchJsonAuth<Dashboard>('/api/v1/dashboards', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      owner_type: 'team',
      owner_id: teamId,
      dashboard_id: template.dashboard_id,
      type: template.type,
      title: template.title,
      description: template.description || '',
      chat_url: template.chat_url || '',
      ai_analytics_url: template.ai_analytics_url || '',
      default_source: template.default_source || '',
      connection_id: template.connection_id || '',
      jira_project: template.jira_project || '',
      icon: template.icon || '',
      category: template.category || '',
      diagrams: template.diagrams || [],
    }),
  })
}

export async function deleteTeamDashboard(autoId: number): Promise<void> {
  const resp = await fetch(`/api/v1/dashboards/${autoId}`, { method: 'DELETE' })
  if (!resp.ok) throw new Error(`Failed to remove dashboard: ${await resp.text()}`)
}

export async function fetchSyncStatus(teamId: string): Promise<SyncStatus> {
  return fetchJsonAuth<SyncStatus>(`/api/v1/teams/${encodeURIComponent(teamId)}/sync-status`)
}

export async function fetchTeamJiraBoardMeta(
  teamId: string,
  boardId: string,
): Promise<{ project_key: string } & JiraProjectMeta> {
  return fetchJsonAuth<{ project_key: string } & JiraProjectMeta>(
    `/api/v1/devlake/teams/${encodeURIComponent(teamId)}/jira-board-meta/${encodeURIComponent(boardId)}`,
  )
}

// --------------- Token / Connection management ---------------

export interface TokenStatus {
  plugin: string
  conn_id: string
  expiry_date: string
}

export interface DevLakeConnection {
  plugin: string
  endpoint: string
  token: string
  expiry_date?: string
  username?: string
  organization?: string
  service?: string
  ci_tool?: TestRegistryCITool
}

export const TESTREGISTRY_CI_TOOLS = ['Openshift CI', 'Tekton CI'] as const
export type TestRegistryCITool = (typeof TESTREGISTRY_CI_TOOLS)[number]

export interface AvailableCodecov {
  connections: Array<{ id: number; name: string; endpoint: string; organization: string; service: string }>
  blueprints: Array<{ id: number; name: string; connection_id: number; scopes: string[] }>
}

export async function fetchAvailableCodecov(teamId: string): Promise<AvailableCodecov> {
  return fetchJsonAuth<AvailableCodecov>(`/api/v1/devlake/teams/${encodeURIComponent(teamId)}/codecov/available`)
}

export async function attachCodecov(teamId: string, connectionId: number, blueprintId: number): Promise<void> {
  await fetchJsonAuth(`/api/v1/devlake/teams/${encodeURIComponent(teamId)}/codecov/attach`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ connection_id: connectionId, blueprint_id: blueprintId }),
  })
}

export async function fetchTokenStatus(teamId: string): Promise<TokenStatus[]> {
  try {
    return await fetchJsonAuth<TokenStatus[]>(`/api/v1/devlake/teams/${encodeURIComponent(teamId)}/token-status`)
  } catch {
    return []
  }
}

export async function createDevLakeConnection(teamId: string, entry: DevLakeConnection): Promise<DevLakeConnection> {
  return fetchJsonAuth<DevLakeConnection>(`/api/v1/devlake/teams/${encodeURIComponent(teamId)}/connections`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(entry),
  })
}

export async function updateDevLakeConnection(
  teamId: string, plugin: string, entry: Partial<DevLakeConnection>,
): Promise<DevLakeConnection> {
  return fetchJsonAuth<DevLakeConnection>(
    `/api/v1/devlake/teams/${encodeURIComponent(teamId)}/connections/${encodeURIComponent(plugin)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(entry),
    },
  )
}

// --------------- Personal tokens ---------------

export interface PersonalToken {
  plugin: string
  endpoint: string
  expiry_date: string
  has_token: boolean
}

export async function fetchPersonalTokens(): Promise<PersonalToken[]> {
  return fetchJsonAuth<PersonalToken[]>('/api/v1/personal-tokens')
}

export async function savePersonalToken(plugin: string, data: {
  endpoint: string; token: string; expiry_date: string; username?: string;
}): Promise<void> {
  await fetchJsonAuth(`/api/v1/personal-tokens/${encodeURIComponent(plugin)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  })
}

export async function deletePersonalToken(plugin: string): Promise<void> {
  const resp = await fetch(`/api/v1/personal-tokens/${encodeURIComponent(plugin)}`, { method: 'DELETE' })
  if (!resp.ok) throw new Error(`Failed to delete token: ${await resp.text()}`)
}

// --------------- My Issues (State of the Union) ---------------

export interface UnifiedIssue {
  source: 'github' | 'gitlab' | 'jira'
  source_id: string
  url: string
  key: string
  title: string
  status: string
  raw_status: string
  priority?: string
  type: string
  labels: string[]
  created: string
  updated: string
  repo: string
}

export interface IssueTransition {
  id: string
  name: string
}

export interface MyIssuesResponse {
  issues: UnifiedIssue[]
  has_tokens: boolean
  errors?: string[]
}

export async function fetchMyIssues(): Promise<MyIssuesResponse> {
  return fetchJsonAuth<MyIssuesResponse>('/api/v1/my-issues')
}

export async function fetchMyContributions(): Promise<MyIssuesResponse> {
  return fetchJsonAuth<MyIssuesResponse>('/api/v1/my-contributions')
}

export async function fetchIssueTransitions(
  source: string, sourceId: string, status: string,
): Promise<IssueTransition[]> {
  const params = new URLSearchParams({ source, source_id: sourceId, status })
  const data = await fetchJsonAuth<{ transitions: IssueTransition[] }>(`/api/v1/my-issues/transitions?${params}`)
  return data.transitions
}

export async function executeIssueTransition(
  source: string, sourceId: string, transitionId: string,
): Promise<void> {
  await fetchJsonAuth('/api/v1/my-issues/transition', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ source, source_id: sourceId, transition_id: transitionId }),
  })
}

// --------------- Quick Links ---------------

export interface QuickLink {
  source: 'github' | 'gitlab' | 'jira'
  kind: string
  name: string
  url: string
  icon?: string
}

export interface MyLinksResponse {
  links: QuickLink[]
}

export async function fetchAiAnalytics(
  diagramData: unknown,
  signal?: AbortSignal,
): Promise<string> {
  const resp = await fetch('/api/v1/ai/analytics', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ diagram_data: diagramData }),
    signal,
  })
  if (!resp.ok) throw new Error(`AI analytics failed: ${await resp.text()}`)
  const json = await resp.json() as { html: string }
  return json.html
}

export async function fetchMyLinks(): Promise<MyLinksResponse> {
  return fetchJsonAuth<MyLinksResponse>('/api/v1/my-links')
}
