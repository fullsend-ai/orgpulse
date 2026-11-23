import { sanitizeHtml } from '../sanitizeHtml'

interface Transfer {
  from: string
  to: string
  hours: string | number
}

interface StagesData {
  transfers?: Transfer[]
  total?: number | null
  note?: string
}

const COLORS = [
  'green',
  'blue',
  'yellow',
  'purple',
  'teal',
  'orange',
  'red',
  'gray',
]

function formatHours(value: string | number): string {
  const num = parseFloat(String(value))
  if (isNaN(num)) return String(value)
  return `${Math.round(num * 10) / 10} hours`
}

export function StagesDiagram({ data }: { data: StagesData }) {
  const transfers = data.transfers || []

  if (transfers.length === 0) {
    return (
      <div className="diagram-table-empty">
        No stage transfer data available.
      </div>
    )
  }

  return (
    <>
      <div className="stages-diagram">
        {transfers.map((t, index) => {
          const fromColor = COLORS[index % COLORS.length]!
          const toColor = COLORS[(index + 1) % COLORS.length]!
          return (
            <div className="stages-row" key={index}>
              <div className="stages-cell stages-from-label">{t.from}</div>
              <div className="stages-cell stages-transfer-cell">
                <div className={`knot-circle ${fromColor}`} />
                <div className={`knot-circle ${toColor}`} />
              </div>
              <div className="stages-cell stages-to-label">{t.to}</div>
              <div className="stages-cell stages-hours">
                {formatHours(t.hours)}
              </div>
            </div>
          )
        })}
      </div>
      {data.total != null && (
        <div className="stages-total">
          <span className="stages-total-label">Total Cycle Time</span>
          <span className="stages-total-value">
            {formatHours(data.total)}
          </span>
        </div>
      )}
      {data.note && (
        <div
          className="stages-note"
          dangerouslySetInnerHTML={{ __html: sanitizeHtml(data.note) }}
        />
      )}
    </>
  )
}

export default StagesDiagram
