import { useEffect, useRef, useState } from 'react'
import {
  Button,
  FormGroup,
  FormSelect,
  FormSelectOption,
} from '@patternfly/react-core'

interface FilterOption {
  value: string
  label: string
}

export type TimeRange = 'days30' | 'days60' | 'days90' | 'custom'

function formatRange(from: string, to: string): string {
  if (!from || !to) return 'Custom Range'
  const f = new Date(from + 'T00:00:00')
  const t = new Date(to + 'T00:00:00')
  const fmt = (d: Date) => d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
  return `${fmt(f)} – ${fmt(t)}`
}

interface TopBarProps {
  title: string
  subtitle?: string
  orgs: FilterOption[]
  products: FilterOption[]
  teams: FilterOption[]
  selectedOrg: string
  selectedProduct: string
  selectedTeam: string
  onOrgChange: (value: string) => void
  onProductChange: (value: string) => void
  onTeamChange: (value: string) => void
  timeRange: TimeRange
  customFrom: string
  customTo: string
  onTimeRangeChange: (value: TimeRange) => void
  onCustomRangeApply: (from: string, to: string) => void
  timeRangeEnabled?: boolean
}

export function TopBar({
  title,
  subtitle,
  orgs,
  products,
  teams,
  selectedOrg,
  selectedProduct,
  selectedTeam,
  onOrgChange,
  onProductChange,
  onTeamChange,
  timeRange,
  customFrom,
  customTo,
  onTimeRangeChange,
  onCustomRangeApply,
  timeRangeEnabled = true,
}: TopBarProps) {
  const [panelOpen, setPanelOpen] = useState(false)
  const [localFrom, setLocalFrom] = useState(customFrom)
  const [localTo, setLocalTo] = useState(customTo)
  const panelRef = useRef<HTMLDivElement>(null)

  const handleTimeRangeChange = (_e: React.FormEvent, val: string) => {
    const range = val as TimeRange
    onTimeRangeChange(range)
    if (range === 'custom') {
      setPanelOpen(true)
    } else {
      setPanelOpen(false)
    }
  }

  const handleApply = () => {
    if (localFrom && localTo) {
      onCustomRangeApply(localFrom, localTo)
      setPanelOpen(false)
    }
  }

  useEffect(() => {
    if (!panelOpen) return
    const handleClick = (e: MouseEvent) => {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) {
        setPanelOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [panelOpen])

  const timeLabel = timeRange === 'custom' && customFrom && customTo
    ? formatRange(customFrom, customTo)
    : undefined

  return (
    <div className="top-bar">
      <div className="top-bar__left">
        <h1 className="top-bar__title">{title}</h1>
        {subtitle && <span className="top-bar__subtitle">{subtitle}</span>}
      </div>
      <div className="top-bar__filters">
        <FormGroup label="Org" fieldId="org-select">
          <FormSelect
            id="org-select"
            value={selectedOrg}
            onChange={(_e, val) => onOrgChange(val)}
            aria-label="Select organization"
          >
            <FormSelectOption value="" label="All Organizations" />
            {orgs.map(o => (
              <FormSelectOption key={o.value} value={o.value} label={o.label} />
            ))}
          </FormSelect>
        </FormGroup>
        <FormGroup label="Product" fieldId="product-select">
          <FormSelect
            id="product-select"
            value={selectedProduct}
            onChange={(_e, val) => onProductChange(val)}
            aria-label="Select product"
            isDisabled={!selectedOrg}
          >
            <FormSelectOption value="" label="All Products" />
            {products.map(p => (
              <FormSelectOption key={p.value} value={p.value} label={p.label} />
            ))}
          </FormSelect>
        </FormGroup>
        <FormGroup label="Team" fieldId="team-select">
          <FormSelect
            id="team-select"
            value={selectedTeam}
            onChange={(_e, val) => onTeamChange(val)}
            aria-label="Select team"
            isDisabled={!selectedProduct}
          >
            <FormSelectOption value="" label="All Teams" />
            {teams.map(t => (
              <FormSelectOption key={t.value} value={t.value} label={t.label} />
            ))}
          </FormSelect>
        </FormGroup>
        <div className="top-bar__separator" />
        <div className={`top-bar__time-range${!timeRangeEnabled ? ' top-bar__time-range--disabled' : ''}`} ref={panelRef}>
          <FormGroup label="Time Range" fieldId="time-range-select">
            <FormSelect
              id="time-range-select"
              value={timeRange}
              onChange={handleTimeRangeChange}
              aria-label="Select time range"
              isDisabled={!timeRangeEnabled}
            >
              <FormSelectOption value="days30" label="Last 30 days" />
              <FormSelectOption value="days60" label="Last 60 days" />
              <FormSelectOption value="days90" label="Last 90 days" />
              <FormSelectOption value="custom" label="Custom Range" />
            </FormSelect>
          </FormGroup>
          {timeRangeEnabled && timeLabel && !panelOpen && (
            <button className="top-bar__range-badge" onClick={() => setPanelOpen(true)}>
              {timeLabel}
            </button>
          )}
          {timeRangeEnabled && panelOpen && (
            <div className="top-bar__date-panel">
              <div className="top-bar__date-panel-row">
                <label htmlFor="custom-from">From</label>
                <input
                  id="custom-from"
                  type="date"
                  value={localFrom}
                  onChange={e => setLocalFrom(e.target.value)}
                />
              </div>
              <div className="top-bar__date-panel-row">
                <label htmlFor="custom-to">To</label>
                <input
                  id="custom-to"
                  type="date"
                  value={localTo}
                  onChange={e => setLocalTo(e.target.value)}
                />
              </div>
              <Button
                variant="primary"
                onClick={handleApply}
                isDisabled={!localFrom || !localTo}
                isBlock
              >
                Apply
              </Button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
