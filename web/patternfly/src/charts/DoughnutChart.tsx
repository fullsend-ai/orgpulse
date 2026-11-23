import { useRef, useEffect } from 'react'
import { Chart as ChartJS } from 'chart.js'
import ChartDataLabels from 'chartjs-plugin-datalabels'
import './chartSetup'

interface DoughnutData {
  labels?: string[]
  datasets?: Record<string, unknown>[]
  note?: string
}

export default function DoughnutChart({ data }: { data: DoughnutData }) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const chartRef = useRef<ChartJS | null>(null)

  useEffect(() => {
    if (!canvasRef.current) return
    if (chartRef.current) chartRef.current.destroy()

    const ctx = canvasRef.current.getContext('2d')
    if (!ctx) return

    chartRef.current = new ChartJS(ctx, {
      type: 'doughnut',
      data: {
        labels: data.labels || [],
        datasets: (data.datasets || []) as never[],
      },
      plugins: [ChartDataLabels as never],
      options: {
        responsive: true,
        maintainAspectRatio: false,
        cutout: '60%',
        plugins: {
          legend: {
            position: 'bottom',
            labels: { padding: 10, font: { size: 12 } },
          },
          tooltip: {
            callbacks: {
              label: function (context: {
                raw: number
                dataset: { data: number[] }
                label: string
              }) {
                const value = context.raw
                const total = context.dataset.data.reduce(
                  (a: number, b: number) => a + b,
                  0,
                )
                const percentage =
                  total > 0 ? Math.round((value / total) * 1000) / 10 : 0
                return `${context.label}: ${percentage}%`
              },
            },
          },
          datalabels: {
            color: '#fff',
            font: { weight: 'bold' as const, size: 14 },
            formatter: function (
              value: number,
              context: { dataset: { data: number[] } },
            ) {
              const total = context.dataset.data.reduce(
                (a: number, b: number) => a + b,
                0,
              )
              if (total === 0 || value === 0) return ''
              const percentage = Math.round((value / total) * 1000) / 10
              return percentage + '%'
            },
          },
        },
      } as never,
    })

    return () => {
      chartRef.current?.destroy()
      chartRef.current = null
    }
  }, [data])

  return (
    <>
      <div className="doughnut-diagram">
        <canvas ref={canvasRef} />
      </div>
      {data.note && <div className="doughnut-note">{data.note}</div>}
    </>
  )
}

export { DoughnutChart }
