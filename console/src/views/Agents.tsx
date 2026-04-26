import { useState, useEffect } from 'react'
import { fetchDelegations, createDelegation, updateDelegation, deleteDelegation } from '../api'
import { t } from '../i18n'

const emptyForm = { owner_id: '', owner_type: 'team', agent_id: '', agent_type: 'agent', allowed_action_types: '', allowed_resources: '', purpose: '', max_risk_class: 'high' }

export default function Agents({ lang }) {
  const [items, setItems] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState(null)
  const [form, setForm] = useState({ ...emptyForm })

  const load = () => {
    setLoading(true)
    fetchDelegations()
      .then((res) => setItems(res.data || []))
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const openCreate = () => { setForm({ ...emptyForm }); setEditingId(null); setShowForm(true) }
  const openEdit = (d) => {
    setForm({
      owner_id: d.owner_id, owner_type: d.owner_type, agent_id: d.agent_id, agent_type: d.agent_type,
      allowed_action_types: (d.allowed_action_types || []).join(', '),
      allowed_resources: (d.allowed_resources || []).join(', '),
      purpose: d.purpose, max_risk_class: d.max_risk_class,
    })
    setEditingId(d.id); setShowForm(true)
  }

  const parseList = (s) => s ? s.split(',').map((x) => x.trim()).filter(Boolean) : []

  const handleSubmit = async (e) => {
    e.preventDefault()
    try {
      const data = {
        ...form,
        allowed_action_types: parseList(form.allowed_action_types),
        allowed_resources: parseList(form.allowed_resources),
      }
      if (editingId) { await updateDelegation(editingId, data) }
      else { await createDelegation(data) }
      setShowForm(false); setEditingId(null); load()
    } catch (err) { setError(err.message) }
  }

  const handleDelete = async (id) => {
    if (!confirm(t(lang, 'confirmDelete'))) return
    try { await deleteDelegation(id); load() } catch (err) { setError(err.message) }
  }

  const toggleEnabled = async (d) => {
    try { await updateDelegation(d.id, { enabled: !d.enabled }); load() } catch (err) { setError(err.message) }
  }

  if (loading && items.length === 0) return <p className="text-gray-400">{t(lang, 'loading')}</p>

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-xl font-bold text-white">{t(lang, 'agentsTitle')}</h2>
        <button onClick={openCreate} className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded font-medium">
          + {t(lang, 'newDelegation')}
        </button>
      </div>

      {error && <p className="text-red-400 text-sm mb-3">{error}</p>}

      {showForm && (
        <form onSubmit={handleSubmit} className="bg-gray-900 border border-gray-800 rounded-lg p-4 mb-4 space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t(lang, 'delOwner')} *</label>
              <input value={form.owner_id} onChange={(e) => setForm({ ...form, owner_id: e.target.value })} required
                placeholder="team-finops" className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white" />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t(lang, 'delAgent')} *</label>
              <input value={form.agent_id} onChange={(e) => setForm({ ...form, agent_id: e.target.value })} required
                placeholder="ops-bot" className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white" />
            </div>
          </div>
          <div>
            <label className="block text-xs text-gray-400 mb-1">{t(lang, 'delAllowedActions')}</label>
            <input value={form.allowed_action_types} onChange={(e) => setForm({ ...form, allowed_action_types: e.target.value })}
              placeholder="alert.silence, alert.escalate" className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white font-mono" />
            <p className="text-xs text-gray-600 mt-1">{t(lang, 'delAllowedActionsHelp')}</p>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t(lang, 'delPurpose')}</label>
              <input value={form.purpose} onChange={(e) => setForm({ ...form, purpose: e.target.value })}
                placeholder="alert management" className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white" />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t(lang, 'delMaxRisk')}</label>
              <select value={form.max_risk_class} onChange={(e) => setForm({ ...form, max_risk_class: e.target.value })}
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white">
                {['low', 'medium', 'high', 'critical'].map((r) => <option key={r} value={r}>{r}</option>)}
              </select>
            </div>
          </div>
          <div className="flex gap-2">
            <button type="submit" className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded">{t(lang, 'save')}</button>
            <button type="button" onClick={() => setShowForm(false)} className="px-4 py-2 bg-gray-700 text-gray-300 text-sm rounded">{t(lang, 'cancel')}</button>
          </div>
        </form>
      )}

      {items.length === 0 && <p className="text-gray-500">{t(lang, 'noDelegations')}</p>}

      <div className="space-y-2">
        {items.map((d) => (
          <div key={d.id} className={`bg-gray-900 border rounded-lg p-4 ${d.enabled ? 'border-gray-800' : 'border-gray-700 opacity-50'}`}>
            <div className="flex items-center gap-3 mb-2">
              <span className="text-gray-500 text-xs">{d.owner_type}</span>
              <span className="text-white font-medium">{d.owner_id}</span>
              <span className="text-gray-500">→</span>
              <span className="text-gray-400 text-xs">{d.agent_type}</span>
              <span className="text-blue-400 font-medium">{d.agent_id}</span>
              <span className="ml-auto text-xs text-gray-500">max: {d.max_risk_class}</span>
            </div>
            {d.allowed_action_types && d.allowed_action_types.length > 0 && (
              <div className="flex flex-wrap gap-1 mb-2">
                {d.allowed_action_types.map((at) => (
                  <span key={at} className="px-2 py-0.5 bg-gray-800 text-gray-300 rounded text-xs font-mono">{at}</span>
                ))}
              </div>
            )}
            {(!d.allowed_action_types || d.allowed_action_types.length === 0) && (
              <p className="text-xs text-gray-600 mb-2">{t(lang, 'delAllActions')}</p>
            )}
            {d.purpose && <p className="text-xs text-gray-500 mb-2">{d.purpose}</p>}
            <div className="flex gap-2">
              <button onClick={() => openEdit(d)} className="px-2 py-1 bg-gray-700 text-gray-300 text-xs rounded hover:bg-gray-600">{t(lang, 'edit')}</button>
              <button onClick={() => toggleEnabled(d)} className={`px-2 py-1 text-xs rounded ${d.enabled ? 'bg-yellow-900 text-yellow-300 hover:bg-yellow-800' : 'bg-green-900 text-green-300 hover:bg-green-800'}`}>
                {d.enabled ? t(lang, 'disable') : t(lang, 'enable')}
              </button>
              <button onClick={() => handleDelete(d.id)} className="px-2 py-1 bg-red-900 text-red-300 text-xs rounded hover:bg-red-800">{t(lang, 'delete')}</button>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
