import { useRef, useEffect } from 'react'
import { Chart as ChartJS } from 'chart.js'
import ChartDataLabels from 'chartjs-plugin-datalabels'
import {
  STAGE_DOUGHNUT_COLOR_MAP,
  stageDoughnutFallbackColor,
  formatTime,
} from './chartSetup'
import './chartSetup'

interface Stage {
  label: string
  value: number
}

interface StageDoughnutData {
  stages?: Stage[]
  resolvedCount?: number
  note?: string
}

export default function StageDoughnut({ data }: { data: StageDoughnutData }) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const chartRef = useRef<ChartJS | null>(null)

  const stages = data.stages || []
  const resolvedCount = data.resolvedCount || 0

  useEffect(() => {
    if (!canvasRef.current || stages.length === 0) return
    if (chartRef.current) chartRef.current.destroy()

    const ctx = canvasRef.current.getContext('2d')
    if (!ctx) return

    let fallbackIdx = 0
    const labels = stages.map((s) => s.label)
    const values = stages.map((s) => s.value)
    const colors = stages.map((s) => {
      if (STAGE_DOUGHNUT_COLOR_MAP[s.label])
        return STAGE_DOUGHNUT_COLOR_MAP[s.label]!
      return stageDoughnutFallbackColor(fallbackIdx++)
    })

    const centerTextPlugin = {
      id: 'stageDoughnutCenterText',
      afterDraw(chart: ChartJS) {
        const { ctx: chartCtx, chartArea } = chart
        const centerX = (chartArea.left + chartArea.right) / 2
        const centerY = (chartArea.top + chartArea.bottom) / 2
        chartCtx.save()
        chartCtx.textAlign = 'center'
        chartCtx.textBaseline = 'middle'
        chartCtx.font = 'bold 20px sans-serif'
        chartCtx.fillStyle = '#151515'
        chartCtx.fillText(String(resolvedCount), centerX, centerY - 8)
        chartCtx.font = '11px sans-serif'
        chartCtx.fillStyle = '#6a6e73'
        chartCtx.fillText('resolved', centerX, centerY + 12)
        chartCtx.restore()
      },
    }

    chartRef.current = new ChartJS(ctx, {
      type: 'doughnut',
      data: {
        labels,
        datasets: [{ data: values, backgroundColor: colors }],
      },
      plugins: [ChartDataLabels as never, centerTextPlugin as never],
      options: {
        responsive: true,
        maintainAspectRatio: false,
        cutout: '55%',
        plugins: {
          legend: {
            position: 'bottom',
            labels: {
              padding: 15,
              font: { size: 11 },
              usePointStyle: true,
              pointStyle: 'circle',
              filter: function (
                item: { index: number },
                chartData: { datasets: Array<{ data: number[] }> },
              ) {
                const total = chartData.datasets[0]!.data.reduce(
                  (a: number, b: number) => a + b,
                  0,
                )
                return (
                  total > 0 &&
                  chartData.datasets[0]!.data[item.index]! / total >= 0.01
                )
              },
            },
          },
          tooltip: {
            callbacks: {
              label: function (context: {
                raw: number
                dataset: { data: number[] }
                label: string
              }) {
                const hours = context.raw
                const total = context.dataset.data.reduce(
                  (a: number, b: number) => a + b,
                  0,
                )
                const pct =
                  total > 0 ? Math.round((hours / total) * 1000) / 10 : 0
                return `${context.label}: ${pct}% (median ${formatTime(hours)})`
              },
            },
          },
          datalabels: {
            color: '#fff',
            font: { weight: 'bold' as const, size: 12 },
            formatter: function (
              value: number,
              context: { dataset: { data: number[] } },
            ) {
              const total = context.dataset.data.reduce(
                (a: number, b: number) => a + b,
                0,
              )
              if (total === 0 || value === 0) return ''
              const pct = Math.round((value / total) * 1000) / 10
              if (pct < 4) return ''
              return pct + '%'
            },
          },
        },
      } as never,
    })

    return () => {
      chartRef.current?.destroy()
      chartRef.current = null
    }
  }, [data, stages, resolvedCount])

  if (stages.length === 0) return null

  return (
    <>
      <div className="stagedoughnut-chart">
        <canvas ref={canvasRef} />
      </div>
      {data.note && (
        <div
          style={{
            fontSize: '11px',
            color: '#6a6e73',
            textAlign: 'center',
            marginTop: '6px',
            padding: '0 10px',
          }}
        >
          {data.note}
        </div>
      )}
    </>
  )
}

export { StageDoughnut }
