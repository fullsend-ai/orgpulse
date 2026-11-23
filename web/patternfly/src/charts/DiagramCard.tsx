import { useState, useCallback, useRef, useEffect, useLayoutEffect, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import type { DataLink, ExtraSource, ExtraSourceLabel } from './DiagramGrid'
import { fetchAiAnalytics } from '../api'
import { sanitizeHtml } from '../sanitizeHtml'
import { safeHref } from '../safeUrl'

interface DiagramCardProps {
  id: string
  documentationUrl?: string
  span?: number
  label?: string
  description?: string
  warning?: string
  loading: boolean
  error?: string | null
  noData?: boolean
  datalinks?: DataLink[]
  extraSources?: ExtraSource[]
  aiEnabled?: boolean
  diagramData?: unknown
  children: ReactNode
  onReload?: () => void
  onExpand?: () => void
}


function ExtraSourcePopup({ source, anchorRect, onClose }: {
  source: ExtraSource
  anchorRect: { top: number; right: number; bottom: number }
  onClose: () => void
}) {
  const popupRef = useRef<HTMLDivElement | null>(null)
  const copyTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [copyStatus, setCopyStatus] = useState<'idle' | 'copied' | 'error'>('idle')
  const [position, setPosition] = useState({ top: 8, left: 8, ready: false })
  const cols: ExtraSourceLabel[] = source.labels.length > 0 ? source.labels : Object.keys(source.rows[0] || {}).map(k => ({ field: k, label: k }))

  useEffect(() => () => {
    if (copyTimeout.current) clearTimeout(copyTimeout.current)
  }, [])

  const copyTable = async () => {
    const values = [
      cols.map(col => col.label),
      ...source.rows.map(row => cols.map(col => {
        const value = row[col.field]
        if (value == null) return ''
        return typeof value === 'number' && !Number.isInteger(value) ? Number(value).toFixed(4) : String(value)
      })),
    ]
    const text = values.map(row => row.map(value => value.replace(/[\t\r\n]+/g, ' ')).join('\t')).join('\n')

    try {
      await navigator.clipboard.writeText(text)
      setCopyStatus('copied')
    } catch {
      setCopyStatus('error')
    }
    if (copyTimeout.current) clearTimeout(copyTimeout.current)
    copyTimeout.current = setTimeout(() => setCopyStatus('idle'), 2000)
  }

  const updatePosition = useCallback(() => {
    const popup = popupRef.current
    if (!popup) return

    const margin = 8
    const gap = 4
    const rect = popup.getBoundingClientRect()
    const left = Math.max(margin, Math.min(anchorRect.right - rect.width, window.innerWidth - margin - rect.width))
    let top = anchorRect.bottom + gap
    if (top + rect.height > window.innerHeight - margin) top = anchorRect.top - rect.height - gap
    top = Math.max(margin, Math.min(top, window.innerHeight - margin - rect.height))
    setPosition({ top, left, ready: true })
  }, [anchorRect])

  useLayoutEffect(() => {
    updatePosition()
    window.addEventListener('resize', updatePosition)
    window.addEventListener('scroll', updatePosition, true)
    return () => {
      window.removeEventListener('resize', updatePosition)
      window.removeEventListener('scroll', updatePosition, true)
    }
  }, [updatePosition])

  return createPortal(
    <>
      <div className="source-data-popup-backdrop" onClick={onClose} />
      <div
        ref={popupRef}
        className="source-data-popup"
        role="dialog"
        aria-label={source.template}
        style={{ top: position.top, left: position.left, visibility: position.ready ? 'visible' : 'hidden' }}
      >
        <div className="source-data-popup__header">
          <span>{source.template}</span>
          <div className="source-data-popup__header-actions">
            <button
              type="button"
              className="source-data-popup__copy"
              aria-label={copyStatus === 'copied' ? 'Copied table data' : copyStatus === 'error' ? 'Could not copy table data' : 'Copy table data'}
              title={copyStatus === 'copied' ? 'Copied' : copyStatus === 'error' ? 'Copy failed' : 'Copy table data'}
              onClick={() => void copyTable()}
            >
              {copyStatus === 'copied' ? (
                <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
                  <path d="m3 8 3.2 3.2L13 4.5" />
                </svg>
              ) : (
                <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.3" aria-hidden="true">
                  <rect x="5.5" y="1.5" width="9" height="11" rx="1" />
                  <path d="M10.5 4.5H3A1.5 1.5 0 0 0 1.5 6v8A1.5 1.5 0 0 0 3 15.5h6A1.5 1.5 0 0 0 10.5 14v-1.5" />
                </svg>
              )}
            </button>
            <button type="button" className="source-data-popup__close" aria-label="Close source data" onClick={onClose}>&times;</button>
          </div>
        </div>
        <div className="source-data-popup__body">
          <table className="source-data-table">
            <thead>
              <tr>
                {cols.map(col => (
                  <th key={col.field}>{col.label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {source.rows.map((row, i) => (
                <tr key={i}>
                  {cols.map(col => {
                    const val = row[col.field]
                    const str = val == null ? '' : String(val)
                    const text = typeof val === 'number' && !Number.isInteger(val) ? Number(val).toFixed(4) : str
                    const href = linkTarget(col.link ? row[col.link] : val)
                    return (
                      <td key={col.field}>
                        {href && text
                          ? <a href={href} target="_blank" rel="noopener noreferrer">{text}</a>
                          : text
                        }
                      </td>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </>,
    document.body,
  )
}

function linkTarget(value: unknown): string | null {
  return typeof value === 'string' && /^https?:\/\//.test(value) ? value : null
}

function useHoverWithDelay(delayMs = 300) {
  const [visible, setVisible] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const onEnter = useCallback(() => {
    if (timer.current) { clearTimeout(timer.current); timer.current = null }
    setVisible(true)
  }, [])

  const onLeave = useCallback(() => {
    timer.current = setTimeout(() => setVisible(false), delayMs)
  }, [delayMs])

  return { visible, onEnter, onLeave }
}

export default function DiagramCard({
  id,
  documentationUrl,
  span,
  label,
  description,
  warning,
  loading,
  error,
  noData,
  datalinks,
  extraSources,
  aiEnabled,
  diagramData,
  children,
  onReload,
  onExpand,
}: DiagramCardProps) {
  const helpBubble = useHoverWithDelay(300)
  const [tooltipPos, setTooltipPos] = useState({ top: 0, left: 0 })
  const actionsPillRef = useRef<HTMLDivElement | null>(null)
  const actionsBubbleRef = useRef<HTMLDivElement | null>(null)
  const [actionsTooltipPos, setActionsTooltipPos] = useState({ top: 0, left: 0 })
  const [openExtra, setOpenExtra] = useState<{
    source: ExtraSource
    anchorRect: { top: number; right: number; bottom: number }
  } | null>(null)
  const bubble = useHoverWithDelay(300)
  const actionsBubble = useHoverWithDelay(300)
  const [aiHtml, setAiHtml] = useState<string | null>(null)
  const [aiLoading, setAiLoading] = useState(false)
  const aiFetchedRef = useRef(false)

  const updateActionsTooltipPos = useCallback(() => {
    const anchor = actionsPillRef.current?.getBoundingClientRect()
    const bubbleElement = actionsBubbleRef.current
    if (!anchor || !bubbleElement) return

    const margin = 8
    const gap = 6
    const main = document.querySelector('.app-main')?.getBoundingClientRect()
    const minLeft = Math.max(margin, (main?.left ?? 0) + margin)
    const maxRight = Math.min(window.innerWidth, main?.right ?? window.innerWidth) - margin
    const availableWidth = Math.max(0, maxRight - minLeft)
    bubbleElement.style.minWidth = `${Math.min(260, availableWidth)}px`
    bubbleElement.style.maxWidth = `${Math.min(400, availableWidth)}px`
    const bubble = bubbleElement.getBoundingClientRect()
    const width = bubble.width
    const left = Math.max(minLeft, Math.min(anchor.right - width, maxRight - width))
    const below = anchor.bottom + gap
    const top = below + bubble.height <= window.innerHeight - margin
      ? below
      : Math.max(margin, anchor.top - bubble.height - gap)
    setActionsTooltipPos({ top, left })
  }, [])

  useLayoutEffect(() => {
    if (!actionsBubble.visible) return
    updateActionsTooltipPos()
    window.addEventListener('resize', updateActionsTooltipPos)
    window.addEventListener('scroll', updateActionsTooltipPos, true)
    return () => {
      window.removeEventListener('resize', updateActionsTooltipPos)
      window.removeEventListener('scroll', updateActionsTooltipPos, true)
    }
  }, [actionsBubble.visible, aiLoading, aiHtml, updateActionsTooltipPos])

  useEffect(() => {
    if (!actionsBubble.visible || !aiEnabled || aiFetchedRef.current) return
    aiFetchedRef.current = true
    setAiLoading(true)
    const ctrl = new AbortController()
    fetchAiAnalytics(diagramData, ctrl.signal)
      .then(html => { setAiHtml(html); setAiLoading(false) })
      .catch(err => {
        if ((err as Error).name !== 'AbortError') {
          setAiHtml('<div style="padding:12px;color:#cb2431;font-size:13px;">Failed to load AI analytics.</div>')
          setAiLoading(false)
        }
      })
    return () => ctrl.abort()
  }, [actionsBubble.visible, aiEnabled, diagramData])

  const spanClass =
    span === 2
      ? ' diagram-card--span-2'
      : span === 3
        ? ' diagram-card--span-3'
        : ''

  const labelHref = (label || '').match(/<a\b[^>]*href\s*=\s*["'](https?:\/\/[^"']+)["'][^>]*>/i)?.[1]
    || (description || '').match(/<a\b[^>]*href\s*=\s*["'](https?:\/\/[^"']+)["'][^>]*>/i)?.[1]
    || documentationUrl
  const cleanLabel = label
    ? label.replace(/<a\b[^>]*>.*?<\/a>/gi, '').replace(/<img\b[^>]*\/?>/gi, '').replace(/\s+\(averages\)/gi, '').trim()
    : undefined
  const isPrStagesCard = cleanLabel === 'Where PRs Spend Their Time'

  const handleHelpEnter = useCallback(
    (e: React.MouseEvent<HTMLElement> | React.FocusEvent<HTMLElement>) => {
      if (!description && !labelHref) return
      const rect = (e.target as HTMLElement).getBoundingClientRect()
      setTooltipPos({
        top: rect.bottom + 8,
        left: Math.max(8, rect.right - 280),
      })
      helpBubble.onEnter()
    },
    [description, labelHref, helpBubble.onEnter],
  )

  const hasDatalinks = datalinks && datalinks.length > 0
  const hasExtraSources = extraSources && extraSources.length > 0
  const showPill = (hasDatalinks || hasExtraSources) && !loading && !error && !noData
  const showActions = aiEnabled && !loading && !error && !noData

  return (
    <div className={`diagram-card${spanClass}${isPrStagesCard ? ' diagram-card--pr-stages' : ''}`} id={`${id}-card`}>
      {cleanLabel && (
        <div className="diagram-card__header">
          <span dangerouslySetInnerHTML={{ __html: sanitizeHtml(cleanLabel) }} />
          {(description || labelHref) && (
            labelHref ? (
              <a
                href={labelHref}
                target="_blank"
                rel="noopener noreferrer"
                className="diagram-card__help"
                aria-label="More information"
                aria-describedby={helpBubble.visible ? `${id}-help` : undefined}
                onMouseEnter={handleHelpEnter}
                onMouseLeave={helpBubble.onLeave}
                onFocus={handleHelpEnter}
                onBlur={helpBubble.onLeave}
              >
                ?
              </a>
            ) : (
              <button
                type="button"
                className="diagram-card__help"
                aria-label="More information"
                aria-describedby={helpBubble.visible ? `${id}-help` : undefined}
                onMouseEnter={handleHelpEnter}
                onMouseLeave={helpBubble.onLeave}
                onFocus={handleHelpEnter}
                onBlur={helpBubble.onLeave}
              >
                ?
              </button>
            )
          )}
          <div className="diagram-card__actions">
            {showActions && (
              <div
                ref={actionsPillRef}
                className="actions-pill-wrapper"
                onMouseEnter={actionsBubble.onEnter}
                onMouseLeave={actionsBubble.onLeave}
              >
                <div className="actions-pill">
                  <svg className="actions-pill__icon" viewBox="0 0 16 16" width="12" height="12" fill="currentColor">
                    <path d="M8 1a.5.5 0 0 1 .5.5V4a.5.5 0 0 1-1 0V1.5A.5.5 0 0 1 8 1zm3.354 1.646a.5.5 0 0 1 0 .708l-1.768 1.768a.5.5 0 1 1-.708-.708l1.768-1.768a.5.5 0 0 1 .708 0zM14.5 7.5a.5.5 0 0 1 0 1H12a.5.5 0 0 1 0-1h2.5zM8 11a.5.5 0 0 1 .5.5V14a.5.5 0 0 1-1 0v-2.5A.5.5 0 0 1 8 11zm-4.354-.646a.5.5 0 0 1 0-.708l1.768-1.768a.5.5 0 1 1 .708.708L4.354 10.354a.5.5 0 0 1-.708 0zM1.5 7.5a.5.5 0 0 0 0 1H4a.5.5 0 0 0 0-1H1.5zM8 5a3 3 0 1 0 0 6 3 3 0 0 0 0-6z" />
                  </svg>
                  Actions
                </div>
                {actionsBubble.visible && typeof document !== 'undefined' && createPortal(
                  <div
                    ref={actionsBubbleRef}
                    className="actions-bubble actions-bubble--visible"
                    style={{ top: actionsTooltipPos.top, left: actionsTooltipPos.left }}
                    onMouseEnter={actionsBubble.onEnter}
                    onMouseLeave={actionsBubble.onLeave}
                  >
                    <div className="actions-bubble__header">AI Insights</div>
                    <div className="actions-bubble__body">
                      {aiLoading ? (
                        <div className="actions-bubble__loading">Analyzing...</div>
                      ) : aiHtml ? (
                        <div dangerouslySetInnerHTML={{ __html: sanitizeHtml(aiHtml) }} />
                      ) : null}
                    </div>
                  </div>,
                  document.body,
                )}
              </div>
            )}
            {showPill && (
              <div
                className="source-data-pill-wrapper"
                onMouseEnter={bubble.onEnter}
                onMouseLeave={bubble.onLeave}
              >
                <div className="source-data-pill">
                  <svg className="source-data-pill__icon" viewBox="0 0 16 16" width="12" height="12" fill="currentColor">
                    <path d="M1 3.5A1.5 1.5 0 0 1 2.5 2h3.879a1.5 1.5 0 0 1 1.06.44l1.122 1.12A1.5 1.5 0 0 0 9.62 4H13.5A1.5 1.5 0 0 1 15 5.5v7a1.5 1.5 0 0 1-1.5 1.5h-11A1.5 1.5 0 0 1 1 12.5v-9z" />
                  </svg>
                  Source Data
                </div>
                {bubble.visible && (
                  <div className="source-data-bubble source-data-bubble--visible">
                    <div className="source-data-bubble__header">Source Data</div>
                    <div className="source-data-bubble__list">
                      {datalinks?.map((link, i) => (
                        <a
                          key={`dl-${i}`}
                          href={safeHref(link.url)}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="source-data-bubble__item"
                        >
                          {link.label}
                          <svg className="source-data-bubble__arrow" viewBox="0 0 16 16" width="12" height="12" fill="currentColor">
                            <path d="M6.22 3.22a.75.75 0 0 1 1.06 0l4.25 4.25a.75.75 0 0 1 0 1.06l-4.25 4.25a.75.75 0 0 1-1.06-1.06L9.94 8 6.22 4.28a.75.75 0 0 1 0-1.06z" />
                          </svg>
                        </a>
                      ))}
                      {extraSources?.map((src, i) => (
                        <button
                          key={`ex-${i}`}
                          className="source-data-bubble__item source-data-bubble__item--btn"
                          onClick={event => {
                            const rect = event.currentTarget.getBoundingClientRect()
                            setOpenExtra({ source: src, anchorRect: { top: rect.top, right: rect.right, bottom: rect.bottom } })
                          }}
                        >
                          {src.template}
                          <span className="source-data-bubble__count">{src.rows.length}</span>
                        </button>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}
            {onExpand && (
              <button
                className="diagram-card__expand-btn"
                title="Expand diagram"
                onClick={onExpand}
              >
                <svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor">
                  <path d="M1.5 1h4a.5.5 0 0 1 0 1H2.707l3.147 3.146a.5.5 0 1 1-.708.708L2 2.707V5.5a.5.5 0 0 1-1 0v-4a.5.5 0 0 1 .5-.5zm13 0a.5.5 0 0 1 .5.5v4a.5.5 0 0 1-1 0V2.707l-3.146 3.147a.5.5 0 1 1-.708-.708L13.293 2H10.5a.5.5 0 0 1 0-1h4zm-13 14a.5.5 0 0 1-.5-.5v-4a.5.5 0 0 1 1 0v2.793l3.146-3.147a.5.5 0 1 1 .708.708L2.707 14H5.5a.5.5 0 0 1 0 1h-4zm13 0h-4a.5.5 0 0 1 0-1h2.793l-3.147-3.146a.5.5 0 0 1 .708-.708L14 13.293V10.5a.5.5 0 0 1 1 0v4a.5.5 0 0 1-.5.5z"/>
                </svg>
              </button>
            )}
            {onReload && (
              <button
                className="diagram-card__reload-btn"
                title="Reload this metric"
                onClick={onReload}
              >
                &#x21bb;
              </button>
            )}
          </div>
        </div>
      )}

      {warning && (
        <div className="diagram-card__warning">{warning}</div>
      )}

      <div className={`diagram-card__loader${loading || error || noData ? '' : ' diagram-card__loader--hidden'}`}>
        {error ? (
          <>
            <div className="diagram-card__loader-text diagram-card__loader-text--error">{error}</div>
            {onReload && (
              <button className="diagram-card__reload-inline" onClick={onReload}>&#x21bb;</button>
            )}
          </>
        ) : noData ? (
          <>
            <div className="diagram-card__loader-text diagram-card__loader-text--nodata">No data available for this time period</div>
            {onReload && (
              <button className="diagram-card__reload-inline" onClick={onReload}>&#x21bb;</button>
            )}
          </>
        ) : (
          <>
            <div className="diagram-card__spinner" />
            <div className="diagram-card__loader-text">Loading diagram...</div>
          </>
        )}
      </div>

      <div className="diagram-card__content" id={id}>
        {children}
      </div>

      {helpBubble.visible && (description || labelHref) && (
        <div
          id={`${id}-help`}
          role="tooltip"
          className="diagram-card__tooltip"
          onMouseEnter={helpBubble.onEnter}
          onMouseLeave={helpBubble.onLeave}
          onFocus={helpBubble.onEnter}
          onBlur={helpBubble.onLeave}
          style={{
            position: 'fixed',
            opacity: 1,
            top: tooltipPos.top,
            left: tooltipPos.left,
          }}
        >
          {description && <div dangerouslySetInnerHTML={{
            __html: sanitizeHtml(description.replace(/<(?!br\s*\/?>)[^>]*>/gi, '')),
          }} />}
          {labelHref && (
            <a className="diagram-card__documentation" href={labelHref} target="_blank" rel="noopener noreferrer">
              Click for more information ↗
            </a>
          )}
        </div>
      )}

      {openExtra && (
        <ExtraSourcePopup source={openExtra.source} anchorRect={openExtra.anchorRect} onClose={() => setOpenExtra(null)} />
      )}
    </div>
  )
}
