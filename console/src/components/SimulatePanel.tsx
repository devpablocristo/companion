import { useState } from 'react'
import { simulateRequest } from '../api'
import { t } from '../i18n'
import RiskBadge from './RiskBadge'
import StatusBadge from './StatusBadge'

const INITIAL = {
  requester_type: 'agent',
  requester_id: '',
  action_type: '',
  target_system: '',
  target_resource: '',
  reason: '',
}

export default function SimulatePanel({ lang, open, onClose }) {
  const [form, setForm] = useState(INITIAL)
  const [result, setResult] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  const set = (k, v) => setForm({ ...form, [k]: v })

  const run = () => {
    setLoading(true)
    setError(null)
    setResult(null)
    simulateRequest(form)
      .then((r) => setResult(r))
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false))
  }

  const reset = () => {
    setForm(INITIAL)
    setResult(null)
    setError(null)
  }

  if (!open) return null

  return (
    <>
      {/* Overlay */}
      <div className="fixed inset-0 bg-black/50 z-40" onClick={onClose} />

      {/* Panel */}
      <div className="fixed right-0 top-0 h-full w-[480px] bg-gray-950 border-l border-gray-800 z-50 flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-800">
          <div>
            <h3 className="text-white font-bold">{t(lang, 'simulateTitle')}</h3>
            <p className="text-gray-500 text-xs mt-0.5">{t(lang, 'simulateDesc')}</p>
          </div>
          <button onClick={onClose} className="text-gray-500 hover:text-white text-xl">&times;</button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-5 py-4 space-y-3">
          {/* Formulario */}
          <div className="grid grid-cols-2 gap-3">
            <Field label={t(lang, 'requester')}>
              <select value={form.requester_type} onChange={(e) => set('requester_type', e.target.value)}
                className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-white text-sm">
                <option value="agent">agent</option>
                <option value="service">service</option>
                <option value="human">human</option>
              </select>
            </Field>
            <Field label="Requester ID">
              <input type="text" value={form.requester_id} onChange={(e) => set('requester_id', e.target.value)}
                placeholder="ops-bot" className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-white text-sm" />
            </Field>
          </div>

          <Field label="Action Type *">
            <input type="text" value={form.action_type} onChange={(e) => set('action_type', e.target.value)}
              placeholder="alert.silence" className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-white text-sm" />
          </Field>

          <div className="grid grid-cols-2 gap-3">
            <Field label="Target System">
              <input type="text" value={form.target_system} onChange={(e) => set('target_system', e.target.value)}
                placeholder="pagerduty" className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-white text-sm" />
            </Field>
            <Field label="Target Resource">
              <input type="text" value={form.target_resource} onChange={(e) => set('target_resource', e.target.value)}
                placeholder="CPU-CRITICAL-PROD" className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-white text-sm" />
            </Field>
          </div>

          <Field label={t(lang, 'reason')}>
            <input type="text" value={form.reason} onChange={(e) => set('reason', e.target.value)}
              placeholder="maintenance window" className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-white text-sm" />
          </Field>

          <div className="flex gap-2 pt-2">
            <button onClick={run} disabled={loading || !form.action_type}
              className="flex-1 px-4 py-2 bg-blue-600 text-white rounded text-sm font-medium hover:bg-blue-500 disabled:opacity-50">
              {loading ? t(lang, 'simulating') : t(lang, 'runSimulation')}
            </button>
            <button onClick={reset} className="px-4 py-2 bg-gray-700 text-gray-300 rounded text-sm hover:bg-gray-600">
              {t(lang, 'clear')}
            </button>
          </div>

          {error && <p className="text-red-400 text-sm">{error}</p>}

          {/* Resultado */}
          {result && (
            <div className="mt-4 space-y-3">
              <div className="border-t border-gray-800 pt-4">
                <h4 className="text-xs font-semibold text-gray-400 uppercase mb-3">{t(lang, 'simulateResult')}</h4>

                {/* Decision card */}
                <div className={`rounded-lg p-4 border ${
                  result.decision === 'allow' ? 'border-green-800 bg-green-900/20' :
                  result.decision === 'deny' ? 'border-red-800 bg-red-900/20' :
                  'border-yellow-800 bg-yellow-900/20'
                }`}>
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-white font-bold text-lg uppercase">{result.decision}</span>
                    <RiskBadge level={result.risk_level} />
                  </div>
                  <p className="text-gray-400 text-sm">{result.decision_reason}</p>
                  <div className="flex items-center gap-3 mt-2">
                    <StatusBadge status={result.status} />
                    {result.would_require_approval && (
                      <span className="text-yellow-400 text-xs">{t(lang, 'wouldRequireApproval')}</span>
                    )}
                  </div>
                  {result.policy_matched && (
                    <p className="text-gray-500 text-xs mt-2">{t(lang, 'policyMatched')}: {result.policy_matched}</p>
                  )}
                </div>
              </div>

              {/* Risk factors */}
              {result.risk_assessment && (
                <div>
                  <h4 className="text-xs font-semibold text-gray-400 uppercase mb-2">{t(lang, 'riskFactors')}</h4>
                  <div className="space-y-1.5">
                    {(result.risk_assessment.factors || []).map((f, i) => (
                      <div key={i} className={`flex items-center gap-2 px-3 py-1.5 rounded text-xs ${
                        f.active ? 'bg-gray-800' : 'bg-gray-900/50 opacity-50'
                      }`}>
                        <span className={`w-2 h-2 rounded-full ${f.active ? 'bg-yellow-400' : 'bg-gray-600'}`} />
                        <span className="text-gray-300 font-mono flex-1">{f.name}</span>
                        <span className={`font-medium ${f.score > 0 ? 'text-red-400' : f.score < 0 ? 'text-green-400' : 'text-gray-500'}`}>
                          {f.score > 0 ? '+' : ''}{f.score.toFixed(2)}
                        </span>
                      </div>
                    ))}
                  </div>
                  <div className="flex items-center justify-between mt-3 px-3 py-2 bg-gray-800 rounded text-xs">
                    <span className="text-gray-400">
                      {t(lang, 'rawScore')}: {result.risk_assessment.raw_score?.toFixed(2)} ×
                      {result.risk_assessment.amplification?.toFixed(1)}
                    </span>
                    <span className="text-white font-bold">
                      = {result.risk_assessment.final_score?.toFixed(2)}
                    </span>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </>
  )
}

function Field({ label, children }) {
  return (
    <div>
      <label className="block text-xs text-gray-400 mb-1">{label}</label>
      {children}
    </div>
  )
}
