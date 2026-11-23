import { useRef, useEffect, useState, useCallback } from 'react'
import { Chart as ChartJS } from 'chart.js'
import './chartSetup'
import { Portal } from '../Portal'
import { safeHref } from '../safeUrl'

interface BarItem {
  label: string
  value: number
}

type DatalinkItem = string | { url: string; text?: string }

interface BarChartData {
  items?: BarItem[]
  labels?: string[]
  datasets?: Array<Record<string, unknown> & { datalinks?: DatalinkItem[][] }>
  options?: Record<string, unknown>
}

function normalizeLink(item: DatalinkItem): { url: string; text: string } {
  if (typeof item === 'string') return { url: item, text: item }
  return { url: item.url, text: item.text || item.url }
}

function PrListLightbox({ title, links, onClose }: { title: string; links: DatalinkItem[]; onClose: () => void }) {
  return (
    <Portal>
    <div className="lightbox-overlay" onClick={onClose}>
      <div className="lightbox-content" onClick={(e) => e.stopPropagation()}>
        <button className="lightbox-close" onClick={onClose}>&times;</button>
        <h3>{title}</h3>
        <ul className="pr-list">
          {links.map((item, i) => {
            const { url, text } = normalizeLink(item)
            return (
              <li key={i}>
                <a href={safeHref(url)} target="_blank" rel="noopener noreferrer">{text}</a>
              </li>
            )
          })}
        </ul>
      </div>
    </div>
    </Portal>
  )
}

const LABEL_FONT = { size: 12, weight: 'bold' as const }
const LABEL_DARK = '#151515'
const LABEL_LIGHT = '#ffffff'

function parseColor(color: unknown): [number, number, number] | null {
  if (typeof color !== 'string') return null
  const c = color.trim()
  const hex = c.match(/^#([0-9a-f]{3,8})$/i)?.[1]
  if (hex) {
    const full = hex.length <= 4 ? hex.split('').map((h) => h + h).join('') : hex
    return [0, 2, 4].map((i) => parseInt(full.slice(i, i + 2), 16)) as [number, number, number]
  }
  const rgb = c.match(/^rgba?\(\s*([\d.]+)[\s,]+([\d.]+)[\s,]+([\d.]+)/i)
  if (rgb) return [Number(rgb[1]), Number(rgb[2]), Number(rgb[3])]
  return null
}

function contrastText(color: unknown): string {
  const rgb = parseColor(color)
  if (!rgb) return LABEL_LIGHT
  const [r, g, b] = rgb.map((v) => {
    const s = v / 255
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }) as [number, number, number]
  return 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.4 ? LABEL_DARK : LABEL_LIGHT
}

interface LabelContext {
  chart: ChartJS
  datasetIndex: number
  dataIndex: number
  dataset: { backgroundColor?: unknown; data?: unknown[] }
}

// Labels sit inside the bar when the text fits, otherwise just past its end;
// stacked bars have no room outside, so labels that don't fit are hidden.
function barLabelOptions(horizontal: boolean, stacked: boolean): Record<string, unknown> {
  const fitsInside = (ctx: LabelContext): boolean => {
    const el = ctx.chart.getDatasetMeta(ctx.datasetIndex).data[ctx.dataIndex]
    if (!el) return false
    const { x, y, base, width, height } = el.getProps(['x', 'y', 'base', 'width', 'height'], true) as Record<string, number>
    const length = horizontal ? Math.abs(x - base) : Math.abs(base - y)
    const thickness = horizontal ? height : width
    const text = String(ctx.dataset.data?.[ctx.dataIndex] ?? '')
    const textLength = horizontal ? text.length * 7.5 + 8 : LABEL_FONT.size + 6
    const textThickness = horizontal ? LABEL_FONT.size + 2 : text.length * 7.5
    return length >= textLength && thickness >= textThickness
  }
  const barColor = (ctx: LabelContext): unknown => {
    const bg = ctx.dataset.backgroundColor
    return Array.isArray(bg) ? bg[ctx.dataIndex % bg.length] : bg
  }
  return {
    font: LABEL_FONT,
    display: (ctx: LabelContext) => {
      const v = ctx.dataset.data?.[ctx.dataIndex]
      if (v === null || v === undefined || v === 0) return false
      return stacked ? fitsInside(ctx) : true
    },
    anchor: (ctx: LabelContext) => (fitsInside(ctx) ? 'center' : 'end'),
    align: (ctx: LabelContext) => (fitsInside(ctx) ? 'center' : 'end'),
    offset: 4,
    color: (ctx: LabelContext) => (fitsInside(ctx) ? contrastText(barColor(ctx)) : LABEL_DARK),
  }
}

export default function BarChart({ data }: { data: BarChartData }) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const chartRef = useRef<ChartJS | null>(null)
  const [prList, setPrList] = useState<{ title: string; links: DatalinkItem[] } | null>(null)

  const openPrListLightbox = useCallback(
    (categoryLabel: string, links: DatalinkItem[]) => {
      if (links.length > 0) setPrList({ title: categoryLabel, links })
    },
    [],
  )

  useEffect(() => {
    if (!canvasRef.current) return
    if (chartRef.current) chartRef.current.destroy()

    const ctx = canvasRef.current.getContext('2d')
    if (!ctx) return

    const datasets = data.datasets || []
    const hasDatalinks = datasets.some((ds) => Array.isArray(ds.datalinks))
    const labels = data.labels || []

    const defaultOptions: Record<string, unknown> = {
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false },
        tooltip: { mode: 'index', intersect: false },
      },
      scales: {
        x: { grid: { display: false } },
        y: { beginAtZero: true, grid: { color: 'rgba(0, 0, 0, 0.1)' } },
      },
      interaction: { mode: 'nearest', axis: 'x', intersect: false },
      onClick: (
        event: { native?: MouseEvent },
        elements: Array<{ index: number; datasetIndex: number }>,
      ) => {
        if (elements.length > 0 && hasDatalinks) {
          const el = elements[0]!
          const categoryLabel = labels[el.index] != null ? String(labels[el.index]) : ''
          const datalinks = Array.isArray(datasets[el.datasetIndex]?.datalinks)
            ? datasets[el.datasetIndex]!.datalinks!
            : []
          const links = Array.isArray(datalinks[el.index])
            ? datalinks[el.index]!.filter(Boolean)
            : []
          openPrListLightbox(categoryLabel, links)
          if (event.native) event.native.stopPropagation()
        }
      },
      onHover: (
        event: { native?: MouseEvent & { target?: HTMLElement } },
        elements: unknown[],
      ) => {
        if (event.native?.target) {
          event.native.target.style.cursor =
            elements.length > 0 && hasDatalinks ? 'pointer' : 'default'
        }
      },
    }

    let chartOptions = data.options
      ? { ...defaultOptions, ...data.options }
      : defaultOptions
    if (data.options?.scales && defaultOptions.scales) {
      chartOptions.scales = {
        ...(defaultOptions.scales as object),
        ...(data.options.scales as object),
      }
    }
    if (data.options?.plugins) {
      chartOptions.plugins = {
        ...(defaultOptions.plugins as object),
        ...(data.options.plugins as object),
      }
    } else {
      chartOptions.plugins = chartOptions.plugins || {}
    }
    const plugins = chartOptions.plugins as Record<string, unknown>
    if (!plugins.datalabels) {
      const horizontal = (chartOptions as Record<string, unknown>).indexAxis === 'y'
      const scales = (chartOptions.scales || {}) as Record<string, Record<string, unknown> | undefined>
      const stacked = !!(scales.x?.stacked || scales.y?.stacked)
      plugins.datalabels = barLabelOptions(horizontal, stacked)
    }
    if (
      !data.options?.plugins ||
      !(data.options.plugins as Record<string, unknown>).legend ||
      !((data.options.plugins as Record<string, Record<string, unknown>>).legend?.display)
    ) {
      plugins.legend = plugins.legend || {}
      ;(plugins.legend as Record<string, unknown>).display = false
    }

    if ((chartOptions as Record<string, unknown>).indexAxis === 'y') {
      chartOptions.interaction = { mode: 'index', axis: 'y', intersect: false }
      const totalValue = datasets.reduce((sum, ds) => {
        return sum + ((ds.data as number[]) || []).reduce((s: number, v: number) => s + (v || 0), 0)
      }, 0)
      if (totalValue > 0) {
        plugins.tooltip = plugins.tooltip || {}
        ;(plugins.tooltip as Record<string, unknown>).callbacks = {
          label: function (context: { raw: number }) {
            const count = context.raw || 0
            const pct = ((count / totalValue) * 100).toFixed(1)
            return `${count} issues (${pct}%)`
          },
        }
      }
      const scales = chartOptions.scales as Record<string, Record<string, unknown>>
      if (scales?.y) {
        scales.y.ticks = scales.y.ticks || {}
        ;(scales.y.ticks as Record<string, unknown>).callback = function (
          this: { getLabelForValue: (v: number) => string },
          value: number,
        ) {
          const label = this.getLabelForValue(value)
          return label ? label.replace(/\s*\(.*?\)/, '') : label
        }
      }
    }

    chartRef.current = new ChartJS(ctx, {
      type: 'bar',
      data: { labels, datasets: datasets as never[] },
      options: chartOptions as never,
    })

    return () => {
      chartRef.current?.destroy()
      chartRef.current = null
    }
  }, [data, openPrListLightbox])

  if (data.items) {
    const items = data.items
    const maxValue = Math.max(...items.map((item) => item.value))
    return (
      <div className="bar-chart">
        {items.map((item, i) => {
          const percentage = (item.value / maxValue) * 100
          return (
            <div className="bar-item" key={i}>
              <div className="bar-label">{item.label}</div>
              <div className="bar-container">
                <div className="bar-fill" style={{ width: `${percentage}%` }}>
                  {item.value}
                </div>
              </div>
            </div>
          )
        })}
      </div>
    )
  }

  return (
    <>
      <div className="bar-diagram">
        <canvas ref={canvasRef} />
      </div>
      {prList && (
        <PrListLightbox
          title={prList.title}
          links={prList.links}
          onClose={() => setPrList(null)}
        />
      )}
    </>
  )
}

export { BarChart }
