import { useEffect, useMemo, useRef } from 'react'

export interface BrowseListItem {
  full_name: string
  plugin: string
  owner?: string
  name?: string
  board_id?: string
}

interface BrowseListProps {
  items: BrowseListItem[]
  keyOf: (item: BrowseListItem) => string
  isItemAdded: (item: BrowseListItem) => boolean
  selected: Set<string>
  onSelectionChange: (selected: Set<string>) => void
  emptyMessage?: string
  filter: string
  groupBy?: (item: BrowseListItem) => string
}

interface ItemGroup {
  name: string
  items: BrowseListItem[]
}

export function BrowseList({
  items, keyOf, isItemAdded, selected, onSelectionChange, emptyMessage, filter, groupBy,
}: BrowseListProps) {
  const anchorKey = useRef<string | null>(null)
  const shiftHeld = useRef(false)

  useEffect(() => {
    const down = (e: KeyboardEvent) => { if (e.key === 'Shift') shiftHeld.current = true }
    const up = (e: KeyboardEvent) => { if (e.key === 'Shift') shiftHeld.current = false }
    window.addEventListener('keydown', down)
    window.addEventListener('keyup', up)
    return () => { window.removeEventListener('keydown', down); window.removeEventListener('keyup', up) }
  }, [])

  useEffect(() => {
    anchorKey.current = null
  }, [filter])

  const visible = useMemo(() => {
    const f = filter.trim().toLowerCase()
    return f ? items.filter(item => item.full_name.toLowerCase().includes(f)) : items
  }, [items, filter])

  const selectableKeys = useMemo(
    () => visible.filter(item => !isItemAdded(item)).map(keyOf),
    [visible, isItemAdded, keyOf],
  )

  const groups = useMemo(() => {
    if (!groupBy) return null
    const map = new Map<string, BrowseListItem[]>()
    for (const item of visible) {
      const name = groupBy(item)
      const list = map.get(name)
      if (list) list.push(item)
      else map.set(name, [item])
    }
    return Array.from(map, ([name, groupItems]) => ({ name, items: groupItems })) as ItemGroup[]
  }, [visible, groupBy])

  const showGroupHeaders = !!groups && groups.length > 1

  const allSelected = selectableKeys.length > 0 && selectableKeys.every(k => selected.has(k))
  const someSelected = selectableKeys.some(k => selected.has(k))

  const toggleKeys = (keys: string[], on: boolean) => {
    if (keys.length === 0) return
    const next = new Set(selected)
    for (const key of keys) {
      if (on) next.add(key)
      else next.delete(key)
    }
    onSelectionChange(next)
  }

  const handleSelectAll = () => toggleKeys(selectableKeys, !allSelected)

  const handleGroupToggle = (group: ItemGroup) => {
    const keys = group.items.filter(item => !isItemAdded(item)).map(keyOf)
    const groupAllSelected = keys.length > 0 && keys.every(k => selected.has(k))
    toggleKeys(keys, !groupAllSelected)
  }

  const handleItemToggle = (key: string, shiftKey: boolean) => {
    const idx = visible.findIndex(item => keyOf(item) === key)
    if (idx < 0) return

    const anchorIdx = anchorKey.current
      ? visible.findIndex(item => keyOf(item) === anchorKey.current)
      : -1

    const next = new Set(selected)
    if (shiftKey && anchorIdx >= 0) {
      const wantSelected = !selected.has(key)
      const start = Math.min(anchorIdx, idx)
      const end = Math.max(anchorIdx, idx)
      for (let i = start; i <= end; i++) {
        const item = visible[i]
        if (isItemAdded(item)) continue
        const itemKey = keyOf(item)
        if (wantSelected) next.add(itemKey)
        else next.delete(itemKey)
      }
    } else {
      if (next.has(key)) next.delete(key)
      else next.add(key)
      anchorKey.current = key
    }
    onSelectionChange(next)
  }

  const renderItem = (item: BrowseListItem, grouped: boolean) => {
    const key = keyOf(item)
    const added = isItemAdded(item)
    const label = grouped
      ? (item.name || item.full_name.split('/').slice(1).join('/') || item.full_name)
      : item.full_name
    return (
      <label
        key={key}
        className={`mt-browse-item${added ? ' already-added' : ''}${grouped ? ' mt-browse-item--grouped' : ''}`}
      >
        <input
          type="checkbox"
          checked={selected.has(key) || added}
          disabled={added}
          aria-label={item.full_name}
          onChange={() => handleItemToggle(key, shiftHeld.current)}
        />
        <span className="mt-source-type-badge">{item.plugin}</span>
        <span>{label}</span>
        {added && <span style={{ fontSize: '0.8em', color: '#6a6e73' }}>(added)</span>}
      </label>
    )
  }

  return (
    <div className="mt-browse-list">
      {visible.length === 0 && (
        <div style={{ padding: 16, textAlign: 'center', color: '#6a6e73', fontSize: '0.88em' }}>
          {emptyMessage ?? 'No items found.'}
        </div>
      )}
      {visible.length > 0 && selectableKeys.length > 0 && (
        <label className="mt-browse-item mt-browse-select-all">
          <input
            type="checkbox"
            checked={allSelected}
            ref={el => { if (el) el.indeterminate = someSelected && !allSelected }}
            onChange={handleSelectAll}
            aria-label="Select all visible items"
          />
          <span style={{ fontWeight: 600, fontSize: '0.88em' }}>
            Select all ({selectableKeys.length})
          </span>
        </label>
      )}
      {showGroupHeaders ? (
        groups!.map(group => {
          const groupKeys = group.items.filter(item => !isItemAdded(item)).map(keyOf)
          const groupAllSelected = groupKeys.length > 0 && groupKeys.every(k => selected.has(k))
          const groupSomeSelected = groupKeys.some(k => selected.has(k))
          return (
            <div key={group.name}>
              <label
                className={`mt-browse-item mt-browse-org-header${groupKeys.length === 0 ? ' already-added' : ''}`}
              >
                <input
                  type="checkbox"
                  checked={groupAllSelected}
                  disabled={groupKeys.length === 0}
                  ref={el => { if (el) el.indeterminate = groupSomeSelected && !groupAllSelected }}
                  onChange={() => handleGroupToggle(group)}
                  aria-label={`Select all ${group.name} repositories`}
                />
                <span style={{ fontWeight: 600, fontSize: '0.88em' }}>
                  {group.name} ({groupKeys.length})
                </span>
              </label>
              {group.items.map(item => renderItem(item, true))}
            </div>
          )
        })
      ) : (
        visible.map(item => renderItem(item, false))
      )}
    </div>
  )
}
