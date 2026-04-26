import { useState, useEffect } from 'react'
import { fetchPolicies, createPolicy, updatePolicy, deletePolicy, archivePolicy, restorePolicy } from '../api'
import { t } from '../i18n'

const EFFECTS = ['allow', 'deny', 'require_approval']
const RISK_LEVELS = ['', 'low', 'medium', 'high']

const emptyForm = {
  name: '', description: '', expression: '', effect: 'allow',
  priority: '10', mode: 'enforced', enabled: true, action_type: '', target_system: '', risk_override: '',
}

export default function Policies({ lang }) {
  const [policies, setPolicies] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [showArchived, setShowArchived] = useState(false)
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState(null)
  const [form, setForm] = useState({ ...emptyForm })

  const load = async () => {
    try {
      setLoading(true)
      const res = await fetchPolicies(showArchived)
      setPolicies(res.data || [])
      setError(null)
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [showArchived])

  const openCreate = () => {
    setForm({ ...emptyForm })
    setEditingId(null)
    setShowForm(true)
  }

  const openEdit = (p) => {
    setForm({
      name: p.name,
      description: p.description || '',
      expression: p.expression,
      effect: p.effect,
      priority: String(p.priority ?? '10'),
      mode: p.mode || 'enforced',
      enabled: p.enabled,
      action_type: p.action_type || '',
      target_system: p.target_system || '',
      risk_override: p.risk_override || '',
    })
    setEditingId(p.id)
    setShowForm(true)
  }

  const handleSubmit = async (e) => {
    e.preventDefault()
    try {
      const data: Record<string, any> = {
        name: form.name,
        description: form.description,
        expression: form.expression,
        effect: form.effect,
        priority: parseInt(form.priority, 10),
        enabled: form.enabled,
      }
      if (form.action_type) data.action_type = form.action_type
      if (form.target_system) data.target_system = form.target_system
      if (form.risk_override) data.risk_override = form.risk_override
      data.mode = form.mode

      if (editingId) {
        await updatePolicy(editingId, data)
      } else {
        await createPolicy(data)
      }
      setShowForm(false)
      setEditingId(null)
      await load()
    } catch (err) {
      setError(err.message)
    }
  }

  const handleDelete = async (id) => {
    if (!confirm(t(lang, 'confirmDelete'))) return
    try {
      await deletePolicy(id)
      await load()
    } catch (err) {
      setError(err.message)
    }
  }

  const handleArchive = async (id) => {
    try {
      await archivePolicy(id)
      await load()
    } catch (err) {
      setError(err.message)
    }
  }

  const handleRestore = async (id) => {
    try {
      await restorePolicy(id)
      await load()
    } catch (err) {
      setError(err.message)
    }
  }

  const effectColor = (effect) => {
    if (effect === 'allow') return 'text-green-400'
    if (effect === 'deny') return 'text-red-400'
    return 'text-yellow-400'
  }

  if (loading && policies.length === 0) return <p className="text-gray-500">{t(lang, 'loading')}</p>

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-xl font-bold">{t(lang, 'policiesTitle')}</h2>
        <div className="flex items-center gap-3">
          <label className="flex items-center gap-2 text-sm text-gray-400 cursor-pointer">
            <input
              type="checkbox"
              checked={showArchived}
              onChange={(e) => setShowArchived(e.target.checked)}
              className="rounded bg-gray-800 border-gray-600"
            />
            {t(lang, 'showArchived')}
          </label>
          <button
            onClick={openCreate}
            className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded font-medium transition-colors"
          >
            + {t(lang, 'newPolicy')}
          </button>
        </div>
      </div>

      {error && <p className="text-red-400 mb-3">{error}</p>}

      {/* Formulario crear/editar */}
      {showForm && (
        <form onSubmit={handleSubmit} className="bg-gray-900 border border-gray-800 rounded-lg p-4 mb-4 space-y-3">
          <h3 className="font-medium text-white mb-2">
            {editingId ? t(lang, 'editPolicy') : t(lang, 'newPolicy')}
          </h3>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t(lang, 'policyName')} *</label>
              <input
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                required
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white"
                placeholder="deny-delete-production"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t(lang, 'policyEffect')} *</label>
              <select
                value={form.effect}
                onChange={(e) => setForm({ ...form, effect: e.target.value })}
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white"
              >
                {EFFECTS.map((eff) => (
                  <option key={eff} value={eff}>{t(lang, `effect${eff.charAt(0).toUpperCase() + eff.slice(1).replace(/_([a-z])/g, (_, c) => c.toUpperCase())}`)}</option>
                ))}
              </select>
            </div>
          </div>
          <div>
            <label className="block text-xs text-gray-400 mb-1">{t(lang, 'policyExpression')} *</label>
            <input
              value={form.expression}
              onChange={(e) => setForm({ ...form, expression: e.target.value })}
              required
              className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white font-mono"
              placeholder='request.action_type == "delete" && request.target_system == "production"'
            />
          </div>
          <div>
            <label className="block text-xs text-gray-400 mb-1">{t(lang, 'policyDescription')}</label>
            <input
              value={form.description}
              onChange={(e) => setForm({ ...form, description: e.target.value })}
              className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white"
              placeholder={t(lang, 'optional')}
            />
          </div>
          <div className="grid grid-cols-4 gap-3">
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t(lang, 'policyPriority')}</label>
              <input
                type="number"
                value={form.priority}
                onChange={(e) => setForm({ ...form, priority: e.target.value })}
                min="1"
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t(lang, 'policyActionType')}</label>
              <input
                value={form.action_type}
                onChange={(e) => setForm({ ...form, action_type: e.target.value })}
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white"
                placeholder={t(lang, 'optional')}
              />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t(lang, 'policyTargetSystem')}</label>
              <input
                value={form.target_system}
                onChange={(e) => setForm({ ...form, target_system: e.target.value })}
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white"
                placeholder={t(lang, 'optional')}
              />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t(lang, 'policyRiskOverride')}</label>
              <select
                value={form.risk_override}
                onChange={(e) => setForm({ ...form, risk_override: e.target.value })}
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white"
              >
                {RISK_LEVELS.map((r) => (
                  <option key={r} value={r}>{r || t(lang, 'none')}</option>
                ))}
              </select>
            </div>
          </div>
          <div className="flex items-center gap-6">
            <label className="flex items-center gap-2 text-sm text-gray-300 cursor-pointer">
              <input
                type="checkbox"
                checked={form.enabled}
                onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
                className="rounded bg-gray-800 border-gray-600"
              />
              {t(lang, 'policyEnabled')}
            </label>
            <div className="flex items-center gap-2">
              <span className="text-xs text-gray-400">{t(lang, 'policyMode')}:</span>
              <button
                type="button"
                onClick={() => setForm({ ...form, mode: form.mode === 'enforced' ? 'shadow' : 'enforced' })}
                className={`px-3 py-1 rounded text-xs font-medium transition-colors ${
                  form.mode === 'shadow'
                    ? 'bg-purple-900 text-purple-300 border border-purple-700'
                    : 'bg-gray-700 text-gray-300 border border-gray-600'
                }`}
              >
                {form.mode === 'shadow' ? t(lang, 'modeShadow') : t(lang, 'modeEnforced')}
              </button>
            </div>
          </div>
          <div className="flex gap-2 pt-2">
            <button
              type="submit"
              className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded font-medium transition-colors"
            >
              {t(lang, 'save')}
            </button>
            <button
              type="button"
              onClick={() => { setShowForm(false); setEditingId(null) }}
              className="px-4 py-2 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded font-medium transition-colors"
            >
              {t(lang, 'cancel')}
            </button>
          </div>
        </form>
      )}

      {/* Lista */}
      {policies.length === 0 && <p className="text-gray-500">{t(lang, 'noPolicies')}</p>}
      <div className="space-y-2">
        {policies.map((p) => (
          <div key={p.id} className={`bg-gray-900 border rounded-lg p-4 ${p.archived_at ? 'border-gray-700 opacity-60' : 'border-gray-800'}`}>
            <div className="flex items-center gap-3 mb-1">
              <span className="font-medium text-white">{p.name}</span>
              <span className={`text-xs font-mono px-2 py-0.5 rounded ${effectColor(p.effect)} bg-gray-800`}>
                {p.effect}
              </span>
              <span className="text-xs text-gray-500">#{p.priority}</span>
              {p.origin === 'learned' && (
                <span className="text-xs bg-purple-900 text-purple-300 px-2 py-0.5 rounded">{t(lang, 'learned')}</span>
              )}
              {p.mode === 'shadow' && (
                <span className="text-xs bg-purple-900 text-purple-300 px-2 py-0.5 rounded">
                  {t(lang, 'modeShadow')} {p.shadow_hits > 0 && `(${p.shadow_hits} hits)`}
                </span>
              )}
              {!p.enabled && (
                <span className="text-xs bg-gray-800 text-gray-500 px-2 py-0.5 rounded">{t(lang, 'disabled')}</span>
              )}
              {p.archived_at && (
                <span className="text-xs bg-gray-800 text-yellow-500 px-2 py-0.5 rounded">{t(lang, 'archived')}</span>
              )}
            </div>
            <p className="text-xs font-mono text-gray-400 mb-1">{p.expression}</p>
            {p.description && <p className="text-xs text-gray-500 mb-2">{p.description}</p>}
            <div className="flex items-center gap-2 text-xs text-gray-600">
              {p.action_type && <span>action: {p.action_type}</span>}
              {p.target_system && <span>target: {p.target_system}</span>}
              {p.risk_override && <span>risk: {p.risk_override}</span>}
            </div>
            <div className="flex gap-2 mt-3">
              {!p.archived_at && (
                <>
                  <button onClick={() => openEdit(p)}
                    className="px-3 py-1 bg-gray-700 hover:bg-gray-600 text-gray-300 text-xs rounded transition-colors">
                    {t(lang, 'edit')}
                  </button>
                  <button onClick={() => handleArchive(p.id)}
                    className="px-3 py-1 bg-yellow-900 hover:bg-yellow-800 text-yellow-300 text-xs rounded transition-colors">
                    {t(lang, 'archive')}
                  </button>
                </>
              )}
              {p.archived_at && (
                <button onClick={() => handleRestore(p.id)}
                  className="px-3 py-1 bg-green-900 hover:bg-green-800 text-green-300 text-xs rounded transition-colors">
                  {t(lang, 'restore')}
                </button>
              )}
              <button onClick={() => handleDelete(p.id)}
                className="px-3 py-1 bg-red-900 hover:bg-red-800 text-red-300 text-xs rounded transition-colors">
                {t(lang, 'delete')}
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
