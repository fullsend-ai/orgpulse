import { useState, useEffect, useCallback } from 'react'
import {
  fetchAllProducts,
  fetchAllTeams,
  fetchOrgs,
  fetchOrgProductLinks,
  updateOrgProductLinks,
  fetchTeamProductLinks,
  createProduct,
  updateProduct,
  deleteProduct,
  fetchProductTeamLinks,
  updateProductTeamLinks,
  type FullProduct,
  type FullTeam,
  type Org,
  type TeamLink,
} from '../api'
import { entityIDError } from '../entityId'
import { Portal } from '../Portal'

function slugify(str: string): string {
  return str.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')
}

interface MyProductsPanelProps {
  email: string
  isAdmin: boolean
}

export default function MyProductsPanel({ email, isAdmin }: MyProductsPanelProps) {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [products, setProducts] = useState<FullProduct[]>([])
  const [allProductIDs, setAllProductIDs] = useState<Set<string>>(new Set())
  const [allTeams, setAllTeams] = useState<FullTeam[]>([])
  const [allOrgs, setAllOrgs] = useState<Org[]>([])
  const [teamProductMap, setTeamProductMap] = useState<Map<string, string>>(new Map())
  const [editing, setEditing] = useState<FullProduct | null>(null)
  const [showWizard, setShowWizard] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [prods, teams, orgs, tpLinks] = await Promise.all([
        fetchAllProducts(), fetchAllTeams(), fetchOrgs(), fetchTeamProductLinks(),
      ])
      setAllProductIDs(new Set(prods.map(product => product.id)))
      setProducts(prods)
      setAllTeams(teams)
      setAllOrgs(orgs)
      const tpMap = new Map<string, string>()
      for (const link of tpLinks) tpMap.set(link.team_id, link.product_id)
      setTeamProductMap(tpMap)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load products')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const handleDelete = async (product: FullProduct) => {
    if (!confirm(`Delete product "${product.name}"?\n\nThis will remove the product and all its dashboards, configuration, and team links. This cannot be undone.`)) return
    try {
      await deleteProduct(product.id)
      setEditing(null)
      void load()
    } catch (err) {
      alert(`Error deleting product: ${err instanceof Error ? err.message : 'Unknown'}`)
    }
  }

  if (loading) {
    return (
      <div className="settings-loader">
        <div className="diagram-card__spinner" />
        <div className="settings-loader-text">Loading products...</div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="sub-error">
        <p className="error-text">{error}</p>
        <button className="sub-btn sub-btn--secondary" onClick={() => void load()}>Retry</button>
      </div>
    )
  }

  return (
    <div>
      <div className="sub-header">
        <div>
          <p className="settings-panel-description">Products where you are listed as the contact.</p>
        </div>
        <button className="sub-btn sub-btn--primary" onClick={() => setShowWizard(true)}>+ New Product</button>
      </div>

      {products.length === 0 && !editing && !showWizard && (
        <div className="sub-empty">
          <p>No products found. You are not listed as the contact on any product.</p>
        </div>
      )}

      {products.length > 0 && (
        <table className="sub-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>ID</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {products.map(product => (
              <tr key={product.id}>
                <td>{product.name}</td>
                <td><code>{product.id}</code></td>
                <td className="sub-actions">
                  <button className="sub-btn sub-btn--small" onClick={() => setEditing(product)}>Edit</button>
                  {isAdmin && <button className="sub-btn sub-btn--small sub-btn--danger" onClick={() => void handleDelete(product)}>Delete</button>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {editing && (
        <ProductEditForm
          product={editing}
          allTeams={allTeams}
          email={email}
          isAdmin={isAdmin}
          onSaved={() => { setEditing(null); void load() }}
          onCancel={() => setEditing(null)}
        />
      )}

      {showWizard && (
        <ProductWizard
          email={email}
          isAdmin={isAdmin}
          allTeams={allTeams}
          allOrgs={allOrgs}
          existingProductIDs={allProductIDs}
          teamProductMap={teamProductMap}
          onCreated={() => { setShowWizard(false); void load() }}
          onCancel={() => setShowWizard(false)}
        />
      )}
    </div>
  )
}

function ProductEditForm({
  product,
  allTeams,
  email,
  isAdmin,
  onSaved,
  onCancel,
}: {
  product: FullProduct
  allTeams: FullTeam[]
  email: string
  isAdmin: boolean
  onSaved: () => void
  onCancel: () => void
}) {
  const [loading, setLoading] = useState(true)
  const [name, setName] = useState(product.name)
  const [contactEmail, setContactEmail] = useState(product.contact_email || '')
  const [authGroup, setAuthGroup] = useState(product.auth_group || '')
  const [linkedTeamIds, setLinkedTeamIds] = useState<Set<string>>(new Set())
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    void (async () => {
      try {
        const links = await fetchProductTeamLinks(product.id)
        setLinkedTeamIds(new Set(links.map(l => l.id)))
      } catch {
        // best effort
      } finally {
        setLoading(false)
      }
    })()
  }, [product.id])

  const toggleTeam = (teamId: string) => {
    setLinkedTeamIds(prev => {
      const next = new Set(prev)
      if (next.has(teamId)) next.delete(teamId)
      else next.add(teamId)
      return next
    })
  }

  const handleSave = async () => {
    if (!name.trim()) { alert('Name is required.'); return }
    setSaving(true)
    try {
      await updateProduct(product.id, {
        name: name.trim(),
        contact_email: contactEmail.trim() || email,
        auth_group: authGroup.trim(),
      })
      const entries: TeamLink[] = Array.from(linkedTeamIds).map((id, i) => ({ id, sort_order: i }))
      await updateProductTeamLinks(product.id, entries)
      onSaved()
    } catch (err) {
      alert(`Error saving product: ${err instanceof Error ? err.message : 'Unknown'}`)
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div className="settings-loader">
        <div className="diagram-card__spinner" />
        <div className="settings-loader-text">Loading product settings...</div>
      </div>
    )
  }

  return (
    <div className="sub-form">
      <h4 className="sub-form-title">Edit Product</h4>

      <div className="sub-form-row">
        <label>Name</label>
        <input className="sub-input" type="text" value={name} onChange={e => setName(e.target.value)} />
      </div>

      <div className="sub-form-row">
        <label>Authorization Group</label>
        <input
          className="sub-input"
          type="text"
          value={authGroup}
          onChange={e => setAuthGroup(e.target.value)}
        />
      </div>

      <div className="sub-form-row">
        <label>Contact Email</label>
        <input
          className="sub-input"
          type="email"
          value={contactEmail}
          onChange={e => setContactEmail(e.target.value)}
          readOnly={!isAdmin}
        />
        {!isAdmin && (
          <span className="sub-empty-hint">Contact email is set from your authenticated identity.</span>
        )}
      </div>

      <div className="sub-form-section">
        <div className="sub-form-section-header">
          <label>Team Associations</label>
        </div>
        <div className="mp-team-checklist">
          {allTeams.length === 0 ? (
            <p className="sub-empty-hint">No teams available.</p>
          ) : (
            allTeams.map(team => (
              <label key={team.id} className="mp-team-item">
                <input
                  type="checkbox"
                  checked={linkedTeamIds.has(team.id)}
                  onChange={() => toggleTeam(team.id)}
                />
                <span>{team.name} <code>{team.id}</code></span>
              </label>
            ))
          )}
        </div>
      </div>

      <div className="sub-form-actions">
        <button
          className="sub-btn sub-btn--primary"
          disabled={saving || !name.trim()}
          onClick={() => void handleSave()}
        >
          {saving ? 'Saving...' : 'Save'}
        </button>
        <button className="sub-btn sub-btn--secondary" onClick={onCancel}>Cancel</button>
      </div>
    </div>
  )
}

function ProductWizard({
  email,
  isAdmin,
  allTeams,
  allOrgs,
  existingProductIDs,
  teamProductMap,
  onCreated,
  onCancel,
}: {
  email: string
  isAdmin: boolean
  allTeams: FullTeam[]
  allOrgs: Org[]
  existingProductIDs: Set<string>
  teamProductMap: Map<string, string>
  onCreated: () => void
  onCancel: () => void
}) {
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [selectedOrg, setSelectedOrg] = useState('')
  const [productName, setProductName] = useState('')
  const [productId, setProductId] = useState('')
  const [productEmail, setProductEmail] = useState(email)
  const [productAuthGroup, setProductAuthGroup] = useState('')
  const [idManuallyEdited, setIdManuallyEdited] = useState(false)
  const [selectedTeams, setSelectedTeams] = useState<string[]>([])
  const productIDError = entityIDError(productId, existingProductIDs, 'Product ID')

  const availableTeams = allTeams.filter(t => !teamProductMap.has(t.id))

  const toggleTeam = (teamId: string) => {
    setSelectedTeams(prev =>
      prev.includes(teamId) ? prev.filter(id => id !== teamId) : [...prev, teamId],
    )
  }

  const handleCreate = async () => {
    if (!selectedOrg) { setError('Organization is required.'); return }
    if (!productName.trim()) { setError('Product name is required.'); return }
    if (productIDError) { setError(productIDError); return }
    if (!productAuthGroup.trim()) { setError('Authorization Group is required.'); return }
    if (selectedTeams.length === 0) { setError('At least one team must be selected.'); return }

    setCreating(true)
    setError(null)
    try {
      const trimmedId = productId.trim()
      await createProduct({
        id: trimmedId,
        name: productName.trim(),
        contact_email: productEmail.trim() || email,
        auth_group: productAuthGroup.trim(),
      })
      const entries: TeamLink[] = selectedTeams.map((id, i) => ({ id, sort_order: i }))
      await updateProductTeamLinks(trimmedId, entries)
      const existing = await fetchOrgProductLinks(selectedOrg)
      await updateOrgProductLinks(selectedOrg, [...existing, { id: trimmedId, sort_order: existing.length }])
      onCreated()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Creation failed')
      setCreating(false)
    }
  }

  return (
    <Portal>
    <div className="mt-wizard-overlay active" onClick={e => { if (e.target === e.currentTarget) onCancel() }}>
      <div className="mt-wizard-modal">
        <div className="mt-wizard-header">
          <h3>New Product</h3>
          <button className="mt-wizard-close" onClick={onCancel}>&times;</button>
        </div>

        <div className="mt-wizard-body">
          {error && <div className="mt-wizard-error">{error}</div>}

          <div className="sub-form-row">
            <label>Organization <span style={{ color: '#c9190b' }}>*</span></label>
            <select className="sub-input" value={selectedOrg} onChange={e => setSelectedOrg(e.target.value)}>
              <option value="">-- Select Organization --</option>
              {allOrgs.map(o => <option key={o.id} value={o.id}>{o.name}</option>)}
            </select>
            <span className="sub-empty-hint">The organization this product belongs to.</span>
          </div>

          <div className="sub-form-row">
            <label>Product Name <span style={{ color: '#c9190b' }}>*</span></label>
            <input
              className="sub-input"
              type="text"
              placeholder="e.g. Developer Platform"
              value={productName}
              onChange={e => {
                setProductName(e.target.value)
                if (!idManuallyEdited) setProductId(slugify(e.target.value))
              }}
            />
          </div>

          <div className="sub-form-row">
            <label>Product ID <span style={{ color: '#c9190b' }}>*</span></label>
            <input
              className="sub-input"
              type="text"
              placeholder="auto-generated from name"
              value={productId}
              onChange={e => { setProductId(e.target.value); setIdManuallyEdited(true) }}
            />
            <span className="sub-empty-hint">Unique identifier (kebab-case). Auto-generated from name.</span>
            {productId.trim() && productIDError && <span className="error-text">{productIDError}</span>}
          </div>

          <div className="sub-form-row">
            <label>Authorization Group <span style={{ color: '#c9190b' }}>*</span></label>
            <input
              className="sub-input"
              type="text"
              placeholder="e.g. my-ldap-group"
              value={productAuthGroup}
              onChange={e => setProductAuthGroup(e.target.value)}
            />
            <span className="sub-empty-hint">The authorization group used to grant access to this product.</span>
          </div>

          <div className="sub-form-row">
            <label>Contact Email</label>
            <input
              className="sub-input"
              type="email"
              value={productEmail}
              onChange={e => setProductEmail(e.target.value)}
              readOnly={!isAdmin}
            />
            {!isAdmin && (
              <span className="sub-empty-hint">Contact email is set from your authenticated identity.</span>
            )}
          </div>

          <div className="sub-form-section">
            <div className="sub-form-section-header">
              <label>Team Associations <span style={{ color: '#c9190b' }}>*</span></label>
            </div>
            <div className="mt-wizard-section-hint">Select at least one team. Only your teams and unassociated teams are shown.</div>
            <div className="mp-team-checklist">
              {availableTeams.length === 0 ? (
                <p className="sub-empty-hint">No teams available. Create teams first in My Teams.</p>
              ) : (
                availableTeams.map(team => (
                  <label key={team.id} className="mp-team-item">
                    <input
                      type="checkbox"
                      checked={selectedTeams.includes(team.id)}
                      onChange={() => toggleTeam(team.id)}
                    />
                    <span>{team.name} <code>{team.id}</code></span>
                  </label>
                ))
              )}
            </div>
          </div>
        </div>

        <div className="mt-wizard-footer">
          <div />
          <div className="mt-wizard-footer-right">
            <button className="sub-btn sub-btn--secondary" onClick={onCancel}>Cancel</button>
            <button
              className="sub-btn sub-btn--primary"
              disabled={creating || productIDError !== null || !selectedOrg}
              onClick={() => void handleCreate()}
            >
              {creating ? 'Creating...' : 'Create Product'}
            </button>
          </div>
        </div>
      </div>
    </div>
    </Portal>
  )
}
