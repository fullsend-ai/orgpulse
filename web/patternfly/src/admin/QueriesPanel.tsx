import { useState, useEffect, useCallback } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'

interface CannedQuery {
  id: number
  name: string
  description?: string
  category?: string
  sql_text: string
  parameters?: unknown[]
  version: number
  updated_at?: string
}

function QueryForm({
  item,
  onSave,
  onCancel,
}: {
  item?: CannedQuery
  onSave: () => void
  onCancel: () => void
}) {
  const { toast } = useAdmin()
  const [name, setName] = useState(item?.name || '')
  const [description, setDescription] = useState(item?.description || '')
  const [category, setCategory] = useState(item?.category || '')
  const [sqlText, setSqlText] = useState(item?.sql_text || '')
  const [parameters, setParameters] = useState(
    item?.parameters ? JSON.stringify(item.parameters, null, 2) : '[]',
  )
  const [version, setVersion] = useState(item?.version ?? 1)
  const [saving, setSaving] = useState(false)

  const handleSubmit = async () => {
    let parsedParams: unknown[]
    try {
      parsedParams = JSON.parse(parameters)
    } catch {
      toast('Parameters must be valid JSON', 'error')
      return
    }
    setSaving(true)
    try {
      const body = {
        name,
        description: description || undefined,
        category: category || undefined,
        sql_text: sqlText,
        parameters: parsedParams,
        version,
      }
      if (item) {
        await adminApi('PUT', `/api/v1/canned-queries/${item.id}`, body)
        toast('Query updated', 'success')
      } else {
        await adminApi('POST', '/api/v1/canned-queries', body)
        toast('Query created', 'success')
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
      <h3 className="adm-modal-title">{item ? 'Edit Query' : 'Add Query'}</h3>
      <label className="adm-label">Name</label>
      <input className="adm-input" type="text" value={name} onChange={e => setName(e.target.value)} />
      <label className="adm-label">Description</label>
      <textarea className="adm-input" value={description} onChange={e => setDescription(e.target.value)} rows={3} />
      <label className="adm-label">Category</label>
      <input className="adm-input" type="text" value={category} onChange={e => setCategory(e.target.value)} />
      <label className="adm-label">SQL Statement</label>
      <textarea
        className="adm-input adm-json-textarea"
        value={sqlText}
        onChange={e => setSqlText(e.target.value)}
        style={{ height: 200 }}
      />
      <label className="adm-label">Parameters (JSON array)</label>
      <textarea
        className="adm-input adm-json-textarea"
        value={parameters}
        onChange={e => setParameters(e.target.value)}
        style={{ height: 120 }}
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

export default function QueriesPanel() {
  const { toast, openModal, closeModal, confirm } = useAdmin()
  const [items, setItems] = useState<CannedQuery[]>([])

  const load = useCallback(async () => {
    try {
      const data = await adminApi<CannedQuery[]>('GET', '/api/v1/canned-queries')
      setItems(data)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to load queries', 'error')
    }
  }, [toast])

  useEffect(() => { void load() }, [load])

  const showForm = (existing?: CannedQuery) => {
    openModal(
      <QueryForm
        item={existing}
        onSave={() => { closeModal(); void load() }}
        onCancel={closeModal}
      />,
    )
  }

  const handleEdit = async (id: number) => {
    try {
      const item = await adminApi<CannedQuery>('GET', `/api/v1/canned-queries/${id}`)
      showForm(item)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to load query', 'error')
    }
  }

  const handleDelete = (id: number) => {
    confirm('Delete this query?', async () => {
      try {
        await adminApi('DELETE', `/api/v1/canned-queries/${id}`)
        toast('Query deleted', 'success')
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
        <h2>SQL Queries</h2>
        <button className="adm-btn adm-btn--primary" onClick={() => showForm()}>+ Add</button>
      </div>
      {items.length === 0 ? (
        <div className="adm-empty">No queries found.</div>
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
            {items.map(q => (
              <tr key={q.id}>
                <td>{q.id}</td>
                <td>{q.name}</td>
                <td>{q.category || '—'}</td>
                <td>{q.version}</td>
                <td>{q.updated_at || '—'}</td>
                <td className="adm-actions">
                  <button className="adm-icon-btn" title="Edit" onClick={() => void handleEdit(q.id)}>&#9998;</button>
                  <button className="adm-icon-btn danger" title="Delete" onClick={() => handleDelete(q.id)}>&#128465;</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
