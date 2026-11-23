import { useRef, useEffect, useState, useCallback } from 'react'
import { Chart as ChartJS } from 'chart.js'
import './chartSetup'
import { Portal } from '../Portal'
import { safeHref } from '../safeUrl'

interface SankeyDataset {
  label?: string
  data?: Array<{
    from: string
    to: string
    flow: number
    count?: number
  }>
  color?: Record<string, string>
  colorMode?: string
  labels?: Record<string, string>
  priority?: Record<string, number>
  column?: Record<string, number>
  size?: string
  minFlow?: number
}

interface SankeyFlow {
  from: string
  to: string
  flow: number
  count?: number
  realFlow?: number
}

const DEFAULT_MIN_FLOW_PERCENT = 2

// Proportionally tiny flows render as hairlines and look broken, so each flow is
// drawn at least minPercent of the total. Tooltips keep the real value.
function applyMinFlow(flows: SankeyFlow[], minPercent: number): SankeyFlow[] {
  if (!(minPercent > 0)) return flows
  const targets = new Set(flows.map((f) => f.to))
  const total = flows.filter((f) => !targets.has(f.from)).reduce((sum, f) => sum + (f.flow || 0), 0)
  const floor = (total * minPercent) / 100
  if (!(floor > 0)) return flows
  return flows.map((f) =>
    f.flow > 0 && f.flow < floor ? { ...f, flow: floor, realFlow: f.flow } : f,
  )
}

interface ExtraConfig {
  location: string
  label?: string
  elements?: Record<string, Array<{ title: string; url?: string }>>
}

interface SankeyData {
  datasets?: SankeyDataset[]
  extra?: {
    config?: ExtraConfig[]
  } | null
}

interface ExtraListState {
  title: string
  items: Array<{ title: string; url?: string }>
}

export default function SankeyChart({ data }: { data: SankeyData }) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const chartRef = useRef<ChartJS | null>(null)
  const [extraList, setExtraList] = useState<ExtraListState | null>(null)

  const extra = data.extra || null
  const inDiagramCfg = extra?.config?.find(
    (c) => c.location === 'inDiagram',
  ) as ExtraConfig | undefined

  const findFlowAtPoint = useCallback(
    (chart: ChartJS, e: MouseEvent): string | null => {
      const meta = chart.getDatasetMeta(0)
      if (!meta?.data) return null
      const datasets = data.datasets || []
      const dataset = datasets[0]
      if (!dataset?.data) return null
      const rect = chart.canvas.getBoundingClientRect()
      const x = e.clientX - rect.left
      const y = e.clientY - rect.top
      for (let i = 0; i < meta.data.length; i++) {
        const el = meta.data[i] as unknown as {
          x: number
          x2: number
          y: number
          height: number
        }
        if (el && el.x != null && el.x2 != null) {
          if (x >= el.x && x <= el.x2 && y >= el.y && y <= el.y + el.height) {
            const item = dataset.data[i]
            if (item) return `${item.from}→${item.to}`
          }
        }
      }
      return null
    },
    [data.datasets],
  )

  useEffect(() => {
    if (!canvasRef.current) return
    if (chartRef.current) chartRef.current.destroy()

    const ctx = canvasRef.current.getContext('2d')
    if (!ctx) return

    const datasets = data.datasets || []
    if (datasets.length === 0) return

    const dataset = datasets[0]!

    chartRef.current = new ChartJS(ctx, {
      type: 'sankey' as never,
      data: {
        datasets: [
          {
            label: dataset.label || 'Flow',
            data: applyMinFlow(dataset.data || [], dataset.minFlow ?? DEFAULT_MIN_FLOW_PERCENT),
            colorFrom: (c: {
              dataset: { data: Array<{ from: string }> }
              dataIndex: number
            }) => {
              const colors = dataset.color || {}
              const from = c.dataset.data[c.dataIndex]?.from
              return (from && colors[from]) || '#3498db'
            },
            colorTo: (c: {
              dataset: { data: Array<{ to: string }> }
              dataIndex: number
            }) => {
              const colors = dataset.color || {}
              const to = c.dataset.data[c.dataIndex]?.to
              return (to && colors[to]) || '#2ecc71'
            },
            colorMode: dataset.colorMode || 'gradient',
            labels: dataset.labels || {},
            priority: dataset.priority || {},
            column: dataset.column || {},
            size: dataset.size || 'max',
          },
        ] as never[],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        // Node labels are vertically centered on their node; keep labels of
        // thin nodes at the top or bottom edge from being clipped.
        layout: { padding: { top: 10, bottom: 10 } },
        plugins: {
          datalabels: { display: false },
          tooltip: {
            callbacks: {
              label: function (context: {
                dataset: {
                  data: SankeyFlow[]
                  labels?: Record<string, string>
                }
                dataIndex: number
              }) {
                const item = context.dataset.data[context.dataIndex]!
                const labels = context.dataset.labels || {}
                const from = labels[item.from] || item.from
                const to = labels[item.to] || item.to
                const flow = item.realFlow ?? item.flow
                const count =
                  item.count != null
                    ? `${item.count} PRs (${flow}%)`
                    : `${flow}%`
                return `${from} → ${to}: ${count}`
              },
            },
          },
        },
      } as never,
    })

    const canvas = canvasRef.current
    const handleClick = (e: MouseEvent) => {
      if (!inDiagramCfg?.elements || !chartRef.current) return
      e.stopPropagation()
      const flowKey = findFlowAtPoint(chartRef.current, e)
      if (flowKey && inDiagramCfg.elements[flowKey]) {
        setExtraList({
          title: flowKey,
          items: inDiagramCfg.elements[flowKey]!,
        })
      }
    }
    const handleMouseMove = (e: MouseEvent) => {
      if (!inDiagramCfg?.elements || !chartRef.current) return
      const flowKey = findFlowAtPoint(chartRef.current, e)
      canvas.style.cursor =
        flowKey && inDiagramCfg.elements[flowKey] ? 'pointer' : ''
    }

    if (inDiagramCfg?.elements) {
      canvas.addEventListener('click', handleClick)
      canvas.addEventListener('mousemove', handleMouseMove)
    }

    return () => {
      canvas.removeEventListener('click', handleClick)
      canvas.removeEventListener('mousemove', handleMouseMove)
      chartRef.current?.destroy()
      chartRef.current = null
    }
  }, [data, inDiagramCfg, findFlowAtPoint])

  return (
    <>
      <div className="sankey-diagram">
        <canvas ref={canvasRef} />
      </div>
      {extraList && (
        <Portal>
        <div className="lightbox-overlay" onClick={() => setExtraList(null)}>
          <div className="lightbox-content" onClick={(e) => e.stopPropagation()}>
            <button className="lightbox-close" onClick={() => setExtraList(null)}>&times;</button>
            <h3>{extraList.title}</h3>
            <ul className="pr-list">
              {extraList.items.map((item, i) => (
                <li key={i}>
                  {safeHref(item.url) ? (
                    <a href={safeHref(item.url)} target="_blank" rel="noopener noreferrer">{item.title}</a>
                  ) : (
                    item.title
                  )}
                </li>
              ))}
            </ul>
          </div>
        </div>
        </Portal>
      )}
    </>
  )
}

export { SankeyChart }
