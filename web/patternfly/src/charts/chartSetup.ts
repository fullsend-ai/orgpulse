import {
  Chart as ChartJS,
  BarController,
  DoughnutController,
  LineController,
  ScatterController,
  CategoryScale,
  LinearScale,
  LogarithmicScale,
  PointElement,
  LineElement,
  BarElement,
  ArcElement,
  Filler,
  Tooltip,
  Legend,
  Title,
} from 'chart.js'
import ChartDataLabels from 'chartjs-plugin-datalabels'
import { SankeyController, Flow } from 'chartjs-chart-sankey'

ChartJS.register(
  BarController,
  DoughnutController,
  LineController,
  ScatterController,
  CategoryScale,
  LinearScale,
  LogarithmicScale,
  PointElement,
  LineElement,
  BarElement,
  ArcElement,
  Filler,
  Tooltip,
  Legend,
  Title,
  ChartDataLabels,
  SankeyController,
  Flow,
)

export const STAGE_DOUGHNUT_COLOR_MAP: Record<string, string> = {
  New: '#6753ac',
  Backlog: '#f0ab00',
  Refinement: '#8481dd',
  'To Do': '#009596',
  'In Progress': '#4394e5',
  'In Development': '#ef9234',
  'In Review': '#e4706e',
  Review: '#59b84c',
  'Code Review': '#00b9e4',
  Approved: '#c46100',
  'Stakeholder Acceptance': '#7b2d8e',
  Testing: '#35caed',
  Done: '#3e8635',
  Closed: '#3e8635',
  Resolved: '#3e8635',
  Obsolete: '#6a6e73',
}

export function stageDoughnutFallbackColor(index: number): string {
  const hue = (index * 137.508) % 360
  return `hsl(${Math.round(hue)}, 55%, 55%)`
}

export function formatTime(hours: number): string {
  if (hours >= 48) return `${Math.round((hours / 24) * 10) / 10} days`
  if (hours >= 1) return `${Math.round(hours * 10) / 10} hrs`
  return `${Math.round(hours * 60)} min`
}

export function normalizeLineChartScaleOptions(
  options: Record<string, unknown>,
): Record<string, unknown> {
  if (!options || typeof options !== 'object') return options
  const scales = options.scales as Record<string, unknown> | undefined
  if (!scales) return options
  const y = scales.y as Record<string, unknown> | undefined
  if (!y || y.type !== 'logarithmic') return options

  const ticks = { ...(y.ticks as Record<string, unknown> || {}) }
  ticks.autoSkip = true
  ticks.maxTicksLimit = 8
  ticks.callback = function (value: unknown) {
    if (value === 0 || !Number.isFinite(value as number)) return ''
    const v = Number(value)
    if (v >= 10000) return Math.round(v).toLocaleString()
    if (v >= 1000) return String(Math.round(v))
    if (v >= 100) return v % 1 === 0 ? String(v) : v.toFixed(1)
    if (v >= 10) return v.toFixed(1)
    if (v >= 1) return v.toFixed(2)
    return v.toFixed(2)
  }

  return {
    ...options,
    scales: {
      ...scales,
      y: { ...y, ticks },
    },
  }
}
