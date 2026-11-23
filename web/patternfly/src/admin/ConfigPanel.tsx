import { useState, useCallback, useEffect, useRef } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'

interface TeamConfig {
  [key: string]: string
}

export default function ConfigPanel() {
  const { toast, openModal, closeModal, confirm, consumeNavParams } = useAdmin()
  const [config, setConfig] = useState<TeamConfig>({})
  const [loading, setLoading] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [entityType, setEntityType] = useState<'org' | 'product' | 'team'>('org')
  const [entityId, setEntityId] = useState('')
  const didInit = useRef(false)

  const load = useCallback(async (et?: string, ei?: string) => {
    const t = et || entityType
    const i = ei || entityId
    if (!i.trim()) {
      toast('Enter an Entity ID', 'error')
      return
    }
    setLoading(true)
    try {
      const data = await adminApi<TeamConfig>('GET', `/api/v1/config/${t}/${encodeURIComponent(i.trim())}`)
      setConfig(data || {})
      setLoaded(true)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed', 'error')
    } finally {
      setLoading(false)
    }
  }, [entityType, entityId, toast])

  useEffect(() => {
    if (didInit.current) return
    didInit.current = true
    const p = consumeNavParams()
    if (p.entityType && p.entityId) {
      const et = p.entityType as 'org' | 'product' | 'team'
      setEntityType(et)
      setEntityId(p.entityId)
      void load(et, p.entityId)
    }
  }, [consumeNavParams, load])

  const openEdit = () => {
    const entries = Object.entries(config)
    let rows = entries.length > 0 ? entries.map(([k, v]) => ({ key: k, value: v })) : [{ key: '', value: '' }]

    const Form = () => {
      const [fRows, setFRows] = useState(rows)
      rows = fRows
      return (
        <div>
          <h3 className="adm-modal-title">Edit Config: {entityType}/{entityId}</h3>
          {fRows.map((row, i) => (
            <div className="adm-kv-row" key={i}>
              <input className="adm-input" placeholder="Key" value={row.key} onChange={e => {
                const updated = [...fRows]
                updated[i] = { key: e.target.value, value: row.value }
                setFRows(updated)
              }} />
              <input className="adm-input" placeholder="Value" value={row.value} onChange={e => {
                const updated = [...fRows]
                updated[i] = { key: row.key, value: e.target.value }
                setFRows(updated)
              }} />
              <button className="adm-btn adm-btn--danger adm-btn--sm" onClick={() => setFRows(fRows.filter((_, j) => j !== i))}>Remove</button>
            </div>
          ))}
          <button className="adm-btn adm-btn--sm" onClick={() => setFRows([...fRows, { key: '', value: '' }])} style={{ marginTop: 8 }}>+ Add</button>
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
            <button className="adm-btn adm-btn--primary" onClick={async () => {
              const body: TeamConfig = {}
              for (const row of rows) { if (row.key.trim()) body[row.key.trim()] = row.value }
              try {
                await adminApi('PUT', `/api/v1/config/${entityType}/${encodeURIComponent(entityId)}`, body)
                toast('Config saved', 'success')
                closeModal()
                void load()
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

  const handleDeleteAll = () => {
    confirm(`Delete all config for ${entityType}/${entityId}?`, async () => {
      try {
        await adminApi('DELETE', `/api/v1/config/${entityType}/${encodeURIComponent(entityId)}`)
        toast('Config deleted', 'success')
        setConfig({})
      } catch (e) {
        if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
      }
    })
  }

  const entries = Object.entries(config)

  return (
    <div>
      <div className="adm-toolbar">
        <select className="adm-input" value={entityType} onChange={e => setEntityType(e.target.value as 'org' | 'product' | 'team')}>
          <option value="org">org</option>
          <option value="product">product</option>
          <option value="team">team</option>
        </select>
        <input className="adm-input" placeholder="Entity ID" value={entityId} onChange={e => setEntityId(e.target.value)} onKeyDown={e => e.key === 'Enter' && load()} />
        <button className="adm-btn adm-btn--primary" onClick={() => load()}>Load</button>
        {loaded && (
          <>
            <button className="adm-btn adm-btn--primary" onClick={openEdit}>Edit</button>
            <button className="adm-btn adm-btn--danger" onClick={handleDeleteAll}>Delete All</button>
          </>
        )}
      </div>
      {loading ? (
        <div className="adm-loader"><div className="diagram-card__spinner" /></div>
      ) : !loaded ? (
        <div className="adm-empty">Enter an entity type and ID, then click Load.</div>
      ) : entries.length === 0 ? (
        <div className="adm-empty">No config entries found.</div>
      ) : (
        <table className="adm-table">
          <thead><tr><th>Key</th><th>Value</th></tr></thead>
          <tbody>
            {entries.map(([k, v]) => (
              <tr key={k}><td>{k}</td><td>{v}</td></tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
