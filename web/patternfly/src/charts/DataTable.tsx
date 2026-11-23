import { useState, useCallback, useMemo } from 'react'

interface TableData {
  columns?: string[]
  rows?: Record<string, unknown>[]
  defaultSort?: string
  fullRepoNames?: boolean
}

type SortDir = 'asc' | 'desc'

export function DataTable({ data }: { data: TableData }) {
  const columns = data.columns || []
  const rows = data.rows || []
  const visibleColumns = useMemo(
    () => columns.filter((col) => !col.startsWith('_')),
    [columns],
  )

  const defaultSortCol = data.defaultSort || 'Median Cycle Time (hrs)'
  const [sortCol, setSortCol] = useState<string | null>(
    visibleColumns.includes(defaultSortCol) ? defaultSortCol : null,
  )
  const [sortDir, setSortDir] = useState<SortDir>('desc')

  const sortedRows = useMemo(() => {
    if (!sortCol) return rows
    const colIdx = visibleColumns.indexOf(sortCol)
    if (colIdx < 0) return rows
    return [...rows].sort((a, b) => {
      const rawA = a[sortCol]
      const rawB = b[sortCol]
      const aVal =
        typeof rawA === 'object' && rawA !== null && 'sort' in rawA
          ? String((rawA as { sort: unknown }).sort)
          : String(rawA ?? '')
      const bVal =
        typeof rawB === 'object' && rawB !== null && 'sort' in rawB
          ? String((rawB as { sort: unknown }).sort)
          : String(rawB ?? '')
      const aNum = parseFloat(aVal)
      const bNum = parseFloat(bVal)
      if (!isNaN(aNum) && !isNaN(bNum)) {
        return sortDir === 'asc' ? aNum - bNum : bNum - aNum
      }
      return sortDir === 'asc'
        ? aVal.localeCompare(bVal)
        : bVal.localeCompare(aVal)
    })
  }, [rows, sortCol, sortDir, visibleColumns])

  const handleSort = useCallback(
    (col: string) => {
      if (sortCol === col) {
        setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
      } else {
        setSortCol(col)
        setSortDir('asc')
      }
    },
    [sortCol],
  )

  if (visibleColumns.length === 0 || rows.length === 0) {
    return (
      <div className="diagram-table-empty">
        No data available for current selection.
      </div>
    )
  }

  return (
    <div className="diagram-table-wrapper">
      <table className="diagram-table">
        <thead>
          <tr>
            {visibleColumns.map((col) => (
              <th
                key={col}
                className="sortable-th"
                data-col={col}
                style={{ cursor: 'pointer' }}
                onClick={() => handleSort(col)}
              >
                {col}{' '}
                <span className="sort-arrow">
                  {sortCol === col
                    ? sortDir === 'asc'
                      ? ' ▲'
                      : ' ▼'
                    : ' ↕'}
                </span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {sortedRows.map((row, rowIdx) => {
            const rowClass = row._rowClass
              ? String(row._rowClass)
              : undefined
            return (
              <tr key={rowIdx} className={rowClass}>
                {visibleColumns.map((col) => {
                  const raw = row[col] !== undefined ? row[col] : ''
                  const isObj =
                    typeof raw === 'object' && raw !== null
                  const val = isObj
                    ? String((raw as { display?: unknown }).display ?? '')
                    : String(raw)

                  if (
                    col === 'Repository' &&
                    !data.fullRepoNames &&
                    typeof val === 'string' &&
                    val.includes('/')
                  ) {
                    const shortName = val.substring(
                      val.lastIndexOf('/') + 1,
                    )
                    return (
                      <td key={col} title={val}>
                        {shortName}
                      </td>
                    )
                  }

                  return <td key={col}>{val}</td>
                })}
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

export default DataTable
