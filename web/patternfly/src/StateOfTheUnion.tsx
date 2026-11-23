import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import {
  fetchMyIssues,
  fetchIssueTransitions,
  executeIssueTransition,
  type UnifiedIssue,
  type IssueTransition,
} from './api'
import SourceIcon, { sourceLabel } from './SourceIcon'
import { safeHref } from './safeUrl'

type SortKey = 'updated' | 'created' | 'title' | 'status' | 'source' | 'type' | 'priority'

const STATUS_COLORS: Record<string, string> = {
  open: '#1a7f37',
  closed: '#8250df',
  in_progress: '#0969da',
}

function relativeTime(dateStr: string): string {
  const date = new Date(dateStr)
  if (isNaN(date.getTime())) return dateStr
  const diff = Date.now() - date.getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}d ago`
  return date.toLocaleDateString()
}

function typeLabel(type: string): string {
  switch (type) {
    case 'pull_request': return 'PR'
    case 'merge_request': return 'MR'
    default: return type.charAt(0).toUpperCase() + type.slice(1)
  }
}

export default function StateOfTheUnion() {
  const [issues, setIssues] = useState<UnifiedIssue[]>([])
  const [hasTokens, setHasTokens] = useState(true)
  const [fetchErrors, setFetchErrors] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [sourceFilter, setSourceFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')
  const [typeFilter, setTypeFilter] = useState('all')
  const [search, setSearch] = useState('')
  const [sortBy, setSortBy] = useState<SortKey>('updated')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc')

  const [transitionTarget, setTransitionTarget] = useState<UnifiedIssue | null>(null)
  const [transitions, setTransitions] = useState<IssueTransition[]>([])
  const [loadingTransitions, setLoadingTransitions] = useState(false)
  const [executing, setExecuting] = useState(false)
  const dropdownRef = useRef<HTMLDivElement>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await fetchMyIssues()
      setIssues(data.issues)
      setHasTokens(data.has_tokens)
      setFetchErrors(data.errors ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load issues')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setTransitionTarget(null)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const availableSources = useMemo(() => [...new Set(issues.map(i => i.source))].sort(), [issues])
  const availableStatuses = useMemo(() => [...new Set(issues.map(i => i.status))].sort(), [issues])
  const availableTypes = useMemo(() => [...new Set(issues.map(i => i.type))].sort(), [issues])

  const filtered = useMemo(() => {
    let result = issues
    if (sourceFilter !== 'all') result = result.filter(i => i.source === sourceFilter)
    if (statusFilter !== 'all') result = result.filter(i => i.status === statusFilter)
    if (typeFilter !== 'all') result = result.filter(i => i.type === typeFilter)
    if (search) {
      const q = search.toLowerCase()
      result = result.filter(i =>
        i.title.toLowerCase().includes(q) ||
        i.key.toLowerCase().includes(q) ||
        i.repo.toLowerCase().includes(q),
      )
    }

    return [...result].sort((a, b) => {
      let cmp = 0
      switch (sortBy) {
        case 'updated': cmp = a.updated.localeCompare(b.updated); break
        case 'created': cmp = a.created.localeCompare(b.created); break
        case 'title': cmp = a.title.localeCompare(b.title); break
        case 'status': cmp = a.status.localeCompare(b.status); break
        case 'source': cmp = a.source.localeCompare(b.source); break
        case 'type': cmp = a.type.localeCompare(b.type); break
        case 'priority': cmp = (a.priority || '').localeCompare(b.priority || ''); break
      }
      return sortDir === 'desc' ? -cmp : cmp
    })
  }, [issues, sourceFilter, statusFilter, typeFilter, search, sortBy, sortDir])

  const handleSort = (key: SortKey) => {
    if (sortBy === key) {
      setSortDir(prev => (prev === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortBy(key)
      setSortDir(key === 'title' ? 'asc' : 'desc')
    }
  }

  const sortIndicator = (key: SortKey) =>
    sortBy === key ? (sortDir === 'asc' ? ' ▲' : ' ▼') : ''

  const handleStatusClick = async (issue: UnifiedIssue) => {
    if (transitionTarget?.source_id === issue.source_id) {
      setTransitionTarget(null)
      return
    }
    setTransitionTarget(issue)
    setTransitions([])
    setLoadingTransitions(true)
    try {
      setTransitions(await fetchIssueTransitions(issue.source, issue.source_id, issue.status))
    } catch {
      setTransitions([])
    } finally {
      setLoadingTransitions(false)
    }
  }

  const handleTransition = async (transition: IssueTransition) => {
    if (!transitionTarget) return
    setExecuting(true)
    try {
      await executeIssueTransition(transitionTarget.source, transitionTarget.source_id, transition.id)
      setIssues(prev => prev.map(i =>
        i.source === transitionTarget.source && i.source_id === transitionTarget.source_id
          ? { ...i, raw_status: transition.name, status: transition.name.toLowerCase().replace(/\s+/g, '_') }
          : i,
      ))
      setTransitionTarget(null)
    } catch (err) {
      window.alert(`Transition failed: ${err instanceof Error ? err.message : 'Unknown error'}`)
    } finally {
      setExecuting(false)
    }
  }

  const renderBody = () => {
    if (loading) {
      return (
        <div className="settings-loader">
          <div className="diagram-card__spinner" />
          <div className="settings-loader-text">Loading your issues...</div>
        </div>
      )
    }

    if (error) {
      return (
        <div className="sotu-empty">
          <p>{error}</p>
          <button className="sub-btn sub-btn--secondary" onClick={() => void load()}>Retry</button>
        </div>
      )
    }

    if (!hasTokens) {
      return (
        <div className="sotu-empty">
          <p>No personal tokens configured.</p>
          <p className="sotu-empty-hint">
            Add a personal token in Settings &rarr; Tokens to see your GitHub, GitLab, and Jira issues here.
            Personal tokens are used to query your assigned items directly from the source systems.
          </p>
        </div>
      )
    }

    if (issues.length === 0) {
      return (
        <>
          {fetchErrors.length > 0 && (
            <div className="sotu-fetch-errors">
              {fetchErrors.map((e, i) => <div key={i} className="sotu-fetch-error">{e}</div>)}
            </div>
          )}
          <div className="sotu-empty">
            <p>{fetchErrors.length > 0 ? 'Could not load items from some sources.' : 'No open items found.'}</p>
            <p className="sotu-empty-hint">
              Items assigned to you in GitHub, GitLab, and Jira will appear here.
            </p>
          </div>
        </>
      )
    }

    return (
      <>
        {fetchErrors.length > 0 && (
          <div className="sotu-fetch-errors">
            {fetchErrors.map((e, i) => <div key={i} className="sotu-fetch-error">{e}</div>)}
          </div>
        )}
        <div className="sotu-toolbar">
        <input
          className="sotu-search"
          type="text"
          placeholder="Search issues..."
          value={search}
          onChange={e => setSearch(e.target.value)}
        />
        <select className="sotu-filter" value={sourceFilter} onChange={e => setSourceFilter(e.target.value)}>
          <option value="all">All Sources</option>
          {availableSources.map(s => <option key={s} value={s}>{sourceLabel(s)}</option>)}
        </select>
        <select className="sotu-filter" value={statusFilter} onChange={e => setStatusFilter(e.target.value)}>
          <option value="all">All Statuses</option>
          {availableStatuses.map(s => <option key={s} value={s}>{s.replace(/_/g, ' ')}</option>)}
        </select>
        <select className="sotu-filter" value={typeFilter} onChange={e => setTypeFilter(e.target.value)}>
          <option value="all">All Types</option>
          {availableTypes.map(t => <option key={t} value={t}>{typeLabel(t)}</option>)}
        </select>
        <button className="sub-btn sub-btn--secondary sotu-refresh" onClick={() => void load()} title="Refresh">
          Refresh
        </button>
      </div>

      <div className="sotu-count">{filtered.length} of {issues.length} items</div>

      <div className="sotu-table-wrap">
        <table className="sotu-table">
          <thead>
            <tr>
              <th className="sotu-th-source" onClick={() => handleSort('source')}>{sortIndicator('source')}</th>
              <th onClick={() => handleSort('title')}>Issue{sortIndicator('title')}</th>
              <th className="sotu-th-status" onClick={() => handleSort('status')}>Status{sortIndicator('status')}</th>
              <th className="sotu-th-type" onClick={() => handleSort('type')}>Type{sortIndicator('type')}</th>
              <th className="sotu-th-priority" onClick={() => handleSort('priority')}>Priority{sortIndicator('priority')}</th>
              <th className="sotu-th-updated" onClick={() => handleSort('updated')}>Updated{sortIndicator('updated')}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map(issue => (
              <tr key={`${issue.source}-${issue.source_id}`}>
                <td>
                  <SourceIcon source={issue.source} />
                </td>
                <td>
                  <a href={safeHref(issue.url)} target="_blank" rel="noopener noreferrer" className="sotu-issue-link">
                    <span className="sotu-issue-meta">
                      <span className="sotu-issue-key">{issue.key}</span>
                      {issue.labels.length > 0 && (
                        <span className="sotu-labels">
                          {issue.labels.slice(0, 3).map(l => (
                            <span key={l} className="sotu-label">{l}</span>
                          ))}
                          {issue.labels.length > 3 && <span className="sotu-label">+{issue.labels.length - 3}</span>}
                        </span>
                      )}
                    </span>
                    <span className="sotu-issue-title">{issue.title}</span>
                  </a>
                </td>
                <td style={{ position: 'relative' }}>
                  <button
                    className="sotu-status-btn"
                    style={{ borderColor: STATUS_COLORS[issue.status] || '#666', color: STATUS_COLORS[issue.status] || '#666' }}
                    onClick={() => void handleStatusClick(issue)}
                    disabled={executing}
                  >
                    {issue.raw_status}
                  </button>
                  {transitionTarget?.source_id === issue.source_id && (
                    <div className="sotu-transition-dropdown" ref={dropdownRef}>
                      {loadingTransitions ? (
                        <div className="sotu-transition-loading">Loading...</div>
                      ) : transitions.length === 0 ? (
                        <div className="sotu-transition-loading">No transitions available</div>
                      ) : (
                        transitions.map(t => (
                          <button
                            key={t.id}
                            className="sotu-transition-item"
                            onClick={() => void handleTransition(t)}
                            disabled={executing}
                          >
                            {executing ? 'Updating...' : t.name}
                          </button>
                        ))
                      )}
                    </div>
                  )}
                </td>
                <td><span className="sotu-type-badge">{typeLabel(issue.type)}</span></td>
                <td>{issue.priority || '-'}</td>
                <td className="sotu-updated">{relativeTime(issue.updated)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      </>
    )
  }

  return (
    <div className="sotu-card">
      <div className="sotu-card-header">
        <h2 className="sotu-card-title">My Work</h2>
      </div>
      <div className="sotu-panel">
        {renderBody()}
      </div>
    </div>
  )
}
