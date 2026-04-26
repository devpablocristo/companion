import { useState, useEffect } from 'react'
import { fetchActionTypes, createActionType, updateActionType, deleteActionType } from '../api'
import { t } from '../i18n'

const RISK_CLASSES = ['low', 'medium', 'high', 'critical']
const riskColor = { low: 'text-green-400', medium: 'text-yellow-400', high: 'text-red-400', critical: 'text-red-300 bg-red-900/30' }

const emptyForm = { name: '', description: '', category: '', risk_class: 'low', reversible: true, requires_break_glass: false }

export default function ActionTypes({ lang }) {
  const [items, setItems] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState(null)
  const [form, setForm] = useState({ ...emptyForm })

  const load = () => {
    setLoading(true)
    fetchActionTypes()
      .then((res) => setItems(res.data || []))
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const openCreate = () => { setForm({ ...emptyForm }); setEditingId(null); setShowForm(true) }
  const openEdit = (at) => {
    setForm({ name: at.name, description: at.description, category: at.category, risk_class: at.risk_class, reversible: at.reversible, requires_break_glass: at.requires_break_glass })
    setEditingId(at.id); setShowForm(true)
  }

  const handleSubmit = async (e) => {
    e.preventDefault()
    try {
      if (editingId) { await updateActionType(editingId, form) }
      else { await createActionType(form) }
      setShowForm(false); setEditingId(null); load()
    } catch (err) { setError(err.message) }
  }

  const handleDelete = async (id) => {
    if (!confirm(t(lang, 'confirmDelete'))) return
    try { await deleteActionType(id); load() } catch (err) { setError(err.message) }
  }

  if (loading && items.length === 0) return <p className="text-gray-400">{t(lang, 'loading')}</p>

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-xl font-bold text-white">{t(lang, 'actionTypesTitle')}</h2>
        <button onClick={openCreate} className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded font-medium">
          + {t(lang, 'newActionType')}
        </button>
      </div>

      {error && <p className="text-red-400 text-sm mb-3">{error}</p>}

      {showForm && (
        <form onSubmit={handleSubmit} className="bg-gray-900 border border-gray-800 rounded-lg p-4 mb-4 space-y-3">
          <div className="grid grid-cols-3 gap-3">
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t(lang, 'atName')} *</label>
              <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required
                placeholder="treasury.transfer" className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white font-mono" />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t(lang, 'atCategory')}</label>
              <input value={form.category} onChange={(e) => setForm({ ...form, category: e.target.value })}
                placeholder="treasury" className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white" />
            </div>
            <div>
              <label className="block text-xs text-gray-400 mb-1">{t(lang, 'atRiskClass')}</label>
              <select value={form.risk_class} onChange={(e) => setForm({ ...form, risk_class: e.target.value })}
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white">
                {RISK_CLASSES.map((r) => <option key={r} value={r}>{r}</option>)}
              </select>
            </div>
          </div>
          <div>
            <label className="block text-xs text-gray-400 mb-1">{t(lang, 'policyDescription')}</label>
            <input value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })}
              className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white" />
          </div>
          <div className="flex items-center gap-6">
            <label className="flex items-center gap-2 text-sm text-gray-300">
              <input type="checkbox" checked={form.reversible} onChange={(e) => setForm({ ...form, reversible: e.target.checked })} />
              {t(lang, 'atReversible')}
            </label>
            <label className="flex items-center gap-2 text-sm text-gray-300">
              <input type="checkbox" checked={form.requires_break_glass} onChange={(e) => setForm({ ...form, requires_break_glass: e.target.checked })} />
              {t(lang, 'atBreakGlass')}
            </label>
          </div>
          <div className="flex gap-2">
            <button type="submit" className="px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white text-sm rounded">{t(lang, 'save')}</button>
            <button type="button" onClick={() => setShowForm(false)} className="px-4 py-2 bg-gray-700 text-gray-300 text-sm rounded">{t(lang, 'cancel')}</button>
          </div>
        </form>
      )}

      <div className="border border-gray-800 rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-gray-800/50">
            <tr className="text-gray-400 text-left">
              <th className="px-4 py-2">{t(lang, 'atName')}</th>
              <th className="px-4 py-2">{t(lang, 'atCategory')}</th>
              <th className="px-4 py-2">{t(lang, 'atRiskClass')}</th>
              <th className="px-4 py-2">{t(lang, 'atReversible')}</th>
              <th className="px-4 py-2">{t(lang, 'breakGlass')}</th>
              <th className="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800">
            {items.map((at) => (
              <tr key={at.id} className="hover:bg-gray-800/30">
                <td className="px-4 py-2.5 text-white font-mono text-xs">{at.name}</td>
                <td className="px-4 py-2.5 text-gray-400 text-xs">{at.category || '—'}</td>
                <td className="px-4 py-2.5"><span className={`text-xs font-medium px-2 py-0.5 rounded ${riskColor[at.risk_class] || 'text-gray-400'}`}>{at.risk_class}</span></td>
                <td className="px-4 py-2.5 text-xs">{at.reversible ? <span className="text-green-400">✓</span> : <span className="text-red-400">✗</span>}</td>
                <td className="px-4 py-2.5 text-xs">{at.requires_break_glass ? <span className="text-red-300">⚠</span> : '—'}</td>
                <td className="px-4 py-2.5 flex gap-1">
                  <button onClick={() => openEdit(at)} className="px-2 py-1 bg-gray-700 text-gray-300 text-xs rounded hover:bg-gray-600">{t(lang, 'edit')}</button>
                  <button onClick={() => handleDelete(at.id)} className="px-2 py-1 bg-red-900 text-red-300 text-xs rounded hover:bg-red-800">{t(lang, 'delete')}</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
