import { useState, useEffect, useCallback } from 'react'
import { fetchMyLinks, type QuickLink } from './api'
import SourceIcon from './SourceIcon'
import { safeHref, safeImageSrc } from './safeUrl'

const kindLabel: Record<string, string> = {
  org: 'Organizations',
  group: 'Groups',
  project: 'Projects',
  filter: 'Saved Filters',
  starred: 'Starred',
}

export default function QuickLinks() {
  const [links, setLinks] = useState<QuickLink[]>([])
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const data = await fetchMyLinks()
      setLinks(data.links)
    } catch {
      setLinks([])
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  if (loading) {
    return (
      <div className="sotu-card qlinks-card">
        <div className="sotu-card-header">
          <h2 className="sotu-card-title">Quick Links</h2>
        </div>
        <div className="qlinks-body">
          <div className="settings-loader">
            <div className="diagram-card__spinner" />
          </div>
        </div>
      </div>
    )
  }

  if (links.length === 0) {
    return (
      <div className="sotu-card qlinks-card">
        <div className="sotu-card-header">
          <h2 className="sotu-card-title">Quick Links</h2>
        </div>
        <div className="qlinks-body">
          <div className="contrib-empty">
            <p>No links available.</p>
          </div>
        </div>
      </div>
    )
  }

  const grouped = new Map<string, QuickLink[]>()
  for (const link of links) {
    const key = kindLabel[link.kind] || link.kind
    const list = grouped.get(key) || []
    list.push(link)
    grouped.set(key, list)
  }

  return (
    <div className="sotu-card qlinks-card">
      <div className="sotu-card-header">
        <h2 className="sotu-card-title">Quick Links</h2>
      </div>
      <div className="qlinks-body">
        {[...grouped.entries()].map(([section, items]) => (
          <div key={section} className="qlinks-section">
            <div className="qlinks-section-title">{section}</div>
            {items.map(link => (
              <a
                key={`${link.source}-${link.kind}-${link.name}`}
                href={safeHref(link.url)}
                target="_blank"
                rel="noopener noreferrer"
                className="qlinks-item"
              >
                {safeImageSrc(link.icon)
                  ? <img src={safeImageSrc(link.icon)} alt="" className="qlinks-icon-img" />
                  : <SourceIcon source={link.source} size={14} />
                }
                <span className="qlinks-name">{link.name}</span>
              </a>
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}
