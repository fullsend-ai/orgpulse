import { useState, useCallback, useEffect, useRef } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'

interface AdminSource {
  id: string
  type: string
  config?: Record<string, unknown>
  repos?: unknown[]
  projects?: unknown[]
}

export default function SourcesPanel() {
  const { toast, openModal, closeModal, confirm, consumeNavParams } = useAdmin()
  const [items, setItems] = useState<AdminSource[]>([])
  const [loading, setLoading] = useState(false)
  const [teamId, setTeamId] = useState('')
  const didInit = useRef(false)

  const load = useCallback(async (tid?: string) => {
    const id = tid || teamId
    if (!id.trim()) {
      toast('Enter a Team ID', 'error')
      return
    }
    setLoading(true)
    try {
      const data = await adminApi<AdminSource[]>('GET', `/api/v1/teams/${encodeURIComponent(id.trim())}/sources`)
      setItems(data)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed', 'error')
    } finally {
      setLoading(false)
    }
  }, [teamId, toast])

  useEffect(() => {
    if (didInit.current) return
    didInit.current = true
    const p = consumeNavParams()
    if (p.teamId) {
      setTeamId(p.teamId)
      void load(p.teamId)
    }
  }, [consumeNavParams, load])

  const openAdd = () => {
    let srcTeamId = teamId
    let sourceId = ''
    let type = 'gitrepos'
    let configJson = '{}'
    const Form = () => {
      const [fTeamId, setFTeamId] = useState(srcTeamId)
      const [fSourceId, setFSourceId] = useState(sourceId)
      const [fType, setFType] = useState(type)
      const [fConfig, setFConfig] = useState(configJson)
      srcTeamId = fTeamId
      sourceId = fSourceId
      type = fType
      configJson = fConfig
      return (
        <div>
          <h3 className="adm-modal-title">Add Source</h3>
          <label>Team ID</label>
          <input className="adm-input" value={fTeamId} onChange={e => setFTeamId(e.target.value)} />
          <label>Source ID</label>
          <input className="adm-input" value={fSourceId} onChange={e => setFSourceId(e.target.value)} autoFocus />
          <label>Type</label>
          <select className="adm-input" value={fType} onChange={e => setFType(e.target.value)}>
            <option value="gitrepos">gitrepos</option>
            <option value="workitems">workitems</option>
            <option value="testsuites">testsuites</option>
          </select>
          <label>Config (JSON)</label>
          <textarea
            className="adm-json-textarea"
            style={{ minHeight: 150 }}
            value={fConfig}
            onChange={e => setFConfig(e.target.value)}
          />
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
            <button
              className="adm-btn adm-btn--primary"
              onClick={async () => {
                let config: unknown
                try { config = JSON.parse(configJson) } catch { toast('Invalid config JSON', 'error'); return }
                try {
                  await adminApi('POST', `/api/v1/teams/${encodeURIComponent(srcTeamId)}/sources`, { id: sourceId, type, config })
                  toast('Source created', 'success')
                  closeModal()
                  void load()
                } catch (e) {
                  if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
                }
              }}
            >Create</button>
          </div>
        </div>
      )
    }
    openModal(<Form />)
  }

  const openEdit = async (s: AdminSource) => {
    let full: AdminSource
    try {
      full = await adminApi<AdminSource>('GET', `/api/v1/teams/${encodeURIComponent(teamId)}/sources/${s.id}`)
    } catch (e) {
      if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
      return
    }

    let configJson = JSON.stringify(full.config || {}, null, 2)
    let reposJson = JSON.stringify(full.repos || [], null, 2)
    let projectsJson = JSON.stringify(full.projects || [], null, 2)
    const srcType = full.type

    const Form = () => {
      const [fConfig, setFConfig] = useState(configJson)
      const [fRepos, setFRepos] = useState(reposJson)
      const [fProjects, setFProjects] = useState(projectsJson)
      configJson = fConfig
      reposJson = fRepos
      projectsJson = fProjects
      return (
        <div>
          <h3 className="adm-modal-title">Edit Source: {s.id}</h3>
          <label>Type</label>
          <input className="adm-input" value={srcType} disabled />
          <label>Config (JSON)</label>
          <textarea className="adm-json-textarea" style={{ minHeight: 150 }} value={fConfig} onChange={e => setFConfig(e.target.value)} />
          <label>Repos (JSON array)</label>
          <textarea className="adm-json-textarea" style={{ minHeight: 120 }} value={fRepos} onChange={e => setFRepos(e.target.value)} />
          <label>Projects (JSON array)</label>
          <textarea className="adm-json-textarea" style={{ minHeight: 120 }} value={fProjects} onChange={e => setFProjects(e.target.value)} />
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
            <button
              className="adm-btn adm-btn--primary"
              onClick={async () => {
                let config: unknown, repos: unknown[], projects: unknown[]
                try { config = JSON.parse(configJson) } catch { toast('Invalid config JSON', 'error'); return }
                try { repos = JSON.parse(reposJson) } catch { toast('Invalid repos JSON', 'error'); return }
                try { projects = JSON.parse(projectsJson) } catch { toast('Invalid projects JSON', 'error'); return }
                try {
                  await adminApi('PUT', `/api/v1/teams/${encodeURIComponent(teamId)}/sources/${s.id}`, { type: srcType, config, repos, projects })
                  toast('Source updated', 'success')
                  closeModal()
                  void load()
                } catch (e) {
                  if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
                }
              }}
            >Save</button>
          </div>
        </div>
      )
    }
    openModal(<Form />)
  }

  const handleDelete = (s: AdminSource) => {
    confirm(`Delete source "${s.id}"?`, async () => {
      try {
        await adminApi('DELETE', `/api/v1/teams/${encodeURIComponent(teamId)}/sources/${s.id}`)
        toast('Source deleted', 'success')
        void load()
      } catch (e) {
        if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
      }
    })
  }

  const itemCount = (s: AdminSource): number => (s.repos?.length || 0) + (s.projects?.length || 0)

  return (
    <div>
      <div className="adm-toolbar">
        <input className="adm-input" placeholder="Team ID" value={teamId} onChange={e => setTeamId(e.target.value)} onKeyDown={e => e.key === 'Enter' && load()} />
        <button className="adm-btn adm-btn--primary" onClick={() => load()}>Load</button>
        <button className="adm-btn adm-btn--primary" onClick={openAdd}>+ Add Source</button>
      </div>
      {loading ? (
        <div className="adm-loader"><div className="diagram-card__spinner" /></div>
      ) : items.length === 0 ? (
        <div className="adm-empty">No sources loaded. Enter a team ID and click Load.</div>
      ) : (
        <table className="adm-table">
          <thead>
            <tr><th>ID</th><th>Type</th><th>Items</th><th>Actions</th></tr>
          </thead>
          <tbody>
            {items.map(s => (
              <tr key={s.id}>
                <td>{s.id}</td>
                <td>{s.type}</td>
                <td>{itemCount(s)}</td>
                <td>
                  <div className="adm-actions">
                    <button className="adm-icon-btn" title="Edit" onClick={() => void openEdit(s)}>&#9998;</button>
                    <button className="adm-icon-btn adm-icon-btn--danger" title="Delete" onClick={() => handleDelete(s)}>&#128465;</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
