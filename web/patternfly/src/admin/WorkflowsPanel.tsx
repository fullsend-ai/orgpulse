import { useState, useEffect, useCallback, useMemo } from 'react'
import { adminApi, AuthError } from './api'
import { useAdmin } from './AdminContext'

// Locks are held by the VS Code extension with a short TTL, so poll to keep lock status current.
const REFRESH_INTERVAL_MS = 15000

interface Workflow {
  id: number
  name: string
  description?: string
  version: number
  has_draft?: boolean
  production_version?: number
  archived?: boolean
  updated_at?: string
  locked: boolean
  locked_session?: string
}

interface WorkflowVersion {
  version: number
  publish_note?: string
  production: boolean
  published_at?: string
}

function VersionBadge({ version, kind }: { version?: number; kind: 'staging' | 'prod' }) {
  if (!version) return <span className="adm-wf-badge adm-wf-badge--none">none</span>
  return <span className={`adm-wf-badge adm-wf-badge--${kind}`}>v{version}</span>
}

function LockBadge({ locked }: { locked: boolean }) {
  return locked
    ? <span className="adm-wf-badge adm-wf-badge--locked" title="Being edited in another session">&#128274; locked</span>
    : <span className="adm-wf-badge adm-wf-badge--none">unlocked</span>
}

function VersionHistoryModal({
  workflowId,
  onChanged,
  onClose,
}: {
  workflowId: number
  onChanged: () => void
  onClose: () => void
}) {
  const { toast } = useAdmin()
  const [workflow, setWorkflow] = useState<Workflow | null>(null)
  const [versions, setVersions] = useState<WorkflowVersion[] | null>(null)
  const [pending, setPending] = useState<number | null>(null)
  const [saving, setSaving] = useState(false)

  const fetchWorkflow = useCallback(async () => {
    const list = await adminApi<Workflow[]>('GET', '/api/v1/workflows')
    return list.find(w => w.id === workflowId) ?? null
  }, [workflowId])

  const load = useCallback(async () => {
    try {
      const [wf, vs] = await Promise.all([
        fetchWorkflow(),
        adminApi<WorkflowVersion[]>('GET', `/api/v1/workflows/${workflowId}/versions`),
      ])
      setWorkflow(wf)
      setVersions([...vs].sort((a, b) => b.version - a.version))
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to load versions', 'error')
    }
  }, [workflowId, fetchWorkflow, toast])

  useEffect(() => {
    void load()
    const t = setInterval(() => void load(), REFRESH_INTERVAL_MS)
    return () => clearInterval(t)
  }, [load])

  const blocked = !workflow || workflow.locked || !!workflow.archived
  const blockedReason = workflow?.locked
    ? 'Workflow is locked by an editing session'
    : workflow?.archived
      ? 'Archived workflows cannot be promoted'
      : undefined

  const setProduction = async (version: number) => {
    setSaving(true)
    try {
      // Re-check the lock right before promoting; it may have been acquired since the last poll.
      const wf = await fetchWorkflow()
      setWorkflow(wf)
      if (!wf || wf.locked) {
        toast('Workflow is locked by an editing session', 'error')
        return
      }
      await adminApi('POST', `/api/v1/workflows/${workflowId}/versions/${version}/production`)
      toast(`Version ${version} set as production`, 'success')
      onChanged()
      await load()
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to set production version', 'error')
    } finally {
      setSaving(false)
      setPending(null)
    }
  }

  return (
    <div>
      <h3 className="adm-modal-title">Version History: {workflow?.name ?? `#${workflowId}`}</h3>
      {workflow && (
        <p className="adm-modal-text">
          Staging <VersionBadge version={workflow.version} kind="staging" />{' '}
          Production <VersionBadge version={workflow.production_version} kind="prod" />{' '}
          <LockBadge locked={workflow.locked} />
        </p>
      )}
      {workflow?.locked && (
        <div className="adm-info-box">
          This workflow is currently locked by an editing session. Switching the production version is
          disabled until the lock is released.
        </div>
      )}
      {versions === null ? (
        <div className="adm-loader"><div className="diagram-card__spinner" /></div>
      ) : versions.length === 0 ? (
        <div className="adm-empty">No published versions yet.</div>
      ) : (
        <div className="adm-wf-versions">
          {versions.map(v => (
            <div key={v.version} className="adm-wf-version-row">
              <span className="adm-wf-version-num">v{v.version}</span>
              <span className="adm-wf-version-date">
                {v.published_at ? new Date(v.published_at).toLocaleString() : ''}
              </span>
              {v.production ? (
                <span className="adm-wf-badge adm-wf-badge--prod">production</span>
              ) : pending === v.version ? (
                <>
                  <span className="adm-wf-version-confirm">Set v{v.version} live for all consumers?</span>
                  <button className="adm-btn adm-btn--ghost adm-btn--sm" disabled={saving} onClick={() => setPending(null)}>
                    Cancel
                  </button>
                  <button
                    className="adm-btn adm-btn--danger adm-btn--sm"
                    disabled={saving || blocked}
                    onClick={() => void setProduction(v.version)}
                  >
                    {saving ? 'Switching...' : 'Confirm'}
                  </button>
                </>
              ) : (
                <button
                  className="adm-btn adm-btn--ghost adm-btn--sm"
                  disabled={blocked || pending !== null}
                  title={blockedReason}
                  onClick={() => setPending(v.version)}
                >
                  Set as Production
                </button>
              )}
              {v.publish_note && <div className="adm-wf-version-note">{v.publish_note}</div>}
            </div>
          ))}
        </div>
      )}
      <div className="adm-modal-actions">
        <button className="adm-btn adm-btn--ghost" onClick={onClose}>Close</button>
      </div>
    </div>
  )
}

export default function WorkflowsPanel() {
  const { toast, openModal, closeModal } = useAdmin()
  const [items, setItems] = useState<Workflow[]>([])
  const [search, setSearch] = useState('')
  const [showArchived, setShowArchived] = useState(false)

  const load = useCallback(async () => {
    try {
      const data = await adminApi<Workflow[]>('GET', '/api/v1/workflows')
      setItems(data)
    } catch (e) {
      if (e instanceof AuthError) return
      toast(e instanceof Error ? e.message : 'Failed to load workflows', 'error')
    }
  }, [toast])

  useEffect(() => {
    void load()
    const t = setInterval(() => void load(), REFRESH_INTERVAL_MS)
    return () => clearInterval(t)
  }, [load])

  const filtered = useMemo(() => {
    const q = search.toLowerCase()
    return items
      .filter(w => !!w.archived === showArchived)
      .filter(w => !q || w.name.toLowerCase().includes(q) || (w.description || '').toLowerCase().includes(q))
  }, [items, search, showArchived])

  const showHistory = (wf: Workflow) => {
    openModal(<VersionHistoryModal workflowId={wf.id} onChanged={() => void load()} onClose={closeModal} />)
  }

  return (
    <div>
      <div className="adm-toolbar">
        <h2>Workflows</h2>
        <input
          className="adm-wf-search"
          type="text"
          placeholder="Search workflows..."
          value={search}
          onChange={e => setSearch(e.target.value)}
        />
        <label className="adm-wf-toggle">
          <input type="checkbox" checked={showArchived} onChange={e => setShowArchived(e.target.checked)} />
          Archived
        </label>
        <button className="adm-btn adm-btn--ghost" onClick={() => void load()}>Refresh</button>
      </div>
      <div className="adm-info-box">
        Workflows are edited with the VS Code extension. Use the history to switch which published version
        serves production.
      </div>
      {filtered.length === 0 ? (
        <div className="adm-empty">
          {search ? 'No matching workflows.' : showArchived ? 'No archived workflows.' : 'No workflows found.'}
        </div>
      ) : (
        <table className="adm-table">
          <thead>
            <tr>
              <th>#</th>
              <th>Name</th>
              <th>Description</th>
              <th>Staging</th>
              <th>Production</th>
              <th>Lock</th>
              <th>Updated</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map(wf => (
              <tr key={wf.id}>
                <td>{wf.id}</td>
                <td>{wf.name}</td>
                <td className="truncate" title={wf.description}>{wf.description || '—'}</td>
                <td>
                  <VersionBadge version={wf.version} kind="staging" />
                  {wf.has_draft && <span className="adm-wf-badge adm-wf-badge--draft">draft</span>}
                </td>
                <td><VersionBadge version={wf.production_version} kind="prod" /></td>
                <td><LockBadge locked={wf.locked} /></td>
                <td>{wf.updated_at || '—'}</td>
                <td className="adm-actions">
                  <button className="adm-icon-btn" title="Version history" onClick={() => showHistory(wf)}>&#128337;</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
