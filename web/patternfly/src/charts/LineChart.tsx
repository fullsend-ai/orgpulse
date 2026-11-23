import { useRef, useEffect } from 'react'
import { Chart as ChartJS } from 'chart.js'
import { normalizeLineChartScaleOptions } from './chartSetup'
import './chartSetup'

interface LineData {
  labels?: string[]
  datasets?: Record<string, unknown>[]
  options?: Record<string, unknown>
  backlogDelta?: number[]
  dailyCreated?: number[]
  dailyCompleted?: number[]
  backgroundZones?: Array<{
    from: number
    to: number
    color: string
    label?: string
    labelColor?: string
  }> | null
  subtype?: string
}

export default function LineChart({ data }: { data: LineData }) {
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
        legend: { position: 'top', labels: { padding: 15, font: { size: 12 } } },
        tooltip: { mode: 'index', intersect: false },
        datalabels: { display: false },
      },
      scales: {
        x: { grid: { display: false } },
        y: { beginAtZero: true, grid: { color: 'rgba(0, 0, 0, 0.1)' } },
      },
      interaction: { mode: 'nearest', axis: 'x', intersect: false },
    }

    let chartOptions = data.options
      ? { ...defaultOptions, ...data.options }
      : defaultOptions
    if (data.options?.plugins) {
      chartOptions.plugins = {
        ...(defaultOptions.plugins as object),
        ...(data.options.plugins as object),
      }
    }
    if (data.options?.scales && defaultOptions.scales) {
      chartOptions.scales = {
        ...(defaultOptions.scales as object),
        ...(data.options.scales as object),
      }
    }
    chartOptions = normalizeLineChartScaleOptions(chartOptions)

    if (data.backlogDelta) {
      const backlogDelta = data.backlogDelta
      const dailyCreated = data.dailyCreated
      const dailyCompleted = data.dailyCompleted
      const plugins = (chartOptions.plugins || {}) as Record<string, unknown>
      const tooltip = (plugins.tooltip || {}) as Record<string, unknown>
      const callbacks = (tooltip.callbacks || {}) as Record<string, unknown>
      callbacks.afterBody = function (
        tooltipItems: Array<{ dataIndex: number }>,
      ) {
        const idx = tooltipItems[0]!.dataIndex
        const lines: string[] = []
        if (dailyCreated && dailyCompleted) {
          lines.push(
            `Today: +${dailyCreated[idx]} created, +${dailyCompleted[idx]} closed`,
          )
        }
        const delta = backlogDelta[idx]!
        if (delta > 0) lines.push(`Backlog Delta: +${delta}`)
        else if (delta < 0) lines.push(`Backlog Delta: ${delta}`)
        else lines.push('Backlog Delta: 0 (balanced)')
        return lines
      }
      tooltip.callbacks = callbacks
      plugins.tooltip = tooltip
      chartOptions.plugins = plugins
    }

    let backgroundZones = data.backgroundZones || null
    if (
      !backgroundZones &&
      data.subtype === 'cycleTimeDistribution' &&
      data.labels
    ) {
      const bucketMaxHours = [1, 4, 8, 24, 48, 72, 168, 336, 672, Infinity]
      const slowH = 48
      const critH = 168
      const slowIdx = bucketMaxHours.findIndex((h) => h > slowH)
      const critIdx = bucketMaxHours.findIndex((h) => h > critH)
      if (slowIdx > 0 && critIdx > slowIdx) {
        backgroundZones = [
          {
            from: 0,
            to: slowIdx,
            color: 'rgba(129,199,132,0.12)',
            label: 'Healthy',
            labelColor: 'rgba(56,142,60,0.5)',
          },
          {
            from: slowIdx,
            to: critIdx,
            color: 'rgba(255,183,77,0.12)',
            label: 'Slow',
            labelColor: 'rgba(230,150,0,0.5)',
          },
          {
            from: critIdx,
            to: data.labels.length,
            color: 'rgba(229,115,115,0.12)',
            label: 'Critical',
            labelColor: 'rgba(198,40,40,0.5)',
          },
        ]
      }
    }

    const inlinePlugins: Record<string, unknown>[] = []
    if (backgroundZones?.length) {
      inlinePlugins.push({
        id: 'backgroundZones',
        beforeDraw: function (chart: ChartJS) {
          const { ctx: c, chartArea, scales } = chart
          if (!chartArea) return
          const xScale = scales.x
          if (!xScale) return
          c.save()
          backgroundZones!.forEach(function (zone) {
            const x1 =
              zone.from === 0
                ? chartArea.left
                : xScale.getPixelForValue(zone.from - 0.5) || chartArea.left
            const x2 =
              zone.to >= (data.labels || []).length
                ? chartArea.right
                : xScale.getPixelForValue(zone.to - 0.5) || chartArea.right
            c.fillStyle = zone.color
            c.fillRect(
              x1,
              chartArea.top,
              x2 - x1,
              chartArea.bottom - chartArea.top,
            )
            if (zone.label) {
              c.fillStyle = zone.labelColor || 'rgba(0,0,0,0.3)'
              c.font = '10px sans-serif'
              c.textAlign = 'center'
              c.fillText(zone.label, (x1 + x2) / 2, chartArea.top + 12)
            }
          })
          c.restore()
        },
      })
    }

    chartRef.current = new ChartJS(ctx, {
      type: 'line',
      data: {
        labels: data.labels || [],
        datasets: (data.datasets || []) as never[],
      },
      options: chartOptions as never,
      plugins: inlinePlugins as never[],
    })

    return () => {
      chartRef.current?.destroy()
      chartRef.current = null
    }
  }, [data])

  return (
    <div className="line-diagram">
      <canvas ref={canvasRef} />
    </div>
  )
}

export { LineChart }
