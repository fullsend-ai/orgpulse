import { buildBody, buildDiagramBody, hasTypeParams, resolveTypeParams, type TypeParams } from './diagramRequest'
import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { fetchDataFreshness, fetchDiagramData, fetchTeamConfig, type Dashboard, type Diagram, type Repo, type WorkitemProject } from '../api'
import DiagramCard from './DiagramCard'
import LineChart from './LineChart'
import BarChart from './BarChart'
import DoughnutChart from './DoughnutChart'
import SankeyChart from './SankeyChart'
import StatsGrid from './StatsGrid'
import DataTable from './DataTable'
import KnotsDiagram from './KnotsDiagram'
import StageDoughnut from './StageDoughnut'
import ScatterChart from './ScatterChart'
import StagesDiagram from './StagesDiagram'
import { Portal } from '../Portal'

type DiagramType =
  | 'table' | 'barChart' | 'stats' | 'knots' | 'info'
  | 'stagedoughnut' | 'sankey' | 'doughnut' | 'line'
  | 'scatter' | 'stages' | 'bar' | 'multi'

const CACHE_TTL_MS = 30 * 60 * 1000
const NO_TYPE_PARAMS: TypeParams = { params: {}, missing: [] }

interface CacheEntry { data: unknown; timestamp: number }

export interface DataLink {
  label: string
  url: string
}

export interface ExtraSourceLabel {
  field: string
  label: string
  link?: string
}

export interface ExtraSource {
  template: string
  labels: ExtraSourceLabel[]
  rows: Array<Record<string, unknown>>
}

interface ExtraConfigEntry {
  field?: string
  labels?: ExtraSourceLabel[]
  location?: string
  template?: string
  type?: string
}

interface ExtraBlock {
  config?: ExtraConfigEntry[]
  data?: Record<string, Array<Record<string, unknown>>>
}

function extractExtraSources(diagramData: Record<string, unknown>): ExtraSource[] {
  const extra = diagramData.extra as ExtraBlock | undefined
  if (!extra?.config || !extra?.data) return []
  const sources: ExtraSource[] = []
  for (const cfg of extra.config) {
    if (cfg.type !== 'openList' || !cfg.field || !cfg.template) continue
    const rows = extra.data[cfg.field]
    if (!Array.isArray(rows) || rows.length === 0) continue
    sources.push({ template: cfg.template, labels: cfg.labels || [], rows })
  }
  return sources
}

interface DiagramState {
  loading: boolean
  dataUpdatedAt?: number
  error: string | null
  noData: boolean
  data: unknown | null
  type: DiagramType | null
  label: string | null
  description: string | null
  warning: string | null
  datalinks: DataLink[]
  extraSources: ExtraSource[]
}

interface DiagramGridProps {
  dashboard: Dashboard
  repos: Repo[]
  selectedRepos: string[]
  projects: WorkitemProject[]
  selectedProjects: string[]
  timeRange: string
  customFrom: string
  customTo: string
}

function dataUpdatedAt(value: unknown): number | undefined {
  const responses = Array.isArray(value) ? value : [value]
  const timestamps = responses
    .map(item => item && typeof item === 'object' ? Number((item as Record<string, unknown>).lastupdate) : 0)
    .filter(timestamp => Number.isFinite(timestamp) && timestamp > 0)
  return timestamps.length > 0 ? Math.min(...timestamps) : undefined
}


function renderDiagramContent(type: DiagramType, data: Record<string, unknown>) {
  switch (type) {
    case 'line': return <LineChart data={data} />
    case 'bar': case 'barChart': return <BarChart data={data} />
    case 'doughnut': return <DoughnutChart data={data} />
    case 'sankey': return <SankeyChart data={data} />
    case 'stats': return <StatsGrid data={data} />
    case 'table': return <DataTable data={data} />
    case 'knots': return <KnotsDiagram data={data} />
    case 'stagedoughnut': return <StageDoughnut data={data} />
    case 'scatter': return <ScatterChart data={data} />
    case 'stages': return <StagesDiagram data={data} />
    case 'info':
      return <div className="diagram-info-message">{(data as { message?: string }).message || ''}</div>
    default:
      return <div style={{ color: '#EE0000' }}>Unknown diagram type: {type}</div>
  }
}

function renderMultiResponse(items: Array<Record<string, unknown>>) {
  const fullWidth: React.ReactNode[] = []
  const inline: React.ReactNode[] = []
  items.forEach((item, i) => {
    const type = item.type as DiagramType
    const data = (item.data || {}) as Record<string, unknown>
    if (item.options && !data.options) {
      (data as Record<string, unknown>).options = item.options
    }
    if (type === 'stats' || type === 'table') {
      fullWidth.push(<div className="multi-widget-full" key={`fw-${i}`}>{renderDiagramContent(type, data)}</div>)
    } else {
      inline.push(<div className="multi-widget-inline" key={`in-${i}`}>{renderDiagramContent(type, data)}</div>)
    }
  })
  return (
    <>
      {fullWidth}
      {inline.length > 0 && <div className="multi-widget-row">{inline}</div>}
    </>
  )
}

export default function DiagramGrid({
  dashboard, repos, selectedRepos, projects, selectedProjects, timeRange, customFrom, customTo,
}: DiagramGridProps) {
  const [states, setStates] = useState<Record<string, DiagramState>>({})
  const abortRef = useRef<AbortController | null>(null)
  const cacheRef = useRef<Map<string, CacheEntry>>(new Map())
  const body = useMemo(
    () => buildBody(dashboard, repos, selectedRepos, projects, selectedProjects, timeRange, customFrom, customTo),
    [dashboard, repos, selectedRepos, projects, selectedProjects, timeRange, customFrom, customTo],
  )
  const needsTypeParams = hasTypeParams(dashboard.type)
  const [typeParams, setTypeParams] = useState<TypeParams | null>(needsTypeParams ? null : NO_TYPE_PARAMS)
  const [lastCollectedAt, setLastCollectedAt] = useState<string | null | undefined>(undefined)

  useEffect(() => {
    let cancelled = false
    setLastCollectedAt(undefined)
    fetchDataFreshness(dashboard.owner_type, dashboard.owner_id)
      .then(result => { if (!cancelled) setLastCollectedAt(result.last_collected_at || null) })
      .catch(() => { if (!cancelled) setLastCollectedAt(null) })
    return () => { cancelled = true }
  }, [dashboard.owner_type, dashboard.owner_id])

  useEffect(() => {
    if (!needsTypeParams) {
      setTypeParams(NO_TYPE_PARAMS)
      return
    }
    let cancelled = false
    setTypeParams(null)
    const config = dashboard.owner_type === 'team'
      ? fetchTeamConfig(dashboard.owner_id).catch(() => ({}))
      : Promise.resolve({})
    config.then(cfg => { if (!cancelled) setTypeParams(resolveTypeParams(dashboard.type, cfg)) })
    return () => { cancelled = true }
  }, [needsTypeParams, dashboard.type, dashboard.owner_type, dashboard.owner_id])

  const loadDiagram = useCallback(
    async (diagram: Diagram, body: Record<string, unknown>, signal: AbortSignal) => {
      const cacheKey = diagram.endpoint + '|' + JSON.stringify(body)
      const cached = cacheRef.current.get(cacheKey)
      if (cached && Date.now() - cached.timestamp < CACHE_TTL_MS) return cached
      const result = await fetchDiagramData(diagram.endpoint, body, signal)
      const entry = { data: result, timestamp: Date.now() }
      cacheRef.current.set(cacheKey, entry)
      return entry
    }, [],
  )

  const loadAllDiagrams = useCallback(async () => {
    if (!dashboard.diagrams?.length || !typeParams) return
    if (abortRef.current) abortRef.current.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setStates(() => {
      const next: Record<string, DiagramState> = {}
      dashboard.diagrams!.forEach(d => {
        const id = `diagram${d.position}`
        next[id] = { loading: true, error: null, noData: false, data: null, type: null, label: d.title || null, description: d.description || null, warning: null, datalinks: [], extraSources: [] }
      })
      return next
    })

    await Promise.all(
      dashboard.diagrams.map(async diagram => {
        if (controller.signal.aborted) return
        const id = `diagram${diagram.position}`
        try {
          const { data: result } = await loadDiagram(diagram, buildDiagramBody(body, typeParams.params, diagram), controller.signal)
          if (controller.signal.aborted) return

          if (!result || (typeof result === 'string' && result.trim() === '')) {
            setStates(prev => ({ ...prev, [id]: { ...prev[id]!, loading: false, noData: true, data: null, type: null, datalinks: [], extraSources: [] } }))
            return
          }

          const jsonData = typeof result === 'string' ? JSON.parse(result) : result

          if (jsonData.error) {
            setStates(prev => ({ ...prev, [id]: { ...prev[id]!, loading: false, noData: true, data: null, type: null, datalinks: [], extraSources: [], label: jsonData.label || prev[id]!.label } }))
            return
          }

          if (Array.isArray(jsonData)) {
            const first = jsonData[0] as Record<string, unknown> | undefined
            const links = Array.isArray(first?.datalinks) ? first.datalinks as DataLink[] : []
            const firstData = (first?.data || {}) as Record<string, unknown>
            const extras = extractExtraSources(firstData)
            setStates(prev => ({
              ...prev,
              [id]: { ...prev[id]!, loading: false, dataUpdatedAt: dataUpdatedAt(jsonData), data: jsonData, type: 'multi', label: (first?.label as string) || prev[id]!.label, description: (first?.description as string) || prev[id]!.description, warning: null, datalinks: links, extraSources: extras },
            }))
          } else {
            const resp = jsonData as Record<string, unknown>
            let diagramData = (resp.data || {}) as Record<string, unknown>
            if (resp.options && !diagramData.options) diagramData = { ...diagramData, options: resp.options }
            if (resp.subtype) diagramData.subtype = resp.subtype
            const links = Array.isArray(resp.datalinks) ? resp.datalinks as DataLink[] : []
            const extras = extractExtraSources(diagramData)
            setStates(prev => ({
              ...prev,
              [id]: { ...prev[id]!, loading: false, dataUpdatedAt: dataUpdatedAt(resp), data: diagramData, type: resp.type as DiagramType, label: (resp.label as string) || prev[id]!.label, description: (resp.description as string) || prev[id]!.description, warning: (resp.warning as string) || null, datalinks: links, extraSources: extras },
            }))
          }
        } catch (err) {
          if ((err as Error).name === 'AbortError') return
          setStates(prev => ({ ...prev, [id]: { ...prev[id]!, loading: false, error: 'Unable to render this diagram. Please try adjusting the date range and reload.' } }))
        }
      }),
    )
  }, [dashboard, body, typeParams, loadDiagram])

  useEffect(() => {
    loadAllDiagrams()
    return () => { abortRef.current?.abort() }
  }, [loadAllDiagrams])

  const handleReload = useCallback(
    (dId: string, diagram: Diagram) => {
      const diagramBody = buildDiagramBody(body, typeParams?.params ?? {}, diagram)
      cacheRef.current.delete(diagram.endpoint + '|' + JSON.stringify(diagramBody))
      setStates(prev => ({ ...prev, [dId]: { ...prev[dId]!, loading: true, dataUpdatedAt: undefined, error: null, noData: false } }))

      const controller = abortRef.current || new AbortController()
      abortRef.current = controller
      loadDiagram(diagram, diagramBody, controller.signal)
        .then(({ data: result }) => {
          if (controller.signal.aborted) return
          const jsonData = typeof result === 'string' ? JSON.parse(result as string) : result
          if (!jsonData || jsonData.error) {
            setStates(prev => ({ ...prev, [dId]: { ...prev[dId]!, loading: false, noData: true, data: null, type: null, datalinks: [], extraSources: [] } }))
            return
          }
          if (Array.isArray(jsonData)) {
            const first = jsonData[0] as Record<string, unknown> | undefined
            const links = Array.isArray(first?.datalinks) ? first.datalinks as DataLink[] : []
            const firstData = (first?.data || {}) as Record<string, unknown>
            const extras = extractExtraSources(firstData)
            setStates(prev => ({ ...prev, [dId]: { ...prev[dId]!, loading: false, dataUpdatedAt: dataUpdatedAt(jsonData), data: jsonData, type: 'multi', label: (first?.label as string) || prev[dId]!.label, datalinks: links, extraSources: extras } }))
          } else {
            const resp = jsonData as Record<string, unknown>
            let diagramData = (resp.data || {}) as Record<string, unknown>
            if (resp.options && !diagramData.options) diagramData = { ...diagramData, options: resp.options }
            if (resp.subtype) diagramData.subtype = resp.subtype
            const links = Array.isArray(resp.datalinks) ? resp.datalinks as DataLink[] : []
            const extras = extractExtraSources(diagramData)
            setStates(prev => ({ ...prev, [dId]: { ...prev[dId]!, loading: false, dataUpdatedAt: dataUpdatedAt(resp), data: diagramData, type: resp.type as DiagramType, label: (resp.label as string) || prev[dId]!.label, warning: (resp.warning as string) || null, datalinks: links, extraSources: extras } }))
          }
        })
        .catch(err => {
          if ((err as Error).name === 'AbortError') return
          setStates(prev => ({ ...prev, [dId]: { ...prev[dId]!, loading: false, error: 'Failed to reload diagram.' } }))
        })
    },
    [body, typeParams, loadDiagram],
  )

  const [expandedId, setExpandedId] = useState<string | null>(null)

  const expandedState = useMemo(() => {
    if (!expandedId) return null
    const state = states[expandedId]
    if (!state?.data || !state.type) return null
    return state
  }, [expandedId, states])

  useEffect(() => {
    if (!expandedId) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setExpandedId(null) }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [expandedId])

  if (!dashboard.diagrams?.length) {
    return <div className="dashboard-empty">No diagrams configured for this dashboard.</div>
  }

  const formatDate = (date: Date, includeYear = true) => {
    const month = date.toLocaleString('en-US', { timeZone: 'UTC', month: 'short' })
    const day = date.toLocaleString('en-US', { timeZone: 'UTC', day: 'numeric' })
    const year = includeYear ? ` ${date.toLocaleString('en-US', { timeZone: 'UTC', year: 'numeric' })}` : ''
    return `${month} ${day}${year}`
  }
  const fromDate = typeof body.from === 'number' ? new Date(body.from * 1000) : null
  const toDate = typeof body.to === 'number' ? new Date(body.to * 1000) : null
  const sameYear = fromDate !== null && toDate !== null
    && fromDate.getUTCFullYear() === toDate.getUTCFullYear()
  const from = fromDate ? formatDate(fromDate, !sameYear) : null
  const to = toDate ? formatDate(toDate) : null
  const refreshedAt = lastCollectedAt ? new Date(lastCollectedAt) : null
  const refreshedDate = refreshedAt === null ? null : formatDate(refreshedAt)
  const refreshedTime = refreshedAt === null ? null : refreshedAt
    .toLocaleTimeString('en-US', { timeZone: 'UTC', hour: '2-digit', minute: '2-digit', hour12: true })
    .replace(/\s/g, '')

  return (
    <>
      <div className="dashboard-metadata">
        <span className="dashboard-metadata__range">
          Showing: <strong>{from && to ? `${from} – ${to}` : from ? `From ${from}` : to ? `Through ${to}` : 'All dates'}</strong>
        </span>
        <span>
          Last data collection (UTC): {lastCollectedAt === undefined ? 'Loading…' : lastCollectedAt === null ? 'Unavailable' : `${refreshedDate} ${refreshedTime}`}
        </span>
      </div>
      {typeParams && typeParams.missing.length > 0 && (
        <div className="dashboard-config-warning" role="alert">
          This dashboard requires the config {typeParams.missing.length === 1 ? 'entry' : 'entries'}{' '}
          {typeParams.missing.map((key, i) => (
            <span key={key}>{i > 0 && ', '}<code>{key}</code></span>
          ))}
          {' '}on the team it is attached to. Until {typeParams.missing.length === 1 ? 'it is' : 'they are'} set,
          the metrics use the workflow defaults. A team manager or admin can set {typeParams.missing.length === 1 ? 'it' : 'them'} in the team config.
        </div>
      )}
      <div className={`diagrams-grid${dashboard.type === 'aicommitscanner' ? ' diagrams-grid--ai-commits' : ''}`}>
        {dashboard.diagrams.map(diagram => {
          const id = `diagram${diagram.position}`
          const state = states[id]
          if (!state) return null
          const s = diagram.span === 2 || diagram.span === 3 ? diagram.span : undefined

          return (
            <DiagramCard
              key={id}
              id={id}
              documentationUrl={diagram.documentation_url}
              span={s}
              label={state.label || diagram.title}
              description={state.description || diagram.description}
              warning={state.warning || undefined}
              loading={state.loading}
              error={state.error}
              noData={state.noData}
              datalinks={state.datalinks}
              extraSources={state.extraSources}
              aiEnabled={diagram.ai_analytics}
              diagramData={state.data}
              onReload={() => handleReload(id, diagram)}
              onExpand={() => setExpandedId(id)}
            >
              {state.data && state.type
                ? state.type === 'multi'
                  ? renderMultiResponse(state.data as Array<Record<string, unknown>>)
                  : renderDiagramContent(state.type, state.data as Record<string, unknown>)
                : null}
            </DiagramCard>
          )
        })}
      </div>

      {expandedState && (
        <Portal>
        <div className="lightbox-overlay" onClick={() => setExpandedId(null)}>
          <div className="lightbox-content" onClick={e => e.stopPropagation()}>
            <button className="lightbox-close" onClick={() => setExpandedId(null)}>&times;</button>
            <div className="lightbox-body">
              {expandedState.label && (
                <div className="diagram-card__header">{expandedState.label.replace(/<[^>]*>/g, '')}</div>
              )}
              {expandedState.type === 'multi'
                ? renderMultiResponse(expandedState.data as Array<Record<string, unknown>>)
                : renderDiagramContent(expandedState.type!, expandedState.data as Record<string, unknown>)}
            </div>
          </div>
        </div>
        </Portal>
      )}
    </>
  )
}
