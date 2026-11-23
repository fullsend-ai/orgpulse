import { useState, useEffect, useCallback } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'
import { fetchConfig } from '../api'

interface ApiKey {
  id: number
  name: string
  key?: string
  scoped_user?: string
  created_at?: string
}

function CreateKeyForm({
  onSave,
  onCancel,
}: {
  onSave: (created: ApiKey) => void
  onCancel: () => void
}) {
  const { toast } = useAdmin()
  const [name, setName] = useState('')
  const [saving, setSaving] = useState(false)

  const handleSubmit = async () => {
    if (!name.trim()) {
      toast('Name is required', 'error')
      return
    }
    setSaving(true)
    try {
      const result = await adminApi<ApiKey>('POST', '/api/v1/keys', { name: name.trim() })
      onSave(result)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Create failed', 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div>
      <h3 className="adm-modal-title">Create API Key</h3>
      <label className="adm-label">Name</label>
      <input className="adm-input" type="text" value={name} onChange={e => setName(e.target.value)} autoFocus />
      <div className="adm-modal-actions">
        <button className="adm-btn adm-btn--ghost" onClick={onCancel}>Cancel</button>
        <button className="adm-btn adm-btn--primary" disabled={saving} onClick={() => void handleSubmit()}>
          {saving ? 'Creating...' : 'Create'}
        </button>
      </div>
    </div>
  )
}

function ShowKeyModal({ apiKey, onClose }: { apiKey: string; onClose: () => void }) {
  return (
    <div>
      <h3 className="adm-modal-title">API Key Created</h3>
      <p className="adm-modal-text">Copy this key now. It will not be shown again.</p>
      <pre className="adm-json-pre" style={{ userSelect: 'all' }}>{apiKey}</pre>
      <div className="adm-modal-actions">
        <button className="adm-btn adm-btn--primary" onClick={onClose}>Done</button>
      </div>
    </div>
  )
}

// Fallback when PUBLIC_API_URL is unset: a separate API route named after
// the dashboard host with an "-api" suffix.
function apiBaseUrl(): string {
  const host = window.location.hostname
  const apiHost = host.replace(/^devlake-dashboard(?=-|\.)/, 'devlake-dashboard-api')
  if (apiHost === host) return ''
  return `https://${apiHost}`
}

export default function KeysPanel() {
  const { toast, openModal, closeModal, confirm } = useAdmin()
  const [items, setItems] = useState<ApiKey[]>([])
  const [externalApiUrl, setExternalApiUrl] = useState(apiBaseUrl)

  useEffect(() => {
    fetchConfig()
      .then(cfg => { if (cfg.public_api_url) setExternalApiUrl(cfg.public_api_url) })
      .catch(() => {})
  }, [])

  const load = useCallback(async () => {
    try {
      const data = await adminApi<ApiKey[]>('GET', '/api/v1/keys')
      setItems(data)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to load keys', 'error')
    }
  }, [toast])

  useEffect(() => { void load() }, [load])

  const handleCreate = () => {
    openModal(
      <CreateKeyForm
        onSave={created => {
          if (created.key) {
            openModal(
              <ShowKeyModal
                apiKey={created.key}
                onClose={() => { closeModal(); void load() }}
              />,
            )
          } else {
            closeModal()
            void load()
          }
        }}
        onCancel={closeModal}
      />,
    )
  }

  const handleDelete = (id: number) => {
    confirm('Delete this API key? This cannot be undone.', async () => {
      try {
        await adminApi('DELETE', `/api/v1/keys/${id}`)
        toast('Key deleted', 'success')
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
        <h2>API Keys</h2>
        <button className="adm-btn adm-btn--primary" onClick={handleCreate}>+ Create Key</button>
      </div>
      {externalApiUrl && (
        <div className="adm-info-box">
          For external API access (VS Code extension, CI), use:{' '}
          <code style={{ userSelect: 'all' }}>{externalApiUrl}</code>
        </div>
      )}
      {items.length === 0 ? (
        <div className="adm-empty">No API keys found.</div>
      ) : (
        <table className="adm-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>User</th>
              <th>Created</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {items.map(k => (
              <tr key={k.id}>
                <td>{k.id}</td>
                <td>{k.name}</td>
                <td>{k.scoped_user || '—'}</td>
                <td>{k.created_at || '—'}</td>
                <td className="adm-actions">
                  <button className="adm-icon-btn danger" title="Delete" onClick={() => handleDelete(k.id)}>&#128465;</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
