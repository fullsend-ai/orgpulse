import { useEffect, useRef } from 'react'

interface Stage {
  label: string
  value: string | number
  icon?: string
}

interface KnotsData {
  stages?: Stage[]
}

const COLORS = ['green', 'yellow', 'blue', 'purple']
const ICONS = ['📋', '⏱️', '👁️', '🔗']

function formatHours(value: string | number): string {
  const num = parseFloat(String(value))
  if (isNaN(num)) return String(value)
  return `${Math.round(num * 10) / 10} hours`
}

export function KnotsDiagram({ data }: { data: KnotsData }) {
  const ref = useRef<HTMLDivElement>(null)
  const stages = data.stages || []

  useEffect(() => {
    if (!ref.current) return
    const diagram = ref.current
    const circles = diagram.querySelectorAll('.knot-circle')
    if (circles.length < 2) return
    const rect = diagram.getBoundingClientRect()
    const first = circles[0]!.getBoundingClientRect()
    const last = circles[circles.length - 1]!.getBoundingClientRect()
    diagram.style.setProperty(
      '--line-left',
      first.left + first.width / 2 - rect.left + 'px',
    )
    diagram.style.setProperty(
      '--line-right',
      rect.right - (last.left + last.width / 2) + 'px',
    )
  }, [stages])

  if (stages.length === 0) return null

  return (
    <div className="knots-diagram" ref={ref}>
      {stages.map((stage, index) => {
        const color = COLORS[index % COLORS.length]!
        const icon = stage.icon || ICONS[index % ICONS.length]!
        return (
          <div className="knot-item" key={index}>
            <div className="knot-label">{stage.label}</div>
            <div className={`knot-circle ${color}`}>{icon}</div>
            <div className="knot-value">{formatHours(stage.value)}</div>
          </div>
        )
      })}
    </div>
  )
}

export default KnotsDiagram
