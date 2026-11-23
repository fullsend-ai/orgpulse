import { useState, useEffect, useCallback } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'
import { entityIDError } from '../entityId'

interface Org {
  id: string
  name: string
  auth_group?: string
  products?: string[]
}

interface TeamLink {
  id: string
  sort_order: number
}

function ProductLinksModal({
  org,
  closeModal,
  toast,
}: {
  org: Org
  closeModal: () => void
  toast: (msg: string, type: 'success' | 'error') => void
}) {
  const [links, setLinks] = useState<TeamLink[]>([])
  const [loading, setLoading] = useState(true)
  const [newId, setNewId] = useState('')
  const [newSort, setNewSort] = useState(0)

  useEffect(() => {
    adminApi<TeamLink[]>('GET', `/api/v1/orgs/${org.id}/products`)
      .then(setLinks)
      .catch(e => {
        if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
      })
      .finally(() => setLoading(false))
  }, [org.id, toast])

  const save = async () => {
    try {
      await adminApi('PUT', `/api/v1/orgs/${org.id}/products`, links)
      toast('Product links saved', 'success')
      closeModal()
    } catch (e) {
      if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
    }
  }

  const addLink = () => {
    if (!newId.trim()) return
    setLinks([...links, { id: newId.trim(), sort_order: newSort }])
    setNewId('')
    setNewSort(0)
  }

  return (
    <div>
      <h3 className="adm-modal-title">Product Links: {org.name}</h3>
      {loading ? (
        <p>Loading...</p>
      ) : (
        <>
          <table className="adm-table">
            <thead>
              <tr><th>Product ID</th><th>Sort Order</th><th></th></tr>
            </thead>
            <tbody>
              {links.map((link, i) => (
                <tr key={i}>
                  <td>{link.id}</td>
                  <td>
                    <input
                      type="number"
                      value={link.sort_order}
                      onChange={e => {
                        const updated = [...links]
                        updated[i] = { id: link.id, sort_order: Number(e.target.value) }
                        setLinks(updated)
                      }}
                      style={{ width: 60 }}
                    />
                  </td>
                  <td>
                    <button className="adm-btn adm-btn--danger adm-btn--sm" onClick={() => setLinks(links.filter((_, j) => j !== i))}>
                      Remove
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="adm-kv-row" style={{ marginTop: 8 }}>
            <input placeholder="Product ID" value={newId} onChange={e => setNewId(e.target.value)} />
            <input type="number" placeholder="Sort" value={newSort} onChange={e => setNewSort(Number(e.target.value))} style={{ width: 60 }} />
            <button className="adm-btn adm-btn--sm" onClick={addLink}>+ Add</button>
          </div>
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
            <button className="adm-btn adm-btn--primary" onClick={() => void save()}>Save</button>
          </div>
        </>
      )}
    </div>
  )
}

export default function OrgsPanel() {
  const { toast, openModal, closeModal, confirm, navigateTo } = useAdmin()
  const [items, setItems] = useState<Org[]>([])
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      const data = await adminApi<Org[]>('GET', '/api/v1/orgs')
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
    let authGroup = ''
    const Form = () => {
      const [fId, setFId] = useState(id)
      const [fName, setFName] = useState(name)
      const [fAuthGroup, setFAuthGroup] = useState(authGroup)
      id = fId; name = fName; authGroup = fAuthGroup
      const idError = entityIDError(fId, items.map(org => org.id), 'ID')
      return (
        <div>
          <h3 className="adm-modal-title">Add Org</h3>
          <label>ID<input value={fId} onChange={e => setFId(e.target.value)} autoFocus /></label>
          {fId.trim() && idError && <p className="error-text">{idError}</p>}
          <label>Name<input value={fName} onChange={e => setFName(e.target.value)} /></label>
          <label>Auth Group<input value={fAuthGroup} onChange={e => setFAuthGroup(e.target.value)} /></label>
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
            <button className="adm-btn adm-btn--primary" disabled={idError !== null} onClick={async () => {
              const normalizedID = id.trim()
              const currentIDError = entityIDError(normalizedID, items.map(org => org.id), 'ID')
              if (currentIDError) { toast(currentIDError, 'error'); return }
              try {
                await adminApi('POST', '/api/v1/orgs', { id: normalizedID, name, auth_group: authGroup || undefined })
                toast('Org created', 'success'); closeModal(); void load()
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

  const openEdit = (org: Org) => {
    let name = org.name
    let authGroup = org.auth_group || ''
    const Form = () => {
      const [fName, setFName] = useState(name)
      const [fAuthGroup, setFAuthGroup] = useState(authGroup)
      name = fName; authGroup = fAuthGroup
      return (
        <div>
          <h3 className="adm-modal-title">Edit Org: {org.id}</h3>
          <label>Name<input value={fName} onChange={e => setFName(e.target.value)} autoFocus /></label>
          <label>Auth Group<input value={fAuthGroup} onChange={e => setFAuthGroup(e.target.value)} /></label>
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
            <button className="adm-btn adm-btn--primary" onClick={async () => {
              try {
                await adminApi('PUT', `/api/v1/orgs/${org.id}`, { name, auth_group: authGroup || undefined })
                toast('Org updated', 'success'); closeModal(); void load()
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

  const handleDelete = (org: Org) => {
    confirm(`Delete org "${org.name}"?`, async () => {
      try {
        await adminApi('DELETE', `/api/v1/orgs/${org.id}`)
        toast('Org deleted', 'success'); void load()
      } catch (e) {
        if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
      }
    })
  }

  if (loading) return <div className="adm-loader"><div className="diagram-card__spinner" /></div>

  return (
    <div>
      <div className="adm-toolbar">
        <button className="adm-btn adm-btn--primary" onClick={openAdd}>+ Add Org</button>
      </div>
      {items.length === 0 ? (
        <div className="adm-empty">No organizations found.</div>
      ) : (
        <table className="adm-table">
          <thead>
            <tr><th>ID</th><th>Name</th><th>Auth Group</th><th>Actions</th></tr>
          </thead>
          <tbody>
            {items.map(o => (
              <tr key={o.id}>
                <td>{o.id}</td>
                <td>{o.name}</td>
                <td>{o.auth_group || ''}</td>
                <td>
                  <div className="adm-actions">
                    <button className="adm-icon-btn" title="Product Links" onClick={() => openModal(<ProductLinksModal org={o} closeModal={closeModal} toast={toast} />)}>&#128279;</button>
                    <span className="adm-act-sep" />
                    <button className="adm-icon-btn" title="Dashboards" onClick={() => navigateTo('dashboards', { ownerType: 'org', ownerId: o.id })}>&#128202;</button>
                    <button className="adm-icon-btn" title="Config" onClick={() => navigateTo('config', { entityType: 'org', entityId: o.id })}>&#9881;&#65039;</button>
                    <span className="adm-act-sep" />
                    <button className="adm-icon-btn" title="Edit" onClick={() => openEdit(o)}>&#9998;&#65039;</button>
                    <button className="adm-icon-btn" title="Delete" onClick={() => handleDelete(o)}>&#128465;&#65039;</button>
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
