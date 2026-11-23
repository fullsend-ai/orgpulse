import { useState, useEffect, useCallback } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'

interface AdminAlert {
  id: number
  email: string
  product_id?: string
  team_id?: string
  url?: string
  payload?: unknown
  ai_analytics_url?: string
  rule?: string
}

function AlertForm({
  item,
  onSave,
  onCancel,
}: {
  item?: AdminAlert
  onSave: () => void
  onCancel: () => void
}) {
  const { toast } = useAdmin()
  const [email, setEmail] = useState(item?.email || '')
  const [productId, setProductId] = useState(item?.product_id || '')
  const [teamId, setTeamId] = useState(item?.team_id || '')
  const [url, setUrl] = useState(item?.url || '')
  const [payload, setPayload] = useState(
    item?.payload ? JSON.stringify(item.payload, null, 2) : '',
  )
  const [aiAnalyticsUrl, setAiAnalyticsUrl] = useState(item?.ai_analytics_url || '')
  const [rule, setRule] = useState(item?.rule || 'data.atomicMetricsValue > 100')
  const [saving, setSaving] = useState(false)

  const handleSubmit = async () => {
    let parsedPayload: unknown = null
    if (payload.trim()) {
      try { parsedPayload = JSON.parse(payload) } catch { toast('Payload must be valid JSON', 'error'); return }
    }
    setSaving(true)
    try {
      const body = {
        email,
        product_id: productId || undefined,
        team_id: teamId || undefined,
        url: url || undefined,
        payload: parsedPayload,
        ai_analytics_url: aiAnalyticsUrl || undefined,
        rule,
      }
      if (item) {
        await adminApi('PUT', `/api/v1/alerts/${item.id}`, body)
        toast('Alert updated', 'success')
      } else {
        await adminApi('POST', '/api/v1/alerts', body)
        toast('Alert created', 'success')
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
      <h3 className="adm-modal-title">{item ? 'Edit Alert' : 'Add Alert'}</h3>
      <label>Email</label>
      <input className="adm-input" type="text" value={email} onChange={e => setEmail(e.target.value)} />
      <label>Product ID</label>
      <input className="adm-input" type="text" value={productId} onChange={e => setProductId(e.target.value)} />
      <label>Team ID</label>
      <input className="adm-input" type="text" value={teamId} onChange={e => setTeamId(e.target.value)} />
      <label>Metrics URL</label>
      <input className="adm-input" type="text" value={url} onChange={e => setUrl(e.target.value)} />
      <label>Payload (JSON)</label>
      <textarea className="adm-json-textarea" style={{ minHeight: 120 }} value={payload} onChange={e => setPayload(e.target.value)} />
      <label>AI Analytics URL</label>
      <input className="adm-input" type="text" value={aiAnalyticsUrl} onChange={e => setAiAnalyticsUrl(e.target.value)} />
      <label>Rule (JS expression)</label>
      <textarea className="adm-json-textarea" style={{ minHeight: 80 }} value={rule} onChange={e => setRule(e.target.value)} />
      <div className="adm-modal-actions">
        <button className="adm-btn adm-btn--ghost" onClick={onCancel}>Cancel</button>
        <button className="adm-btn adm-btn--primary" disabled={saving} onClick={() => void handleSubmit()}>
          {saving ? 'Saving...' : item ? 'Update' : 'Create'}
        </button>
      </div>
    </div>
  )
}

export default function AdminAlertsPanel() {
  const { toast, openModal, closeModal, confirm } = useAdmin()
  const [items, setItems] = useState<AdminAlert[]>([])

  const load = useCallback(async () => {
    try {
      const data = await adminApi<AdminAlert[]>('GET', '/api/v1/alerts')
      setItems(data)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to load alerts', 'error')
    }
  }, [toast])

  useEffect(() => { void load() }, [load])

  const showForm = (existing?: AdminAlert) => {
    openModal(
      <AlertForm
        item={existing}
        onSave={() => { closeModal(); void load() }}
        onCancel={closeModal}
      />,
    )
  }

  const handleEdit = async (id: number) => {
    try {
      const item = await adminApi<AdminAlert>('GET', `/api/v1/alerts/${id}`)
      showForm(item)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to load alert', 'error')
    }
  }

  const handleDelete = (id: number) => {
    confirm('Delete this alert?', async () => {
      try {
        await adminApi('DELETE', `/api/v1/alerts/${id}`)
        toast('Alert deleted', 'success')
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
        <h2>Alerts</h2>
        <button className="adm-btn adm-btn--primary" onClick={() => showForm()}>+ Add</button>
      </div>
      {items.length === 0 ? (
        <div className="adm-empty">No alerts found.</div>
      ) : (
        <table className="adm-table">
          <thead>
            <tr>
              <th>#</th><th>Email</th><th>Product</th><th>Team</th>
              <th>URL</th><th>Rule</th><th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {items.map(a => (
              <tr key={a.id}>
                <td>{a.id}</td>
                <td>{a.email}</td>
                <td>{a.product_id || '—'}</td>
                <td>{a.team_id || '—'}</td>
                <td className="adm-truncate">{a.url || '—'}</td>
                <td className="adm-truncate">{a.rule || '—'}</td>
                <td>
                  <div className="adm-actions">
                    <button className="adm-icon-btn" title="Edit" onClick={() => void handleEdit(a.id)}>&#9998;</button>
                    <button className="adm-icon-btn adm-icon-btn--danger" title="Delete" onClick={() => handleDelete(a.id)}>&#128465;</button>
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
