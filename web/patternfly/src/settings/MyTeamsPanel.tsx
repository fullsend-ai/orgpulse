import { useState, useEffect, useCallback, useMemo } from 'react'
import { Portal } from '../Portal'
import { BrowseList, type BrowseListItem } from './BrowseList'
import {
  fetchAllTeams,
  updateTeam,
  offboardTeam,
  fetchOrgs,
  fetchProducts,
  fetchOrgProductLinks,
  fetchConfigKeys,
  fetchDashboardCatalog,
  fetchTeamConfig,
  updateTeamConfig,
  fetchTeamDashboards,
  createTeamDashboard,
  deleteTeamDashboard,
  fetchSyncStatus,
  fetchTeamJiraBoardMeta,
  testConnection,
  fetchProviderRepos,
  lookupProviderRepo,
  fetchProviderProjects,
  fetchTestRegistryScopes,
  TESTREGISTRY_CI_TOOLS,
  type TestRegistryCITool,
  fetchJiraMeta,
  fetchProxyJiraBoardMeta,
  type FullTeam,
  type Org,
  type Product,
  type ConfigKey,
  type Dashboard,
  type DashboardTemplate,
  type ProviderRepo,
  type JiraProjectMeta,
  type JiraScopeConfig,
  type OnboardResult,
  type TeamConfig,
  type SyncStatus,
  type Source,
} from '../api'
import { entityIDError } from '../entityId'

function slugify(str: string): string {
  return str.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')
}

function contactEmailError(value: string): string | null {
  const email = value.trim()
  if (!email) return null
  const parts = email.split('@')
  if (parts.length !== 2 || !parts[0] || !parts[1] || email.includes(' '))
    return 'Contact email must be a valid email address.'
  if (parts[0].startsWith('`') || parts[0].endsWith('`'))
    return 'Contact email must be a valid email address.'
  const domain = parts[1]
  if (!domain.includes('.') || domain.startsWith('.') || domain.endsWith('.'))
    return 'Contact email must include a valid domain.'
  return null
}

interface MyTeamsPanelProps {
  email: string
  isAdmin: boolean
  devlakeEnabled: boolean
}

export default function MyTeamsPanel({ email, isAdmin, devlakeEnabled }: MyTeamsPanelProps) {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [teams, setTeams] = useState<FullTeam[]>([])
  const [allTeamIDs, setAllTeamIDs] = useState<Set<string>>(new Set())
  const [editing, setEditing] = useState<FullTeam | null>(null)
  const [showWizard, setShowWizard] = useState(false)
  const [deleteMsg, setDeleteMsg] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const all = await fetchAllTeams()
      setAllTeamIDs(new Set(all.map(team => team.id)))
      setTeams(all)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load teams')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const handleDelete = async (team: FullTeam) => {
    const ok = window.confirm(
      `Delete team "${team.name}"?\n\nThis will remove the team and all its sources, dashboards, and configuration. This cannot be undone.`,
    )
    if (!ok) return
    setDeleteMsg(`Deleting team "${team.name}"...`)
    try {
      await offboardTeam(team.id)
      setEditing(null)
      setDeleteMsg(`Team "${team.name}" deleted.`)
      setTimeout(() => { setDeleteMsg(null); void load() }, 2500)
    } catch (err) {
      setDeleteMsg(`Failed to delete team: ${err instanceof Error ? err.message : 'Unknown'}`)
      setTimeout(() => setDeleteMsg(null), 5000)
    }
  }

  if (loading) {
    return (
      <div className="settings-loader">
        <div className="diagram-card__spinner" />
        <div className="settings-loader-text">Loading teams...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="sub-error">
        <p className="error-text">{error}</p>
        <button className="sub-btn sub-btn--secondary" onClick={() => void load()}>Retry</button>
      </div>
    )
  }

  if (editing) {
    return (
      <TeamEditor
        team={editing}
        isAdmin={isAdmin}
        email={email}
        onBack={() => { setEditing(null); void load() }}
        onDelete={() => void handleDelete(editing)}
      />
    )
  }

  return (
    <div>
      <div className="sub-header">
        <div>
          <p className="settings-panel-description">
            Teams where you are listed as the contact.
          </p>
        </div>
        <button className="sub-btn sub-btn--primary" onClick={() => setShowWizard(true)}>
          + New Team
        </button>
      </div>

      {deleteMsg && (
        <div className="mt-delete-result">
          <div className="mt-delete-result-text">{deleteMsg}</div>
        </div>
      )}

      {teams.length === 0 && !deleteMsg && (
        <div className="sub-empty">
          <p>No teams found. You are not listed as the contact on any team.</p>
        </div>
      )}

      {teams.length > 0 && (
        <table className="sub-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>ID</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {teams.map(team => (
              <tr key={team.id}>
                <td>{team.name}</td>
                <td><code>{team.id}</code></td>
                <td className="sub-actions">
                  <button className="sub-btn sub-btn--small" onClick={() => setEditing(team)}>Edit</button>
                  {isAdmin && <button className="sub-btn sub-btn--small sub-btn--danger" onClick={() => void handleDelete(team)}>Delete</button>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {showWizard && (
        <TeamWizard
          email={email}
          isAdmin={isAdmin}
          devlakeEnabled={devlakeEnabled}
          existingTeamIDs={allTeamIDs}
          onCreated={() => { setShowWizard(false); void load() }}
          onCancel={() => setShowWizard(false)}
        />
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Team Editor (tabbed: General, Sources, Dashboards)
// ---------------------------------------------------------------------------

type EditTab = 'general' | 'sources' | 'dashboards'

function TeamEditor({
  team, isAdmin, email, onBack, onDelete,
}: {
  team: FullTeam
  isAdmin: boolean
  email: string
  onBack: () => void
  onDelete: () => void
}) {
  const [tab, setTab] = useState<EditTab>('general')
  const [name, setName] = useState(team.name)
  const [contactEmail, setContactEmail] = useState(team.contact_email ?? email)
  const [authGroup, setAuthGroup] = useState(team.auth_group ?? '')
  const [saving, setSaving] = useState(false)
  const [saveMsg, setSaveMsg] = useState<string | null>(null)

  const [sources, setSources] = useState<Source[]>([])
  const [configKeys, setConfigKeys] = useState<ConfigKey[]>([])
  const [kvRows, setKvRows] = useState<Array<{ key: string; value: string }>>([])
  const [devlakeConfig, setDevlakeConfig] = useState<Record<string, string>>({})
  const [dashboards, setDashboards] = useState<Dashboard[]>([])
  const [catalog, setCatalog] = useState<DashboardTemplate[]>([])
  const [syncStatus, setSyncStatus] = useState<SyncStatus | null>(null)
  const [loadingTab, setLoadingTab] = useState(true)

  const loadAll = useCallback(async () => {
    setLoadingTab(true)
    try {
      const [cfg, keys, src, dbs, cat, sync] = await Promise.all([
        fetchTeamConfig(team.id),
        fetchConfigKeys(),
        fetchEditorSources(team.id),
        fetchTeamDashboards(team.id),
        fetchDashboardCatalog(),
        fetchSyncStatus(team.id).catch(() => null),
      ])
      setConfigKeys(keys)
      setSources(src)
      setDashboards(dbs)
      setCatalog(cat)
      setSyncStatus(sync)

      const devKeys: Record<string, string> = {}
      const nonDevKeys: Array<{ key: string; value: string }> = []
      for (const [k, v] of Object.entries(cfg)) {
        if (k.startsWith('devlake_')) devKeys[k] = v
        else nonDevKeys.push({ key: k, value: v })
      }
      setDevlakeConfig(devKeys)
      setKvRows(nonDevKeys)
    } catch {
      // best effort
    } finally {
      setLoadingTab(false)
    }
  }, [team.id])

  useEffect(() => { void loadAll() }, [loadAll])

  const reloadSources = useCallback(async () => {
    const [updated, status] = await Promise.all([
      fetchEditorSources(team.id),
      fetchSyncStatus(team.id).catch(() => null),
    ])
    setSources(updated)
    setSyncStatus(status)
  }, [team.id])

  const handleSave = async () => {
    if (!name.trim()) { window.alert('Name is required.'); return }
    if (!authGroup.trim()) { window.alert('Authorization Group is required.'); return }
    const emailErr = contactEmailError(contactEmail)
    if (emailErr) { window.alert(emailErr); return }
    setSaving(true)
    setSaveMsg(null)
    try {
      await updateTeam(team.id, { name: name.trim(), contact_email: contactEmail.trim(), auth_group: authGroup.trim() })
      const cfg: TeamConfig = { ...devlakeConfig }
      for (const row of kvRows) { if (row.key) cfg[row.key] = row.value }
      await updateTeamConfig(team.id, cfg)
      setSaveMsg('Team saved.')
      setTimeout(() => setSaveMsg(null), 3000)
    } catch (err) {
      setSaveMsg(`Error: ${err instanceof Error ? err.message : 'Unknown'}`)
    } finally {
      setSaving(false)
    }
  }

  const handleAddDashboard = async (template: DashboardTemplate) => {
    try {
      await createTeamDashboard(team.id, template)
      const d = await fetchTeamDashboards(team.id)
      setDashboards(d)
    } catch (err) {
      window.alert(`Error adding dashboard: ${err instanceof Error ? err.message : 'Unknown'}`)
    }
  }

  const handleRemoveDashboard = async (autoId: number) => {
    if (!window.confirm('Remove this dashboard from the team?')) return
    try {
      await deleteTeamDashboard(autoId)
      setDashboards(prev => prev.filter(d => d.auto_id !== autoId))
    } catch (err) {
      window.alert(`Error removing dashboard: ${err instanceof Error ? err.message : 'Unknown'}`)
    }
  }

  const handleOnboardSource = async (
    sourceId: string,
    items: Record<string, string>[],
    jiraScopeConfigs?: Record<string, JiraScopeConfig>,
  ) => {
    try {
      const body: Record<string, unknown> = { team_id: team.id, source_id: sourceId, items }
      if (jiraScopeConfigs && Object.keys(jiraScopeConfigs).length > 0) {
        body.jira_scope_configs = jiraScopeConfigs
      }
      const resp = await fetch('/api/v1/onboard-source', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!resp.ok) throw new Error(await resp.text())
      await reloadSources()
    } catch (err) {
      window.alert(`Error adding source: ${err instanceof Error ? err.message : 'Unknown'}`)
    }
  }

  const handleOffboardSource = async (sourceId: string, itemId: string, repoName?: string) => {
    const label = repoName ? `Remove ${repoName} from sources?` : 'Remove this item from the source?'
    if (!window.confirm(label)) return
    try {
      const resp = await fetch('/api/v1/offboard-source', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ team_id: team.id, source_id: sourceId, item_id: itemId }),
      })
      if (!resp.ok) {
        const text = await resp.text()
        let msg = text
        try { const body = JSON.parse(text) as { error?: string }; if (body.error) msg = body.error } catch { /* raw */ }
        throw new Error(msg)
      }
      await reloadSources()
    } catch (err) {
      window.alert(`Error removing source: ${err instanceof Error ? err.message : 'Unknown'}`)
    }
  }

  const usedKeys = kvRows.map(r => r.key).filter(Boolean)
  const tabs: { id: EditTab; label: string }[] = [
    { id: 'general', label: 'General' },
    { id: 'sources', label: 'Sources' },
    { id: 'dashboards', label: 'Dashboards' },
  ]

  return (
    <div>
      <div className="sub-header" style={{ alignItems: 'center' }}>
        <button className="sub-btn sub-btn--secondary" onClick={onBack}>&larr; Back to list</button>
        <h3 style={{ margin: 0, lineHeight: 1 }}>Edit Team: {team.name}</h3>
        {isAdmin && (
          <button className="sub-btn sub-btn--small sub-btn--danger" style={{ marginLeft: 'auto' }} onClick={onDelete}>
            Delete Team
          </button>
        )}
      </div>

      <div className="editor-tabs">
        {tabs.map(t => (
          <button key={t.id} className={`editor-tab${tab === t.id ? ' active' : ''}`} onClick={() => setTab(t.id)}>
            {t.label}
          </button>
        ))}
      </div>

      {loadingTab && (
        <div className="settings-loader">
          <div className="diagram-card__spinner" />
          <div className="settings-loader-text">Loading...</div>
        </div>
      )}

      {/* General Tab */}
      {!loadingTab && tab === 'general' && (
        <div className="sub-form">
          <div className="sub-form-row">
            <label>Team ID</label>
            <input type="text" className="sub-input" value={team.id} disabled />
          </div>
          <div className="sub-form-row">
            <label>Team Name</label>
            <input type="text" className="sub-input" value={name} onChange={e => setName(e.target.value)} />
          </div>
          <div className="sub-form-row">
            <label>Authorization Group <span style={{ color: '#c9190b' }}>*</span></label>
            <input type="text" className="sub-input" value={authGroup} onChange={e => setAuthGroup(e.target.value)} required />
          </div>
          <div className="sub-form-row">
            <label>Contact Email</label>
            <input type="email" className="sub-input" value={contactEmail} onChange={e => setContactEmail(e.target.value)} readOnly={!isAdmin} />
            {!isAdmin && <span className="mt-wizard-section-hint">Contact email is set from your authenticated identity.</span>}
            {isAdmin && contactEmail.trim().toLowerCase() !== email.trim().toLowerCase() && (
              <span className="mt-wizard-section-hint" style={{ color: '#b35c00' }}>
                Warning: this differs from your OAuth email ({email}). The team will be assigned to this contact.
              </span>
            )}
          </div>

          <div className="sub-form-section" style={{ marginTop: 20 }}>
            <div className="sub-form-section-header">
              <label>Configuration</label>
              {usedKeys.length < configKeys.length && (
                <button className="sub-btn sub-btn--small" onClick={() => setKvRows(prev => [...prev, { key: '', value: '' }])}>
                  + Add
                </button>
              )}
            </div>
            <div>
              {kvRows.length === 0 ? (
                <p className="sub-empty-hint">No configuration entries.</p>
              ) : (
                kvRows.map((row, idx) => (
                  <div key={idx} className="mt-kv-row">
                    <select
                      className="sub-input"
                      value={row.key}
                      onChange={e => setKvRows(prev => prev.map((r, i) => i === idx ? { ...r, key: e.target.value } : r))}
                    >
                      {!row.key && <option value="">-- select --</option>}
                      {configKeys.filter(ck => !usedKeys.includes(ck.key) || ck.key === row.key).map(ck => (
                        <option key={ck.key} value={ck.key}>{ck.label}</option>
                      ))}
                    </select>
                    <input
                      className="sub-input"
                      type={configKeys.find(ck => ck.key === row.key)?.type === 'number' ? 'number' : 'text'}
                      placeholder={configKeys.find(ck => ck.key === row.key)?.description || ''}
                      value={row.value}
                      onChange={e => setKvRows(prev => prev.map((r, i) => i === idx ? { ...r, value: e.target.value } : r))}
                    />
                    <button className="sub-btn sub-btn--small sub-btn--danger" onClick={() => setKvRows(prev => prev.filter((_, i) => i !== idx))}>
                      X
                    </button>
                  </div>
                ))
              )}
            </div>
          </div>

          {saveMsg && (
            <div className={saveMsg.startsWith('Error') ? 'mt-wizard-error' : 'mt-delete-result'} style={{ marginTop: 16 }}>
              <div className="mt-delete-result-text">{saveMsg}</div>
            </div>
          )}

          <div className="sub-form-actions" style={{ marginTop: 20 }}>
            <button className="sub-btn sub-btn--primary" disabled={saving || !name.trim()} onClick={() => void handleSave()}>
              {saving ? 'Saving...' : 'Save'}
            </button>
          </div>
        </div>
      )}

      {/* Sources Tab */}
      {!loadingTab && tab === 'sources' && (
        <EditorSourcesTab
          sources={sources}
          teamId={team.id}
          devlakeConfig={devlakeConfig}
          syncStatus={syncStatus}
          onOnboard={handleOnboardSource}
          onOffboard={handleOffboardSource}
        />
      )}

      {/* Dashboards Tab */}
      {!loadingTab && tab === 'dashboards' && (
        <div>
          {dashboards.length > 0 && (
            <table className="sub-table" style={{ marginBottom: 20 }}>
              <thead>
                <tr><th>Title</th><th>Type</th><th>Actions</th></tr>
              </thead>
              <tbody>
                {dashboards.map(d => (
                  <tr key={d.auto_id}>
                    <td>{d.title}</td>
                    <td>{d.type}</td>
                    <td>
                      <button className="sub-btn sub-btn--small sub-btn--danger" onClick={() => void handleRemoveDashboard(d.auto_id)}>
                        Remove
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {dashboards.length === 0 && <p className="sub-empty-hint" style={{ marginBottom: 16 }}>No dashboards configured.</p>}

          {catalog.length > 0 && (() => {
            const available = catalog.filter(t => !dashboards.some(d => d.dashboard_id === t.dashboard_id))
            if (available.length === 0) return null
            return (
              <div>
                <h4 style={{ margin: '0 0 12px' }}>Add Dashboard from Catalog</h4>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                  {available.map(t => (
                    <button key={t.dashboard_id} className="sub-btn sub-btn--secondary" onClick={() => void handleAddDashboard(t)}>
                      + {t.title}
                    </button>
                  ))}
                </div>
              </div>
            )
          })()}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Editor Sources Tab
// ---------------------------------------------------------------------------

function EditorSourcesTab({
  sources, teamId, devlakeConfig, syncStatus, onOnboard, onOffboard,
}: {
  sources: Source[]
  teamId: string
  devlakeConfig: Record<string, string>
  syncStatus: SyncStatus | null
  onOnboard: (sourceId: string, items: Record<string, string>[], jiraScopeConfigs?: Record<string, JiraScopeConfig>) => Promise<void>
  onOffboard: (sourceId: string, itemId: string, repoName?: string) => Promise<void>
}) {
  const grouped = groupSources(sources)

  return (
    <div>
      <p className="settings-panel-description" style={{ marginBottom: 16 }}>
        Repositories and projects associated with this team.
      </p>

      {sources.length > 0 && (() => {
        const gitGroup = grouped.find(g => g.sourceType === 'gitrepos')
        const jiraGroup = grouped.find(g => g.sourceType === 'workitems')
        const gitItems = gitGroup?.items ?? []
        const gh = gitItems.filter(s => s.type === 'github').length
        const gl = gitItems.filter(s => s.type === 'gitlab').length
        const jira = jiraGroup?.items.length ?? 0
        const tests = grouped.find(g => g.sourceType === 'testsuites')?.items.length ?? 0
        const failed = syncStatus?.last_sync_result?.failed ?? []
        const parts: string[] = []
        if (gh > 0) parts.push(`${gh} GitHub repo${gh !== 1 ? 's' : ''}`)
        if (gl > 0) parts.push(`${gl} GitLab repo${gl !== 1 ? 's' : ''}`)
        if (jira > 0) parts.push(`${jira} Jira project${jira !== 1 ? 's' : ''}`)
        if (tests > 0) parts.push(`${tests} TestRegistry scope${tests !== 1 ? 's' : ''}`)
        if (parts.length === 0) return null
        return (
          <p style={{ marginBottom: 12, fontSize: '0.85em', opacity: 0.8 }}>
            {parts.join(', ')}
            {failed.length > 0 && <span style={{ color: '#c9190b' }}>, {failed.length} failed</span>}
          </p>
        )
      })()}

      {grouped.length === 0 && <p className="sub-empty-hint">No sources configured.</p>}

      {grouped.map(g => (
        <SourceGroup
          key={g.sourceType}
          sourceType={g.sourceType}
          items={g.items}
          teamId={teamId}
          devlakeConfig={devlakeConfig}
          syncStatus={syncStatus}
          onOnboard={(items, scopeConfigs) => onOnboard(g.sourceType, items, scopeConfigs)}
          onOffboard={(itemId, repoName) => onOffboard(g.sourceType, itemId, repoName)}
        />
      ))}

      {!grouped.some(g => g.sourceType === 'gitrepos') && (
        <SourceGroup sourceType="gitrepos" items={[]} teamId={teamId} devlakeConfig={devlakeConfig} syncStatus={syncStatus}
          onOnboard={(items, sc) => onOnboard('gitrepos', items, sc)} onOffboard={(id, n) => onOffboard('gitrepos', id, n)} />
      )}
      {!grouped.some(g => g.sourceType === 'workitems') && (
        <SourceGroup sourceType="workitems" items={[]} teamId={teamId} devlakeConfig={devlakeConfig} syncStatus={syncStatus}
          onOnboard={(items, sc) => onOnboard('workitems', items, sc)} onOffboard={(id, n) => onOffboard('workitems', id, n)} />
      )}
      {!grouped.some(g => g.sourceType === 'testsuites') && (
        <SourceGroup sourceType="testsuites" items={[]} teamId={teamId} devlakeConfig={devlakeConfig} syncStatus={syncStatus}
          onOnboard={items => onOnboard('testsuites', items)} onOffboard={(id, n) => onOffboard('testsuites', id, n)} />
      )}
    </div>
  )
}

interface GroupedSource { sourceType: string; items: Source[] }

function groupSources(sources: Source[]): GroupedSource[] {
  const map = new Map<string, Source[]>()
  for (const s of sources) {
    const group = s.type === 'github' || s.type === 'gitlab' ? 'gitrepos'
      : s.type === 'jira' ? 'workitems'
        : s.type === 'testregistry' ? 'testsuites'
          : s.type
    if (!map.has(group)) map.set(group, [])
    map.get(group)!.push(s)
  }
  return Array.from(map.entries()).map(([sourceType, items]) => ({ sourceType, items }))
}

// ---------------------------------------------------------------------------
// Source Group (git repos or Jira boards)
// ---------------------------------------------------------------------------

function SourceGroup({
  sourceType, items, teamId, devlakeConfig, syncStatus, onOnboard, onOffboard,
}: {
  sourceType: string
  items: Source[]
  teamId: string
  devlakeConfig: Record<string, string>
  syncStatus: SyncStatus | null
  onOnboard: (items: Record<string, string>[], jiraScopeConfigs?: Record<string, JiraScopeConfig>) => void
  onOffboard: (itemId: string, repoName?: string) => void
}) {
  const codecovProvider = devlakeConfig.devlake_codecov_service?.startsWith('gitlab') ? 'gitlab' : 'github'
  const hasCodecov = !!devlakeConfig.devlake_codecov_connection_id && !!devlakeConfig.devlake_codecov_organization
  const isTests = sourceType === 'testsuites'
  const [addType, setAddType] = useState(sourceType === 'gitrepos' && hasCodecov &&
    !devlakeConfig.devlake_github_token && !devlakeConfig.devlake_gitlab_token ? codecovProvider
    : sourceType === 'gitrepos' ? 'github' : isTests ? 'testregistry' : 'jira')
  // TestRegistry (Tekton CI) connections have no token; browsing goes through the DevLake connection.
  const hasTokenForType = (plugin: string) => plugin === 'testregistry'
    ? !!devlakeConfig.devlake_testregistry_connection_id
    : !!devlakeConfig[`devlake_${plugin}_token`]
  const canAdd = sourceType === 'gitrepos'
    ? hasTokenForType('github') || hasTokenForType('gitlab') || hasCodecov
    : hasTokenForType(isTests ? 'testregistry' : 'jira')
  const testRegistryCITool = devlakeConfig.devlake_testregistry_ci_tool ?? ''
  const testRegistryOrg = devlakeConfig.devlake_testregistry_organization ?? ''
  const groupLabel = sourceType === 'gitrepos' ? 'Git Repositories' : isTests ? 'Test Suites (TestRegistry)' : 'Jira Boards'
  const itemLabel = sourceType === 'gitrepos' ? 'Repository' : isTests ? 'Scope' : 'Board'
  const emptyLabel = sourceType === 'gitrepos' ? 'repositories' : isTests ? 'TestRegistry scopes' : 'boards'

  useEffect(() => {
    if (sourceType === 'gitrepos' && hasCodecov && !devlakeConfig.devlake_github_token && !devlakeConfig.devlake_gitlab_token) {
      setAddType(codecovProvider)
    }
  }, [sourceType, hasCodecov, codecovProvider, devlakeConfig.devlake_github_token, devlakeConfig.devlake_gitlab_token])
  const [addOwner, setAddOwner] = useState('')
  const [addName, setAddName] = useState('')
  const [validating, setValidating] = useState(false)
  const [addError, setAddError] = useState<string | null>(null)
  const [showBrowse, setShowBrowse] = useState(false)
  const [pendingJiraItems, setPendingJiraItems] = useState<Record<string, string>[] | null>(null)

  const handleAddItem = async () => {
    if (isTests) {
      if (!addOwner.trim()) { setAddError('Scope name is required.'); return }
      setAddError(null)
      onOnboard([{ type: 'testregistry', project_name: addOwner.trim() }])
      setAddOwner('')
      return
    }
    if (sourceType === 'gitrepos') {
      if (!addOwner.trim() || !addName.trim()) { setAddError('Owner and Name are required.'); return }
    } else {
      if (!addOwner.trim()) { setAddError('Board ID is required.'); return }
      if (!/^\d+$/.test(addOwner.trim())) { setAddError('Board ID must be numeric.'); return }
    }
    setAddError(null)
    setValidating(true)
    try {
      if (sourceType === 'gitrepos') {
        onOnboard([{ type: addType, owner: addOwner.trim(), name: addName.trim() }])
      } else {
        const boardId = addOwner.trim()
        const metadata = await fetchTeamJiraBoardMeta(teamId, boardId)
        const jiraProject = metadata.project_key?.trim()
        if (!jiraProject) {
          throw new Error(`Could not determine the Jira project key for board ${boardId}.`)
        }
        const jiraItems = [{ type: addType, board_id: boardId, jira_project: jiraProject }]
        setPendingJiraItems(jiraItems)
      }
      setAddOwner('')
      setAddName('')
    } catch (err) {
      setAddError(err instanceof Error ? err.message : 'Unable to add source.')
    } finally {
      setValidating(false)
    }
  }

  return (
    <div className="sub-form" style={{ marginBottom: 20 }}>
      <div className="sub-form-section-header">
        <label>{groupLabel}</label>
      </div>

      {isTests && testRegistryCITool && (
        <p className="sub-empty-hint" style={{ marginBottom: 8 }}>
          {testRegistryCITool} · {testRegistryCITool === 'Openshift CI' ? 'GitHub' : 'Quay'} organization <code>{testRegistryOrg}</code>
        </p>
      )}

      {items.length > 0 && (
        <table className="sub-table" style={{ marginBottom: 12 }}>
          <thead>
            <tr>
              <th>Type</th>
              <th>{itemLabel}</th>
              {syncStatus?.last_sync_result && <th>Status</th>}
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {items.map(src => {
              const repoFullName = src.full_name || `${src.owner}/${src.name}`
              const resolved = syncStatus?.last_sync_result?.resolved ?? []
              const failed = syncStatus?.last_sync_result?.failed ?? []
              const isSynced = resolved.includes(repoFullName)
              const isFailed = failed.includes(repoFullName)
              return (
                <tr key={src.id}>
                  <td><span className="mt-source-type-badge">{src.type}</span></td>
                  <td>{repoFullName}</td>
                  {syncStatus?.last_sync_result && (
                    <td>
                      {isSynced && <span style={{ color: '#3e8635' }}>Registered</span>}
                      {isFailed && <span style={{ color: '#c9190b' }}>Failed</span>}
                      {!isSynced && !isFailed && <span style={{ color: '#6a6e73' }}>Pending</span>}
                    </td>
                  )}
                  <td>
                    <button className="sub-btn sub-btn--small sub-btn--danger" onClick={() => onOffboard(src.id, repoFullName)}>
                      Remove
                    </button>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}

      {items.length === 0 && (
        <p className="sub-empty-hint" style={{ marginBottom: 8 }}>
          No {emptyLabel} yet.
        </p>
      )}

      {!canAdd && (
        <p className="sub-empty-hint" style={{ marginTop: 8 }}>
          No {sourceType === 'gitrepos' ? 'GitHub, GitLab, or Codecov' : isTests ? 'TestRegistry' : 'Jira'} connection configured for this team.
        </p>
      )}

      {canAdd && (
        <>
          <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap', marginTop: 8 }}>
            <select className="sub-input" value={addType} onChange={e => setAddType(e.target.value)} style={{ width: 100, flex: 'none' }}>
              {sourceType === 'gitrepos' ? (
                <>
                  {(hasTokenForType('github') || (hasCodecov && codecovProvider === 'github')) && <option value="github">github</option>}
                  {(hasTokenForType('gitlab') || (hasCodecov && codecovProvider === 'gitlab')) && <option value="gitlab">gitlab</option>}
                </>
              ) : isTests ? (
                <option value="testregistry">testregistry</option>
              ) : (
                <option value="jira">jira</option>
              )}
            </select>
            <input
              className="sub-input" type="text"
              placeholder={sourceType === 'gitrepos' ? 'Owner'
                : isTests ? (testRegistryCITool === 'Tekton CI' ? `${testRegistryOrg || 'quay-org'}/repo` : 'Repo name')
                  : 'Board ID'}
              value={addOwner}
              onChange={e => setAddOwner(e.target.value)}
              style={{ flex: sourceType === 'gitrepos' || isTests ? 1 : '0 0 140px', minWidth: 100 }}
            />
            {sourceType === 'gitrepos' && (
              <input className="sub-input" type="text" placeholder="Name" value={addName} onChange={e => setAddName(e.target.value)} style={{ flex: 1, minWidth: 100 }} />
            )}
            <button className="sub-btn sub-btn--small" onClick={() => void handleAddItem()} disabled={validating}>
              {validating ? 'Checking...' : '+ Add'}
            </button>
            {hasTokenForType(addType) && (
              <button className="sub-btn sub-btn--small sub-btn--secondary" onClick={() => setShowBrowse(!showBrowse)}>
                Browse
              </button>
            )}
          </div>
          {addError && <p style={{ color: '#c9190b', fontSize: '0.85em', marginTop: 4 }}>{addError}</p>}

          {showBrowse && hasTokenForType(addType) && (
            <EditorBrowsePanel
              teamId={teamId}
              pluginType={addType}
              existingItems={items.map(s =>
                sourceType === 'gitrepos'
                  ? `${s.type}:${s.owner}/${s.name}`.toLowerCase()
                  : isTests
                    ? `testregistry:${s.full_name ?? s.name}`.toLowerCase()
                    : `jira:${s.board_id ?? s.owner}`.toLowerCase(),
              )}
              onAdd={selected => {
                if (sourceType === 'workitems') {
                  setPendingJiraItems(selected)
                } else {
                  onOnboard(selected)
                }
                setShowBrowse(false)
              }}
              onClose={() => setShowBrowse(false)}
            />
          )}

          {pendingJiraItems && (
            <JiraScopeConfigModal
              teamId={teamId}
              boards={pendingJiraItems.map(item => ({ board_id: item.board_id || '', jira_project: item.jira_project }))}
              onConfirm={scopeConfigs => {
                onOnboard(pendingJiraItems, Object.keys(scopeConfigs).length > 0 ? scopeConfigs : undefined)
                setPendingJiraItems(null)
              }}
              onCancel={() => { onOnboard(pendingJiraItems); setPendingJiraItems(null) }}
            />
          )}
        </>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Editor Browse Panel (uses team's existing DevLake connections)
// ---------------------------------------------------------------------------

function EditorBrowsePanel({
  teamId, pluginType, existingItems, onAdd, onClose,
}: {
  teamId: string
  pluginType: string
  existingItems: string[]
  onAdd: (items: Record<string, string>[]) => void
  onClose: () => void
}) {
  const [loading, setLoading] = useState(pluginType !== 'jira')
  const [allItems, setAllItems] = useState<BrowseListItem[]>([])
  const [filter, setFilter] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [browseError, setBrowseError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setSelected(new Set())
    const query = pluginType === 'jira' ? searchQuery.trim() : ''
    if (pluginType === 'jira' && query.length < 2) {
      setAllItems([])
      setBrowseError(null)
      setLoading(false)
      return () => { cancelled = true }
    }

    void (async () => {
      setLoading(true)
      setBrowseError(null)
      try {
        const params = new URLSearchParams()
        if (pluginType === 'jira' && query) params.set('search', query)
        const suffix = params.toString() ? `?${params.toString()}` : ''
        const resp = await fetch(
          `/api/v1/devlake/teams/${encodeURIComponent(teamId)}/browse-remote/${encodeURIComponent(pluginType)}${suffix}`,
        )
        if (!resp.ok) throw new Error(`Browse failed (${resp.status})`)
        const data = await resp.json() as BrowseListItem[]
        if (!cancelled) {
          const seen = new Set<string>()
          const unique = data.filter(item => {
            const key = `${item.plugin}:${item.full_name}`.toLowerCase()
            if (seen.has(key)) return false
            seen.add(key)
            return true
          })
          unique.sort((a, b) => a.full_name.localeCompare(b.full_name))
          setAllItems(unique)
        }
      } catch {
        if (!cancelled) {
          setBrowseError(pluginType === 'jira'
            ? 'Unable to search Jira boards.'
            : pluginType === 'testregistry'
              ? 'Unable to load TestRegistry scopes. Check the connection under Tokens.'
              : `Unable to load ${pluginType} repositories.`)
          setAllItems([])
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => { cancelled = true }
  }, [teamId, pluginType, searchQuery])

  const existingSet = useMemo(() => new Set(existingItems), [existingItems])
  const itemKey = useCallback((item: BrowseListItem) =>
    item.plugin === 'jira'
      ? `jira:${item.board_id ?? item.owner}`.toLowerCase()
      : item.plugin === 'testregistry'
        ? `testregistry:${item.full_name}`.toLowerCase()
        : `${item.plugin}:${item.owner}/${item.name}`.toLowerCase(), [])
  const browseNoun = pluginType === 'jira' ? 'projects' : pluginType === 'testregistry' ? 'scopes' : 'repos'

  const isItemAdded = useCallback(
    (item: BrowseListItem) => existingSet.has(itemKey(item)),
    [existingSet, itemKey],
  )
  const repoGroup = useCallback(
    (item: BrowseListItem) => item.owner || item.full_name.split('/')[0] || '',
    [],
  )

  const handleAdd = () => {
    const byKey = new Map(allItems.map(item => [itemKey(item), item]))
    const sel = Array.from(selected)
      .map(key => byKey.get(key))
      .filter((item): item is BrowseListItem => !!item && !isItemAdded(item))
    if (sel.length === 0) return
    const onboardItems = sel.map((item): Record<string, string> => {
      if (item.plugin === 'jira') {
        return { type: item.plugin, board_id: item.board_id || '', jira_project: item.owner || '' }
      }
      if (item.plugin === 'testregistry') {
        return { type: item.plugin, project_name: item.full_name }
      }
      return {
        type: item.plugin,
        owner: item.owner || item.full_name?.split('/')[0] || '',
        name: item.name || item.full_name?.split('/').slice(1).join('/') || '',
      }
    })
    onAdd(onboardItems)
  }

  return (
    <div className="mt-browse-panel" style={{ marginTop: 12 }}>
      <div className="mt-browse-filter">
        <input
          className="sub-input" type="text"
          placeholder={pluginType === 'jira' ? 'Search Jira boards...' : `Filter ${browseNoun}...`}
          value={filter}
          onChange={e => setFilter(e.target.value)}
          onKeyDown={e => { if (pluginType === 'jira' && e.key === 'Enter') setSearchQuery(filter.trim()) }}
          style={{ flex: 1 }}
          autoFocus
        />
        {pluginType === 'jira' && (
          <button className="sub-btn sub-btn--small sub-btn--secondary" disabled={filter.trim().length < 2 || loading}
            onClick={() => setSearchQuery(filter.trim())}>
            Search
          </button>
        )}
      </div>

      {loading && (
        <div className="settings-loader" style={{ padding: '20px 0' }}>
          <div className="diagram-card__spinner" />
          <div className="settings-loader-text">{pluginType === 'jira' ? 'Searching...' : `Loading ${browseNoun}...`}</div>
        </div>
      )}
      {browseError && !loading && <div className="mt-wizard-error">{browseError}</div>}

      {!loading && !browseError && (
        <>
          <BrowseList
            items={allItems}
            keyOf={itemKey}
            isItemAdded={isItemAdded}
            selected={selected}
            onSelectionChange={setSelected}
            emptyMessage={allItems.length === 0 ? `No ${browseNoun} found` : 'No matches'}
            filter={filter}
            groupBy={pluginType === 'jira' || pluginType === 'testregistry' ? undefined : repoGroup}
          />
          <div className="mt-browse-footer">
            <span style={{ fontSize: '0.85em', opacity: 0.7 }}>{selected.size} selected</span>
            <div style={{ display: 'flex', gap: 6 }}>
              <button className="sub-btn sub-btn--small sub-btn--secondary" onClick={onClose}>Cancel</button>
              <button className="sub-btn sub-btn--small sub-btn--primary" disabled={selected.size === 0} onClick={handleAdd}>
                Add Selected
              </button>
            </div>
          </div>
        </>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Jira Scope Config Modal (for edit-team source onboarding)
// ---------------------------------------------------------------------------

function JiraScopeConfigModal({
  teamId, boards, onConfirm, onCancel,
}: {
  teamId: string
  boards: Array<{ board_id: string; jira_project?: string }>
  onConfirm: (configs: Record<string, JiraScopeConfig>) => void
  onCancel: () => void
}) {
  const [metaByBoard, setMetaByBoard] = useState<
    Record<string, { loading: boolean; meta?: JiraProjectMeta; projectKey?: string; error?: string }>
  >({})
  const [configs, setConfigs] = useState<Record<string, JiraScopeConfig>>({})

  useEffect(() => {
    for (const board of boards) {
      if (metaByBoard[board.board_id]) continue
      setMetaByBoard(prev => ({ ...prev, [board.board_id]: { loading: true } }))
      fetchTeamJiraBoardMeta(teamId, board.board_id)
        .then(result => {
          const spField = result.fields?.find(f => f.name.trim().toLowerCase() === 'story points')
          setMetaByBoard(prev => ({
            ...prev,
            [board.board_id]: { loading: false, meta: { issue_types: result.issue_types, fields: result.fields }, projectKey: result.project_key },
          }))
          setConfigs(prev => {
            if (prev[board.board_id]) return prev
            return { ...prev, [board.board_id]: { requirement_types: [], bug_types: [], story_point_field: spField?.id ?? '' } }
          })
        })
        .catch(err => setMetaByBoard(prev => ({ ...prev, [board.board_id]: { loading: false, error: err instanceof Error ? err.message : String(err) } })))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [boards, teamId])

  const toggleType = (boardId: string, role: 'requirement_types' | 'bug_types', typeName: string) => {
    setConfigs(prev => {
      const cfg = prev[boardId] ?? { requirement_types: [], bug_types: [], story_point_field: '' }
      const list = cfg[role]
      return { ...prev, [boardId]: { ...cfg, [role]: list.includes(typeName) ? list.filter(t => t !== typeName) : [...list, typeName] } }
    })
  }

  return (
    <Portal>
    <div className="mt-wizard-overlay" onClick={e => { if (e.target === e.currentTarget) onCancel() }}>
      <div className="mt-wizard-modal mt-wizard-modal--compact">
        <div className="mt-wizard-header">
          <h3>Jira Scope Configuration</h3>
          <button className="mt-wizard-close" onClick={onCancel}>&times;</button>
        </div>
        <div className="mt-wizard-body">
          <div className="mt-wizard-section-hint">
            Configure issue type mappings and story point fields for each board.
          </div>

          {boards.map(board => {
            const entry = metaByBoard[board.board_id]
            const cfg = configs[board.board_id]
            return (
              <div key={board.board_id} className="sub-form-section" style={{ marginBottom: 20 }}>
                <div className="sub-form-section-header">
                  <label>
                    <span className="mt-source-type-badge" style={{ marginRight: 8 }}>{entry?.projectKey || board.jira_project || board.board_id}</span>
                    Board {board.board_id}
                  </label>
                </div>
                {(!entry || entry.loading) && (
                  <div className="settings-loader" style={{ justifyContent: 'flex-start', padding: '12px 0' }}>
                    <div className="diagram-card__spinner" />
                    <div className="settings-loader-text">Loading issue types and fields...</div>
                  </div>
                )}
                {entry?.error && <div className="mt-wizard-error">{entry.error}</div>}
                {entry?.meta && cfg && (
                  <div className="mt-jira-cfg-body">
                    <div className="sub-form-row" style={{ marginBottom: 12 }}>
                      <label style={{ fontWeight: 600, marginBottom: 4, display: 'block' }}>Requirement Issue Types</label>
                      <div className="mt-jira-type-list">
                        {(entry.meta.issue_types ?? []).map(t => (
                          <label key={t.name} className="mt-jira-type-cb">
                            <input type="checkbox" checked={cfg.requirement_types.includes(t.name)} onChange={() => toggleType(board.board_id, 'requirement_types', t.name)} />
                            {' '}{t.name}
                          </label>
                        ))}
                        {(entry.meta.issue_types ?? []).length === 0 && <span style={{ opacity: 0.6 }}>No issue types found</span>}
                      </div>
                    </div>
                    <div className="sub-form-row" style={{ marginBottom: 12 }}>
                      <label style={{ fontWeight: 600, marginBottom: 4, display: 'block' }}>Bug Issue Types</label>
                      <div className="mt-jira-type-list">
                        {(entry.meta.issue_types ?? []).map(t => (
                          <label key={t.name} className="mt-jira-type-cb">
                            <input type="checkbox" checked={cfg.bug_types.includes(t.name)} onChange={() => toggleType(board.board_id, 'bug_types', t.name)} />
                            {' '}{t.name}
                          </label>
                        ))}
                      </div>
                    </div>
                    <div className="sub-form-row">
                      <label style={{ fontWeight: 600, marginBottom: 4, display: 'block' }}>Story Points Field</label>
                      <select className="sub-input" value={cfg.story_point_field}
                        onChange={e => setConfigs(prev => ({ ...prev, [board.board_id]: { ...cfg, story_point_field: e.target.value } }))}
                        style={{ maxWidth: 400 }}>
                        <option value="">(none)</option>
                        {[...(entry.meta.fields ?? [])].sort((a, b) => {
                          const ar = a.name.trim().toLowerCase() === 'story points'
                          const br = b.name.trim().toLowerCase() === 'story points'
                          if (ar !== br) return ar ? -1 : 1
                          return a.name.localeCompare(b.name)
                        }).map(f => <option key={f.id} value={f.id}>{f.name}{f.custom ? ' (custom)' : ''}</option>)}
                      </select>
                    </div>
                  </div>
                )}
              </div>
            )
          })}
        </div>
        <div className="mt-wizard-footer">
          <div />
          <div className="mt-wizard-footer-right">
            <button className="sub-btn sub-btn--secondary" onClick={onCancel}>Skip</button>
            <button className="sub-btn sub-btn--primary" onClick={() => onConfirm(configs)}>Save Configuration</button>
          </div>
        </div>
      </div>
    </div>
    </Portal>
  )
}

// ---------------------------------------------------------------------------
// Helper: fetch sources in editor format
// ---------------------------------------------------------------------------

async function fetchEditorSources(teamId: string): Promise<Source[]> {
  const resp = await fetch(`/api/v1/teams/${encodeURIComponent(teamId)}/sources`)
  if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
  const rows = await resp.json() as Array<{
    id: string; type: string
    repos?: Array<{ id: string; type: string; owner: string; name: string }>
    projects?: Array<{ id: string; project_name?: string; jira_project?: string; board_id?: string; type: string }>
  }>
  const flat: Source[] = []
  for (const row of rows) {
    if (row.repos) {
      for (const r of row.repos) {
        flat.push({ id: r.id, type: r.type, owner: r.owner, name: r.name, full_name: `${r.owner}/${r.name}` })
      }
    }
    if (row.projects) {
      for (const p of row.projects) {
        if (p.type === 'testregistry') {
          const fullName = p.project_name ?? ''
          flat.push({ id: p.id, type: p.type, owner: '', name: fullName, full_name: fullName })
          continue
        }
        flat.push({
          id: p.id, type: p.type, owner: p.jira_project ?? '', name: p.board_id ? `Board ${p.board_id}` : '',
          full_name: p.board_id ? `Board ${p.board_id}` : (p.jira_project ?? ''), board_id: p.board_id,
        })
      }
    }
  }
  return flat
}

// ---------------------------------------------------------------------------
// Connection plugin definitions
// ---------------------------------------------------------------------------

const CONN_PLUGIN_DEFS = [
  { id: 'github' as const, label: 'GitHub', defaultEndpoint: 'https://api.github.com/', hasUser: false, tokenLabel: 'Personal Access Token' },
  { id: 'gitlab' as const, label: 'GitLab', defaultEndpoint: '', hasUser: false, tokenLabel: 'Access Token' },
  { id: 'jira' as const, label: 'Jira', defaultEndpoint: '', hasUser: true, tokenLabel: 'API Token' },
  { id: 'codecov' as const, label: 'Codecov', defaultEndpoint: 'https://api.codecov.io', hasUser: false, tokenLabel: 'Codecov API Token' },
  { id: 'testregistry' as const, label: 'TestRegistry', defaultEndpoint: '', hasUser: false, tokenLabel: 'GitHub Token' },
]

interface WizardConn {
  enabled: boolean
  plugin: 'github' | 'gitlab' | 'jira' | 'codecov' | 'testregistry'
  ci_tool: TestRegistryCITool | ''
  endpoint: string
  token: string
  organization: string
  service: string
  expiry_date: string
  never_expires: boolean
  username: string
  testStatus: '' | 'testing' | 'ok' | 'fail'
}

// A TestRegistry connection is usable once its CI tool and organization are
// set; only Openshift CI needs a (GitHub) token.
function isTestRegistryReady(c: WizardConn): boolean {
  return c.plugin === 'testregistry' && c.enabled && !!c.ci_tool && !!c.organization.trim() &&
    (c.ci_tool === 'Tekton CI' || !!c.token.trim())
}

// ---------------------------------------------------------------------------
// Browse panel (repo/project discovery)
// ---------------------------------------------------------------------------

function WizardBrowsePanel({
  connections, existingKeys, onAdd, onClose, mode,
}: {
  connections: WizardConn[]
  existingKeys: Set<string>
  onAdd: (items: ProviderRepo[]) => void
  onClose: () => void
  mode: 'repos' | 'projects' | 'testsuites'
}) {
  const [loading, setLoading] = useState(mode !== 'projects')
  const [items, setItems] = useState<ProviderRepo[]>([])
  const [filter, setFilter] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [error, setError] = useState<string | null>(null)

  const wizardItemKey = useCallback((item: BrowseListItem) =>
    mode === 'repos'
      ? `${item.plugin}:${item.owner}/${item.name}`.toLowerCase()
      : mode === 'testsuites'
        ? item.full_name.toLowerCase()
        : (item.board_id ?? '').toLowerCase(), [mode])
  const isItemAdded = useCallback(
    (item: BrowseListItem) => existingKeys.has(wizardItemKey(item)),
    [existingKeys, wizardItemKey],
  )
  const repoGroup = useCallback(
    (item: BrowseListItem) => item.owner || item.full_name.split('/')[0] || '',
    [],
  )

  useEffect(() => {
    let cancelled = false
    setSelected(new Set())
    const jiraQuery = mode === 'projects' ? searchQuery : ''
    if (mode === 'projects' && jiraQuery.length < 2) {
      setError('Enter a Jira board name, then click Search.')
      setLoading(false)
      return () => { cancelled = true }
    }

    void (async () => {
      setLoading(true)
      setError(null)
      try {
        const fetches: Promise<ProviderRepo[]>[] = []
        for (const conn of connections) {
          if (mode === 'testsuites' && isTestRegistryReady(conn) && conn.ci_tool) {
            fetches.push(fetchTestRegistryScopes(conn.ci_tool, conn.organization.trim(), conn.token.trim()))
            continue
          }
          if (!conn.enabled || !conn.token) continue
          if (mode === 'repos' && (conn.plugin === 'github' || conn.plugin === 'gitlab')) {
            fetches.push(fetchProviderRepos(conn.plugin, conn.endpoint, conn.token))
          } else if (mode === 'projects' && conn.plugin === 'jira' && conn.username) {
            fetches.push(fetchProviderProjects(conn.endpoint, conn.token, conn.username, jiraQuery))
          }
        }
        if (fetches.length === 0) {
          setError(mode === 'repos' ? 'No git provider connections configured.'
            : mode === 'testsuites' ? 'No TestRegistry connection configured.'
              : 'No Jira connection configured.')
          if (!cancelled) { setItems([]); setLoading(false) }
          return
        }
        const results = await Promise.all(fetches)
        if (!cancelled) {
          const seen = new Set<string>()
          setItems(results.flat().filter(item => {
            const key = wizardItemKey(item)
            if (seen.has(key)) return false
            seen.add(key)
            return true
          }))
          setLoading(false)
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Browse failed.')
          setLoading(false)
        }
      }
    })()
    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode === 'projects' ? searchQuery : 'once'])

  const handleAdd = () => {
    const byKey = new Map(items.map(item => [wizardItemKey(item), item]))
    const sel = Array.from(selected)
      .map(key => byKey.get(key))
      .filter((item): item is ProviderRepo => !!item && !isItemAdded(item))
    if (sel.length) onAdd(sel)
  }

  return (
    <div className="mt-browse-panel">
      <div className="mt-browse-filter">
        {mode === 'projects' && (
          <>
            <input
              className="sub-input"
              type="text"
              placeholder="Search Jira boards..."
              value={searchQuery}
              onChange={e => setSearchQuery(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter') setSearchQuery(searchQuery) }}
              style={{ flex: 1 }}
            />
            <button className="sub-btn sub-btn--small" onClick={() => setSearchQuery(searchQuery)}>
              Search
            </button>
          </>
        )}
        {mode !== 'projects' && (
          <input
            className="sub-input"
            type="text"
            placeholder={mode === 'testsuites' ? 'Filter scopes...' : 'Filter repositories...'}
            value={filter}
            onChange={e => setFilter(e.target.value)}
            style={{ flex: 1 }}
          />
        )}
      </div>

      {loading && (
        <div className="settings-loader" style={{ padding: '20px 0' }}>
          <div className="diagram-card__spinner" />
          <div className="settings-loader-text">Loading...</div>
        </div>
      )}

      {error && !loading && <div className="mt-wizard-error">{error}</div>}

      {!loading && !error && (
        <>
          <BrowseList
            items={items}
            keyOf={wizardItemKey}
            isItemAdded={isItemAdded}
            selected={selected}
            onSelectionChange={setSelected}
            filter={mode !== 'projects' ? filter : ''}
            groupBy={mode === 'repos' ? repoGroup : undefined}
          />
          <div className="mt-browse-footer">
            <button className="sub-btn sub-btn--small sub-btn--secondary" onClick={onClose}>Cancel</button>
            <button
              className="sub-btn sub-btn--small sub-btn--primary"
              disabled={selected.size === 0}
              onClick={handleAdd}
            >
              Add Selected
            </button>
          </div>
        </>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Wizard Sources Step
// ---------------------------------------------------------------------------

function WizardSourcesStep({
  connections, gitRepos, setGitRepos, jiraProjects, setJiraProjects, testScopes, setTestScopes,
}: {
  connections: WizardConn[]
  gitRepos: Array<{ type: string; owner: string; name: string }>
  setGitRepos: React.Dispatch<React.SetStateAction<Array<{ type: string; owner: string; name: string }>>>
  jiraProjects: Array<{ board_id: string; jira_project: string }>
  setJiraProjects: React.Dispatch<React.SetStateAction<Array<{ board_id: string; jira_project: string }>>>
  testScopes: string[]
  setTestScopes: React.Dispatch<React.SetStateAction<string[]>>
}) {
  const [showBrowseRepos, setShowBrowseRepos] = useState(false)
  const [showBrowseProjects, setShowBrowseProjects] = useState(false)
  const [showBrowseTests, setShowBrowseTests] = useState(false)
  const testRegistryConn = connections.find(isTestRegistryReady)
  const hasTestRegistry = !!testRegistryConn
  const existingTestKeys = new Set(testScopes.map(s => s.trim().toLowerCase()))

  const hasGitProvider = connections.some(c => c.enabled && c.token && (c.plugin === 'github' || c.plugin === 'gitlab'))
  const hasJira = connections.some(c => c.enabled && c.token && c.plugin === 'jira')
  const codecovConn = connections.find(c => c.enabled && c.token && c.plugin === 'codecov')
  const hasCodecov = !!codecovConn
  const codecovProvider = codecovConn?.service.startsWith('gitlab') ? 'gitlab' : 'github'

  const existingRepoKeys = new Set(gitRepos.map(r => `${r.type}:${r.owner}/${r.name}`.toLowerCase()))
  const existingProjectKeys = new Set(jiraProjects.map(p => p.board_id.toLowerCase()))

  return (
    <>
      <div className="mt-wizard-section-hint">
        Add sources for this team. You can also add more later via the edit form.
        {!hasCodecov && ' This step is optional.'}
      </div>
      {hasCodecov && (
        <div className="mt-wizard-section-hint">
          Add a {codecovProvider} repository owned by {codecovConn.organization}. A separate {codecovProvider} connection is optional for Codecov collection.
          {!hasGitProvider && ' Enter the repository manually; browsing requires a GitHub or GitLab connection.'}
        </div>
      )}

      {!hasGitProvider && !hasJira && !hasCodecov && !hasTestRegistry && (
        <p className="sub-empty-hint">
          No connections with tokens configured. Go back to the Connections step and add at least one.
        </p>
      )}

      {(hasGitProvider || hasCodecov) && (
        <div className="sub-form-section" style={{ marginBottom: hasJira || hasTestRegistry ? 20 : 0 }}>
          <div className="sub-form-section-header">
            <label>Git Repositories</label>
            <div style={{ display: 'flex', gap: 6 }}>
              {hasGitProvider && (
                <button className="sub-btn sub-btn--small sub-btn--secondary" onClick={() => setShowBrowseRepos(!showBrowseRepos)}>
                  {showBrowseRepos ? 'Close Browse' : 'Browse'}
                </button>
              )}
              <button className="sub-btn sub-btn--small" onClick={() => setGitRepos(prev => [...prev, { type: codecovProvider, owner: codecovConn?.organization ?? '', name: '' }])}>
                + Add Repo
              </button>
            </div>
          </div>

          {hasGitProvider && showBrowseRepos && (
            <WizardBrowsePanel
              connections={connections}
              existingKeys={existingRepoKeys}
              onAdd={items => {
                const newRepos = items
                  .filter(i => !existingRepoKeys.has(`${i.plugin}:${i.owner}/${i.name}`.toLowerCase()))
                  .map(i => ({ type: i.plugin, owner: i.owner, name: i.name }))
                if (newRepos.length) setGitRepos(prev => [...prev, ...newRepos])
                setShowBrowseRepos(false)
              }}
              onClose={() => setShowBrowseRepos(false)}
              mode="repos"
            />
          )}

          {gitRepos.length === 0 && !showBrowseRepos ? (
            <p className="sub-empty-hint">No repositories added yet.</p>
          ) : (
            gitRepos.map((repo, idx) => (
              <div key={idx} style={{ display: 'flex', gap: 6, alignItems: 'center', marginBottom: 8 }}>
                <select
                  className="sub-input"
                  value={repo.type}
                  onChange={e => setGitRepos(prev => prev.map((r, i) => i === idx ? { ...r, type: e.target.value } : r))}
                  style={{ width: 100, flex: 'none' }}
                >
                  {(hasGitProvider || codecovProvider === 'github') && <option value="github">github</option>}
                  {(hasGitProvider || codecovProvider === 'gitlab') && <option value="gitlab">gitlab</option>}
                </select>
                <input
                  className="sub-input"
                  type="text"
                  placeholder="Owner"
                  value={repo.owner}
                  onChange={e => setGitRepos(prev => prev.map((r, i) => i === idx ? { ...r, owner: e.target.value } : r))}
                  style={{ flex: 1 }}
                />
                <input
                  className="sub-input"
                  type="text"
                  placeholder="Name"
                  value={repo.name}
                  onChange={e => setGitRepos(prev => prev.map((r, i) => i === idx ? { ...r, name: e.target.value } : r))}
                  style={{ flex: 1 }}
                />
                <button className="sub-btn sub-btn--small sub-btn--danger" onClick={() => setGitRepos(prev => prev.filter((_, i) => i !== idx))}>
                  X
                </button>
              </div>
            ))
          )}
        </div>
      )}

      {hasJira && (
        <div className="sub-form-section" style={{ marginBottom: hasTestRegistry ? 20 : 0 }}>
          <div className="sub-form-section-header">
            <label>Jira Boards</label>
            <div style={{ display: 'flex', gap: 6 }}>
              <button className="sub-btn sub-btn--small sub-btn--secondary" onClick={() => setShowBrowseProjects(!showBrowseProjects)}>
                {showBrowseProjects ? 'Close Browse' : 'Browse'}
              </button>
              <button className="sub-btn sub-btn--small" onClick={() => setJiraProjects(prev => [...prev, { board_id: '', jira_project: '' }])}>
                + Add Board
              </button>
            </div>
          </div>

          {showBrowseProjects && (
            <WizardBrowsePanel
              connections={connections}
              existingKeys={existingProjectKeys}
              onAdd={items => {
                const newProjects = items
                  .filter(i => !existingProjectKeys.has((i.board_id ?? '').toLowerCase()))
                  .map(i => ({ board_id: i.board_id ?? '', jira_project: i.owner }))
                if (newProjects.length) setJiraProjects(prev => [...prev, ...newProjects])
                setShowBrowseProjects(false)
              }}
              onClose={() => setShowBrowseProjects(false)}
              mode="projects"
            />
          )}

          {jiraProjects.length === 0 && !showBrowseProjects ? (
            <p className="sub-empty-hint">No boards added yet.</p>
          ) : (
            jiraProjects.map((proj, idx) => (
              <div key={idx} style={{ display: 'flex', gap: 6, alignItems: 'center', marginBottom: 8 }}>
                <span className="mt-source-type-badge" style={{ flex: 'none' }}>jira</span>
                <input
                  className="sub-input"
                  type="text"
                  placeholder="Board ID (e.g. 12345)"
                  value={proj.board_id}
                  onChange={e => setJiraProjects(prev => prev.map((p, i) => i === idx ? { ...p, board_id: e.target.value } : p))}
                  style={{ flex: 1 }}
                />
                <button className="sub-btn sub-btn--small sub-btn--danger" onClick={() => setJiraProjects(prev => prev.filter((_, i) => i !== idx))}>
                  X
                </button>
              </div>
            ))
          )}
        </div>
      )}

      {hasTestRegistry && testRegistryConn && (
        <div className="sub-form-section">
          <div className="sub-form-section-header">
            <label>Test Suites (TestRegistry)</label>
            <div style={{ display: 'flex', gap: 6 }}>
              <button className="sub-btn sub-btn--small sub-btn--secondary" onClick={() => setShowBrowseTests(!showBrowseTests)}>
                {showBrowseTests ? 'Close Browse' : 'Browse'}
              </button>
              <button className="sub-btn sub-btn--small" onClick={() => setTestScopes(prev => [...prev, ''])}>
                + Add Scope
              </button>
            </div>
          </div>
          <div className="mt-wizard-section-hint">
            {testRegistryConn.ci_tool} · {testRegistryConn.ci_tool === 'Openshift CI' ? 'GitHub' : 'Quay'} organization {testRegistryConn.organization.trim()}
          </div>

          {showBrowseTests && (
            <WizardBrowsePanel
              connections={connections}
              existingKeys={existingTestKeys}
              onAdd={items => {
                const added = items.map(i => i.full_name).filter(n => !existingTestKeys.has(n.toLowerCase()))
                if (added.length) setTestScopes(prev => [...prev.filter(s => s.trim()), ...added])
                setShowBrowseTests(false)
              }}
              onClose={() => setShowBrowseTests(false)}
              mode="testsuites"
            />
          )}

          {testScopes.length === 0 && !showBrowseTests ? (
            <p className="sub-empty-hint">No scopes added yet.</p>
          ) : (
            testScopes.map((scope, idx) => (
              <div key={idx} style={{ display: 'flex', gap: 6, alignItems: 'center', marginBottom: 8 }}>
                <span className="mt-source-type-badge" style={{ flex: 'none' }}>testregistry</span>
                <input
                  className="sub-input"
                  type="text"
                  placeholder={testRegistryConn.ci_tool === 'Tekton CI' ? `${testRegistryConn.organization.trim()}/repo` : 'Repo name'}
                  value={scope}
                  onChange={e => setTestScopes(prev => prev.map((s, i) => i === idx ? e.target.value : s))}
                  style={{ flex: 1 }}
                />
                <button className="sub-btn sub-btn--small sub-btn--danger" onClick={() => setTestScopes(prev => prev.filter((_, i) => i !== idx))}>
                  X
                </button>
              </div>
            ))
          )}
        </div>
      )}
    </>
  )
}

// ---------------------------------------------------------------------------
// Wizard Jira Settings Step
// ---------------------------------------------------------------------------

function WizardJiraSettingsStep({
  connections, jiraProjects, setJiraProjects, jiraScopeConfigs, setJiraScopeConfigs,
}: {
  connections: WizardConn[]
  jiraProjects: Array<{ board_id: string; jira_project: string }>
  setJiraProjects: React.Dispatch<React.SetStateAction<Array<{ board_id: string; jira_project: string }>>>
  jiraScopeConfigs: Record<string, JiraScopeConfig>
  setJiraScopeConfigs: React.Dispatch<React.SetStateAction<Record<string, JiraScopeConfig>>>
}) {
  const [metaByProject, setMetaByProject] = useState<
    Record<string, { loading: boolean; meta?: JiraProjectMeta; error?: string }>
  >({})

  const jiraConn = connections.find(c => c.enabled && c.token && c.plugin === 'jira')

  useEffect(() => {
    if (!jiraConn) return
    for (const proj of jiraProjects) {
      let key = proj.jira_project
      if (!key) key = `board:${proj.board_id}`
      if (metaByProject[key]) continue
      setMetaByProject(prev => ({ ...prev, [key]: { loading: true } }))
      const metaPromise = proj.jira_project
        ? fetchJiraMeta(jiraConn.endpoint, jiraConn.token, jiraConn.username, proj.jira_project)
            .then(meta => ({ project_key: proj.jira_project, ...meta }))
        : fetchProxyJiraBoardMeta(jiraConn.endpoint, jiraConn.token, jiraConn.username, proj.board_id)
      const boardId = proj.board_id
      metaPromise
        .then(result => {
          const meta: JiraProjectMeta = { issue_types: result.issue_types, fields: result.fields }
          if (!proj.jira_project && result.project_key) {
            setJiraProjects(prev => prev.map(p => p.board_id === boardId ? { ...p, jira_project: result.project_key } : p))
          }
          const spField = meta.fields.find(f => f.name.trim().toLowerCase() === 'story points')
          setMetaByProject(prev => ({ ...prev, [key]: { loading: false, meta } }))
          setJiraScopeConfigs(prev => {
            if (prev[key]) return prev
            return { ...prev, [key]: { requirement_types: [], bug_types: [], story_point_field: spField?.id ?? '' } }
          })
        })
        .catch(err => setMetaByProject(prev => ({ ...prev, [key]: { loading: false, error: err instanceof Error ? err.message : String(err) } })))
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [jiraProjects, jiraConn?.token])

  if (!jiraConn) {
    return <div className="mt-wizard-section-hint">No Jira connection configured.</div>
  }

  const toggleType = (projectKey: string, role: 'requirement_types' | 'bug_types', typeName: string) => {
    setJiraScopeConfigs(prev => {
      const cfg = prev[projectKey] ?? { requirement_types: [], bug_types: [], story_point_field: '' }
      const list = cfg[role]
      const next = list.includes(typeName) ? list.filter(t => t !== typeName) : [...list, typeName]
      return { ...prev, [projectKey]: { ...cfg, [role]: next } }
    })
  }

  const setStoryPointField = (projectKey: string, fieldId: string) => {
    setJiraScopeConfigs(prev => {
      const cfg = prev[projectKey] ?? { requirement_types: [], bug_types: [], story_point_field: '' }
      return { ...prev, [projectKey]: { ...cfg, story_point_field: fieldId } }
    })
  }

  return (
    <>
      <div className="mt-wizard-section-hint">
        Configure issue type mappings and story point fields for each Jira project.
        This tells DevLake how to classify your Jira issues.
      </div>

      {jiraProjects.map(proj => {
        const key = proj.jira_project || `board:${proj.board_id}`
        if (!key) return null
        const entry = metaByProject[key]
        const cfg = jiraScopeConfigs[key]
        const displayLabel = proj.jira_project || `Board ${proj.board_id}`

        return (
          <div key={key} className="sub-form-section" style={{ marginBottom: 20 }}>
            <div className="sub-form-section-header">
              <label>
                <span className="mt-source-type-badge" style={{ marginRight: 8 }}>{displayLabel}</span>
                {displayLabel}
              </label>
            </div>

            {(!entry || entry.loading) && (
              <div className="mt-jira-cfg-body">
                <div className="settings-loader" style={{ justifyContent: 'flex-start' }}>
                  <div className="diagram-card__spinner" />
                  <div className="settings-loader-text">Loading issue types and fields...</div>
                </div>
              </div>
            )}

            {entry?.error && <div className="mt-wizard-error">{entry.error}</div>}

            {entry?.meta && cfg && (
              <div className="mt-jira-cfg-body">
                <div className="sub-form-row" style={{ marginBottom: 12 }}>
                  <label style={{ fontWeight: 600, marginBottom: 4, display: 'block' }}>
                    Requirement Issue Types
                  </label>
                  <div className="mt-jira-type-list">
                    {(entry.meta.issue_types ?? []).map(t => (
                      <label key={t.name} className="mt-jira-type-cb">
                        <input
                          type="checkbox"
                          checked={cfg.requirement_types.includes(t.name)}
                          onChange={() => toggleType(key, 'requirement_types', t.name)}
                        />
                        {' '}{t.name}
                      </label>
                    ))}
                    {(entry.meta.issue_types ?? []).length === 0 && (
                      <span style={{ opacity: 0.6 }}>No issue types found</span>
                    )}
                  </div>
                </div>

                <div className="sub-form-row" style={{ marginBottom: 12 }}>
                  <label style={{ fontWeight: 600, marginBottom: 4, display: 'block' }}>
                    Bug Issue Types
                  </label>
                  <div className="mt-jira-type-list">
                    {(entry.meta.issue_types ?? []).map(t => (
                      <label key={t.name} className="mt-jira-type-cb">
                        <input
                          type="checkbox"
                          checked={cfg.bug_types.includes(t.name)}
                          onChange={() => toggleType(key, 'bug_types', t.name)}
                        />
                        {' '}{t.name}
                      </label>
                    ))}
                  </div>
                </div>

                <div className="sub-form-row">
                  <label style={{ fontWeight: 600, marginBottom: 4, display: 'block' }}>
                    Story Points Field
                  </label>
                  <select
                    className="sub-input"
                    value={cfg.story_point_field}
                    onChange={e => setStoryPointField(key, e.target.value)}
                    style={{ maxWidth: 400 }}
                  >
                    <option value="">(none)</option>
                    {[...(entry.meta.fields ?? [])]
                      .sort((a, b) => {
                        const ar = a.name.trim().toLowerCase() === 'story points'
                        const br = b.name.trim().toLowerCase() === 'story points'
                        if (ar !== br) return ar ? -1 : 1
                        return a.name.localeCompare(b.name)
                      })
                      .map(f => (
                        <option key={f.id} value={f.id}>
                          {f.name}{f.custom ? ' (custom)' : ''}
                        </option>
                      ))}
                  </select>
                </div>
              </div>
            )}
          </div>
        )
      })}
    </>
  )
}

// ---------------------------------------------------------------------------
// Wizard Result
// ---------------------------------------------------------------------------

function WizardResult({ result, teamName }: { result: OnboardResult; teamName: string }) {
  const hasErrors = result.status === 'error' || (result.sync?.errors && result.sync.errors.length > 0)
  const syncCreated = result.sync?.created ?? []
  const syncUpdated = result.sync?.updated ?? []
  const syncSkipped = result.sync?.skipped ?? []
  const syncErrors = result.sync?.errors ?? []
  const connIds = result.connection_ids ?? {}
  const hasSync = syncCreated.length > 0 || syncUpdated.length > 0 || syncSkipped.length > 0 || syncErrors.length > 0
  const hasConnections = Object.keys(connIds).length > 0

  const summaryText = [
    `Team: ${teamName} (${result.team_id})`,
    `Status: ${result.status}`,
    result.error ? `Error: ${result.error}` : '',
    hasConnections ? `Connections: ${Object.entries(connIds).map(([p, id]) => `${p}=#${id}`).join(', ')}` : '',
    syncCreated.length > 0 ? `Created: ${syncCreated.join(', ')}` : '',
    syncUpdated.length > 0 ? `Updated: ${syncUpdated.join(', ')}` : '',
    syncSkipped.length > 0 ? `Skipped: ${syncSkipped.join(', ')}` : '',
    syncErrors.length > 0 ? `Sync errors: ${syncErrors.join(', ')}` : '',
  ].filter(Boolean).join('\n')

  return (
    <div>
      <div className={`mt-wizard-result-banner mt-wizard-result-banner--${hasErrors ? 'error' : 'ok'}`}>
        <span className="mt-wizard-result-icon">{hasErrors ? '⚠' : '✔'}</span>
        <span>{result.status === 'error' ? 'Team creation encountered errors' : 'Team created successfully'}</span>
      </div>

      {result.error && <div className="mt-wizard-error" style={{ marginTop: 12 }}>{result.error}</div>}

      <table className="sub-table" style={{ marginTop: 16 }}>
        <tbody>
          <tr><td style={{ fontWeight: 600, width: 140 }}>Team ID</td><td><code>{result.team_id}</code></td></tr>
          <tr><td style={{ fontWeight: 600 }}>Team Name</td><td>{teamName}</td></tr>
          <tr><td style={{ fontWeight: 600 }}>Status</td><td>{result.status}</td></tr>
        </tbody>
      </table>

      {hasConnections && (
        <div style={{ marginTop: 16 }}>
          <strong>DevLake Connections</strong>
          <table className="sub-table" style={{ marginTop: 8 }}>
            <thead><tr><th>Plugin</th><th>Connection ID</th></tr></thead>
            <tbody>
              {Object.entries(connIds).map(([plugin, id]) => (
                <tr key={plugin}>
                  <td><span className="mt-source-type-badge">{plugin}</span></td>
                  <td><code>{id}</code></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {hasSync && (
        <div style={{ marginTop: 16 }}>
          <strong>DevLake Sync</strong>
          {syncCreated.length > 0 && (
            <div style={{ marginTop: 8 }}>
              <span style={{ color: '#3e8635', fontWeight: 500 }}>Created:</span>
              <ul className="mt-wizard-result-list">{syncCreated.map((s, i) => <li key={i}>{s}</li>)}</ul>
            </div>
          )}
          {syncUpdated.length > 0 && (
            <div style={{ marginTop: 8 }}>
              <span style={{ color: '#0066cc', fontWeight: 500 }}>Updated:</span>
              <ul className="mt-wizard-result-list">{syncUpdated.map((s, i) => <li key={i}>{s}</li>)}</ul>
            </div>
          )}
          {syncSkipped.length > 0 && (
            <div style={{ marginTop: 8 }}>
              <span style={{ opacity: 0.7 }}>Skipped:</span>
              <ul className="mt-wizard-result-list">{syncSkipped.map((s, i) => <li key={i}>{s}</li>)}</ul>
            </div>
          )}
          {syncErrors.length > 0 && (
            <div style={{ marginTop: 8 }}>
              <span style={{ color: '#c9190b', fontWeight: 500 }}>Errors:</span>
              <ul className="mt-wizard-result-list mt-wizard-result-list--error">{syncErrors.map((s, i) => <li key={i}>{s}</li>)}</ul>
            </div>
          )}
        </div>
      )}

      <div style={{ marginTop: 16 }}>
        <button className="sub-btn sub-btn--small sub-btn--secondary" onClick={() => void navigator.clipboard.writeText(summaryText)}>
          Copy to Clipboard
        </button>
        <span className="mt-wizard-section-hint" style={{ marginLeft: 8, marginBottom: 0 }}>
          Copy this summary for your records or to share with support.
        </span>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Full Team Wizard
// ---------------------------------------------------------------------------

function TeamWizard({
  email, isAdmin, devlakeEnabled, existingTeamIDs, onCreated, onCancel,
}: {
  email: string
  isAdmin: boolean
  devlakeEnabled: boolean
  existingTeamIDs: Set<string>
  onCreated: () => void
  onCancel: () => void
}) {
  const [step, setStep] = useState(0)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [result, setResult] = useState<OnboardResult | null>(null)

  const [teamName, setTeamName] = useState('')
  const [teamId, setTeamId] = useState('')
  const [teamEmail, setTeamEmail] = useState(email)
  const [teamAuthGroup, setTeamAuthGroup] = useState('')
  const [idManuallyEdited, setIdManuallyEdited] = useState(false)

  const [orgs, setOrgs] = useState<Org[]>([])
  const [products, setProducts] = useState<Product[]>([])
  const [selectedOrgId, setSelectedOrgId] = useState('')
  const [selectedProductId, setSelectedProductId] = useState('')
  const [orgProductIds, setOrgProductIds] = useState<string[]>([])

  const [connections, setConnections] = useState<WizardConn[]>(
    CONN_PLUGIN_DEFS.map(p => ({
      enabled: false,
      plugin: p.id,
      ci_tool: '',
      endpoint: p.defaultEndpoint,
      token: '',
      organization: '',
      service: 'github',
      expiry_date: '',
      never_expires: false,
      username: '',
      testStatus: '',
    })),
  )

  const [gitRepos, setGitRepos] = useState<Array<{ type: string; owner: string; name: string }>>([])
  const [jiraProjects, setJiraProjects] = useState<Array<{ board_id: string; jira_project: string }>>([])
  const [jiraScopeConfigs, setJiraScopeConfigs] = useState<Record<string, JiraScopeConfig>>({})
  const [testScopes, setTestScopes] = useState<string[]>([])
  const [selectedDashboards, setSelectedDashboards] = useState<DashboardTemplate[]>([])
  const [configKeys, setConfigKeys] = useState<ConfigKey[]>([])
  const [catalogTemplates, setCatalogTemplates] = useState<DashboardTemplate[]>([])
  const [kvRows, setKvRows] = useState<Array<{ key: string; value: string }>>([])
  const [validatingSources, setValidatingSources] = useState(false)

  const hasJiraProjects = jiraProjects.some(p => p.board_id)

  const WIZARD_STEPS = useMemo(() => {
    const base = devlakeEnabled
      ? ['Organization', 'Team Info', 'Connections', 'Sources']
      : ['Organization', 'Team Info', 'Sources']
    if (hasJiraProjects) base.push('Jira Settings')
    base.push('Dashboards', 'Configuration')
    return base
  }, [devlakeEnabled, hasJiraProjects])

  useEffect(() => {
    void (async () => {
      const [keys, cat, orgList, prodList] = await Promise.all([
        fetchConfigKeys().catch(() => [] as ConfigKey[]),
        fetchDashboardCatalog().catch(() => [] as DashboardTemplate[]),
        fetchOrgs().catch(() => [] as Org[]),
        fetchProducts().catch(() => [] as Product[]),
      ])
      setConfigKeys(keys)
      setCatalogTemplates(cat)
      setOrgs(orgList)
      setProducts(prodList)
    })()
  }, [])

  const clampedStep = Math.min(step, WIZARD_STEPS.length - 1)
  const stepLabel = WIZARD_STEPS[clampedStep]
  const teamIDError = entityIDError(teamId, existingTeamIDs, 'Team ID')

  const validate = (): string | null => {
    if (stepLabel === 'Organization') {
      if (!selectedOrgId) return 'Select an organization.'
      if (!selectedProductId) return 'Select a product.'
    }
    if (stepLabel === 'Team Info') {
      if (!teamName.trim()) return 'Team name is required.'
      if (teamIDError) return teamIDError
      if (!teamAuthGroup.trim()) return 'Authorization Group is required.'
      const emailErr = contactEmailError(teamEmail)
      if (emailErr) return emailErr
    }
    if (stepLabel === 'Connections') {
      for (const c of connections) {
        if (!c.enabled) continue
        if (c.plugin === 'testregistry') {
          if (!c.ci_tool) return 'TestRegistry: CI tool is required.'
          if (!c.organization.trim()) return `TestRegistry: ${c.ci_tool === 'Openshift CI' ? 'GitHub' : 'Quay'} organization is required.`
          if (c.ci_tool === 'Tekton CI') continue
          if (!c.token.trim()) return 'TestRegistry: GitHub token is required for Openshift CI.'
          if (!c.never_expires && !c.expiry_date) return 'TestRegistry: Token expiry date is required (or mark as "Never expires").'
          continue
        }
        if (!c.endpoint.trim()) return `${c.plugin}: Endpoint is required.`
        if (!c.token.trim()) return `${c.plugin}: Token is required.`
        if (c.plugin === 'codecov' && !c.organization.trim()) return 'Codecov: Organization is required.'
        if (c.plugin === 'jira' && !c.username.trim()) return `${c.plugin}: Username / Email is required.`
        if (!c.never_expires && !c.expiry_date) return `${c.plugin}: Token expiry date is required (or mark as "Never expires").`
      }
    }
    if (stepLabel === 'Dashboards') {
      if (selectedDashboards.length === 0) return 'Select at least one dashboard.'
    }
    return null
  }

  const validationError = validate()
  const isValid = validationError === null

  const handleNext = async () => {
    const err = validate()
    if (err) { setError(err); return }

    if (stepLabel === 'Sources') {
      const codecov = connections.find(c => c.enabled && c.plugin === 'codecov')
      if (codecov) {
        const provider = codecov.service.startsWith('gitlab') ? 'gitlab' : 'github'
        const matchingRepo = gitRepos.some(r => r.type === provider && r.owner.trim().toLowerCase() === codecov.organization.trim().toLowerCase())
        if (!matchingRepo) {
          setError(`Add a ${provider} repository owned by ${codecov.organization} to collect Codecov data.`)
          return
        }
      }
      if (jiraProjects.length > 0) {
        const boardIds = jiraProjects.map(p => p.board_id.trim())
        if (boardIds.some(id => !id)) { setError('Every Jira board must have a Board ID.'); return }
        if (boardIds.some(id => !/^\d+$/.test(id))) { setError('Board IDs must be numeric.'); return }
        if (new Set(boardIds).size !== boardIds.length) { setError('Duplicate board IDs are not allowed.'); return }
      } else {
        const hasJiraConn = connections.some(c => c.enabled && c.token && c.plugin === 'jira')
        if (hasJiraConn) {
          const skip = window.confirm(
            'You have a Jira connection but no boards added. Without boards, no Jira data will be synced.\n\nContinue without Jira boards?',
          )
          if (!skip) return
        }
      }

      const scopes = testScopes.map(s => s.trim())
      if (scopes.length > 0) {
        if (scopes.some(s => !s)) { setError('Every TestRegistry scope must have a name.'); return }
        if (new Set(scopes.map(s => s.toLowerCase())).size !== scopes.length) { setError('Duplicate TestRegistry scopes are not allowed.'); return }
        const conn = connections.find(isTestRegistryReady)
        if (!conn || !conn.ci_tool) { setError('Configure the TestRegistry connection before adding scopes.'); return }
        setValidatingSources(true)
        setError(null)
        try {
          const available = await fetchTestRegistryScopes(conn.ci_tool, conn.organization.trim(), conn.token.trim())
          const byLower = new Map(available.map(a => [a.full_name.toLowerCase(), a.full_name]))
          const missing = scopes.filter(s => !byLower.has(s.toLowerCase()))
          if (missing.length > 0) {
            setError(`Not offered by the TestRegistry connection: ${missing.join(', ')}`)
            return
          }
          setTestScopes(scopes.map(s => byLower.get(s.toLowerCase()) ?? s))
        } catch (err) {
          setError(err instanceof Error ? err.message : 'Unable to validate TestRegistry scopes.')
          return
        } finally {
          setValidatingSources(false)
        }
      }

      if (gitRepos.length > 0) {
        const repos = gitRepos.map(r => ({ ...r, owner: r.owner.trim(), name: r.name.trim() }))
        if (repos.some(r => !r.owner || !r.name)) { setError('Every repository must have an owner and name.'); return }
        const repoKeys = repos.map(r => `${r.type}:${r.owner}/${r.name}`.toLowerCase())
        if (new Set(repoKeys).size !== repoKeys.length) { setError('Duplicate repositories are not allowed.'); return }

        setValidatingSources(true)
        setError(null)
        try {
          const lookupWarnings: string[] = []
          for (const repo of repos) {
            const conn = connections.find(c => c.enabled && c.token && c.plugin === repo.type)
            if (!conn) {
              const codecov = connections.find(c => c.enabled && c.token && c.plugin === 'codecov')
              const provider = codecov?.service.startsWith('gitlab') ? 'gitlab' : 'github'
              if (!codecov || repo.type !== provider || repo.owner.toLowerCase() !== codecov.organization.trim().toLowerCase()) {
                throw new Error(`No ${repo.type} connection is configured for ${repo.owner}/${repo.name}.`)
              }
              continue
            }
            const result = await lookupProviderRepo(
              repo.type as 'github' | 'gitlab',
              conn.endpoint,
              conn.token,
              repo.owner,
              repo.name,
            )
            if (!result.valid) {
              throw new Error(result.error || `${repo.type} repository ${repo.owner}/${repo.name} was not found or you do not have access to it.`)
            }
            if (result.warning) {
              lookupWarnings.push(`${repo.owner}/${repo.name}: ${result.warning}`)
            }
          }
          if (lookupWarnings.length > 0) {
            const proceed = window.confirm(
              `${lookupWarnings.join('\n')}\n\nContinue without complete verification?`,
            )
            if (!proceed) {
              setError(lookupWarnings.join(' '))
              return
            }
          }
          setGitRepos(repos)
        } catch (validationError) {
          setError(validationError instanceof Error ? validationError.message : 'Unable to validate repositories.')
          return
        } finally {
          setValidatingSources(false)
        }
      }
    }

    setError(null)
    setStep(s => s + 1)
  }

  const handleBack = () => { setError(null); setStep(s => s - 1) }

  const handleCreate = async () => {
    setCreating(true)
    setError(null)
    try {
      const sources: Array<{ id: string; type: string; repos?: unknown[]; projects?: unknown[] }> = []
      if (gitRepos.length > 0) {
        sources.push({ id: 'gitrepos', type: 'gitrepos', repos: gitRepos.map((r, i) => ({ id: String(i), ...r })) })
      }
      if (jiraProjects.length > 0) {
        sources.push({
          id: 'workitems', type: 'workitems',
          projects: jiraProjects.map((p, i) => ({ id: String(i), type: 'jira', jira_project: p.jira_project, board_id: p.board_id })),
        })
      }
      const scopeNames = testScopes.map(s => s.trim()).filter(Boolean)
      if (scopeNames.length > 0) {
        sources.push({
          id: 'testsuites', type: 'testsuites',
          projects: scopeNames.map((name, i) => ({ id: `testregistry-${i}`, type: 'testregistry', project_name: name })),
        })
      }

      const cfgObj: Record<string, string> = {}
      for (const row of kvRows) { if (row.key) cfgObj[row.key] = row.value }

      const enabledConns = connections
        .filter(c => c.enabled && (c.plugin === 'testregistry' ? isTestRegistryReady(c) : !!c.token))
        .map(c => {
          if (c.plugin === 'testregistry') {
            const needsToken = c.ci_tool === 'Openshift CI'
            return {
              plugin: c.plugin,
              endpoint: '',
              token: needsToken ? c.token.trim() : '',
              organization: c.organization.trim(),
              ci_tool: c.ci_tool || undefined,
              expiry_date: !needsToken || c.never_expires ? 'never' : c.expiry_date || undefined,
            }
          }
          return {
            plugin: c.plugin,
            endpoint: c.endpoint,
            token: c.token,
            organization: c.plugin === 'codecov' ? c.organization.trim() : undefined,
            service: c.plugin === 'codecov' ? c.service : undefined,
            expiry_date: c.never_expires ? 'never' : c.expiry_date || undefined,
            username: c.username || undefined,
          }
        })

      const remappedScopeConfigs: Record<string, JiraScopeConfig> = {}
      for (const proj of jiraProjects) {
        const cfgKey = proj.jira_project || `board:${proj.board_id}`
        const cfg = jiraScopeConfigs[cfgKey]
        if (cfg && proj.board_id) remappedScopeConfigs[proj.board_id] = cfg
      }
      const hasScopeConfigs = Object.keys(remappedScopeConfigs).length > 0

      const payload = {
        team: { id: teamId.trim(), name: teamName.trim(), contact_email: teamEmail.trim(), auth_group: teamAuthGroup.trim() },
        product_id: selectedProductId || undefined,
        connections: enabledConns.length > 0 ? enabledConns : undefined,
        sources,
        dashboards: selectedDashboards,
        config: cfgObj,
        jira_scope_configs: hasScopeConfigs ? remappedScopeConfigs : undefined,
      }

      const resp = await fetch('/api/v1/onboard', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      const body = await resp.json().catch(() => null) as OnboardResult | null
      if (!resp.ok) {
        const errMsg = (body as Record<string, unknown> | null)?.error as string ?? `HTTP ${resp.status}`
        setResult({
          team_id: teamId.trim(),
          status: 'error',
          error: errMsg,
          sync: body?.sync,
          connection_ids: body?.connection_ids,
        })
      } else {
        setResult(body ?? { team_id: teamId.trim(), status: 'created' })
      }
    } catch (err) {
      setResult({
        team_id: teamId.trim(),
        status: 'error',
        error: err instanceof Error ? err.message : 'Creation failed',
      })
    } finally {
      setCreating(false)
    }
  }

  const selectedDashIds = selectedDashboards.map(d => d.dashboard_id)

  const toggleDashboard = (tmpl: DashboardTemplate) => {
    setSelectedDashboards(prev =>
      prev.some(d => d.dashboard_id === tmpl.dashboard_id)
        ? prev.filter(d => d.dashboard_id !== tmpl.dashboard_id)
        : [...prev, tmpl],
    )
  }

  const updateConn = (plugin: string, patch: Partial<WizardConn>) => {
    setConnections(prev => prev.map(c => c.plugin === plugin ? { ...c, ...patch } : c))
  }

  const handleOrgChange = async (orgId: string) => {
    setSelectedOrgId(orgId)
    setSelectedProductId('')
    if (!orgId) { setOrgProductIds([]); return }
    try {
      const links = await fetchOrgProductLinks(orgId)
      setOrgProductIds(links.map(l => l.id))
    } catch { setOrgProductIds([]) }
  }

  const filteredProducts = selectedOrgId
    ? products.filter(p => orgProductIds.includes(p.id))
    : products

  const handleTestConnection = async (conn: WizardConn) => {
    if (conn.plugin === 'testregistry') {
      if (!isTestRegistryReady(conn) || !conn.ci_tool) {
        setError('Select a CI tool and enter the organization (and GitHub token for Openshift CI) first.')
        return
      }
      updateConn(conn.plugin, { testStatus: 'testing' })
      setError(null)
      try {
        const res = await testConnection('testregistry', '', conn.ci_tool === 'Openshift CI' ? conn.token.trim() : '', {
          organization: conn.organization.trim(),
          ci_tool: conn.ci_tool,
        })
        updateConn(conn.plugin, { testStatus: res.success ? 'ok' : 'fail' })
        if (!res.success) setError(res.message)
        else if (res.expiry_date && !conn.expiry_date && !conn.never_expires) updateConn(conn.plugin, { expiry_date: res.expiry_date, testStatus: 'ok' })
      } catch (err) {
        updateConn(conn.plugin, { testStatus: 'fail' })
        setError(err instanceof Error ? err.message : 'Test failed')
      }
      return
    }
    if (!conn.endpoint || !conn.token) { setError('Provide endpoint and token first.'); return }
    if (conn.plugin === 'jira' && !conn.username) { setError('Username / Email is required for Jira connections.'); return }
    updateConn(conn.plugin, { testStatus: 'testing' })
    setError(null)
    try {
      const res = await testConnection(conn.plugin, conn.endpoint, conn.token, {
        username: conn.username || undefined,
        organization: conn.plugin === 'codecov' ? conn.organization.trim() : undefined,
        service: conn.plugin === 'codecov' ? conn.service : undefined,
      })
      updateConn(conn.plugin, { testStatus: res.success ? 'ok' : 'fail' })
      if (!res.success) setError(res.message)
      else if (res.expiry_date && !conn.expiry_date) updateConn(conn.plugin, { expiry_date: res.expiry_date, testStatus: 'ok' })
    } catch (err) {
      updateConn(conn.plugin, { testStatus: 'fail' })
      setError(err instanceof Error ? err.message : 'Test failed')
    }
  }

  return (
    <Portal>
    <div className="mt-wizard-overlay" onClick={e => { if (e.target === e.currentTarget) onCancel() }}>
      <div className="mt-wizard-modal">
        <div className="mt-wizard-header">
          <h3>New Team</h3>
          <button className="mt-wizard-close" onClick={onCancel}>&times;</button>
        </div>

        <div className="mt-wizard-steps">
          {WIZARD_STEPS.map((label, i) => (
            <div
              key={i}
              className={`mt-wizard-step${i < clampedStep ? ' done' : ''}${i === clampedStep ? ' active' : ''}`}
            >
              <span className="mt-wizard-step-num">{i + 1}</span>
              <span>{label}</span>
            </div>
          ))}
        </div>

        <div className="mt-wizard-body">
          {error && <div className="mt-wizard-error">{error}</div>}

          {/* Step: Organization */}
          {stepLabel === 'Organization' && (
            <>
              <div className="mt-wizard-section-hint">
                Select the organization and product this team belongs to.
              </div>
              <div className="sub-form-row">
                <label>Organization <span style={{ color: '#c9190b' }}>*</span></label>
                <select className="sub-input" value={selectedOrgId} onChange={e => void handleOrgChange(e.target.value)}>
                  <option value="">-- select organization --</option>
                  {orgs.map(o => <option key={o.id} value={o.id}>{o.name}</option>)}
                </select>
              </div>
              <div className="sub-form-row">
                <label>Product <span style={{ color: '#c9190b' }}>*</span></label>
                <select className="sub-input" value={selectedProductId} onChange={e => setSelectedProductId(e.target.value)} disabled={!selectedOrgId}>
                  <option value="">-- select product --</option>
                  {filteredProducts.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                </select>
                {selectedOrgId && filteredProducts.length === 0 && (
                  <span className="mt-wizard-section-hint">No products found for this organization.</span>
                )}
              </div>
            </>
          )}

          {/* Step: Team Info */}
          {stepLabel === 'Team Info' && (
            <>
              <div className="sub-form-row">
                <label>Team Name <span style={{ color: '#c9190b' }}>*</span></label>
                <input
                  className="sub-input" type="text" placeholder="e.g. Platform Engineering"
                  value={teamName}
                  onChange={e => { setTeamName(e.target.value); if (!idManuallyEdited) setTeamId(slugify(e.target.value)) }}
                />
              </div>
              <div className="sub-form-row">
                <label>Team ID <span style={{ color: '#c9190b' }}>*</span></label>
                <input
                  className="sub-input" type="text" placeholder="auto-generated from name"
                  value={teamId}
                  onChange={e => { setTeamId(e.target.value); setIdManuallyEdited(true) }}
                />
                <span className="mt-wizard-section-hint">
                  Unique identifier (kebab-case). Auto-generated from name.
                </span>
                {teamId.trim() && teamIDError && <span className="error-text">{teamIDError}</span>}
              </div>
              <div className="sub-form-row">
                <label>Authorization Group <span style={{ color: '#c9190b' }}>*</span></label>
                <input
                  className="sub-input" type="text" placeholder="e.g. my-ldap-group"
                  value={teamAuthGroup}
                  onChange={e => setTeamAuthGroup(e.target.value)}
                />
                <span className="mt-wizard-section-hint">
                  The authorization group used to grant access to this team.
                </span>
              </div>
              <div className="sub-form-row">
                <label>Contact Email</label>
                <input className="sub-input" type="email" value={teamEmail} onChange={e => setTeamEmail(e.target.value)} readOnly={!isAdmin} />
                {!isAdmin && (
                  <span className="mt-wizard-section-hint">
                    Contact email is set from your authenticated identity.
                  </span>
                )}
                {isAdmin && teamEmail.trim().toLowerCase() !== email.trim().toLowerCase() && (
                  <span className="mt-wizard-section-hint" style={{ color: '#b35c00' }}>
                    Warning: this differs from your OAuth email ({email}). The team will be assigned to this contact.
                  </span>
                )}
              </div>
            </>
          )}

          {/* Step: Connections */}
          {stepLabel === 'Connections' && (
            <>
              <div className="mt-wizard-section-hint">
                Configure DevLake connections for this team. Each connection requires an access token, except TestRegistry with Tekton CI.
              </div>
              {CONN_PLUGIN_DEFS.map(pDef => {
                const conn = connections.find(c => c.plugin === pDef.id)!
                return (
                  <div key={pDef.id} className="mt-conn-section">
                    <div className="mt-conn-header" onClick={() => updateConn(pDef.id, { enabled: !conn.enabled })}>
                      <input
                        type="checkbox" checked={conn.enabled}
                        onChange={e => updateConn(pDef.id, { enabled: e.target.checked })}
                        onClick={e => e.stopPropagation()}
                      />
                      <label>{pDef.label}</label>
                    </div>
                    {conn.enabled && pDef.id === 'testregistry' && (
                      <div className="mt-conn-body">
                        <div className="mt-conn-field">
                          <label>CI Tool</label>
                          <select className="sub-input" value={conn.ci_tool}
                            onChange={e => updateConn(pDef.id, {
                              ci_tool: e.target.value as TestRegistryCITool | '',
                              organization: '', token: '', expiry_date: '', never_expires: false, testStatus: '',
                            })}>
                            <option value="">-- Select CI Tool --</option>
                            {TESTREGISTRY_CI_TOOLS.map(t => <option key={t} value={t}>{t}</option>)}
                          </select>
                          <div className="mt-wizard-section-hint">
                            {conn.ci_tool === 'Openshift CI' && 'Collects Prow jobs; scopes are the repos under openshift/release ci-operator/config/<org>. '}
                            {conn.ci_tool === 'Tekton CI' && 'Collects Tekton PipelineRun artifacts stored in Quay.io; scopes are the Quay repositories. '}
                            The CI tool and organization cannot be changed after the team is created.
                          </div>
                        </div>
                        {conn.ci_tool && (
                          <div className="mt-conn-field">
                            <label>{conn.ci_tool === 'Openshift CI' ? 'GitHub Organization' : 'Quay Organization'}</label>
                            <input className="sub-input" type="text"
                              placeholder={conn.ci_tool === 'Openshift CI' ? 'e.g. konflux-ci' : 'e.g. konflux-test-storage'}
                              value={conn.organization}
                              onChange={e => updateConn(pDef.id, { organization: e.target.value, testStatus: '' })} />
                          </div>
                        )}
                        {conn.ci_tool === 'Openshift CI' && (
                          <>
                            <div className="mt-conn-field">
                              <label>{pDef.tokenLabel}</label>
                              <input className="sub-input" type="password" placeholder="Paste your GitHub token here"
                                value={conn.token}
                                onChange={e => updateConn(pDef.id, { token: e.target.value, testStatus: '' })} />
                            </div>
                            <div className="mt-conn-field">
                              <label>Token Expiry Date</label>
                              <input className="sub-input" type="date" value={conn.expiry_date}
                                disabled={conn.never_expires}
                                onChange={e => updateConn(pDef.id, { expiry_date: e.target.value })} />
                              <label className="mt-conn-never">
                                <input type="checkbox" checked={conn.never_expires}
                                  onChange={e => updateConn(pDef.id, { never_expires: e.target.checked, expiry_date: e.target.checked ? '' : conn.expiry_date })} />
                                {' '}Never expires
                              </label>
                            </div>
                          </>
                        )}
                        <div className="mt-conn-test-row">
                          <button className="sub-btn sub-btn--small" disabled={conn.testStatus === 'testing'}
                            onClick={() => void handleTestConnection(conn)}>
                            Test Connection
                          </button>
                          {conn.testStatus === 'ok' && <span className="mt-conn-test-badge mt-conn-test-badge--ok">Verified</span>}
                          {conn.testStatus === 'fail' && <span className="mt-conn-test-badge mt-conn-test-badge--fail">Failed</span>}
                          {conn.testStatus === 'testing' && <span className="mt-conn-test-badge mt-conn-test-badge--testing">Testing...</span>}
                        </div>
                      </div>
                    )}
                    {conn.enabled && pDef.id !== 'testregistry' && (
                      <div className="mt-conn-body">
                        <div className="mt-conn-field">
                          <label>Endpoint</label>
                          <input
                            className="sub-input" type="text"
                            placeholder={pDef.id === 'gitlab' ? 'Enter your GitLab API endpoint' : pDef.defaultEndpoint || 'https://your-instance.atlassian.net'}
                            aria-describedby={pDef.id === 'gitlab' ? 'wizard-gitlab-endpoint-examples' : undefined}
                            value={conn.endpoint}
                            onChange={e => updateConn(pDef.id, { endpoint: e.target.value, testStatus: '' })}
                          />
                          {pDef.id === 'gitlab' && (
                            <div id="wizard-gitlab-endpoint-examples" className="mt-wizard-section-hint">
                              Examples: https://gitlab.com/api/v4/ or https://gitlab.example.com/api/v4/
                            </div>
                          )}
                        </div>
                        {conn.plugin === 'codecov' && (
                          <>
                            <div className="mt-conn-field">
                              <label>Organization</label>
                              <input className="sub-input" type="text" placeholder="e.g. konflux-ci"
                                value={conn.organization}
                                onChange={e => updateConn(pDef.id, { organization: e.target.value, testStatus: '' })} />
                            </div>
                            <div className="mt-conn-field">
                              <label>Repository service</label>
                              <select className="sub-input" value={conn.service}
                                onChange={e => updateConn(pDef.id, { service: e.target.value, testStatus: '' })}>
                                <option value="github">GitHub</option>
                                <option value="gitlab">GitLab</option>
                                <option value="github_enterprise">GitHub Enterprise</option>
                                <option value="gitlab_enterprise">GitLab Enterprise</option>
                              </select>
                            </div>
                          </>
                        )}
                        <div className="mt-conn-field">
                          <label>{pDef.tokenLabel}</label>
                          <input
                            className="sub-input" type="password" placeholder="Paste your token here"
                            value={conn.token}
                            onChange={e => updateConn(pDef.id, { token: e.target.value, testStatus: '' })}
                          />
                        </div>
                        <div className="mt-conn-field">
                          <label>Token Expiry Date</label>
                          <input
                            className="sub-input" type="date" value={conn.expiry_date}
                            disabled={conn.never_expires}
                            onChange={e => updateConn(pDef.id, { expiry_date: e.target.value })}
                          />
                          <label className="mt-conn-never">
                            <input
                              type="checkbox" checked={conn.never_expires}
                              onChange={e => updateConn(pDef.id, { never_expires: e.target.checked, expiry_date: e.target.checked ? '' : conn.expiry_date })}
                            />
                            {' '}Never expires
                          </label>
                        </div>
                        {pDef.hasUser && (
                          <div className="mt-conn-field">
                            <label>Username / Email</label>
                            <input
                              className="sub-input" type="text" placeholder="user@company.com"
                              value={conn.username}
                              onChange={e => updateConn(pDef.id, { username: e.target.value })}
                            />
                          </div>
                        )}
                        <div className="mt-conn-test-row">
                          <button
                            className="sub-btn sub-btn--small"
                            disabled={conn.testStatus === 'testing'}
                            onClick={() => void handleTestConnection(conn)}
                          >
                            Test Connection
                          </button>
                          {conn.testStatus === 'ok' && <span className="mt-conn-test-badge mt-conn-test-badge--ok">Verified</span>}
                          {conn.testStatus === 'fail' && <span className="mt-conn-test-badge mt-conn-test-badge--fail">Failed</span>}
                          {conn.testStatus === 'testing' && <span className="mt-conn-test-badge mt-conn-test-badge--testing">Testing...</span>}
                        </div>
                      </div>
                    )}
                  </div>
                )
              })}
              {validationError && <div className="mt-wizard-section-hint" role="status">To continue: {validationError}</div>}
            </>
          )}

          {/* Step: Sources */}
          {stepLabel === 'Sources' && (
            <WizardSourcesStep
              connections={connections}
              gitRepos={gitRepos}
              setGitRepos={setGitRepos}
              jiraProjects={jiraProjects}
              setJiraProjects={setJiraProjects}
              testScopes={testScopes}
              setTestScopes={setTestScopes}
            />
          )}

          {/* Step: Jira Settings */}
          {stepLabel === 'Jira Settings' && (
            <WizardJiraSettingsStep
              connections={connections}
              jiraProjects={jiraProjects}
              setJiraProjects={setJiraProjects}
              jiraScopeConfigs={jiraScopeConfigs}
              setJiraScopeConfigs={setJiraScopeConfigs}
            />
          )}

          {/* Step: Dashboards */}
          {stepLabel === 'Dashboards' && (
            <>
              <div className="mt-wizard-section-hint">
                Select which dashboards to enable for this team.
              </div>
              <div className="mt-wizard-dash-grid">
                {catalogTemplates.map(tmpl => (
                  <label
                    key={tmpl.dashboard_id}
                    className={`mt-wizard-dash-item${selectedDashIds.includes(tmpl.dashboard_id) ? ' selected' : ''}`}
                  >
                    <input type="checkbox" checked={selectedDashIds.includes(tmpl.dashboard_id)} onChange={() => toggleDashboard(tmpl)} />
                    <div className="mt-wizard-dash-item-info">
                      <div className="mt-wizard-dash-item-title">{tmpl.title}</div>
                      {tmpl.description && <div className="mt-wizard-dash-item-desc">{tmpl.description}</div>}
                    </div>
                  </label>
                ))}
              </div>
            </>
          )}

          {/* Step: Configuration */}
          {stepLabel === 'Configuration' && (
            <>
              <div className="sub-form-section">
                <div className="sub-form-section-header">
                  <label>Configuration</label>
                  {kvRows.length < configKeys.length && (
                    <button className="sub-btn sub-btn--small" onClick={() => setKvRows(prev => [...prev, { key: '', value: '' }])}>
                      + Add
                    </button>
                  )}
                </div>
                <div>
                  {kvRows.length === 0 ? (
                    <p className="sub-empty-hint">No configuration entries. This step is optional.</p>
                  ) : (
                    kvRows.map((row, idx) => {
                      const used = kvRows.map(r => r.key).filter(Boolean)
                      return (
                        <div key={idx} className="mt-kv-row">
                          <select
                            className="sub-input"
                            value={row.key}
                            onChange={e => setKvRows(prev => prev.map((r, i) => i === idx ? { ...r, key: e.target.value } : r))}
                          >
                            {!row.key && <option value="">-- select --</option>}
                            {configKeys
                              .filter(ck => !used.includes(ck.key) || ck.key === row.key)
                              .map(ck => <option key={ck.key} value={ck.key}>{ck.label}</option>)}
                          </select>
                          <input
                            className="sub-input"
                            type={configKeys.find(ck => ck.key === row.key)?.type === 'number' ? 'number' : 'text'}
                            value={row.value}
                            onChange={e => setKvRows(prev => prev.map((r, i) => i === idx ? { ...r, value: e.target.value } : r))}
                          />
                          <button className="sub-btn sub-btn--small sub-btn--danger" onClick={() => setKvRows(prev => prev.filter((_, i) => i !== idx))}>
                            X
                          </button>
                        </div>
                      )
                    })
                  )}
                </div>
              </div>

              <div className="mt-wizard-summary">
                <strong>Summary</strong>
                <br />
                Organization: <strong>{orgs.find(o => o.id === selectedOrgId)?.name || '—'}</strong>
                <br />
                Product: <strong>{products.find(p => p.id === selectedProductId)?.name || '—'}</strong>
                <br />
                Team: <strong>{teamName || '(unnamed)'}</strong> ({teamId || '—'})
                <br />
                Contact: {teamEmail || '—'}
                <br />
                Git Repos: {gitRepos.length}
                {jiraProjects.length > 0 && (
                  <>
                    <br />
                    Jira Projects: {jiraProjects.length}
                    {Object.keys(jiraScopeConfigs).length > 0 && <> (mappings configured)</>}
                  </>
                )}
                {testScopes.length > 0 && (
                  <>
                    <br />
                    TestRegistry Scopes: {testScopes.length}
                  </>
                )}
                <br />
                Dashboards: {selectedDashboards.length} selected
                <br />
                Config entries: {kvRows.filter(r => r.key).length}
              </div>
            </>
          )}

          {/* Result */}
          {result && <WizardResult result={result} teamName={teamName} />}
        </div>

        <div className="mt-wizard-footer">
          {result ? (
            <>
              <div />
              <div className="mt-wizard-footer-right">
                <button className="sub-btn sub-btn--primary" onClick={onCreated}>Close</button>
              </div>
            </>
          ) : (
            <>
              <div>
                {clampedStep > 0 && (
                  <button className="sub-btn sub-btn--secondary" onClick={handleBack}>Back</button>
                )}
              </div>
              <div className="mt-wizard-footer-right">
                <button className="sub-btn sub-btn--secondary" onClick={onCancel}>Cancel</button>
                {clampedStep < WIZARD_STEPS.length - 1 ? (
                  <button
                    className="sub-btn sub-btn--primary"
                    disabled={!isValid || validatingSources}
                    onClick={() => void handleNext()}
                  >
                    {validatingSources ? 'Validating sources...' : 'Next'}
                  </button>
                ) : (
                  <button
                    className="sub-btn sub-btn--primary"
                    disabled={creating}
                    onClick={() => void handleCreate()}
                  >
                    {creating ? 'Creating...' : 'Create Team'}
                  </button>
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
    </Portal>
  )
}
