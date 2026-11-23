import { sanitizeHtml } from '../sanitizeHtml'

interface Metric {
  label: string
  value: string | number
  explain?: string
  goal?: number | string | null
  informational?: boolean
}

interface StatsData {
  metrics?: Metric[]
}

function formatValue(value: string | number): string | number {
  const str = String(value)
  if (/[^\d.\-eE]/.test(str.trim())) return str
  const num = parseFloat(str)
  if (!isNaN(num)) {
    if (num !== 0 && Math.abs(num) < 1) return Math.floor(num * 100) / 100
    if (Math.abs(num) >= 100) return Math.round(num)
    return Math.floor(num * 10) / 10
  }
  return str
}

export function StatsGrid({ data }: { data: StatsData }) {
  const metrics = data.metrics || []
  if (metrics.length === 0) return null

  return (
    <div className="stats-grid">
      {metrics.map((metric, i) => {
        const displayValue = formatValue(metric.value)
        let goalHtml = ''
        if (
          metric.goal !== undefined &&
          metric.goal !== null &&
          metric.goal !== 'auto'
        ) {
          const goalText = metric.goal + '%'
          const numVal =
            typeof metric.value === 'number' ? metric.value : null
          const numGoal =
            typeof metric.goal === 'number' ? metric.goal : null
          let cls = 'stat-goal'
          if (metric.informational) {
            cls += ' stat-goal--info'
          } else if (numVal !== null && numGoal !== null) {
            cls += numVal >= numGoal ? ' stat-goal--met' : ' stat-goal--missed'
          }
          const infoTag = metric.informational
            ? ' <span class="stat-goal-tag">informational</span>'
            : ''
          goalHtml = `<div class="${cls}">Target: ${goalText}${infoTag}</div>`
        }

        return (
          <div className="stat-card" key={i}>
            <div className="stat-value">{displayValue}</div>
            <div className="stat-label">{metric.label}</div>
            {metric.explain && (
              <div
                className="stat-explain"
                dangerouslySetInnerHTML={{
                  __html: sanitizeHtml(
                    metric.explain.replace(
                      /\d+\.\d+/g,
                      (m) => String(formatValue(parseFloat(m))),
                    ),
                  ),
                }}
              />
            )}
            {goalHtml && (
              <div dangerouslySetInnerHTML={{ __html: sanitizeHtml(goalHtml) }} />
            )}
          </div>
        )
      })}
    </div>
  )
}

export default StatsGrid
