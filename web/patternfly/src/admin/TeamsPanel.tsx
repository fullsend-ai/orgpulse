import { useState, useEffect, useCallback, lazy, Suspense } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'
import { entityIDError } from '../entityId'

const ImportTeamModal = lazy(() => import('./ImportTeamModal'))

interface AdminTeam {
  id: string
  name: string
  contact_email?: string
  auth_group?: string
  fetch_url?: string
  fetch_check_url?: string
}

export default function TeamsPanel() {
  const { toast, openModal, closeModal, confirm, navigateTo } = useAdmin()
  const [items, setItems] = useState<AdminTeam[]>([])
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      const data = await adminApi<AdminTeam[]>('GET', '/api/v1/teams')
      setItems(data)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed', 'error')
    } finally {
      setLoading(false)
    }
  }, [toast])

  useEffect(() => { void load() }, [load])

  const openAdd = () => {
    let id = ''
    let name = ''
    let contactEmail = ''
    let authGroup = ''
    let fetchUrl = ''
    let fetchCheckUrl = ''
    const Form = () => {
      const [fId, setFId] = useState(id)
      const [fName, setFName] = useState(name)
      const [fEmail, setFEmail] = useState(contactEmail)
      const [fAuthGroup, setFAuthGroup] = useState(authGroup)
      const [fFetchUrl, setFFetchUrl] = useState(fetchUrl)
      const [fFetchCheckUrl, setFFetchCheckUrl] = useState(fetchCheckUrl)
      id = fId; name = fName; contactEmail = fEmail; authGroup = fAuthGroup; fetchUrl = fFetchUrl; fetchCheckUrl = fFetchCheckUrl
      const idError = entityIDError(fId, items.map(team => team.id), 'ID')
      return (
        <div>
          <h3 className="adm-modal-title">Add Team</h3>
          <label>ID<input value={fId} onChange={e => setFId(e.target.value)} autoFocus /></label>
          {fId.trim() && idError && <p className="error-text">{idError}</p>}
          <label>Name<input value={fName} onChange={e => setFName(e.target.value)} /></label>
          <label>Contact Email<input value={fEmail} onChange={e => setFEmail(e.target.value)} /></label>
          <label>Auth Group <span style={{ color: '#c9190b' }}>*</span><input value={fAuthGroup} onChange={e => setFAuthGroup(e.target.value)} required /></label>
          <label>Fetch URL<input value={fFetchUrl} onChange={e => setFFetchUrl(e.target.value)} /></label>
          <label>Fetch Check URL<input value={fFetchCheckUrl} onChange={e => setFFetchCheckUrl(e.target.value)} /></label>
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
            <button className="adm-btn adm-btn--primary" disabled={idError !== null} onClick={async () => {
              const normalizedID = id.trim()
              const currentIDError = entityIDError(normalizedID, items.map(team => team.id), 'ID')
              if (currentIDError) { toast(currentIDError, 'error'); return }
              if (!authGroup.trim()) { toast('Auth Group is required.', 'error'); return }
              try {
                await adminApi('POST', '/api/v1/teams', {
                  id: normalizedID, name,
                  contact_email: contactEmail || undefined,
                  auth_group: authGroup.trim(),
                  fetch_url: fetchUrl || undefined,
                  fetch_check_url: fetchCheckUrl || undefined,
                })
                toast('Team created', 'success'); closeModal(); void load()
              } catch (e) {
                if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
              }
            }}>Create</button>
          </div>
        </div>
      )
    }
    openModal(<Form />)
  }

  const openEdit = (t: AdminTeam) => {
    let name = t.name
    let contactEmail = t.contact_email || ''
    let authGroup = t.auth_group || ''
    let fetchUrl = t.fetch_url || ''
    let fetchCheckUrl = t.fetch_check_url || ''
    const Form = () => {
      const [fName, setFName] = useState(name)
      const [fEmail, setFEmail] = useState(contactEmail)
      const [fAuthGroup, setFAuthGroup] = useState(authGroup)
      const [fFetchUrl, setFFetchUrl] = useState(fetchUrl)
      const [fFetchCheckUrl, setFFetchCheckUrl] = useState(fetchCheckUrl)
      name = fName; contactEmail = fEmail; authGroup = fAuthGroup; fetchUrl = fFetchUrl; fetchCheckUrl = fFetchCheckUrl
      return (
        <div>
          <h3 className="adm-modal-title">Edit Team: {t.id}</h3>
          <label>Name<input value={fName} onChange={e => setFName(e.target.value)} autoFocus /></label>
          <label>Contact Email<input value={fEmail} onChange={e => setFEmail(e.target.value)} /></label>
          <label>Auth Group <span style={{ color: '#c9190b' }}>*</span><input value={fAuthGroup} onChange={e => setFAuthGroup(e.target.value)} required /></label>
          <label>Fetch URL<input value={fFetchUrl} onChange={e => setFFetchUrl(e.target.value)} /></label>
          <label>Fetch Check URL<input value={fFetchCheckUrl} onChange={e => setFFetchCheckUrl(e.target.value)} /></label>
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
            <button className="adm-btn adm-btn--primary" onClick={async () => {
              if (!authGroup.trim()) { toast('Auth Group is required.', 'error'); return }
              try {
                await adminApi('PUT', `/api/v1/teams/${t.id}`, {
                  name,
                  contact_email: contactEmail || undefined,
                  auth_group: authGroup.trim(),
                  fetch_url: fetchUrl || undefined,
                  fetch_check_url: fetchCheckUrl || undefined,
                })
                toast('Team updated', 'success'); closeModal(); void load()
              } catch (e) {
                if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
              }
            }}>Save</button>
          </div>
        </div>
      )
    }
    openModal(<Form />)
  }

  const handleDelete = (t: AdminTeam) => {
    confirm(`Delete team "${t.name}"?`, async () => {
      try {
        await adminApi('DELETE', `/api/v1/teams/${t.id}`)
        toast('Team deleted', 'success'); void load()
      } catch (e) {
        if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
      }
    })
  }

  const handleTrigger = (t: AdminTeam) => {
    confirm(`Trigger DevLake pipeline run for all blueprints of team "${t.name}"?`, async () => {
      try {
        const result = await adminApi<{ triggered: string[]; errors: string[] }>(
          'POST',
          `/api/v1/devlake/teams/${t.id}/trigger`,
        )
        if (result.errors?.length) {
          toast(`Triggered ${result.triggered?.length ?? 0} blueprint(s), ${result.errors.length} error(s)`, 'error')
        } else {
          toast(`Triggered ${result.triggered?.length ?? 0} blueprint(s)`, 'success')
        }
      } catch (e) {
        if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Trigger failed', 'error')
      }
    })
  }

  const handleSync = async (t: AdminTeam) => {
    try {
      const result = await adminApi<{ created: string[]; updated: string[]; errors: string[] }>(
        'POST',
        `/api/v1/devlake/teams/${t.id}/sync`,
      )
      if (result.errors?.length) {
        toast(`Synced team with ${result.errors.length} error(s): ${result.errors.join('; ')}`, 'error')
      } else {
        toast(`Synced team: ${result.created?.length ?? 0} blueprint(s) created, ${result.updated?.length ?? 0} updated`, 'success')
      }
    } catch (e) {
      if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Sync failed', 'error')
    }
  }

  if (loading) return <div className="adm-loader"><div className="diagram-card__spinner" /></div>

  return (
    <div>
      <div className="adm-toolbar">
        <button className="adm-btn adm-btn--primary" onClick={openAdd}>+ Add Team</button>
        <button className="adm-btn adm-btn--ghost" onClick={() => openModal(
          <Suspense fallback={<div className="adm-loader"><div className="diagram-card__spinner" /></div>}>
            <ImportTeamModal onDone={load} />
          </Suspense>
        )}>Import JSON</button>
      </div>
      {items.length === 0 ? (
        <div className="adm-empty">No teams found.</div>
      ) : (
        <table className="adm-table">
          <thead>
            <tr><th>ID</th><th>Name</th><th>Email</th><th>Auth Group</th><th>Actions</th></tr>
          </thead>
          <tbody>
            {items.map(t => (
              <tr key={t.id}>
                <td>{t.id}</td>
                <td>{t.name}</td>
                <td>{t.contact_email || ''}</td>
                <td>{t.auth_group || ''}</td>
                <td>
                  <div className="adm-actions">
                    <button className="adm-icon-btn" title="Dashboards" onClick={() => navigateTo('dashboards', { ownerType: 'team', ownerId: t.id })}>&#128202;</button>
                    <button className="adm-icon-btn" title="Sources" onClick={() => navigateTo('sources', { teamId: t.id })}>&#128230;</button>
                    <button className="adm-icon-btn" title="Config" onClick={() => navigateTo('config', { entityType: 'team', entityId: t.id })}>&#9881;&#65039;</button>
                    <button className="adm-icon-btn" title="Sync DevLake Blueprints" onClick={() => void handleSync(t)}>&#8635;</button>
                    <button className="adm-icon-btn" title="Trigger DevLake Run" onClick={() => handleTrigger(t)}>&#9654;&#65039;</button>
                    <span className="adm-act-sep" />
                    <button className="adm-icon-btn" title="Edit" onClick={() => openEdit(t)}>&#9998;&#65039;</button>
                    <button className="adm-icon-btn" title="Delete" onClick={() => handleDelete(t)}>&#128465;&#65039;</button>
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
