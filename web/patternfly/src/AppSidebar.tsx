import { useRef } from 'react'
import {
  Nav,
  NavItem,
  NavList,
  Tooltip,
} from '@patternfly/react-core'
import {
  HomeIcon,
  ChartLineIcon,
  ChartBarIcon,
  AngleDoubleLeftIcon,
  AngleDoubleRightIcon,
  UserIcon,
  CogIcon,
  SignOutAltIcon,
  CodeBranchIcon,
  CheckCircleIcon,
  TaskIcon,
  ShieldAltIcon,
  BrainIcon,
  TachometerAltIcon,
  TableIcon,
} from '@patternfly/react-icons'
import type { ComponentType } from 'react'
import type { Dashboard } from './api'

const iconMap: Record<string, ComponentType> = {
  ChartLineIcon,
  ChartBarIcon,
  CodeBranchIcon,
  CheckCircleIcon,
  TaskIcon,
  ShieldAltIcon,
  BrainIcon,
  TachometerAltIcon,
  TableIcon,
}

function DashboardIcon({ name }: { name?: string }) {
  const Icon = (name && iconMap[name]) || ChartLineIcon
  return <Icon />
}

interface AppSidebarProps {
  isOpen: boolean
  onToggle: () => void
  username?: string
  isAdmin?: boolean
  testMode?: boolean
  dashboards: Dashboard[]
  activeDashboard: string
  onDashboardSelect: (id: string) => void
  onSettingsClick: () => void
  onLogout?: () => void
}

export function AppSidebar({
  isOpen,
  onToggle,
  username,
  dashboards,
  activeDashboard,
  isAdmin,
  testMode,
  onLogout,
  onDashboardSelect,
  onSettingsClick,
}: AppSidebarProps) {
  const isHome = !activeDashboard
  const hasToggled = useRef(false)
  const animate = hasToggled.current
  if (!hasToggled.current) hasToggled.current = true

  if (!isOpen) {
    return (
      <div className={`sidebar-rail${animate ? ' sidebar-rail--animated' : ''}`}>
        <div className="sidebar-rail__header">
          <Tooltip content="Expand sidebar" position="right">
            <button
              className="sidebar-rail__item"
              onClick={onToggle}
              aria-label="Expand sidebar"
            >
              <AngleDoubleRightIcon />
            </button>
          </Tooltip>
        </div>
        <nav className="sidebar-rail__nav">
          <Tooltip content="Home" position="right">
            <button
              className={`sidebar-rail__item${isHome ? ' sidebar-rail__item--active' : ''}`}
              onClick={() => { onDashboardSelect(''); onToggle() }}
              aria-label="Home"
            >
              <HomeIcon />
            </button>
          </Tooltip>
          {dashboards.map(db => (
            <Tooltip key={db.dashboard_id} content={db.title} position="right">
              <button
                className={`sidebar-rail__item${activeDashboard === db.dashboard_id ? ' sidebar-rail__item--active' : ''}`}
                onClick={() => { onDashboardSelect(db.dashboard_id); onToggle() }}
                aria-label={db.title}
              >
                <DashboardIcon name={db.icon} />
              </button>
            </Tooltip>
          ))}
        </nav>
        <div className="sidebar-rail__footer">
          {testMode && (
            <Tooltip content="Test Mode" position="right">
              <span className="sidebar-rail__badge sidebar-rail__badge--test">T</span>
            </Tooltip>
          )}
          {isAdmin && (
            <Tooltip content="Admin" position="right">
              <span className="sidebar-rail__badge sidebar-rail__badge--admin">A</span>
            </Tooltip>
          )}
          <Tooltip content={username || 'User'} position="right">
            <button className="sidebar-rail__item" aria-label="User">
              <UserIcon />
            </button>
          </Tooltip>
          {onLogout && (
            <Tooltip content="Log out" position="right">
              <button className="sidebar-rail__item" aria-label="Log out" onClick={onLogout}>
                <SignOutAltIcon />
              </button>
            </Tooltip>
          )}
          <Tooltip content="Settings" position="right">
            <button className="sidebar-rail__item" aria-label="Settings" onClick={onSettingsClick}>
              <CogIcon />
            </button>
          </Tooltip>
        </div>
      </div>
    )
  }

  return (
    <div className={`app-sidebar${animate ? ' app-sidebar--animated' : ''}`}>
      <div className="sidebar-header">
        <img src="https://www.redhat.com/favicon.ico" alt="" className="sidebar-header__logo" />
        <div className="sidebar-header__text">
          <div className="sidebar-header__title">Pulse</div>
          <div className="sidebar-header__subtitle">Engineering Metrics</div>
        </div>
        <button className="sidebar-header__collapse" onClick={onToggle} aria-label="Collapse sidebar">
          <AngleDoubleLeftIcon />
        </button>
      </div>
      <div className="sidebar-body">
        <Nav>
          <NavList>
            <NavItem
              itemId="home"
              isActive={isHome}
              onClick={() => onDashboardSelect('')}
              icon={<HomeIcon />}
            >
              Home
            </NavItem>
            {dashboards.map(db => (
              <NavItem
                key={db.dashboard_id}
                itemId={db.dashboard_id}
                isActive={activeDashboard === db.dashboard_id}
                onClick={() => onDashboardSelect(db.dashboard_id)}
                icon={<DashboardIcon name={db.icon} />}
              >
                {db.title}
              </NavItem>
            ))}
          </NavList>
        </Nav>
        {dashboards.length === 0 && (
          <div className="sidebar-empty">
            Select an organization, product, or team to see available dashboards
          </div>
        )}
      </div>
      <div className="sidebar-footer">
        {(testMode || isAdmin) && (
          <div className="sidebar-footer__badges">
            {testMode && <span className="sidebar-badge sidebar-badge--test">Test Mode</span>}
            {isAdmin && <span className="sidebar-badge sidebar-badge--admin">Admin</span>}
          </div>
        )}
        <div className="sidebar-footer__user">
          <UserIcon />
          <span className="sidebar-footer__username">{username || 'User'}</span>
          {onLogout && (
            <button className="sidebar-footer__settings" aria-label="Log out" onClick={onLogout}>
              <SignOutAltIcon />
            </button>
          )}
          <button className="sidebar-footer__settings" aria-label="Settings" onClick={onSettingsClick}>
            <CogIcon />
          </button>
        </div>
      </div>
    </div>
  )
}
