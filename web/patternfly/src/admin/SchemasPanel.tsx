import { useState, useEffect, useCallback } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'

interface JsonSchema {
  id: number
  name: string
  description?: string
  json_schema: unknown
  version: number
  updated_at?: string
}

function SchemaForm({
  item,
  onSave,
  onCancel,
}: {
  item?: JsonSchema
  onSave: () => void
  onCancel: () => void
}) {
  const { toast } = useAdmin()
  const [name, setName] = useState(item?.name || '')
  const [description, setDescription] = useState(item?.description || '')
  const [schemaText, setSchemaText] = useState(() => {
    if (!item) return ''
    if (typeof item.json_schema === 'object') {
      return JSON.stringify(item.json_schema, null, 2)
    }
    return String(item.json_schema || '')
  })
  const [saving, setSaving] = useState(false)

  const handleSubmit = async () => {
    try {
      JSON.parse(schemaText)
    } catch {
      toast('JSON Schema must be valid JSON', 'error')
      return
    }
    setSaving(true)
    try {
      const body = {
        name,
        description: description || undefined,
        json_schema: JSON.parse(schemaText),
      }
      if (item) {
        await adminApi('PUT', `/api/v1/json-schemas/${item.id}`, body)
        toast('Schema updated', 'success')
      } else {
        await adminApi('POST', '/api/v1/json-schemas', body)
        toast('Schema created', 'success')
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
      <h3 className="adm-modal-title">{item ? 'Edit Schema' : 'Add Schema'}</h3>
      <label className="adm-label">Name</label>
      <input className="adm-input" type="text" value={name} onChange={e => setName(e.target.value)} />
      <label className="adm-label">Description</label>
      <textarea className="adm-input" value={description} onChange={e => setDescription(e.target.value)} rows={3} />
      <label className="adm-label">JSON Schema</label>
      <textarea
        className="adm-input adm-json-textarea"
        value={schemaText}
        onChange={e => setSchemaText(e.target.value)}
        style={{ height: 300 }}
      />
      {item && (
        <>
          <label className="adm-label">Version</label>
          <input className="adm-input" type="text" value={item.version} disabled />
        </>
      )}
      <div className="adm-modal-actions">
        <button className="adm-btn adm-btn--ghost" onClick={onCancel}>Cancel</button>
        <button className="adm-btn adm-btn--primary" disabled={saving} onClick={() => void handleSubmit()}>
          {saving ? 'Saving...' : item ? 'Update' : 'Create'}
        </button>
      </div>
    </div>
  )
}

export default function SchemasPanel() {
  const { toast, openModal, closeModal, confirm } = useAdmin()
  const [items, setItems] = useState<JsonSchema[]>([])

  const load = useCallback(async () => {
    try {
      const data = await adminApi<JsonSchema[]>('GET', '/api/v1/json-schemas')
      setItems(data)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to load schemas', 'error')
    }
  }, [toast])

  useEffect(() => { void load() }, [load])

  const showForm = (existing?: JsonSchema) => {
    openModal(
      <SchemaForm
        item={existing}
        onSave={() => { closeModal(); void load() }}
        onCancel={closeModal}
      />,
    )
  }

  const handleEdit = async (id: number) => {
    try {
      const item = await adminApi<JsonSchema>('GET', `/api/v1/json-schemas/${id}`)
      showForm(item)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to load schema', 'error')
    }
  }

  const handleDelete = (id: number) => {
    confirm('Delete this schema?', async () => {
      try {
        await adminApi('DELETE', `/api/v1/json-schemas/${id}`)
        toast('Schema deleted', 'success')
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
        <h2>JSON Schemas</h2>
        <button className="adm-btn adm-btn--primary" onClick={() => showForm()}>+ Add</button>
      </div>
      {items.length === 0 ? (
        <div className="adm-empty">No schemas found.</div>
      ) : (
        <table className="adm-table">
          <thead>
            <tr>
              <th>#</th>
              <th>Name</th>
              <th>Description</th>
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
                <td className="adm-truncate">{s.description || '—'}</td>
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
