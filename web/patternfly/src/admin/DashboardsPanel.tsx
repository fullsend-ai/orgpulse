import { useState, useCallback, useEffect, useRef } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'

interface DiagramConfig {
  position: number
  type: string
  endpoint: string
  span?: number
  title?: string
  description?: string
  ai_analytics: boolean
}

interface Dashboard {
  auto_id: number
  dashboard_id: string
  owner_type: string
  owner_id: string
  type: string
  title: string
  description?: string
  chat_url?: string
  ai_analytics_url?: string
  default_source?: string
  connection_id?: string
  jira_project?: string
  icon?: string
  category?: string
  diagrams?: DiagramConfig[]
}

export default function DashboardsPanel() {
  const { toast, openModal, closeModal, confirm, consumeNavParams } = useAdmin()
  const [items, setItems] = useState<Dashboard[]>([])
  const [loading, setLoading] = useState(false)
  const [ownerType, setOwnerType] = useState<'org' | 'product' | 'team'>('org')
  const [ownerId, setOwnerId] = useState('')
  const didInit = useRef(false)

  const load = useCallback(async (ot?: string, oi?: string) => {
    const t = ot || ownerType
    const i = oi || ownerId
    if (!i.trim()) { toast('Enter an Owner ID', 'error'); return }
    setLoading(true)
    try {
      const data = await adminApi<Dashboard[]>(
        'GET',
        `/api/v1/dashboards?owner_type=${t}&owner_id=${encodeURIComponent(i.trim())}`,
      )
      setItems(data)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed', 'error')
    } finally {
      setLoading(false)
    }
  }, [ownerType, ownerId, toast])

  useEffect(() => {
    if (didInit.current) return
    didInit.current = true
    const p = consumeNavParams()
    if (p.ownerType && p.ownerId) {
      const ot = p.ownerType as 'org' | 'product' | 'team'
      setOwnerType(ot)
      setOwnerId(p.ownerId)
      void load(ot, p.ownerId)
    }
  }, [consumeNavParams, load])

  const openAdd = () => {
    let dashOwnerType = ownerType
    let dashOwnerId = ownerId
    let dashboardId = ''
    let type = ''
    let title = ''
    let description = ''
    let icon = ''
    let category = ''
    const Form = () => {
      const [fOwnerType, setFOwnerType] = useState(dashOwnerType)
      const [fOwnerId, setFOwnerId] = useState(dashOwnerId)
      const [fDashId, setFDashId] = useState(dashboardId)
      const [fType, setFType] = useState(type)
      const [fTitle, setFTitle] = useState(title)
      const [fDesc, setFDesc] = useState(description)
      const [fIcon, setFIcon] = useState(icon)
      const [fCategory, setFCategory] = useState(category)
      dashOwnerType = fOwnerType; dashOwnerId = fOwnerId; dashboardId = fDashId
      type = fType; title = fTitle; description = fDesc; icon = fIcon; category = fCategory
      return (
        <div>
          <h3 className="adm-modal-title">Add Dashboard</h3>
          <label>Owner Type
            <select value={fOwnerType} onChange={e => setFOwnerType(e.target.value as 'org' | 'product' | 'team')}>
              <option value="org">org</option><option value="product">product</option><option value="team">team</option>
            </select>
          </label>
          <label>Owner ID<input value={fOwnerId} onChange={e => setFOwnerId(e.target.value)} /></label>
          <label>Dashboard ID<input value={fDashId} onChange={e => setFDashId(e.target.value)} autoFocus /></label>
          <label>Type<input value={fType} onChange={e => setFType(e.target.value)} /></label>
          <label>Title<input value={fTitle} onChange={e => setFTitle(e.target.value)} /></label>
          <label>Description<input value={fDesc} onChange={e => setFDesc(e.target.value)} /></label>
          <label>Icon<input value={fIcon} onChange={e => setFIcon(e.target.value)} placeholder="e.g. ChartLineIcon" /></label>
          <label>Category<input value={fCategory} onChange={e => setFCategory(e.target.value)} placeholder="Folder name for sidebar grouping" /></label>
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
            <button className="adm-btn adm-btn--primary" onClick={async () => {
              try {
                await adminApi('POST', '/api/v1/dashboards', {
                  owner_type: dashOwnerType, owner_id: dashOwnerId,
                  dashboard_id: dashboardId, type, title,
                  description: description || undefined,
                  icon: icon || undefined,
                  category: category || undefined,
                })
                toast('Dashboard created', 'success'); closeModal(); void load()
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

  const openEdit = async (d: Dashboard) => {
    let full: Dashboard
    try {
      full = await adminApi<Dashboard>('GET', `/api/v1/dashboards/${d.auto_id}`)
    } catch (e) {
      if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
      return
    }

    let dashboardId = full.dashboard_id
    let type = full.type
    let title = full.title
    let description = full.description || ''
    let chatUrl = full.chat_url || ''
    let aiAnalyticsUrl = full.ai_analytics_url || ''
    let defaultSource = full.default_source || ''
    let connectionId = full.connection_id || ''
    let jiraProject = full.jira_project || ''
    let icon = full.icon || ''
    let category = full.category || ''
    let diagramsJson = JSON.stringify(full.diagrams || [], null, 2)

    const Form = () => {
      const [fDashId, setFDashId] = useState(dashboardId)
      const [fType, setFType] = useState(type)
      const [fTitle, setFTitle] = useState(title)
      const [fDesc, setFDesc] = useState(description)
      const [fChatUrl, setFChatUrl] = useState(chatUrl)
      const [fAiUrl, setFAiUrl] = useState(aiAnalyticsUrl)
      const [fDefaultSrc, setFDefaultSrc] = useState(defaultSource)
      const [fConnId, setFConnId] = useState(connectionId)
      const [fJira, setFJira] = useState(jiraProject)
      const [fIcon, setFIcon] = useState(icon)
      const [fCategory, setFCategory] = useState(category)
      const [fDiagrams, setFDiagrams] = useState(diagramsJson)
      dashboardId = fDashId; type = fType; title = fTitle; description = fDesc
      chatUrl = fChatUrl; aiAnalyticsUrl = fAiUrl; defaultSource = fDefaultSrc
      connectionId = fConnId; jiraProject = fJira; icon = fIcon; category = fCategory
      diagramsJson = fDiagrams
      return (
        <div>
          <h3 className="adm-modal-title">Edit Dashboard #{d.auto_id}</h3>
          <label>Dashboard ID<input value={fDashId} onChange={e => setFDashId(e.target.value)} autoFocus /></label>
          <label>Type<input value={fType} onChange={e => setFType(e.target.value)} /></label>
          <label>Title<input value={fTitle} onChange={e => setFTitle(e.target.value)} /></label>
          <label>Description<input value={fDesc} onChange={e => setFDesc(e.target.value)} /></label>
          <label>Icon<input value={fIcon} onChange={e => setFIcon(e.target.value)} placeholder="e.g. ChartLineIcon" /></label>
          <label>Category<input value={fCategory} onChange={e => setFCategory(e.target.value)} placeholder="Folder name for sidebar grouping" /></label>
          <label>Chat URL<input value={fChatUrl} onChange={e => setFChatUrl(e.target.value)} /></label>
          <label>AI Analytics URL<input value={fAiUrl} onChange={e => setFAiUrl(e.target.value)} /></label>
          <label>Default Source<input value={fDefaultSrc} onChange={e => setFDefaultSrc(e.target.value)} /></label>
          <label>Connection ID<input value={fConnId} onChange={e => setFConnId(e.target.value)} /></label>
          <label>Jira Project<input value={fJira} onChange={e => setFJira(e.target.value)} /></label>
          <label>Diagrams (JSON)</label>
          <textarea
            className="adm-code-textarea"
            value={fDiagrams}
            onChange={e => setFDiagrams(e.target.value)}
            rows={10}
          />
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
            <button className="adm-btn adm-btn--primary" onClick={async () => {
              let diagrams: DiagramConfig[]
              try { diagrams = JSON.parse(diagramsJson) } catch { toast('Invalid diagrams JSON', 'error'); return }
              try {
                await adminApi('PUT', `/api/v1/dashboards/${d.auto_id}`, {
                  dashboard_id: dashboardId, type, title,
                  description: description || undefined,
                  chat_url: chatUrl || undefined,
                  ai_analytics_url: aiAnalyticsUrl || undefined,
                  default_source: defaultSource || undefined,
                  connection_id: connectionId || undefined,
                  jira_project: jiraProject || undefined,
                  icon: icon || undefined,
                  category: category || undefined,
                  diagrams,
                })
                toast('Dashboard updated', 'success'); closeModal(); void load()
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

  const handleDelete = (d: Dashboard) => {
    confirm(`Delete dashboard "${d.title}"?`, async () => {
      try {
        await adminApi('DELETE', `/api/v1/dashboards/${d.auto_id}`)
        toast('Dashboard deleted', 'success'); void load()
      } catch (e) {
        if (!(e instanceof AuthError)) toast(e instanceof Error ? e.message : 'Failed', 'error')
      }
    })
  }

  return (
    <div>
      <div className="adm-toolbar">
        <select value={ownerType} onChange={e => setOwnerType(e.target.value as 'org' | 'product' | 'team')}>
          <option value="org">org</option><option value="product">product</option><option value="team">team</option>
        </select>
        <input placeholder="Owner ID" value={ownerId} onChange={e => setOwnerId(e.target.value)} onKeyDown={e => e.key === 'Enter' && load()} />
        <button className="adm-btn adm-btn--primary" onClick={() => load()}>Load</button>
        <button className="adm-btn adm-btn--primary" onClick={openAdd}>+ Add Dashboard</button>
      </div>
      {loading ? (
        <div className="adm-loader"><div className="diagram-card__spinner" /></div>
      ) : items.length === 0 ? (
        <div className="adm-empty">No dashboards loaded. Enter an owner and click Load.</div>
      ) : (
        <table className="adm-table">
          <thead>
            <tr><th>#</th><th>Dashboard ID</th><th>Type</th><th>Title</th><th>Diagrams</th><th>Actions</th></tr>
          </thead>
          <tbody>
            {items.map((d, i) => (
              <tr key={d.auto_id}>
                <td>{i + 1}</td>
                <td>{d.dashboard_id}</td>
                <td>{d.type}</td>
                <td>{d.title}</td>
                <td>{d.diagrams?.length ?? 0}</td>
                <td>
                  <div className="adm-actions">
                    <button className="adm-icon-btn" title="Edit" onClick={() => void openEdit(d)}>&#9998;&#65039;</button>
                    <button className="adm-icon-btn" title="Delete" onClick={() => handleDelete(d)}>&#128465;&#65039;</button>
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
