import { useState, useEffect, useCallback } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'

interface CannedScript {
  id: number
  name: string
  description?: string
  category?: string
  js_text: string
  version: number
  updated_at?: string
}

function ScriptForm({
  item,
  onSave,
  onCancel,
}: {
  item?: CannedScript
  onSave: () => void
  onCancel: () => void
}) {
  const { toast } = useAdmin()
  const [name, setName] = useState(item?.name || '')
  const [description, setDescription] = useState(item?.description || '')
  const [category, setCategory] = useState(item?.category || '')
  const [jsText, setJsText] = useState(item?.js_text || '')
  const [version, setVersion] = useState(item?.version ?? 1)
  const [saving, setSaving] = useState(false)

  const handleSubmit = async () => {
    setSaving(true)
    try {
      const body = {
        name,
        description: description || undefined,
        category: category || undefined,
        js_text: jsText,
        version,
      }
      if (item) {
        await adminApi('PUT', `/api/v1/canned-scripts/${item.id}`, body)
        toast('Script updated', 'success')
      } else {
        await adminApi('POST', '/api/v1/canned-scripts', body)
        toast('Script created', 'success')
      }
      onSave()
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Save failed', 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      <h3 className="adm-modal-title">{item ? 'Edit Script' : 'Add Script'}</h3>
      <label className="adm-label">Name</label>
      <input className="adm-input" type="text" value={name} onChange={e => setName(e.target.value)} />
      <label className="adm-label">Description</label>
      <textarea className="adm-input" value={description} onChange={e => setDescription(e.target.value)} rows={3} />
      <label className="adm-label">Category</label>
      <input className="adm-input" type="text" value={category} onChange={e => setCategory(e.target.value)} />
      <label className="adm-label">JavaScript</label>
      <textarea
        className="adm-input adm-json-textarea"
        value={jsText}
        onChange={e => setJsText(e.target.value)}
        style={{ height: 250 }}
      />
      <label className="adm-label">Version</label>
      <input className="adm-input" type="number" value={version} onChange={e => setVersion(Number(e.target.value))} />
      <div className="adm-modal-actions">
        <button className="adm-btn adm-btn--ghost" onClick={onCancel}>Cancel</button>
        <button className="adm-btn adm-btn--primary" disabled={saving} onClick={() => void handleSubmit()}>
          {saving ? 'Saving...' : item ? 'Update' : 'Create'}
        </button>
      </div>
    </div>
  )
}

export default function ScriptsPanel() {
  const { toast, openModal, closeModal, confirm } = useAdmin()
  const [items, setItems] = useState<CannedScript[]>([])

  const load = useCallback(async () => {
    try {
      const data = await adminApi<CannedScript[]>('GET', '/api/v1/canned-scripts')
      setItems(data)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to load scripts', 'error')
    }
  }, [toast])

  useEffect(() => { void load() }, [load])

  const showForm = (existing?: CannedScript) => {
    openModal(
      <ScriptForm
        item={existing}
        onSave={() => { closeModal(); void load() }}
        onCancel={closeModal}
      />,
    )
  }

  const handleEdit = async (id: number) => {
    try {
      const item = await adminApi<CannedScript>('GET', `/api/v1/canned-scripts/${id}`)
      showForm(item)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to load script', 'error')
    }
  }

  const handleDelete = (id: number) => {
    confirm('Delete this script?', async () => {
      try {
        await adminApi('DELETE', `/api/v1/canned-scripts/${id}`)
        toast('Script deleted', 'success')
        void load()
      } catch (e) {
        if (e instanceof AuthError) return
        toast(e instanceof Error ? e.message : 'Delete failed', 'error')
      }
    })
  }

  return (
    <div>
      <div className="adm-toolbar">
        <h2>JS Scripts</h2>
        <button className="adm-btn adm-btn--primary" onClick={() => showForm()}>+ Add</button>
      </div>
      {items.length === 0 ? (
        <div className="adm-empty">No scripts found.</div>
      ) : (
        <table className="adm-table">
          <thead>
            <tr>
              <th>#</th>
              <th>Name</th>
              <th>Category</th>
              <th>Version</th>
              <th>Updated</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {items.map(s => (
              <tr key={s.id}>
                <td>{s.id}</td>
                <td>{s.name}</td>
                <td>{s.category || '—'}</td>
                <td>{s.version}</td>
                <td>{s.updated_at || '—'}</td>
                <td className="adm-actions">
                  <button className="adm-icon-btn" title="Edit" onClick={() => void handleEdit(s.id)}>&#9998;</button>
                  <button className="adm-icon-btn danger" title="Delete" onClick={() => handleDelete(s.id)}>&#128465;</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
