import { useState, useCallback } from 'react'
import { getAdminKey } from './api'

interface QueryResult {
  columns: string[]
  rows: Record<string, unknown>[]
}

interface TablesResponse {
  tables: string[]
}

export default function DevLakeDBPanel() {
  const [sql, setSql] = useState('')
  const [result, setResult] = useState<QueryResult | null>(null)
  const [tables, setTables] = useState<string[] | null>(null)
  const [error, setError] = useState('')
  const [elapsed, setElapsed] = useState<number | null>(null)
  const [running, setRunning] = useState(false)
  const [showTables, setShowTables] = useState(false)

  const runQuery = useCallback(
    async (query?: string) => {
      const q = (query ?? sql).trim()
      if (!q) return
      setRunning(true)
      setError('')
      setResult(null)
      setElapsed(null)
      setShowTables(false)
      const start = performance.now()
      try {
        const resp = await fetch('/api/v1/devlake-db/query', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'X-API-Key': getAdminKey(),
          },
          body: JSON.stringify({ sql: q }),
        })
        const data = await resp.json()
        if (!resp.ok || (data as Record<string, string>).error) {
          setError((data as Record<string, string>).error || resp.statusText)
          return
        }
        setResult(data as QueryResult)
        setElapsed(Math.round(performance.now() - start))
      } catch (e) {
        setError(e instanceof Error ? e.message : 'Query failed')
      } finally {
        setRunning(false)
      }
    },
    [sql],
  )

  const loadTables = useCallback(async () => {
    setError('')
    setResult(null)
    setElapsed(null)
    try {
      const resp = await fetch('/api/v1/devlake-db/tables', {
        headers: { 'X-API-Key': getAdminKey() },
      })
      const data = await resp.json()
      if (!resp.ok) {
        setError((data as Record<string, string>).error || resp.statusText)
        return
      }
      if ((data as Record<string, string>).error) {
        setError((data as Record<string, string>).error)
        return
      }
      const body = data as TablesResponse
      setTables(body.tables || [])
      setShowTables(true)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load tables')
    }
  }, [])

  const describeTable = (name: string) => {
    const q = `DESCRIBE \`${name}\``
    setSql(q)
    void runQuery(q)
  }

  const clearQuery = () => {
    setSql('')
    setResult(null)
    setError('')
    setElapsed(null)
    setShowTables(false)
  }

  const previewTable = (name: string) => {
    const q = `SELECT * FROM \`${name}\` LIMIT 20`
    setSql(q)
    void runQuery(q)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      void runQuery()
    }
  }

  return (
    <div>
      <div className="adm-toolbar">
        <h2>DevLake DB</h2>
        <button className="adm-btn adm-btn--ghost" onClick={() => void loadTables()}>Tables</button>
        <button className="adm-btn adm-btn--ghost" onClick={clearQuery}>Clear</button>
        <button className="adm-btn adm-btn--primary" disabled={running} onClick={() => void runQuery()}>
          {running ? 'Running...' : 'Run Query'}
        </button>
      </div>
      <textarea
        className="adm-json-textarea"
        style={{ width: '100%', boxSizing: 'border-box', marginBottom: 12 }}
        rows={6}
        value={sql}
        onChange={e => setSql(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="SELECT * FROM _tool_github_repos LIMIT 10"
      />

      {error && (
        <pre style={{
          color: '#dc2626',
          fontFamily: 'monospace',
          fontSize: 13,
          background: '#fef2f2',
          padding: 12,
          borderRadius: 4,
          marginBottom: 12,
          whiteSpace: 'pre-wrap',
        }}>
          {error}
        </pre>
      )}

      {result && (
        <>
          <div style={{ fontSize: 12, color: '#64748b', marginBottom: 8 }}>
            {result.rows.length} row{result.rows.length !== 1 ? 's' : ''}
            {elapsed != null && ` · ${elapsed}ms`}
          </div>
          <div style={{ overflowX: 'auto' }}>
            <table className="adm-table">
              <thead>
                <tr>
                  {result.columns.map(col => <th key={col}>{col}</th>)}
                </tr>
              </thead>
              <tbody>
                {result.rows.map((row, i) => (
                  <tr key={i}>
                    {result.columns.map(col => (
                      <td key={col}>{row[col] != null ? String(row[col]) : ''}</td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}

      {showTables && tables && (
        <table className="adm-table">
          <thead>
            <tr>
              <th>#</th>
              <th>Table Name</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {tables.map((name, i) => (
              <tr key={name}>
                <td>{i + 1}</td>
                <td>{name}</td>
                <td className="adm-actions">
                  <button className="adm-btn adm-btn--ghost adm-btn--sm" onClick={() => describeTable(name)}>Describe</button>
                  <button className="adm-btn adm-btn--ghost adm-btn--sm" onClick={() => previewTable(name)}>Preview</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
