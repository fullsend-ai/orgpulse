import { useState, useEffect, useCallback } from 'react'
import {
  fetchAllTeams,
  fetchTeamConfig,
  fetchTokenStatus,
  fetchPersonalTokens,
  savePersonalToken,
  deletePersonalToken,
  testConnection,
  createDevLakeConnection,
  updateDevLakeConnection,
  fetchAvailableCodecov,
  attachCodecov,
  TESTREGISTRY_CI_TOOLS,
  type TestRegistryCITool,
  type FullTeam,
  type TokenStatus,
  type PersonalToken,
  type DevLakeConnection,
  type AvailableCodecov,
} from '../api'
import SourceIcon from '../SourceIcon'
import { Portal } from '../Portal'

const PLUGINS = [
  { id: 'github', label: 'GitHub', defaultEndpoint: 'https://api.github.com/', hasUser: false },
  { id: 'gitlab', label: 'GitLab', defaultEndpoint: '', hasUser: false },
  { id: 'jira', label: 'Jira', defaultEndpoint: '', hasUser: true },
  { id: 'codecov', label: 'Codecov', defaultEndpoint: 'https://api.codecov.io', hasUser: false },
  { id: 'testregistry', label: 'TestRegistry', defaultEndpoint: '', hasUser: false },
] as const

// Plugins that only exist as team DevLake connections (no personal token).
const TEAM_ONLY_PLUGINS = new Set<string>(['codecov', 'testregistry'])

function testRegistryOrgLabel(ciTool: string): string {
  return ciTool === 'Openshift CI' ? 'GitHub Organization' : 'Quay Organization'
}

function TestRegistryCIToolHint({ ciTool }: { ciTool: string }) {
  return (
    <span className="mt-wizard-section-hint">
      {ciTool === 'Openshift CI' && 'Collects Prow jobs from prow.ci.openshift.org. Scopes are the repos under openshift/release ci-operator/config/<org>, listed through the GitHub API.'}
      {ciTool === 'Tekton CI' && 'Collects Tekton PipelineRun artifacts stored as OCI images in the Quay.io organization. Scopes are the Quay repositories.'}
      {!ciTool && 'Selects where test results are collected from.'}
      {' '}The CI tool and organization cannot be changed after the connection is created.
    </span>
  )
}

type PluginDef = (typeof PLUGINS)[number]

function expiryClass(dateStr: string): string {
  const expiry = new Date(dateStr + 'T00:00:00')
  const daysLeft = Math.ceil((expiry.getTime() - Date.now()) / 86400000)
  if (daysLeft < 0) return 'expired'
  if (daysLeft <= 14) return 'warning'
  return 'ok'
}

interface TeamData {
  team: FullTeam
  config: Record<string, string>
  tokenStatuses: TokenStatus[]
}

interface TokenRow {
  scope: 'personal' | 'team'
  teamId: string
  teamName: string
  plugin: PluginDef
  connId: string
  endpoint: string
  expiryDate: string
  organization: string
  service: string
  ciTool: string
}

interface TokenManagementPanelProps {
  email: string
  isAdmin: boolean
  devlakeEnabled: boolean
}

export default function TokenManagementPanel({ email, isAdmin, devlakeEnabled }: TokenManagementPanelProps) {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [teamData, setTeamData] = useState<TeamData[]>([])
  const [personalTokens, setPersonalTokens] = useState<PersonalToken[]>([])
  const [showAdd, setShowAdd] = useState(false)
  const [showAttach, setShowAttach] = useState(false)
  const [updatingRow, setUpdatingRow] = useState<TokenRow | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [allTeams, personal] = await Promise.all([
        fetchAllTeams(),
        fetchPersonalTokens(),
      ])
      setPersonalTokens(personal)

      const lower = email.toLowerCase()
      const myTeams = isAdmin ? allTeams : allTeams.filter(t => (t.contact_email || '').toLowerCase() === lower)

      if (myTeams.length === 0) {
        setTeamData([])
        setLoading(false)
        return
      }

      const data = await Promise.all(
        myTeams.map(async team => {
          const [cfg, statuses] = await Promise.all([
            fetchTeamConfig(team.id).catch(() => ({}) as Record<string, string>),
            devlakeEnabled ? fetchTokenStatus(team.id) : Promise.resolve([] as TokenStatus[]),
          ])
          return { team, config: cfg, tokenStatuses: statuses }
        }),
      )
      setTeamData(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load token status')
    } finally {
      setLoading(false)
    }
  }, [email, isAdmin, devlakeEnabled])

  useEffect(() => { void load() }, [load])

  const rows: TokenRow[] = []

  for (const pt of personalTokens) {
    const p = PLUGINS.find(pl => pl.id === pt.plugin)
    if (!p) continue
    rows.push({
      scope: 'personal',
      teamId: '',
      teamName: '',
      plugin: p,
      connId: '',
      endpoint: pt.endpoint,
      expiryDate: pt.expiry_date || '',
      organization: '',
      service: 'github',
      ciTool: '',
    })
  }

  if (devlakeEnabled) {
    for (const td of teamData) {
      const statusMap: Record<string, TokenStatus> = {}
      for (const s of td.tokenStatuses) statusMap[s.plugin] = s

      for (const p of PLUGINS) {
        const configKey = `devlake_${p.id}_connection_id`
        if (td.config[configKey]) {
          rows.push({
            scope: 'team',
            teamId: td.team.id,
            teamName: td.team.name,
            plugin: p,
            connId: td.config[configKey] ?? '',
            endpoint: td.config[`devlake_${p.id}_endpoint`] ?? p.defaultEndpoint,
            expiryDate: statusMap[p.id]?.expiry_date || '',
            organization: td.config[`devlake_${p.id}_organization`] ?? '',
            service: td.config[`devlake_${p.id}_service`] ?? 'github',
            ciTool: td.config[`devlake_${p.id}_ci_tool`] ?? '',
          })
        }
      }
    }
  }

  rows.sort((a, b) => {
    if (a.scope !== b.scope) return a.scope === 'personal' ? -1 : 1
    return a.teamName.localeCompare(b.teamName) || a.plugin.label.localeCompare(b.plugin.label)
  })

  const handleDeletePersonal = async (pluginId: string) => {
    if (!window.confirm(`Remove your personal ${pluginId} token?`)) return
    try {
      await deletePersonalToken(pluginId)
      void load()
    } catch (err) {
      window.alert(`Failed to delete: ${err instanceof Error ? err.message : 'Unknown'}`)
    }
  }

  if (loading) {
    return (
      <div className="settings-loader">
        <div className="diagram-card__spinner" />
        <div className="settings-loader-text">Loading token status...</div>
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

  return (
    <div>
      <div className="sub-header">
        <div>
          <p className="settings-panel-description">
            Manage your personal access tokens and team DevLake connections.
          </p>
        </div>
        <div className="sub-actions">
          {devlakeEnabled && isAdmin && teamData.length > 0 && (
            <button className="sub-btn sub-btn--secondary" onClick={() => setShowAttach(true)}>Attach Existing Codecov</button>
          )}
          <button className="sub-btn sub-btn--primary" onClick={() => setShowAdd(true)}>+ New Token</button>
        </div>
      </div>

      {rows.length === 0 && (
        <div className="sub-empty">
          <p>No tokens configured yet. Click &quot;+ New Token&quot; to add one.</p>
        </div>
      )}

      {rows.length > 0 && (
        <table className="sub-table">
          <thead>
            <tr>
              <th>Scope</th>
              <th>Plugin</th>
              <th>Endpoint</th>
              <th>Expiry</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {rows.map(row => (
              <tr key={`${row.scope}-${row.teamId}-${row.plugin.id}`}>
                <td>
                  {row.scope === 'personal' ? (
                    <span className="mt-conn-expiry mt-conn-expiry--ok">Personal</span>
                  ) : (
                    <span className="mt-source-type-badge">{row.teamName}</span>
                  )}
                </td>
                <td><SourceIcon source={row.plugin.id} /></td>
                <td className="sotu-updated">
                  {row.plugin.id === 'testregistry'
                    ? <><span className="mt-source-type-badge">{row.ciTool || 'unknown CI tool'}</span> <code>{row.organization}</code></>
                    : <code>{row.endpoint}</code>}
                </td>
                <td>
                  {row.expiryDate === 'never' ? (
                    <span className="mt-conn-expiry mt-conn-expiry--ok">Never</span>
                  ) : row.expiryDate ? (
                    <span className={`mt-conn-expiry mt-conn-expiry--${expiryClass(row.expiryDate)}`}>
                      {row.expiryDate}
                    </span>
                  ) : (
                    <span className="mt-conn-expiry mt-conn-expiry--unknown">Not set</span>
                  )}
                </td>
                <td className="sub-actions">
                  {(row.plugin.id !== 'testregistry' || row.ciTool === 'Openshift CI') && (
                    <button className="sub-btn sub-btn--small" onClick={() => setUpdatingRow(row)}>Update</button>
                  )}
                  {row.scope === 'personal' && (
                    <button className="sub-btn sub-btn--small sub-btn--danger" onClick={() => void handleDeletePersonal(row.plugin.id)}>
                      Remove
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {showAdd && (
        <AddTokenWizard
          teamData={teamData}
          personalTokens={personalTokens}
          devlakeEnabled={devlakeEnabled}
          onSaved={() => { setShowAdd(false); void load() }}
          onCancel={() => setShowAdd(false)}
        />
      )}

      {showAttach && (
        <AttachCodecovDialog
          teamData={teamData.filter(td => !td.config.devlake_codecov_connection_id)}
          onSaved={() => { setShowAttach(false); void load() }}
          onCancel={() => setShowAttach(false)}
        />
      )}

      {updatingRow && (
        <UpdateTokenWizard
          row={updatingRow}
          onSaved={() => { setUpdatingRow(null); void load() }}
          onCancel={() => setUpdatingRow(null)}
        />
      )}
    </div>
  )
}

function AttachCodecovDialog({ teamData, onSaved, onCancel }: {
  teamData: TeamData[]
  onSaved: () => void
  onCancel: () => void
}) {
  const [teamId, setTeamId] = useState('')
  const [available, setAvailable] = useState<AvailableCodecov | null>(null)
  const [connectionId, setConnectionId] = useState(0)
  const [blueprintId, setBlueprintId] = useState(0)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!teamId) return
    let active = true
    fetchAvailableCodecov(teamId)
      .then(data => { if (active) setAvailable(data) })
      .catch(err => { if (active) setError(err instanceof Error ? err.message : 'Failed to load Codecov resources') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [teamId])

  const blueprints = available?.blueprints.filter(bp => bp.connection_id === connectionId) ?? []
  const selected = available?.blueprints.find(bp => bp.id === blueprintId)

  const save = async () => {
    setSaving(true)
    setError(null)
    try {
      await attachCodecov(teamId, connectionId, blueprintId)
      onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to attach Codecov blueprint')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mt-wizard-overlay active" onClick={e => { if (e.target === e.currentTarget) onCancel() }}>
      <div className="mt-wizard-modal">
        <div className="mt-wizard-header">
          <h3>Attach Existing Codecov</h3>
          <button className="mt-wizard-close" onClick={onCancel}>&times;</button>
        </div>
        <div className="mt-wizard-body">
          {error && <div className="mt-wizard-error">{error}</div>}
          <p className="mt-wizard-section-hint">Use an existing DevLake Codecov connection and blueprint. The blueprint must cover repositories already added to this team.</p>
          <div className="sub-form-row">
            <label>Team</label>
            <select className="sub-input" value={teamId} onChange={e => {
              setTeamId(e.target.value); setAvailable(null); setConnectionId(0); setBlueprintId(0); setError(null); setLoading(!!e.target.value)
            }}>
              <option value="">-- Select Team --</option>
              {teamData.map(td => <option key={td.team.id} value={td.team.id}>{td.team.name}</option>)}
            </select>
          </div>
          {teamId && loading && <p>Loading Codecov resources...</p>}
          {teamId && available && (
            <>
              <div className="sub-form-row">
                <label>Codecov connection</label>
                <select className="sub-input" value={connectionId} onChange={e => { setConnectionId(Number(e.target.value)); setBlueprintId(0) }}>
                  <option value={0}>-- Select Connection --</option>
                  {available.connections.map(conn => (
                    <option key={conn.id} value={conn.id}>{conn.name} (#{conn.id}, {conn.organization}/{conn.service})</option>
                  ))}
                </select>
              </div>
              {connectionId > 0 && (
                <div className="sub-form-row">
                  <label>Blueprint</label>
                  <select className="sub-input" value={blueprintId} onChange={e => setBlueprintId(Number(e.target.value))}>
                    <option value={0}>-- Select Blueprint --</option>
                    {blueprints.map(bp => <option key={bp.id} value={bp.id}>{bp.name} (#{bp.id})</option>)}
                  </select>
                  {blueprints.length === 0 && <span className="mt-wizard-section-hint">No unattached Codecov blueprint uses this connection.</span>}
                </div>
              )}
              {selected && <p className="mt-wizard-section-hint">Scopes: {selected.scopes.join(', ')}</p>}
            </>
          )}
          {teamData.length === 0 && <p>This team already has a Codecov connection.</p>}
        </div>
        <div className="mt-wizard-footer">
          <div />
          <div className="mt-wizard-footer-right">
            <button className="sub-btn sub-btn--secondary" onClick={onCancel}>Cancel</button>
            <button className="sub-btn sub-btn--primary" disabled={!teamId || !connectionId || !blueprintId || saving} onClick={() => void save()}>
              {saving ? 'Attaching...' : 'Attach'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

// --------------- Add Token Wizard ---------------

function AddTokenWizard({
  teamData, personalTokens, devlakeEnabled, onSaved, onCancel,
}: {
  teamData: TeamData[]
  personalTokens: PersonalToken[]
  devlakeEnabled: boolean
  onSaved: () => void
  onCancel: () => void
}) {
  const [step, setStep] = useState(0)
  const [error, setError] = useState<string | null>(null)

  const [scope, setScope] = useState<'personal' | 'team'>('personal')
  const [teamId, setTeamId] = useState('')
  const [pluginId, setPluginId] = useState('')
  const [endpoint, setEndpoint] = useState('')
  const [token, setToken] = useState('')
  const [expiryDate, setExpiryDate] = useState('')
  const [neverExpires, setNeverExpires] = useState(false)
  const [username, setUsername] = useState('')
  const [organization, setOrganization] = useState('')
  const [service, setService] = useState('github')
  const [ciTool, setCITool] = useState<TestRegistryCITool | ''>('')

  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null)
  const [testPassed, setTestPassed] = useState(false)
  const [saving, setSaving] = useState(false)

  const selectedPlugin = PLUGINS.find(p => p.id === pluginId)
  const isTestRegistry = pluginId === 'testregistry'
  // Tekton CI reads public Quay.io data and needs no token.
  const needsToken = !isTestRegistry || ciTool === 'Openshift CI'
  const canTest = isTestRegistry
    ? !!ciTool && !!organization.trim() && (!needsToken || !!token.trim())
    : !!token.trim() && !!endpoint.trim()

  const availablePersonalPlugins = PLUGINS.filter(
    p => !TEAM_ONLY_PLUGINS.has(p.id) && !personalTokens.some(pt => pt.plugin === p.id),
  )

  const selectedTeamData = teamData.find(td => td.team.id === teamId)
  const availableTeamPlugins = selectedTeamData
    ? PLUGINS.filter(p => !selectedTeamData.config[`devlake_${p.id}_connection_id`])
    : PLUGINS

  const STEPS = ['Type & Plugin', 'Connection & Test', 'Save']

  const validate = (): string | null => {
    if (step === 0) {
      if (!pluginId) return 'Plugin is required.'
      if (scope === 'team' && !teamId) return 'Team is required.'
    }
    if (step === 1) {
      if (isTestRegistry) {
        if (!ciTool) return 'CI tool is required.'
        if (!organization.trim()) return `${testRegistryOrgLabel(ciTool)} is required.`
      } else if (!endpoint.trim()) return 'Endpoint is required.'
      if (needsToken && !token.trim()) return 'Token is required.'
      if (needsToken && !neverExpires && !expiryDate.trim()) return 'Expiry date is required.'
      if (pluginId === 'jira' && !username.trim()) return 'Username is required for Jira.'
      if (pluginId === 'codecov' && !organization.trim()) return 'Organization is required for Codecov.'
      if (!testPassed) return 'Test the connection before proceeding.'
    }
    return null
  }

  const stepValid = validate() === null

  const resetTest = () => { setTestPassed(false); setTestResult(null) }

  const handleNext = () => {
    const err = validate()
    if (err) { setError(err); return }
    setError(null)
    setStep(s => s + 1)
  }

  const handleTest = async () => {
    if (!canTest) return
    setTesting(true)
    setTestResult(null)
    try {
      const result = await testConnection(pluginId, isTestRegistry ? '' : endpoint.trim(), needsToken ? token.trim() : '', {
        username: pluginId === 'jira' ? username : undefined,
        organization: pluginId === 'codecov' || isTestRegistry ? organization.trim() : undefined,
        service: pluginId === 'codecov' ? service : undefined,
        ci_tool: isTestRegistry && ciTool ? ciTool : undefined,
      })
      setTestResult(result)
      setTestPassed(result.success)
      if (result.success && result.expiry_date && !expiryDate && !neverExpires) {
        setExpiryDate(result.expiry_date)
      }
    } catch {
      setTestResult({ success: false, message: 'Test failed' })
      setTestPassed(false)
    } finally {
      setTesting(false)
    }
  }

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      const expiry = neverExpires ? 'never' : expiryDate.trim()
      if (scope === 'personal') {
        await savePersonalToken(pluginId, {
          endpoint: endpoint.trim(),
          token: token.trim(),
          expiry_date: expiry,
          username: pluginId === 'jira' ? username : undefined,
        })
      } else {
        const entry: DevLakeConnection = {
          plugin: pluginId,
          endpoint: isTestRegistry ? '' : endpoint.trim(),
          token: needsToken ? token.trim() : '',
          expiry_date: needsToken ? expiry : 'never',
        }
        if (pluginId === 'jira') entry.username = username
        if (pluginId === 'codecov') {
          entry.organization = organization.trim()
          entry.service = service
        }
        if (isTestRegistry && ciTool) {
          entry.ci_tool = ciTool
          entry.organization = organization.trim()
        }
        await createDevLakeConnection(teamId, entry)
      }
      onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Creation failed')
      setSaving(false)
    }
  }

  const handleScopeChange = (newScope: 'personal' | 'team') => {
    setScope(newScope)
    setPluginId('')
    setTeamId('')
    setEndpoint('')
    setToken('')
    setTestPassed(false)
    setTestResult(null)
    setOrganization('')
    setService('github')
    setCITool('')
  }

  return (
    <Portal>
    <div className="mt-wizard-overlay active" onClick={e => { if (e.target === e.currentTarget) onCancel() }}>
      <div className="mt-wizard-modal">
        <div className="mt-wizard-header">
          <h3>New Token</h3>
          <button className="mt-wizard-close" onClick={onCancel}>&times;</button>
        </div>

        <div className="mt-wizard-steps">
          {STEPS.map((label, i) => (
            <div key={i} className={`mt-wizard-step${i < step ? ' done' : ''}${i === step ? ' active' : ''}`}>
              <span className="mt-wizard-step-num">{i + 1}</span>
              <span>{label}</span>
            </div>
          ))}
        </div>

        <div className="mt-wizard-body">
          {error && <div className="mt-wizard-error">{error}</div>}

          {step === 0 && (
            <>
              <div className="sub-form-row">
                <label>Token Type <span style={{ color: '#c9190b' }}>*</span></label>
                <select className="sub-input" value={scope} onChange={e => handleScopeChange(e.target.value as 'personal' | 'team')}>
                  <option value="personal">Personal (for My Items dashboard)</option>
                  {devlakeEnabled && <option value="team">Team / DevLake (for data syncing)</option>}
                </select>
                <span className="mt-wizard-section-hint">
                  {scope === 'personal'
                    ? 'Personal tokens are used to query your assigned issues from source systems.'
                    : 'Team tokens are used by DevLake to sync data for the team.'}
                </span>
              </div>

              {scope === 'team' && (
                <div className="sub-form-row">
                  <label>Team <span style={{ color: '#c9190b' }}>*</span></label>
                  <select
                    className="sub-input"
                    value={teamId}
                    onChange={e => { setTeamId(e.target.value); setPluginId('') }}
                  >
                    <option value="">-- Select Team --</option>
                    {teamData.map(td => (
                      <option key={td.team.id} value={td.team.id}>{td.team.name}</option>
                    ))}
                  </select>
                  {teamData.length === 0 && (
                    <span className="mt-wizard-section-hint">No teams found. Create a team first in My Teams.</span>
                  )}
                </div>
              )}

              <div className="sub-form-row">
                <label>Plugin <span style={{ color: '#c9190b' }}>*</span></label>
                <select
                  className="sub-input"
                  value={pluginId}
                  disabled={scope === 'team' && !teamId}
                  onChange={e => {
                    setPluginId(e.target.value)
                    const p = PLUGINS.find(pl => pl.id === e.target.value)
                    if (p) setEndpoint(p.defaultEndpoint)
                    setToken('')
                    setTestPassed(false)
                    setTestResult(null)
                    setOrganization('')
                    setService('github')
                    setCITool('')
                  }}
                >
                  <option value="">-- Select Plugin --</option>
                  {(scope === 'personal' ? availablePersonalPlugins : availableTeamPlugins).map(p => (
                    <option key={p.id} value={p.id}>{p.label}</option>
                  ))}
                </select>
                {scope === 'personal' && availablePersonalPlugins.length === 0 && (
                  <span className="mt-wizard-section-hint">All plugins already have personal tokens configured.</span>
                )}
                {scope === 'team' && teamId && availableTeamPlugins.length === 0 && (
                  <span className="mt-wizard-section-hint">All plugins already have connections for this team.</span>
                )}
              </div>
            </>
          )}

          {step === 1 && isTestRegistry && (
            <>
              <div className="sub-form-row">
                <label>CI Tool <span style={{ color: '#c9190b' }}>*</span></label>
                <select className="sub-input" value={ciTool}
                  onChange={e => {
                    setCITool(e.target.value as TestRegistryCITool | '')
                    setOrganization('')
                    setToken('')
                    setExpiryDate('')
                    setNeverExpires(false)
                    resetTest()
                  }}>
                  <option value="">-- Select CI Tool --</option>
                  {TESTREGISTRY_CI_TOOLS.map(t => <option key={t} value={t}>{t}</option>)}
                </select>
                <TestRegistryCIToolHint ciTool={ciTool} />
              </div>
              {ciTool && (
                <div className="sub-form-row">
                  <label>{testRegistryOrgLabel(ciTool)} <span style={{ color: '#c9190b' }}>*</span></label>
                  <input className="sub-input" type="text"
                    placeholder={ciTool === 'Openshift CI' ? 'e.g. konflux-ci' : 'e.g. konflux-test-storage'}
                    value={organization}
                    onChange={e => { setOrganization(e.target.value); resetTest() }} />
                </div>
              )}
              {ciTool === 'Openshift CI' && (
                <>
                  <div className="sub-form-row">
                    <label>GitHub Token <span style={{ color: '#c9190b' }}>*</span></label>
                    <input className="sub-input" type="password" placeholder="Paste your GitHub token here"
                      value={token}
                      onChange={e => { setToken(e.target.value); resetTest() }} />
                    <span className="mt-wizard-section-hint">Needs read access to the organization; used to list openshift/release CI configs.</span>
                  </div>
                  <div className="sub-form-row">
                    <label>Expiry Date <span style={{ color: '#c9190b' }}>*</span></label>
                    <input className="sub-input" type="date"
                      value={expiryDate} disabled={neverExpires}
                      onChange={e => setExpiryDate(e.target.value)} />
                    <label className="mt-conn-never">
                      <input type="checkbox" checked={neverExpires}
                        onChange={e => { setNeverExpires(e.target.checked); if (e.target.checked) setExpiryDate('') }}
                      />{' '}Never expires
                    </label>
                  </div>
                </>
              )}
              <div style={{ marginTop: 16 }}>
                <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  <button className="sub-btn sub-btn--small" disabled={testing || !canTest} onClick={() => void handleTest()}>
                    {testing ? 'Testing...' : 'Test Connection'}
                  </button>
                  {testResult && (
                    <span className={`mt-conn-test-badge mt-conn-test-badge--${testResult.success ? 'ok' : 'fail'}`}>
                      {testResult.success ? 'Connection OK' : testResult.message}
                    </span>
                  )}
                </div>
              </div>
            </>
          )}

          {step === 1 && !isTestRegistry && (
            <>
              <div className="sub-form-row">
                <label>Endpoint <span style={{ color: '#c9190b' }}>*</span></label>
                <input
                  className="sub-input" type="text"
                  placeholder={pluginId === 'gitlab' ? 'Enter your GitLab API endpoint' : selectedPlugin?.defaultEndpoint || 'https://your-instance.atlassian.net'}
                  aria-describedby={pluginId === 'gitlab' ? 'token-gitlab-endpoint-examples' : undefined}
                  value={endpoint}
                  onChange={e => { setEndpoint(e.target.value); setTestPassed(false); setTestResult(null) }}
                />
                {pluginId === 'gitlab' && (
                  <span id="token-gitlab-endpoint-examples" className="mt-wizard-section-hint">
                    Examples: https://gitlab.com/api/v4/ or https://gitlab.example.com/api/v4/
                  </span>
                )}
              </div>
              <div className="sub-form-row">
                <label>Token <span style={{ color: '#c9190b' }}>*</span></label>
                <input
                  className="sub-input" type="password" placeholder="Paste your token here"
                  value={token}
                  onChange={e => { setToken(e.target.value); setTestPassed(false); setTestResult(null) }}
                />
              </div>
              {pluginId === 'codecov' && (
                <>
                  <div className="sub-form-row">
                    <label>Organization <span style={{ color: '#c9190b' }}>*</span></label>
                    <input className="sub-input" type="text" placeholder="e.g. konflux-ci" value={organization}
                      onChange={e => { setOrganization(e.target.value); setTestPassed(false); setTestResult(null) }} />
                  </div>
                  <div className="sub-form-row">
                    <label>Repository service</label>
                    <select className="sub-input" value={service}
                      onChange={e => { setService(e.target.value); setTestPassed(false); setTestResult(null) }}>
                      <option value="github">GitHub</option>
                      <option value="gitlab">GitLab</option>
                      <option value="github_enterprise">GitHub Enterprise</option>
                      <option value="gitlab_enterprise">GitLab Enterprise</option>
                    </select>
                    <span className="mt-wizard-section-hint">Collection uses this team’s GitHub or GitLab repositories owned by the selected organization.</span>
                  </div>
                </>
              )}
              <div className="sub-form-row">
                <label>Expiry Date <span style={{ color: '#c9190b' }}>*</span></label>
                <input
                  className="sub-input" type="date"
                  value={expiryDate} disabled={neverExpires}
                  onChange={e => setExpiryDate(e.target.value)}
                />
                <label className="mt-conn-never">
                  <input type="checkbox" checked={neverExpires}
                    onChange={e => { setNeverExpires(e.target.checked); if (e.target.checked) setExpiryDate('') }}
                  />{' '}Never expires
                </label>
              </div>
              {pluginId === 'jira' && (
                <div className="sub-form-row">
                  <label>Username / Email <span style={{ color: '#c9190b' }}>*</span></label>
                  <input
                    className="sub-input" type="text" placeholder="user@company.com"
                    value={username}
                    onChange={e => { setUsername(e.target.value); setTestPassed(false); setTestResult(null) }}
                  />
                </div>
              )}
              <div style={{ marginTop: 16 }}>
                <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  <button className="sub-btn sub-btn--small" disabled={testing || !token.trim() || !endpoint.trim()} onClick={() => void handleTest()}>
                    {testing ? 'Testing...' : 'Test Connection'}
                  </button>
                  {testResult && (
                    <span className={`mt-conn-test-badge mt-conn-test-badge--${testResult.success ? 'ok' : 'fail'}`}>
                      {testResult.success ? 'Connection OK' : testResult.message}
                    </span>
                  )}
                </div>
              </div>
            </>
          )}

          {step === 2 && (
            <div className="mt-wizard-summary">
              <strong>Summary</strong><br />
              Type: <strong>{scope === 'personal' ? 'Personal' : 'Team (DevLake)'}</strong><br />
              {scope === 'team' && <>Team: <strong>{teamData.find(td => td.team.id === teamId)?.team.name || teamId}</strong><br /></>}
              Plugin: <strong>{selectedPlugin?.label}</strong><br />
              {isTestRegistry ? (
                <>
                  CI Tool: <strong>{ciTool}</strong> (cannot be changed later)<br />
                  {testRegistryOrgLabel(ciTool)}: <strong>{organization}</strong><br />
                </>
              ) : <>Endpoint: <code>{endpoint}</code><br /></>}
              {pluginId === 'codecov' && <>Organization: <strong>{organization}</strong><br />Service: <strong>{service}</strong><br /></>}
              {needsToken && <>Expiry: {neverExpires ? 'Never' : expiryDate || '—'}<br /></>}
              Test: <span className={`mt-conn-test-badge mt-conn-test-badge--ok`}>Passed</span>
            </div>
          )}
        </div>

        <div className="mt-wizard-footer">
          <div>
            {step > 0 && <button className="sub-btn sub-btn--secondary" onClick={() => { setStep(s => s - 1); setError(null) }}>Back</button>}
          </div>
          <div className="mt-wizard-footer-right">
            <button className="sub-btn sub-btn--secondary" onClick={onCancel}>Cancel</button>
            {step < STEPS.length - 1 ? (
              <button className="sub-btn sub-btn--primary" disabled={!stepValid} onClick={handleNext}>Next</button>
            ) : (
              <button className="sub-btn sub-btn--primary" disabled={!testPassed || saving} onClick={() => void handleSave()}>
                {saving ? 'Saving...' : 'Save'}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
    </Portal>
  )
}

// --------------- Update Token Wizard ---------------

const UPDATE_STEPS = ['Credentials & Test', 'Save']

function UpdateTokenWizard({
  row, onSaved, onCancel,
}: {
  row: TokenRow
  onSaved: () => void
  onCancel: () => void
}) {
  const [step, setStep] = useState(0)
  const [error, setError] = useState<string | null>(null)

  const [token, setToken] = useState('')
  const [expiryDate, setExpiryDate] = useState(
    row.expiryDate && row.expiryDate !== 'never' ? row.expiryDate : '',
  )
  const [neverExpires, setNeverExpires] = useState(row.expiryDate === 'never')
  const [username, setUsername] = useState('')
  const [organization, setOrganization] = useState(row.organization)
  const [service, setService] = useState(row.service)

  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null)
  const [testPassed, setTestPassed] = useState(false)
  const [saving, setSaving] = useState(false)

  const validate = (): string | null => {
    if (step === 0) {
      if (!token.trim()) return 'Token is required.'
      if (!neverExpires && !expiryDate.trim()) return 'Expiry date is required.'
      if (row.plugin.id === 'jira' && !username.trim()) return 'Username is required for Jira.'
      if (row.plugin.id === 'codecov' && !organization.trim()) return 'Organization is required for Codecov.'
      if (!testPassed) return 'Test the connection before proceeding.'
    }
    return null
  }

  const stepValid = validate() === null

  const handleTest = async () => {
    if (!token.trim()) return
    setTesting(true)
    setTestResult(null)
    try {
      const isTestRegistry = row.plugin.id === 'testregistry'
      const result = await testConnection(
        row.plugin.id,
        isTestRegistry ? '' : row.endpoint || row.plugin.defaultEndpoint,
        token.trim(),
        {
          username: row.plugin.id === 'jira' ? username : undefined,
          organization: row.plugin.id === 'codecov' || isTestRegistry ? organization.trim() : undefined,
          service: row.plugin.id === 'codecov' ? service : undefined,
          ci_tool: isTestRegistry ? row.ciTool as TestRegistryCITool : undefined,
        },
      )
      setTestResult(result)
      setTestPassed(result.success)
      if (result.success && result.expiry_date && !expiryDate && !neverExpires) {
        setExpiryDate(result.expiry_date)
      }
    } catch {
      setTestResult({ success: false, message: 'Test failed' })
      setTestPassed(false)
    } finally {
      setTesting(false)
    }
  }

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      const expiry = neverExpires ? 'never' : expiryDate.trim()
      if (row.scope === 'personal') {
        await savePersonalToken(row.plugin.id, {
          endpoint: row.endpoint || row.plugin.defaultEndpoint,
          token: token.trim(),
          expiry_date: expiry,
          username: row.plugin.id === 'jira' ? username : undefined,
        })
      } else {
        const entry: Partial<DevLakeConnection> = {
          token: token.trim(),
          expiry_date: expiry,
        }
        if (row.plugin.id === 'jira') entry.username = username
        if (row.plugin.id === 'codecov') {
          entry.organization = organization.trim()
          entry.service = service
        }
        await updateDevLakeConnection(row.teamId, row.plugin.id, entry)
      }
      onSaved()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Update failed')
      setSaving(false)
    }
  }

  const scopeLabel = row.scope === 'personal' ? 'Personal' : row.teamName

  return (
    <Portal>
    <div className="mt-wizard-overlay active" onClick={e => { if (e.target === e.currentTarget) onCancel() }}>
      <div className="mt-wizard-modal">
        <div className="mt-wizard-header">
          <h3>Update Token — {scopeLabel} / {row.plugin.label}</h3>
          <button className="mt-wizard-close" onClick={onCancel}>&times;</button>
        </div>

        <div className="mt-wizard-steps">
          {UPDATE_STEPS.map((label, i) => (
            <div key={i} className={`mt-wizard-step${i < step ? ' done' : ''}${i === step ? ' active' : ''}`}>
              <span className="mt-wizard-step-num">{i + 1}</span>
              <span>{label}</span>
            </div>
          ))}
        </div>

        <div className="mt-wizard-body">
          {error && <div className="mt-wizard-error">{error}</div>}

          {step === 0 && (
            <>
              {row.plugin.id === 'testregistry' && (
                <>
                  <div className="sub-form-row">
                    <label>CI Tool</label>
                    <input className="sub-input" type="text" value={row.ciTool} disabled readOnly />
                  </div>
                  <div className="sub-form-row">
                    <label>{testRegistryOrgLabel(row.ciTool)}</label>
                    <input className="sub-input" type="text" value={row.organization} disabled readOnly />
                    <span className="mt-wizard-section-hint">
                      The CI tool and organization cannot be changed: the registered scopes come from this source.
                    </span>
                  </div>
                </>
              )}
              <div className="sub-form-row">
                <label>{row.plugin.id === 'testregistry' ? 'New GitHub Token' : 'New Token'} <span style={{ color: '#c9190b' }}>*</span></label>
                <input
                  className="sub-input" type="password" placeholder="Paste your new token here"
                  value={token}
                  onChange={e => { setToken(e.target.value); setTestPassed(false); setTestResult(null) }}
                />
              </div>
              <div className="sub-form-row">
                <label>New Expiry Date <span style={{ color: '#c9190b' }}>*</span></label>
                <input
                  className="sub-input" type="date"
                  value={expiryDate} disabled={neverExpires}
                  onChange={e => setExpiryDate(e.target.value)}
                />
                <label className="mt-conn-never">
                  <input type="checkbox" checked={neverExpires}
                    onChange={e => { setNeverExpires(e.target.checked); if (e.target.checked) setExpiryDate('') }}
                  />{' '}Never expires
                </label>
              </div>
              {row.plugin.id === 'jira' && (
                <div className="sub-form-row">
                  <label>Username / Email <span style={{ color: '#c9190b' }}>*</span></label>
                  <input
                    className="sub-input" type="text" placeholder="user@company.com"
                    value={username}
                    onChange={e => { setUsername(e.target.value); setTestPassed(false); setTestResult(null) }}
                  />
                </div>
              )}
              {row.plugin.id === 'codecov' && (
                <>
                  <div className="sub-form-row">
                    <label>Organization <span style={{ color: '#c9190b' }}>*</span></label>
                    <input className="sub-input" type="text" value={organization}
                      onChange={e => { setOrganization(e.target.value); setTestPassed(false); setTestResult(null) }} />
                  </div>
                  <div className="sub-form-row">
                    <label>Repository service</label>
                    <select className="sub-input" value={service}
                      onChange={e => { setService(e.target.value); setTestPassed(false); setTestResult(null) }}>
                      <option value="github">GitHub</option>
                      <option value="gitlab">GitLab</option>
                      <option value="github_enterprise">GitHub Enterprise</option>
                      <option value="gitlab_enterprise">GitLab Enterprise</option>
                    </select>
                  </div>
                </>
              )}
              <div style={{ marginTop: 16 }}>
                <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  <button className="sub-btn sub-btn--small" disabled={testing || !token.trim()} onClick={() => void handleTest()}>
                    {testing ? 'Testing...' : 'Test Connection'}
                  </button>
                  {testResult && (
                    <span className={`mt-conn-test-badge mt-conn-test-badge--${testResult.success ? 'ok' : 'fail'}`}>
                      {testResult.success ? 'Connection OK' : testResult.message}
                    </span>
                  )}
                </div>
              </div>
            </>
          )}

          {step === 1 && (
            <div className="mt-wizard-summary">
              <strong>Summary</strong><br />
              Scope: <strong>{scopeLabel}</strong><br />
              Plugin: <strong>{row.plugin.label}</strong><br />
              {row.connId && <>Connection: #{row.connId}<br /></>}
              {row.plugin.id === 'testregistry' && <>CI Tool: <strong>{row.ciTool}</strong><br />{testRegistryOrgLabel(row.ciTool)}: <strong>{row.organization}</strong><br /></>}
              {row.plugin.id === 'codecov' && <>Organization: <strong>{organization}</strong><br />Service: <strong>{service}</strong><br /></>}
              Expiry: {neverExpires ? 'Never' : expiryDate || '—'}<br />
              Test: <span className={`mt-conn-test-badge mt-conn-test-badge--ok`}>Passed</span>
            </div>
          )}
        </div>

        <div className="mt-wizard-footer">
          <div>
            {step > 0 && <button className="sub-btn sub-btn--secondary" onClick={() => { setStep(0); setError(null) }}>Back</button>}
          </div>
          <div className="mt-wizard-footer-right">
            <button className="sub-btn sub-btn--secondary" onClick={onCancel}>Cancel</button>
            {step === 0 ? (
              <button className="sub-btn sub-btn--primary" disabled={!stepValid} onClick={() => { const err = validate(); if (err) { setError(err); return }; setError(null); setStep(1) }}>Next</button>
            ) : (
              <button className="sub-btn sub-btn--primary" disabled={!testPassed || saving} onClick={() => void handleSave()}>
                {saving ? 'Updating...' : 'Update'}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
    </Portal>
  )
}
