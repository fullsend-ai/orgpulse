import { useRef, useEffect } from 'react'
import { Chart as ChartJS } from 'chart.js'
import './chartSetup'

interface ScatterDataset {
  data?: Array<{ x: string | number; y: string | number }>
  [key: string]: unknown
}

interface ScatterData {
  datasets?: ScatterDataset[]
  options?: Record<string, unknown>
}

export default function ScatterChart({ data }: { data: ScatterData }) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const chartRef = useRef<ChartJS | null>(null)

  useEffect(() => {
    if (!canvasRef.current) return
    if (chartRef.current) chartRef.current.destroy()

    const ctx = canvasRef.current.getContext('2d')
    if (!ctx) return

    const defaultOptions: Record<string, unknown> = {
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: {
          display: true,
          position: 'top',
          labels: { padding: 15, font: { size: 12 } },
        },
        tooltip: { mode: 'point', intersect: false },
      },
      scales: {
        x: {
          type: 'linear',
          position: 'bottom',
          beginAtZero: true,
          grid: { color: 'rgba(0, 0, 0, 0.1)' },
        },
        y: {
          type: 'linear',
          beginAtZero: true,
          grid: { color: 'rgba(0, 0, 0, 0.1)' },
        },
      },
      interaction: { mode: 'nearest', intersect: false },
    }

    const chartOptions = data.options
      ? { ...defaultOptions, ...data.options }
      : defaultOptions

    const processedDatasets = (data.datasets || []).map((dataset) => {
      const processed = { ...dataset }
      if (Array.isArray(processed.data)) {
        processed.data = processed.data.map((point) => {
          if (typeof point === 'object' && point !== null) {
            return {
              x: typeof point.x === 'string' ? parseFloat(point.x) : point.x,
              y: typeof point.y === 'string' ? parseFloat(point.y) : point.y,
            }
          }
          return point
        })
      }
      return processed
    })

    chartRef.current = new ChartJS(ctx, {
      type: 'scatter',
      data: { datasets: processedDatasets as never[] },
      options: chartOptions as never,
    })

    return () => {
      chartRef.current?.destroy()
      chartRef.current = null
    }
  }, [data])

  return (
    <div className="scatter-diagram">
      <canvas ref={canvasRef} />
    </div>
  )
}

export { ScatterChart }
