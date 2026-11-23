import { useState, useEffect, useCallback } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'
import { entityIDError } from '../entityId'

interface Org {
  id: string
  name: string
}

interface Product {
  id: string
  name: string
  contact_email?: string
  auth_group?: string
  org_id?: string
}

interface TeamLink {
  id: string
  sort_order: number
}

function TeamLinksModal({
  product,
  closeModal,
  toast,
}: {
  product: Product
  closeModal: () => void
  toast: (msg: string, type: 'success' | 'error') => void
}) {
  const [links, setLinks] = useState<TeamLink[]>([])
  const [loading, setLoading] = useState(true)
  const [newId, setNewId] = useState('')
  const [newSort, setNewSort] = useState(0)

  useEffect(() => {
    adminApi<TeamLink[]>('GET', `/api/v1/products/${product.id}/teams`)
      .then(setLinks)
      .catch(e => {
        if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
      })
      .finally(() => setLoading(false))
  }, [product.id, toast])

  const save = async () => {
    try {
      await adminApi('PUT', `/api/v1/products/${product.id}/teams`, links)
      toast('Team links saved', 'success')
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
      <h3 className="adm-modal-title">Team Links: {product.name}</h3>
      {loading ? (
        <p>Loading...</p>
      ) : (
        <>
          <table className="adm-table">
            <thead>
              <tr><th>Team ID</th><th>Sort Order</th><th></th></tr>
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
            <input placeholder="Team ID" value={newId} onChange={e => setNewId(e.target.value)} />
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

export default function ProductsPanel() {
  const { toast, openModal, closeModal, confirm, navigateTo } = useAdmin()
  const [items, setItems] = useState<Product[]>([])
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      const data = await adminApi<Product[]>('GET', '/api/v1/products')
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
    let orgId = ''
    const Form = () => {
      const [fId, setFId] = useState(id)
      const [fName, setFName] = useState(name)
      const [fEmail, setFEmail] = useState(contactEmail)
      const [fAuthGroup, setFAuthGroup] = useState(authGroup)
      const [fOrgId, setFOrgId] = useState(orgId)
      const [orgList, setOrgList] = useState<Org[]>([])
      id = fId; name = fName; contactEmail = fEmail; authGroup = fAuthGroup; orgId = fOrgId
      const idError = entityIDError(fId, items.map(product => product.id), 'ID')
      useEffect(() => {
        adminApi<Org[]>('GET', '/api/v1/orgs').then(setOrgList).catch(() => {})
      }, [])
      return (
        <div>
          <h3 className="adm-modal-title">Add Product</h3>
          <label>Org *
            <select value={fOrgId} onChange={e => setFOrgId(e.target.value)}>
              <option value="">-- Select Org --</option>
              {orgList.map(o => <option key={o.id} value={o.id}>{o.name}</option>)}
            </select>
          </label>
          <label>ID<input value={fId} onChange={e => setFId(e.target.value)} autoFocus /></label>
          {fId.trim() && idError && <p className="error-text">{idError}</p>}
          <label>Name<input value={fName} onChange={e => setFName(e.target.value)} /></label>
          <label>Contact Email<input value={fEmail} onChange={e => setFEmail(e.target.value)} /></label>
          <label>Auth Group<input value={fAuthGroup} onChange={e => setFAuthGroup(e.target.value)} /></label>
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
            <button className="adm-btn adm-btn--primary" disabled={idError !== null || !orgId} onClick={async () => {
              const normalizedID = id.trim()
              const currentIDError = entityIDError(normalizedID, items.map(product => product.id), 'ID')
              if (currentIDError) { toast(currentIDError, 'error'); return }
              if (!orgId) { toast('Org is required', 'error'); return }
              try {
                await adminApi('POST', '/api/v1/products', { id: normalizedID, name, contact_email: contactEmail || undefined, auth_group: authGroup || undefined })
                const existing = await adminApi<Array<{ id: string; sort_order: number }>>('GET', `/api/v1/orgs/${orgId}/products`)
                await adminApi('PUT', `/api/v1/orgs/${orgId}/products`, [...existing, { id: normalizedID, sort_order: existing.length }])
                toast('Product created', 'success'); closeModal(); void load()
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

  const openEdit = (p: Product) => {
    let name = p.name
    let contactEmail = p.contact_email || ''
    let authGroup = p.auth_group || ''
    const Form = () => {
      const [fName, setFName] = useState(name)
      const [fEmail, setFEmail] = useState(contactEmail)
      const [fAuthGroup, setFAuthGroup] = useState(authGroup)
      name = fName; contactEmail = fEmail; authGroup = fAuthGroup
      return (
        <div>
          <h3 className="adm-modal-title">Edit Product: {p.id}</h3>
          <label>ID<input value={p.id} disabled /></label>
          <label>Name<input value={fName} onChange={e => setFName(e.target.value)} autoFocus /></label>
          <label>Contact Email<input value={fEmail} onChange={e => setFEmail(e.target.value)} /></label>
          <label>Auth Group<input value={fAuthGroup} onChange={e => setFAuthGroup(e.target.value)} /></label>
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
            <button className="adm-btn adm-btn--primary" onClick={async () => {
              try {
                await adminApi('PUT', `/api/v1/products/${p.id}`, { name, contact_email: contactEmail || undefined, auth_group: authGroup || undefined })
                toast('Product updated', 'success'); closeModal(); void load()
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

  const handleDelete = (p: Product) => {
    confirm(`Delete product "${p.name}"?`, async () => {
      try {
        await adminApi('DELETE', `/api/v1/products/${p.id}`)
        toast('Product deleted', 'success'); void load()
      } catch (e) {
        if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
      }
    })
  }

  if (loading) return <div className="adm-loader"><div className="diagram-card__spinner" /></div>

  return (
    <div>
      <div className="adm-toolbar">
        <button className="adm-btn adm-btn--primary" onClick={openAdd}>+ Add Product</button>
      </div>
      {items.length === 0 ? (
        <div className="adm-empty">No products found.</div>
      ) : (
        <table className="adm-table">
          <thead>
            <tr><th>ID</th><th>Name</th><th>Email</th><th>Auth Group</th><th>Actions</th></tr>
          </thead>
          <tbody>
            {items.map(p => (
              <tr key={p.id}>
                <td>{p.id}</td>
                <td>{p.name}</td>
                <td>{p.contact_email || ''}</td>
                <td>{p.auth_group || ''}</td>
                <td>
                  <div className="adm-actions">
                    <button className="adm-icon-btn" title="Team Links" onClick={() => openModal(<TeamLinksModal product={p} closeModal={closeModal} toast={toast} />)}>&#128279;</button>
                    <span className="adm-act-sep" />
                    <button className="adm-icon-btn" title="Dashboards" onClick={() => navigateTo('dashboards', { ownerType: 'product', ownerId: p.id })}>&#128202;</button>
                    <button className="adm-icon-btn" title="Config" onClick={() => navigateTo('config', { entityType: 'product', entityId: p.id })}>&#9881;&#65039;</button>
                    <span className="adm-act-sep" />
                    <button className="adm-icon-btn" title="Edit" onClick={() => openEdit(p)}>&#9998;&#65039;</button>
                    <button className="adm-icon-btn" title="Delete" onClick={() => handleDelete(p)}>&#128465;&#65039;</button>
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
