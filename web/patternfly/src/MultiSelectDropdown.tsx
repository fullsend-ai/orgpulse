import { useEffect, useRef, useState } from 'react'

export interface MultiSelectItem {
  value: string
  label: string
  group?: string
}

interface MultiSelectDropdownProps {
  id: string
  label: string
  items: MultiSelectItem[]
  selected: string[]
  onChange: (selected: string[]) => void
  grouped?: boolean
}

export function MultiSelectDropdown({
  id,
  label,
  items,
  selected,
  onChange,
  grouped,
}: MultiSelectDropdownProps) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [local, setLocal] = useState<Set<string>>(new Set(selected))
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    setLocal(new Set(selected))
  }, [selected])

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        onChange(Array.from(local))
        setOpen(false)
        setSearch('')
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open, local, onChange])

  const toggle = (val: string) => {
    setLocal(prev => {
      const next = new Set(prev)
      if (next.has(val)) next.delete(val)
      else next.add(val)
      return next
    })
  }

  const selectAll = () => setLocal(new Set(items.map(i => i.value)))
  const clearAll = () => setLocal(new Set())

  const filtered = search
    ? items.filter(i =>
        i.label.toLowerCase().includes(search.toLowerCase()) ||
        (i.group || '').toLowerCase().includes(search.toLowerCase()),
      )
    : items

  const renderItems = () => {
    if (!grouped) {
      return filtered.map(item => (
        <label key={item.value} className="msd__option">
          <input
            type="checkbox"
            checked={local.has(item.value)}
            onChange={() => toggle(item.value)}
          />
          <span>{item.label}</span>
        </label>
      ))
    }

    const groups = new Map<string, MultiSelectItem[]>()
    for (const item of filtered) {
      const g = item.group || ''
      if (!groups.has(g)) groups.set(g, [])
      groups.get(g)!.push(item)
    }

    const elements: JSX.Element[] = []
    for (const [group, groupItems] of groups) {
      if (group) {
        elements.push(
          <div key={`g-${group}`} className="msd__group-header">{group}</div>,
        )
      }
      for (const item of groupItems) {
        elements.push(
          <label key={item.value} className="msd__option">
            <input
              type="checkbox"
              checked={local.has(item.value)}
              onChange={() => toggle(item.value)}
            />
            <span>{item.label}</span>
          </label>,
        )
      }
    }
    return elements
  }

  const count = local.size

  return (
    <div className="msd" ref={containerRef} id={id}>
      <button
        type="button"
        className="msd__toggle"
        onClick={() => {
          if (open) {
            onChange(Array.from(local))
            setSearch('')
          }
          setOpen(!open)
        }}
        aria-expanded={open}
      >
        <span className="msd__toggle-text">{label}</span>
        {count > 0 && count < items.length && (
          <span className="msd__badge">{count}</span>
        )}
        <span className="msd__caret">▾</span>
      </button>
      {open && (
        <div className="msd__panel">
          <input
            className="msd__search"
            type="text"
            placeholder="Search…"
            value={search}
            onChange={e => setSearch(e.target.value)}
            autoFocus
          />
          <div className="msd__actions">
            <button type="button" onClick={selectAll}>Select all</button>
            <button type="button" onClick={clearAll}>Clear all</button>
          </div>
          <div className="msd__list">
            {filtered.length === 0 && (
              <div className="msd__empty">No matches</div>
            )}
            {renderItems()}
          </div>
        </div>
      )}
    </div>
  )
}
