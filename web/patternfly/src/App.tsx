import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { AppSidebar } from './AppSidebar'
import { MultiSelectDropdown } from './MultiSelectDropdown'
import { TopBar, type TimeRange } from './TopBar'
import DiagramGrid from './charts/DiagramGrid'
import { usesRepoFilter } from './charts/diagramRequest'
import {
  fetchAuthMe,
  fetchConfig,
  fetchDashboards,
  fetchOrgs,
  fetchProducts,
  fetchTeamSources,
  fetchTeams,
  type AuthGrant,
  type Dashboard,
  type FrontendConfig,
  type Org,
  type Product,
  type Repo,
  type Team,
  type WorkitemProject,
} from './api'

const SettingsPage = lazy(() => import('./settings/SettingsPage'))
const AdminPage = lazy(() => import('./admin/AdminPage'))
const StateOfTheUnion = lazy(() => import('./StateOfTheUnion'))
const MyContributions = lazy(() => import('./MyContributions'))
const QuickLinks = lazy(() => import('./QuickLinks'))

const INIT_PARAMS = new URLSearchParams(window.location.search)

function validDate(value: string | null): string {
  if (!value || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return ''
  const date = new Date(value + 'T00:00:00Z')
  return Number.isFinite(date.getTime()) && date.toISOString().slice(0, 10) === value ? value : ''
}

const INIT_FROM = validDate(INIT_PARAMS.get('from'))
const INIT_TO = validDate(INIT_PARAMS.get('to'))
const INIT_RANGE = INIT_PARAMS.get('range')
const INITIAL_TIME_RANGE: TimeRange = INIT_RANGE === 'days60' || INIT_RANGE === 'days90'
  ? INIT_RANGE
  : INIT_RANGE === 'custom' && INIT_FROM && INIT_TO && INIT_FROM <= INIT_TO ? 'custom' : 'days30'

function scopeInLists(orgs: Org[], products: Product[], teams: Team[], org: string, product: string, team: string): boolean {
  if (!org && !product && !team) return false
  if (org && !orgs.some(item => item.id === org)) return false
  if (product && !products.some(item => item.id === product)) return false
  if (team && !teams.some(item => item.id === team)) return false
  return true
}

function landingScope(orgs: Org[], products: Product[], grant: AuthGrant): { org: string; product: string; team: string } | null {
  const org = grant.orgs?.find(item => item.aggregate)
  if (org) return { org: org.id, product: '', team: '' }
  const product = grant.products?.find(item => item.aggregate)
  if (product) {
    const parent = orgs.find(item => item.products?.includes(product.id))
    return { org: parent?.id || '', product: product.id, team: '' }
  }
  const team = grant.teams?.[0]
  if (!team) return null
  const parentProduct = products.find(item => item.teams?.includes(team))
  const parentOrg = parentProduct ? orgs.find(item => item.products?.includes(parentProduct.id)) : undefined
  return { org: parentOrg?.id || '', product: parentProduct?.id || '', team }
}

export function App() {
  const [config, setConfig] = useState<FrontendConfig | null>(null)
  const [orgs, setOrgs] = useState<Org[]>([])
  const [allProducts, setAllProducts] = useState<Product[]>([])
  const [allTeams, setAllTeams] = useState<Team[]>([])
  const [dashboards, setDashboards] = useState<Dashboard[]>([])
  const [activeDashboard, setActiveDashboard] = useState('')
  const pendingDashboard = useRef(INIT_PARAMS.get('dashboard') || '')
  const [sidebarOpen, setSidebarOpen] = useState(true)
  const [selectedOrg, setSelectedOrg] = useState(INIT_PARAMS.get('org') || '')
  const [selectedProduct, setSelectedProduct] = useState(INIT_PARAMS.get('product') || '')
  const [selectedTeam, setSelectedTeam] = useState(INIT_PARAMS.get('team') || '')
  const [timeRange, setTimeRange] = useState<TimeRange>(INITIAL_TIME_RANGE)
  const [customFrom, setCustomFrom] = useState(INIT_FROM)
  const [customTo, setCustomTo] = useState(INIT_TO)
  const [repos, setRepos] = useState<Repo[]>([])
  const sourceKey = `${selectedTeam}|${selectedProduct}`
  const [loadedSourceKey, setLoadedSourceKey] = useState<string | null>(null)
  const [sourceLoadError, setSourceLoadError] = useState(false)
  const [projects, setProjects] = useState<WorkitemProject[]>([])
  const [selectedRepos, setSelectedRepos] = useState<string[]>(() => [...new Set(INIT_PARAMS.getAll('repo').filter(Boolean))])
  const [selectedProjects, setSelectedProjects] = useState<string[]>([])
  const [scopeReady, setScopeReady] = useState(false)
  const [noAccess, setNoAccess] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [settingsClosing, setSettingsClosing] = useState(false)
  const [adminOpen, setAdminOpen] = useState(false)
  const [adminClosing, setAdminClosing] = useState(false)

  const closeSettings = () => {
    setSettingsClosing(true)
    setTimeout(() => { setSettingsOpen(false); setSettingsClosing(false) }, 250)
  }

  const closeAdmin = () => {
    setAdminClosing(true)
    setTimeout(() => { setAdminOpen(false); setAdminClosing(false) }, 250)
  }

  const openAdmin = () => {
    setSettingsOpen(false)
    setSettingsClosing(false)
    setAdminOpen(true)
  }

  useEffect(() => {
    let cancelled = false
    fetchConfig().then(setConfig).catch(() => setConfig(null))
    Promise.all([fetchOrgs(), fetchProducts(), fetchTeams(), fetchAuthMe().catch(() => null)])
      .then(([o, p, t, me]) => {
        if (cancelled) return
        const loadedOrgs = o || []
        const loadedProducts = p || []
        const loadedTeams = t || []
        setOrgs(loadedOrgs)
        setAllProducts(loadedProducts)
        setAllTeams(loadedTeams)
        const urlOrg = INIT_PARAMS.get('org') || ''
        const urlProduct = INIT_PARAMS.get('product') || ''
        const urlTeam = INIT_PARAMS.get('team') || ''
        const urlEmpty = !urlOrg && !urlProduct && !urlTeam
        const grant = me?.grant
        if (!me || !grant) {
          setNoAccess(false)
        } else if (me.is_admin && urlEmpty) {
          setNoAccess(false)
        } else if (scopeInLists(loadedOrgs, loadedProducts, loadedTeams, urlOrg, urlProduct, urlTeam)) {
          setNoAccess(false)
        } else if (me.is_admin) {
          setSelectedOrg('')
          setSelectedProduct('')
          setSelectedTeam('')
          setNoAccess(false)
        } else {
          const next = landingScope(loadedOrgs, loadedProducts, grant)
          if (!next) {
            setSelectedOrg('')
            setSelectedProduct('')
            setSelectedTeam('')
            setNoAccess(true)
          } else {
            setSelectedOrg(next.org)
            setSelectedProduct(next.product)
            setSelectedTeam(next.team)
            setNoAccess(false)
          }
        }
        setScopeReady(true)
      })
      .catch(() => { if (!cancelled) setScopeReady(true) })
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    let ownerType: string
    let ownerId: string

    if (selectedTeam) {
      ownerType = 'team'
      ownerId = selectedTeam
    } else if (selectedProduct) {
      ownerType = 'product'
      ownerId = selectedProduct
    } else if (selectedOrg) {
      ownerType = 'org'
      ownerId = selectedOrg
    } else {
      setDashboards([])
      setActiveDashboard('')
      return
    }

    fetchDashboards(ownerType, ownerId)
      .then(dbs => {
        const list = dbs || []
        setDashboards(list)
        const pending = pendingDashboard.current
        if (pending && list.some(d => d.dashboard_id === pending)) {
          setActiveDashboard(pending)
        } else {
          setActiveDashboard(list.length > 0 ? list[0]!.dashboard_id : '')
        }
        pendingDashboard.current = ''
      })
      .catch(() => {
        setDashboards([])
        setActiveDashboard('')
        pendingDashboard.current = ''
      })
  }, [selectedOrg, selectedProduct, selectedTeam])

  useEffect(() => {
    let cancelled = false
    setSelectedProjects([])
    setLoadedSourceKey(null)
    setSourceLoadError(false)

    const teamIds = selectedTeam
      ? [selectedTeam]
      : (allProducts.find(p => p.id === selectedProduct)?.teams || [])

    Promise.all(teamIds.map(tid => fetchTeamSources(tid)))
      .then(results => {
        if (cancelled) return
        const mergedRepos = new Map<string, Repo>()
        const mergedProjects = new Map<string, WorkitemProject>()
        for (const { repos: r, projects: p } of results) {
          for (const repo of r) mergedRepos.set(`${repo.owner}/${repo.name}`, repo)
          for (const project of p) {
            if (project.type === 'jira') mergedProjects.set(project.id, project)
          }
        }
        setRepos([...mergedRepos.values()])
        setProjects([...mergedProjects.values()])
        setLoadedSourceKey(sourceKey)
      })
      .catch(() => {
        if (cancelled) return
        setSourceLoadError(true)
        setRepos([])
        setProjects([])
        setLoadedSourceKey(sourceKey)
      })

    return () => { cancelled = true }
  }, [selectedTeam, selectedProduct, allProducts, sourceKey])

  useEffect(() => {
    const params = new URLSearchParams()
    if (selectedOrg) params.set('org', selectedOrg)
    if (selectedProduct) params.set('product', selectedProduct)
    if (selectedTeam) params.set('team', selectedTeam)
    if (activeDashboard) params.set('dashboard', activeDashboard)
    for (const repo of selectedRepos) params.append('repo', repo)
    params.set('range', timeRange)
    if (timeRange === 'custom') {
      if (customFrom) params.set('from', customFrom)
      if (customTo) params.set('to', customTo)
    }
    const search = params.toString()
    const url = search ? `${window.location.pathname}?${search}` : window.location.pathname
    window.history.replaceState(null, '', url)
  }, [selectedOrg, selectedProduct, selectedTeam, activeDashboard, selectedRepos, timeRange, customFrom, customTo])

  const productMap = useMemo(() => {
    const map = new Map<string, Product>()
    for (const p of allProducts) map.set(p.id, p)
    return map
  }, [allProducts])

  const teamMap = useMemo(() => {
    const map = new Map<string, Team>()
    for (const t of allTeams) map.set(t.id, t)
    return map
  }, [allTeams])

  const orgOptions = useMemo(
    () => orgs.map(o => ({ value: o.id, label: o.name })),
    [orgs],
  )

  const productOptions = useMemo(() => {
    if (!selectedOrg) return []
    const org = orgs.find(o => o.id === selectedOrg)
    if (!org?.products) return []
    return org.products
      .map(pid => productMap.get(pid))
      .filter((p): p is Product => !!p)
      .sort((a, b) => a.name.localeCompare(b.name))
      .map(p => ({ value: p.id, label: p.name }))
  }, [selectedOrg, orgs, productMap])

  const teamOptions = useMemo(() => {
    if (!selectedProduct) return []
    const product = productMap.get(selectedProduct)
    if (!product?.teams) return []
    return product.teams
      .map(tid => teamMap.get(tid))
      .filter((t): t is Team => !!t)
      .sort((a, b) => (a.name || a.id).localeCompare(b.name || b.id))
      .map(t => ({ value: t.id, label: t.name || t.id }))
  }, [selectedProduct, productMap, teamMap])

  const currentDashboard = useMemo(
    () => dashboards.find(d => d.dashboard_id === activeDashboard),
    [dashboards, activeDashboard],
  )

  const isIssueDashboard = currentDashboard?.type === 'issuecycletime'
  const usesRepos = !isIssueDashboard && usesRepoFilter(currentDashboard?.type)

  const repoItems = useMemo(
    () => repos.map(r => ({
      value: `${r.owner}/${r.name}`,
      label: `${r.owner}/${r.name}`,
      group: r.type,
    })),
    [repos],
  )

  const projectItems = useMemo(
    () => projects.map(p => ({
      value: p.id,
      label: p.projectname || p.jiraproject || p.id,
      group: p.type,
    })),
    [projects],
  )

  const unavailableRepos = selectedRepos.filter(value => !repoItems.some(item => item.value === value))

  const topBarTitle = currentDashboard?.title || 'State of the Union'
  const topBarSubtitle = currentDashboard?.description || 'Your personalized overview across the platform'

  if (adminOpen && config) {
    return (
      <BrowserRouter>
        <Suspense fallback={<div className="settings-loader"><div className="diagram-card__spinner" /></div>}>
          <AdminPage isAdmin={config.is_admin} closing={adminClosing} onBack={closeAdmin} />
        </Suspense>
      </BrowserRouter>
    )
  }

  return (
    <BrowserRouter>
      <AppSidebar
        isOpen={sidebarOpen}
        onToggle={() => setSidebarOpen(prev => !prev)}
        username={config?.user_name || ''}
        isAdmin={config?.is_admin}
        testMode={config?.test_mode}
        dashboards={dashboards}
        activeDashboard={activeDashboard}
        onDashboardSelect={id => { setActiveDashboard(id); if (settingsOpen) closeSettings() }}
        onSettingsClick={() => settingsOpen ? closeSettings() : setSettingsOpen(true)}
        onLogout={() => { window.location.href = '/oauth2/sign_out' }}
      />
      <main className={`app-main ${sidebarOpen ? 'app-main--expanded' : 'app-main--collapsed'}`}>
        {settingsOpen && config ? (
          <Suspense fallback={<div className="settings-loader"><div className="diagram-card__spinner" /></div>}>
            <SettingsPage config={config} closing={settingsClosing} onBack={closeSettings} onAdmin={openAdmin} />
          </Suspense>
        ) : (
        <>
        <TopBar
          title={topBarTitle}
          subtitle={topBarSubtitle}
          orgs={orgOptions}
          products={productOptions}
          teams={teamOptions}
          selectedOrg={selectedOrg}
          selectedProduct={selectedProduct}
          selectedTeam={selectedTeam}
          onOrgChange={v => { setSelectedOrg(v); setSelectedProduct(''); setSelectedTeam(''); setSelectedRepos([]); setSelectedProjects([]) }}
          onProductChange={v => { setSelectedProduct(v); setSelectedTeam(''); setSelectedRepos([]); setSelectedProjects([]) }}
          onTeamChange={v => { setSelectedTeam(v); setSelectedRepos([]); setSelectedProjects([]) }}
          timeRange={timeRange}
          customFrom={customFrom}
          customTo={customTo}
          onTimeRangeChange={setTimeRange}
          onCustomRangeApply={(from, to) => { setCustomFrom(from); setCustomTo(to) }}
          timeRangeEnabled={!!currentDashboard}
        />
        {!scopeReady ? (
          <div className="settings-loader"><div className="diagram-card__spinner" /></div>
        ) : noAccess ? (
          <div className="dashboard-content">
            <div className="dashboard-empty">You do not have access to an organization, product, or team.</div>
          </div>
        ) : (
        <Routes>
          <Route path="/" element={
            !currentDashboard ? (
              <div className="dashboard-content dashboard-content--fit">
                <div className="home-columns">
                  <div className="home-col-main">
                    <Suspense fallback={<div className="settings-loader"><div className="diagram-card__spinner" /></div>}>
                      <StateOfTheUnion />
                    </Suspense>
                  </div>
                  <div className="home-col-side">
                    <Suspense fallback={<div className="settings-loader"><div className="diagram-card__spinner" /></div>}>
                      <QuickLinks />
                    </Suspense>
                    <Suspense fallback={<div className="settings-loader"><div className="diagram-card__spinner" /></div>}>
                      <MyContributions />
                    </Suspense>
                  </div>
                </div>
              </div>
            ) : (
              <div className="dashboard-content">
                {(usesRepos && repoItems.length > 0) ? (
                  <div className="content-filter-bar">
                    <span className="content-filter-bar__label">Repository</span>
                    <MultiSelectDropdown
                      id="repo-select"
                      label={
                        selectedRepos.length === 0 || selectedRepos.length === repoItems.length
                          ? `All Repositories (${repoItems.length})`
                          : `${selectedRepos.length} selected`
                      }
                      items={repoItems}
                      selected={selectedRepos.length > 0 ? selectedRepos : repoItems.map(r => r.value)}
                      onChange={sel => setSelectedRepos(sel.length === repoItems.length ? [] : sel)}
                      grouped
                    />
                  </div>
                ) : (isIssueDashboard && projectItems.length > 0) ? (
                  <div className="content-filter-bar">
                    <span className="content-filter-bar__label">Project</span>
                    <MultiSelectDropdown
                      id="project-select"
                      label={
                        selectedProjects.length === 0 || selectedProjects.length === projectItems.length
                          ? `All Projects (${projectItems.length})`
                          : `${selectedProjects.length} selected`
                      }
                      items={projectItems}
                      selected={selectedProjects.length > 0 ? selectedProjects : projectItems.map(p => p.value)}
                      onChange={sel => setSelectedProjects(sel.length === projectItems.length ? [] : sel)}
                      grouped
                    />
                  </div>
                ) : null}
                {loadedSourceKey === sourceKey && (sourceLoadError ? (
                  <div className="dashboard-empty">Unable to load team sources. Please reload and try again.</div>
                ) : usesRepos && unavailableRepos.length > 0 ? (
                  <div className="dashboard-empty">The linked repository selection is unavailable for this team or product: {unavailableRepos.join(', ')}. Choose an available repository to view metrics.</div>
                ) : isIssueDashboard && projects.length === 0 ? (
                  <div className="dashboard-empty">No Jira source is configured for this selection. Add a Jira source to view issue metrics.</div>
                ) : <DiagramGrid
                  key={`${sourceKey}|${activeDashboard}`}
                  dashboard={currentDashboard}
                  repos={repos}
                  selectedRepos={selectedRepos}
                  projects={projects}
                  selectedProjects={selectedProjects}
                  timeRange={timeRange}
                  customFrom={customFrom}
                  customTo={customTo}
                />)}
              </div>
            )
          } />
        </Routes>
        )}
        </>
        )}
      </main>
    </BrowserRouter>
  )
}
