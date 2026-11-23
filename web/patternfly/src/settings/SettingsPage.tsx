import { useState, lazy, Suspense } from 'react'
import type { FrontendConfig } from '../api'

const MyTeamsPanel = lazy(() => import('./MyTeamsPanel'))
const MyProductsPanel = lazy(() => import('./MyProductsPanel'))
const TokenManagementPanel = lazy(() => import('./TokenManagementPanel'))

interface TabDef {
  id: string
  label: string
}

const TABS: TabDef[] = [
  { id: 'myteams', label: 'My Teams' },
  { id: 'myproducts', label: 'My Products' },
  { id: 'tokens', label: 'Tokens' },
]

interface SettingsPageProps {
  config: FrontendConfig
  closing?: boolean
  onBack: () => void
  onAdmin?: () => void
}

export default function SettingsPage({ config, closing, onBack, onAdmin }: SettingsPageProps) {
  const [activeTab, setActiveTab] = useState('myteams')

  const activeTabDef = TABS.find(t => t.id === activeTab)

  const renderPanel = () => {
    switch (activeTab) {
      case 'myteams':
        return <MyTeamsPanel email={config.user_email} isAdmin={config.is_admin} devlakeEnabled={config.devlake_enabled} />
      case 'myproducts':
        return <MyProductsPanel email={config.user_email} isAdmin={config.is_admin} />
      case 'tokens':
        return <TokenManagementPanel email={config.user_email} isAdmin={config.is_admin} devlakeEnabled={config.devlake_enabled} />
      default:
        return null
    }
  }

  return (
    <div className={`settings-container${closing ? ' settings-container--closing' : ''}`}>
      <div className="settings-sidebar">
        <button className="settings-back-link" onClick={onBack}>
          &larr; Back to Dashboard
        </button>
        <ul className="settings-tab-list">
          {TABS.map(tab => (
            <li key={tab.id}>
              <button
                className={`settings-tab-link${activeTab === tab.id ? ' active' : ''}`}
                onClick={() => setActiveTab(tab.id)}
              >
                {tab.label}
              </button>
            </li>
          ))}
        </ul>
        {config.is_admin && onAdmin && (
          <button className="settings-admin-link" onClick={onAdmin}>
            Administration
          </button>
        )}
      </div>
      <div className="settings-main">
        <div className="settings-main__header">
          <h2 className="settings-main__title">{activeTabDef?.label ?? 'Settings'}</h2>
        </div>
        <Suspense
          fallback={
            <div className="settings-loader">
              <div className="diagram-card__spinner" />
              <div className="settings-loader-text">Loading...</div>
            </div>
          }
        >
          {renderPanel()}
        </Suspense>
      </div>
    </div>
  )
}
