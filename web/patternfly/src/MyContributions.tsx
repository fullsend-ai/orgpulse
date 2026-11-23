import { useState, useEffect, useCallback } from 'react'
import { fetchMyContributions, type UnifiedIssue } from './api'
import SourceIcon from './SourceIcon'
import { safeHref } from './safeUrl'

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

export default function MyContributions() {
  const [issues, setIssues] = useState<UnifiedIssue[]>([])
  const [hasTokens, setHasTokens] = useState(true)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await fetchMyContributions()
      setIssues(data.issues)
      setHasTokens(data.has_tokens)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const renderBody = () => {
    if (loading) {
      return (
        <div className="settings-loader">
          <div className="diagram-card__spinner" />
        </div>
      )
    }

    if (error) {
      return (
        <div className="contrib-empty">
          <p>{error}</p>
          <button className="sub-btn sub-btn--secondary" onClick={() => void load()}>Retry</button>
        </div>
      )
    }

    if (!hasTokens || issues.length === 0) {
      return (
        <div className="contrib-empty">
          <p>{!hasTokens ? 'No personal tokens configured.' : 'No contributions found.'}</p>
        </div>
      )
    }

    return (
      <div className="contrib-list">
        {issues.map(issue => (
          <a
            key={`${issue.source}-${issue.source_id}`}
            href={safeHref(issue.url)}
            target="_blank"
            rel="noopener noreferrer"
            className="contrib-item"
          >
            <SourceIcon source={issue.source} size={14} />
            <span className="contrib-key">{issue.key}</span>
            <span className="contrib-title">{issue.title}</span>
            <span className="contrib-time">{relativeTime(issue.updated)}</span>
          </a>
        ))}
      </div>
    )
  }

  return (
    <div className="sotu-card contrib-card">
      <div className="sotu-card-header">
        <h2 className="sotu-card-title">My Contributions</h2>
      </div>
      <div className="contrib-body">
        {renderBody()}
      </div>
    </div>
  )
}
