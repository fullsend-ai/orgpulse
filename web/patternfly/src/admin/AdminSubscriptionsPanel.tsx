import { useState, useEffect, useCallback } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'

interface AdminSubscription {
  id: number
  email: string
  product_id?: string
  team_id?: string
  ai_analytics_url?: string
  interval_days?: number
  elements?: unknown[]
  last_run_at?: string
}

function SubscriptionForm({
  item,
  onSave,
  onCancel,
}: {
  item?: AdminSubscription
  onSave: () => void
  onCancel: () => void
}) {
  const { toast } = useAdmin()
  const [email, setEmail] = useState(item?.email || '')
  const [productId, setProductId] = useState(item?.product_id || '')
  const [teamId, setTeamId] = useState(item?.team_id || '')
  const [aiAnalyticsUrl, setAiAnalyticsUrl] = useState(item?.ai_analytics_url || '')
  const [intervalDays, setIntervalDays] = useState(item?.interval_days ?? 7)
  const [elements, setElements] = useState(
    item?.elements ? JSON.stringify(item.elements, null, 2) : '[]',
  )
  const [lastRun] = useState(item?.last_run_at || '')
  const [saving, setSaving] = useState(false)

  const handleSubmit = async () => {
    let parsedElements: unknown[]
    try { parsedElements = JSON.parse(elements) } catch { toast('Elements must be valid JSON', 'error'); return }
    setSaving(true)
    try {
      const body = {
        email,
        product_id: productId || undefined,
        team_id: teamId || undefined,
        ai_analytics_url: aiAnalyticsUrl || undefined,
        interval_days: intervalDays,
        elements: parsedElements,
      }
      if (item) {
        await adminApi('PUT', `/api/v1/subscriptions/${item.id}`, body)
        toast('Subscription updated', 'success')
      } else {
        await adminApi('POST', '/api/v1/subscriptions', body)
        toast('Subscription created', 'success')
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
      <h3 className="adm-modal-title">{item ? 'Edit Subscription' : 'Add Subscription'}</h3>
      <label>Email</label>
      <input className="adm-input" type="text" value={email} onChange={e => setEmail(e.target.value)} />
      <label>Product ID</label>
      <input className="adm-input" type="text" value={productId} onChange={e => setProductId(e.target.value)} />
      <label>Team ID</label>
      <input className="adm-input" type="text" value={teamId} onChange={e => setTeamId(e.target.value)} />
      <label>AI Analytics URL</label>
      <input className="adm-input" type="text" value={aiAnalyticsUrl} onChange={e => setAiAnalyticsUrl(e.target.value)} />
      <label>Interval Days</label>
      <input className="adm-input" type="number" value={intervalDays} onChange={e => setIntervalDays(Number(e.target.value))} />
      <label>Elements (JSON array)</label>
      <textarea className="adm-json-textarea" style={{ minHeight: 200 }} value={elements} onChange={e => setElements(e.target.value)} />
      {item && (
        <>
          <label>Last Run</label>
          <input className="adm-input" type="text" value={lastRun} disabled />
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

export default function AdminSubscriptionsPanel() {
  const { toast, openModal, closeModal, confirm } = useAdmin()
  const [items, setItems] = useState<AdminSubscription[]>([])

  const load = useCallback(async () => {
    try {
      const data = await adminApi<AdminSubscription[]>('GET', '/api/v1/subscriptions')
      setItems(data)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to load subscriptions', 'error')
    }
  }, [toast])

  useEffect(() => { void load() }, [load])

  const showForm = (existing?: AdminSubscription) => {
    openModal(
      <SubscriptionForm
        item={existing}
        onSave={() => { closeModal(); void load() }}
        onCancel={closeModal}
      />,
    )
  }

  const handleEdit = async (id: number) => {
    try {
      const item = await adminApi<AdminSubscription>('GET', `/api/v1/subscriptions/${id}`)
      showForm(item)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to load subscription', 'error')
    }
  }

  const handleDelete = (id: number) => {
    confirm('Delete this subscription?', async () => {
      try {
        await adminApi('DELETE', `/api/v1/subscriptions/${id}`)
        toast('Subscription deleted', 'success')
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
        <h2>Subscriptions</h2>
        <button className="adm-btn adm-btn--primary" onClick={() => showForm()}>+ Add</button>
      </div>
      {items.length === 0 ? (
        <div className="adm-empty">No subscriptions found.</div>
      ) : (
        <table className="adm-table">
          <thead>
            <tr>
              <th>#</th><th>Email</th><th>Product</th><th>Team</th>
              <th>AI Analytics</th><th>Interval</th><th>Last Run</th><th>Elements</th><th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {items.map(s => (
              <tr key={s.id}>
                <td>{s.id}</td>
                <td>{s.email}</td>
                <td>{s.product_id || '—'}</td>
                <td>{s.team_id || '—'}</td>
                <td className="adm-truncate">{s.ai_analytics_url || '—'}</td>
                <td>{s.interval_days != null ? `${s.interval_days}d` : '—'}</td>
                <td>{s.last_run_at || '—'}</td>
                <td>{Array.isArray(s.elements) ? s.elements.length : 0}</td>
                <td>
                  <div className="adm-actions">
                    <button className="adm-icon-btn" title="Edit" onClick={() => void handleEdit(s.id)}>&#9998;</button>
                    <button className="adm-icon-btn adm-icon-btn--danger" title="Delete" onClick={() => handleDelete(s.id)}>&#128465;</button>
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
