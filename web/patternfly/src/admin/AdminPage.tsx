import { useState, useEffect, useCallback, lazy, Suspense } from 'react'
import { AdminProvider } from './AdminContext'
import { getAdminKey, setAdminKey, clearAdminKey, adminApi } from './api'

const OrgsPanel = lazy(() => import('./OrgsPanel'))
const ProductsPanel = lazy(() => import('./ProductsPanel'))
const TeamsPanel = lazy(() => import('./TeamsPanel'))
const DashboardsPanel = lazy(() => import('./DashboardsPanel'))
const SourcesPanel = lazy(() => import('./SourcesPanel'))
const ConfigPanel = lazy(() => import('./ConfigPanel'))
const SubscriptionsPanel = lazy(() => import('./AdminSubscriptionsPanel'))
const AlertsPanel = lazy(() => import('./AdminAlertsPanel'))
const QueriesPanel = lazy(() => import('./QueriesPanel'))
const ScriptsPanel = lazy(() => import('./ScriptsPanel'))
const SchemasPanel = lazy(() => import('./SchemasPanel'))
const WorkflowsPanel = lazy(() => import('./WorkflowsPanel'))
const KeysPanel = lazy(() => import('./KeysPanel'))
const DevLakeDBPanel = lazy(() => import('./DevLakeDBPanel'))

interface TabDef {
  id: string
  label: string
}

const TABS: TabDef[] = [
  { id: 'orgs', label: 'Organizations' },
  { id: 'products', label: 'Products' },
  { id: 'teams', label: 'Teams' },
  { id: 'dashboards', label: 'Dashboards' },
  { id: 'sources', label: 'Sources' },
  { id: 'config', label: 'Configuration' },
  { id: 'subscriptions', label: 'Subscriptions' },
  { id: 'alerts', label: 'Alerts' },
  { id: 'queries', label: 'SQL Queries' },
  { id: 'scripts', label: 'JS Scripts' },
  { id: 'schemas', label: 'JSON Schemas' },
  { id: 'workflows', label: 'Workflows' },
  { id: 'keys', label: 'API Keys' },
  { id: 'devlakedb', label: 'DevLake DB' },
]

interface AdminPageProps {
  isAdmin: boolean
  closing?: boolean
  onBack: () => void
}

export default function AdminPage({ isAdmin, closing, onBack }: AdminPageProps) {
  const [authed, setAuthed] = useState<boolean | null>(null)
  const [activeTab, setActiveTab] = useState('orgs')
  const [loginError, setLoginError] = useState('')
  const [apiKeyInput, setApiKeyInput] = useState('')

  useEffect(() => {
    if (isAdmin) {
      setAuthed(true)
      return
    }
    const key = getAdminKey()
    if (key) {
      adminApi('GET', '/api/v1/orgs')
        .then(() => setAuthed(true))
        .catch(() => { clearAdminKey(); setAuthed(false) })
    } else {
      setAuthed(false)
    }
  }, [isAdmin])

  const handleLogin = () => {
    const key = apiKeyInput.trim()
    if (!key) return
    setAdminKey(key)
    fetch('/api/v1/orgs', { headers: { 'X-API-Key': key } })
      .then(r => {
        if (!r.ok) { setLoginError('Invalid API key'); clearAdminKey(); return }
        setAuthed(true)
      })
      .catch(() => { setLoginError('Connection error'); clearAdminKey() })
  }

  const handleNavigate = useCallback((tab: string) => {
    setActiveTab(tab)
  }, [])

  const activeTabDef = TABS.find(t => t.id === activeTab)

  if (authed === null) {
    return (
      <div className="adm-login-wrapper">
        <div className="adm-loader"><div className="diagram-card__spinner" /></div>
      </div>
    )
  }

  if (!authed) {
    return (
      <div className={`adm-login-wrapper${closing ? ' adm-container--closing' : ''}`}>
        <div className="adm-login">
          <h2>Admin Login</h2>
          {loginError && <div className="adm-login-error">{loginError}</div>}
          <input
            type="password"
            className="adm-input"
            placeholder="Admin API Key"
            value={apiKeyInput}
            onChange={e => setApiKeyInput(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && handleLogin()}
            autoFocus
          />
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="adm-btn adm-btn--ghost" onClick={onBack}>Back</button>
            <button className="adm-btn adm-btn--primary" onClick={handleLogin} style={{ flex: 1 }}>Login</button>
          </div>
        </div>
      </div>
    )
  }

  const renderPanel = () => {
    switch (activeTab) {
      case 'orgs': return <OrgsPanel />
      case 'products': return <ProductsPanel />
      case 'teams': return <TeamsPanel />
      case 'dashboards': return <DashboardsPanel />
      case 'sources': return <SourcesPanel />
      case 'config': return <ConfigPanel />
      case 'subscriptions': return <SubscriptionsPanel />
      case 'alerts': return <AlertsPanel />
      case 'queries': return <QueriesPanel />
      case 'scripts': return <ScriptsPanel />
      case 'schemas': return <SchemasPanel />
      case 'workflows': return <WorkflowsPanel />
      case 'keys': return <KeysPanel />
      case 'devlakedb': return <DevLakeDBPanel />
      default: return null
    }
  }

  return (
    <AdminProvider onNavigate={handleNavigate}>
      <div className={`adm-container${closing ? ' adm-container--closing' : ''}`}>
        <div className="adm-sidebar">
          <div className="adm-sidebar-header">
            <img src="https://www.redhat.com/favicon.ico" alt="" className="adm-sidebar-header__logo" />
            <div className="adm-sidebar-header__text">
              <div className="adm-sidebar-header__title">Pulse</div>
              <div className="adm-sidebar-header__subtitle">Administration</div>
            </div>
          </div>
          <ul className="adm-sidebar-nav">
            {TABS.map(tab => (
              <li key={tab.id}>
                <button
                  className={`adm-sidebar-link${activeTab === tab.id ? ' active' : ''}`}
                  onClick={() => setActiveTab(tab.id)}
                >
                  {tab.label}
                </button>
              </li>
            ))}
          </ul>
          <button className="adm-sidebar-back" onClick={onBack}>&larr; Back to Dashboard</button>
        </div>
        <div className="adm-main">
          <div className="adm-main__header">
            <h2 className="adm-main__title">{activeTabDef?.label ?? 'Admin'}</h2>
          </div>
          <div className="adm-main__content">
            <Suspense fallback={<div className="adm-loader"><div className="diagram-card__spinner" /><span>Loading...</span></div>}>
              {renderPanel()}
            </Suspense>
          </div>
        </div>
      </div>
    </AdminProvider>
  )
}
