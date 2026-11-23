import { useState, useRef, useEffect } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'

interface ImportAction {
  type: string
  description: string
  status?: string
}

interface RepoDuplicate {
  owner: string
  name: string
  repo_type: string
  existing_teams: string[]
}

interface RequiredConnection {
  plugin: string
  default_endpoint: string
  needs_username: boolean
}

interface PreviewResponse {
  team: { id: string; name: string; contact_email?: string; exists: boolean }
  actions: ImportAction[]
  duplicates: RepoDuplicate[]
  required_connections: RequiredConnection[]
  warnings: string[]
  errors: string[]
  valid: boolean
}

interface RevertInfo {
  team_id: string
  sources_created: string[]
  product_labels: string[]
  config_keys: string[]
}

interface SyncResult {
  created?: string[]
  updated?: string[]
  deleted?: string[]
  skipped?: string[]
  errors?: string[]
}

interface ExecuteResponse {
  team_id: string
  team_name: string
  timestamp: string
  actions: ImportAction[]
  duplicates: RepoDuplicate[]
  sync_result?: SyncResult
  revert_info: RevertInfo
}

interface ConnectionInput {
  plugin: string
  token: string
  endpoint: string
  username: string
  expiry_date: string
}

interface ProductOption {
  id: string
  name: string
}

type Step = 'upload' | 'preview' | 'executing' | 'report'

export default function ImportTeamModal({ onDone }: { onDone: () => void }) {
  const { toast, closeModal } = useAdmin()
  const fileRef = useRef<HTMLInputElement>(null)
  const [step, setStep] = useState<Step>('upload')
  const [teamJson, setTeamJson] = useState<unknown>(null)
  const [fileName, setFileName] = useState('')
  const [preview, setPreview] = useState<PreviewResponse | null>(null)
  const [connections, setConnections] = useState<ConnectionInput[]>([])
  const [productId, setProductId] = useState('')
  const [products, setProducts] = useState<ProductOption[]>([])
  const [report, setReport] = useState<ExecuteResponse | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    adminApi<ProductOption[]>('GET', '/api/v1/products')
      .then(setProducts)
      .catch(() => {})
  }, [])

  const handleFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    setFileName(file.name)
    setError('')
    const reader = new FileReader()
    reader.onload = async () => {
      try {
        const parsed = JSON.parse(reader.result as string)
        setTeamJson(parsed)
        setLoading(true)
        const resp = await adminApi<PreviewResponse>(
          'POST',
          '/api/v1/admin/import-team/preview',
          parsed,
        )
        resp.actions = resp.actions ?? []
        resp.duplicates = resp.duplicates ?? []
        resp.required_connections = resp.required_connections ?? []
        resp.warnings = resp.warnings ?? []
        resp.errors = resp.errors ?? []
        setPreview(resp)
        setConnections(
          resp.required_connections.map(rc => ({
            plugin: rc.plugin,
            token: '',
            endpoint: rc.default_endpoint || '',
            username: '',
            expiry_date: '',
          })),
        )
        setStep('preview')
      } catch (err) {
        if (err instanceof AuthError) return
        setError(err instanceof Error ? err.message : 'Failed to parse JSON')
      } finally {
        setLoading(false)
      }
    }
    reader.readAsText(file)
    e.target.value = ''
  }

  const updateConnection = (idx: number, field: keyof ConnectionInput, value: string) => {
    setConnections(prev => prev.map((c, i) => (i === idx ? { ...c, [field]: value } : c)))
  }

  const connectionsValid = connections.every(
    c =>
      c.token.trim() !== '' &&
      c.endpoint.trim() !== '' &&
      c.expiry_date.trim() !== '' &&
      (c.plugin !== 'jira' || c.username.trim() !== ''),
  )

  const handleExecute = async () => {
    setStep('executing')
    try {
      const resp = await adminApi<ExecuteResponse>(
        'POST',
        '/api/v1/admin/import-team/execute',
        {
          team: teamJson,
          product_id: productId || undefined,
          connections: connections.filter(c => c.token.trim() !== ''),
        },
      )
      resp.actions = resp.actions ?? []
      resp.duplicates = resp.duplicates ?? []
      setReport(resp)
      setStep('report')
      toast('Team imported successfully', 'success')
      onDone()
    } catch (err) {
      if (err instanceof AuthError) return
      setError(err instanceof Error ? err.message : 'Import failed')
      setStep('preview')
      toast('Import failed', 'error')
    }
  }

  const downloadReport = () => {
    if (!report) return
    const blob = new Blob([JSON.stringify(report, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `import-report-${report.team_id}-${report.timestamp.replace(/:/g, '-')}.json`
    a.click()
    URL.revokeObjectURL(url)
  }

  const statusIcon = (status?: string) => {
    if (status === 'success') return '✅'
    if (status === 'error') return '❌'
    return '•'
  }

  return (
    <div>
      <h3 className="adm-modal-title">Import Team from JSON</h3>

      {step === 'upload' && (
        <div>
          <p className="adm-modal-text">
            Upload a legacy team JSON file. The file will be validated and a
            preview of all actions will be shown before import.
          </p>
          <input
            ref={fileRef}
            type="file"
            accept=".json"
            style={{ display: 'none' }}
            onChange={handleFile}
          />
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
            <button
              className="adm-btn adm-btn--primary"
              onClick={() => fileRef.current?.click()}
              disabled={loading}
            >
              {loading ? 'Analyzing...' : 'Choose JSON File'}
            </button>
            {fileName && <span style={{ fontSize: 13, color: '#64748b' }}>{fileName}</span>}
          </div>
          {error && (
            <div className="adm-info-box" style={{ background: '#fef2f2', borderColor: '#fecaca', color: '#dc2626' }}>
              {error}
            </div>
          )}
          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
          </div>
        </div>
      )}

      {step === 'preview' && preview && (
        <div>
          <div className="adm-info-box">
            <strong>Team:</strong> {preview.team.name} ({preview.team.id})
            {preview.team.contact_email && <> &mdash; {preview.team.contact_email}</>}
          </div>

          {preview.errors.length > 0 && (
            <div className="adm-info-box" style={{ background: '#fef2f2', borderColor: '#fecaca', color: '#dc2626' }}>
              <strong>Errors:</strong>
              <ul style={{ margin: '4px 0 0 16px', padding: 0 }}>
                {preview.errors.map((e, i) => <li key={i}>{e}</li>)}
              </ul>
            </div>
          )}

          {preview.warnings.length > 0 && (
            <div className="adm-info-box" style={{ background: '#fffbeb', borderColor: '#fde68a', color: '#92400e' }}>
              <strong>Warnings:</strong>
              <ul style={{ margin: '4px 0 0 16px', padding: 0 }}>
                {preview.warnings.map((w, i) => <li key={i}>{w}</li>)}
              </ul>
            </div>
          )}

          {preview.actions.length > 0 && (
            <>
              <h4 style={{ fontSize: 14, marginBottom: 8 }}>Actions ({preview.actions.length})</h4>
              <table className="adm-table" style={{ marginBottom: 16 }}>
                <thead>
                  <tr><th>Type</th><th>Description</th></tr>
                </thead>
                <tbody>
                  {preview.actions.map((a, i) => (
                    <tr key={i}>
                      <td><code style={{ fontSize: 11 }}>{a.type}</code></td>
                      <td style={{ whiteSpace: 'normal' }}>{a.description}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}

          {preview.duplicates.length > 0 && (
            <>
              <h4 style={{ fontSize: 14, marginBottom: 8 }}>Duplicate Repositories</h4>
              <table className="adm-table" style={{ marginBottom: 16 }}>
                <thead>
                  <tr><th>Repo</th><th>Type</th><th>Already in Teams</th></tr>
                </thead>
                <tbody>
                  {preview.duplicates.map((d, i) => (
                    <tr key={i}>
                      <td>{d.owner}/{d.name}</td>
                      <td>{d.repo_type}</td>
                      <td>{d.existing_teams.join(', ')}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}

          {products.length > 0 && (
            <div style={{ marginBottom: 16 }}>
              <h4 style={{ fontSize: 14, marginBottom: 8 }}>Assign to Product</h4>
              <select className="adm-input" value={productId} onChange={e => setProductId(e.target.value)}>
                <option value="">— None —</option>
                {products.map(p => <option key={p.id} value={p.id}>{p.name} ({p.id})</option>)}
              </select>
            </div>
          )}

          {connections.length > 0 && (
            <>
              <h4 style={{ fontSize: 14, marginBottom: 8 }}>Source Connections</h4>
              <div className="adm-info-box" style={{ background: '#fffbeb', borderColor: '#fde68a', color: '#92400e' }}>
                Provide access tokens for each source type used by this team.
              </div>
              {connections.map((conn, idx) => (
                <div key={conn.plugin} style={{
                  background: '#f8fafc', border: '1px solid #e2e8f0', borderRadius: 6, padding: 16, marginBottom: 12,
                }}>
                  <strong style={{ fontSize: 13, textTransform: 'uppercase' }}>{conn.plugin}</strong>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, marginTop: 8 }}>
                    <div>
                      <label className="adm-label">Token *</label>
                      <input className="adm-input" type="password" value={conn.token}
                        onChange={e => updateConnection(idx, 'token', e.target.value)}
                        placeholder={`${conn.plugin} access token`} />
                    </div>
                    <div>
                      <label className="adm-label">Endpoint *</label>
                      <input className="adm-input" value={conn.endpoint}
                        onChange={e => updateConnection(idx, 'endpoint', e.target.value)}
                        placeholder="API endpoint URL" />
                    </div>
                    {conn.plugin === 'jira' && (
                      <div>
                        <label className="adm-label">Username *</label>
                        <input className="adm-input" value={conn.username}
                          onChange={e => updateConnection(idx, 'username', e.target.value)}
                          placeholder="Jira username / email" />
                      </div>
                    )}
                    <div>
                      <label className="adm-label">Token Expiry *</label>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
                        <input type="checkbox" checked={conn.expiry_date === 'never'}
                          onChange={e => updateConnection(idx, 'expiry_date', e.target.checked ? 'never' : '')} />
                        <span style={{ fontSize: 13 }}>No expiry</span>
                      </div>
                      {conn.expiry_date !== 'never' && (
                        <input className="adm-input" type="date" value={conn.expiry_date}
                          onChange={e => updateConnection(idx, 'expiry_date', e.target.value)} />
                      )}
                    </div>
                  </div>
                </div>
              ))}
            </>
          )}

          {error && (
            <div className="adm-info-box" style={{ background: '#fef2f2', borderColor: '#fecaca', color: '#dc2626' }}>
              {error}
            </div>
          )}

          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={closeModal}>Cancel</button>
            <button className="adm-btn adm-btn--ghost" onClick={() => {
              setStep('upload'); setPreview(null); setError(''); setFileName(''); setConnections([]); setProductId('')
            }}>Back</button>
            <button className="adm-btn adm-btn--primary"
              disabled={!preview.valid || (connections.length > 0 && !connectionsValid)}
              onClick={() => void handleExecute()}>
              Confirm Import
            </button>
          </div>
        </div>
      )}

      {step === 'executing' && (
        <div>
          <div className="adm-loader"><div className="diagram-card__spinner" /></div>
          <p className="adm-modal-text" style={{ textAlign: 'center', marginTop: 16 }}>Importing team...</p>
        </div>
      )}

      {step === 'report' && report && (
        <div>
          <div className="adm-info-box">
            <strong>Import complete:</strong> {report.team_name} ({report.team_id})
            <br />
            <small>Timestamp: {report.timestamp}</small>
          </div>

          <h4 style={{ fontSize: 14, marginBottom: 8 }}>Results ({report.actions.length})</h4>
          <table className="adm-table" style={{ marginBottom: 16 }}>
            <thead>
              <tr><th style={{ width: 30 }}></th><th>Type</th><th>Description</th></tr>
            </thead>
            <tbody>
              {report.actions.map((a, i) => (
                <tr key={i}>
                  <td>{statusIcon(a.status)}</td>
                  <td><code style={{ fontSize: 11 }}>{a.type}</code></td>
                  <td style={{ whiteSpace: 'normal' }}>{a.description}</td>
                </tr>
              ))}
            </tbody>
          </table>

          {report.duplicates.length > 0 && (
            <>
              <h4 style={{ fontSize: 14, marginBottom: 8 }}>Duplicate Repositories</h4>
              <table className="adm-table" style={{ marginBottom: 16 }}>
                <thead>
                  <tr><th>Repo</th><th>Type</th><th>Also in Teams</th></tr>
                </thead>
                <tbody>
                  {report.duplicates.map((d, i) => (
                    <tr key={i}>
                      <td>{d.owner}/{d.name}</td>
                      <td>{d.repo_type}</td>
                      <td>{d.existing_teams.join(', ')}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}

          {report.sync_result && (
            <>
              <h4 style={{ fontSize: 14, marginBottom: 8 }}>DevLake Sync</h4>
              <div className="adm-info-box">
                {(report.sync_result.created?.length ?? 0) > 0 && (
                  <div>Created: {report.sync_result.created!.join(', ')}</div>
                )}
                {(report.sync_result.updated?.length ?? 0) > 0 && (
                  <div>Updated: {report.sync_result.updated!.join(', ')}</div>
                )}
                {(report.sync_result.skipped?.length ?? 0) > 0 && (
                  <div>Skipped: {report.sync_result.skipped!.join(', ')}</div>
                )}
                {(report.sync_result.errors?.length ?? 0) > 0 && (
                  <div style={{ color: '#dc2626' }}>Errors: {report.sync_result.errors!.join(', ')}</div>
                )}
              </div>
            </>
          )}

          <div className="adm-modal-actions">
            <button className="adm-btn adm-btn--ghost" onClick={downloadReport}>Download Report</button>
            <button className="adm-btn adm-btn--primary" onClick={closeModal}>Done</button>
          </div>
        </div>
      )}
    </div>
  )
}
