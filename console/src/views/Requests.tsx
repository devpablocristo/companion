import { useState, useEffect } from 'react'
import { fetchRequests, fetchReplay, fetchEvidence, fetchAttestation } from '../api'
import { t } from '../i18n'
import StatusBadge from '../components/StatusBadge'
import RiskBadge from '../components/RiskBadge'

const STATUS_OPTIONS = ['', 'allowed', 'denied', 'pending_approval', 'approved', 'rejected', 'executed', 'failed']

const eventColors = {
  received: 'border-gray-500', evaluated: 'border-gray-500',
  allowed: 'border-green-500', approved: 'border-green-500', executed: 'border-green-500',
  denied: 'border-red-500', rejected: 'border-red-500', execution_failed: 'border-red-500',
  sent_to_approval: 'border-yellow-500', expired: 'border-gray-600',
}
const dotColors = {
  received: 'bg-gray-500', evaluated: 'bg-gray-500',
  allowed: 'bg-green-500', approved: 'bg-green-500', executed: 'bg-green-500',
  denied: 'bg-red-500', rejected: 'bg-red-500', execution_failed: 'bg-red-500',
  sent_to_approval: 'bg-yellow-500', expired: 'bg-gray-600',
}

export default function Requests({ lang }) {
  const [requests, setRequests] = useState<any[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [statusFilter, setStatusFilter] = useState('')
  const [actionFilter, setActionFilter] = useState('')
  const [expanded, setExpanded] = useState(null)
  const [replay, setReplay] = useState(null)
  const [replayLoading, setReplayLoading] = useState(false)
  const [attestation, setAttestation] = useState(null)

  const load = () => {
    setLoading(true)
    setError(null)
    const params: Record<string, string> = {}
    if (statusFilter) params.status = statusFilter
    if (actionFilter) params.action_type = actionFilter
    fetchRequests(params)
      .then((res) => setRequests(res?.data || []))
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [statusFilter, actionFilter])

  const toggleExpand = (id) => {
    if (expanded === id) {
      setExpanded(null)
      setReplay(null)
      return
    }
    setExpanded(id)
    setReplay(null)
    setAttestation(null)
    setReplayLoading(true)
    fetchReplay(id)
      .then((r) => setReplay(r))
      .catch(() => setReplay(null))
      .finally(() => setReplayLoading(false))
    fetchAttestation(id)
      .then((a) => setAttestation(a))
      .catch(() => setAttestation(null))
  }

  const formatDate = (d) => {
    if (!d) return '—'
    return new Date(d).toLocaleString(lang === 'es' ? 'es-AR' : 'en-US', {
      month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit'
    })
  }

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-xl font-bold text-white">{t(lang, 'requestsTitle')}</h2>
        <button onClick={load} className="px-3 py-1.5 bg-gray-700 text-gray-300 rounded text-sm hover:bg-gray-600">
          {t(lang, 'refresh')}
        </button>
      </div>

      <div className="flex gap-3 mb-4">
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="bg-gray-800 text-gray-300 border border-gray-700 rounded px-3 py-1.5 text-sm"
        >
          <option value="">{t(lang, 'allStatuses')}</option>
          {STATUS_OPTIONS.filter(Boolean).map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </select>
        <input
          type="text"
          value={actionFilter}
          onChange={(e) => setActionFilter(e.target.value)}
          placeholder={t(lang, 'filterByAction')}
          className="bg-gray-800 text-gray-300 border border-gray-700 rounded px-3 py-1.5 text-sm placeholder-gray-500"
        />
      </div>

      {error && <p className="text-red-400 text-sm mb-4">{error}</p>}
      {loading && <p className="text-gray-400 text-sm">{t(lang, 'loading')}</p>}

      {!loading && requests.length === 0 && (
        <p className="text-gray-500 text-sm">{t(lang, 'noRequests')}</p>
      )}

      {!loading && requests.length > 0 && (
        <div className="space-y-2">
          {requests.map((r) => (
            <div key={r.id} className="border border-gray-800 rounded-lg overflow-hidden">
              {/* Fila principal */}
              <div
                className={`flex items-center gap-4 px-4 py-3 cursor-pointer transition-colors ${
                  expanded === r.id ? 'bg-gray-800/60' : 'hover:bg-gray-800/30'
                }`}
                onClick={() => toggleExpand(r.id)}
              >
                <span className="text-gray-500 text-xs w-32 shrink-0">{formatDate(r.created_at)}</span>
                <span className="text-gray-300 text-sm w-28 shrink-0">
                  <span className="text-gray-600 text-xs">{r.requester_type}/</span>{r.requester_id}
                </span>
                <span className="text-white font-mono text-xs w-36 shrink-0">{r.action_type}</span>
                <span className="text-gray-400 text-xs w-24 shrink-0">{r.target_system || '—'}</span>
                <span className="w-16 shrink-0"><RiskBadge level={r.risk_level} /></span>
                <span className="w-20 shrink-0"><StatusBadge status={r.status} /></span>
                <span className="ml-auto text-gray-600 text-xs">
                  {expanded === r.id ? '▲' : '▼'}
                </span>
              </div>

              {/* Detalle expandido con replay */}
              {expanded === r.id && (
                <div className="border-t border-gray-800 bg-gray-900/50 px-4 py-4">
                  {/* Info de la request */}
                  <div className="grid grid-cols-2 gap-3 text-xs mb-4">
                    <div>
                      <span className="text-gray-500">{t(lang, 'decision')}:</span>{' '}
                      <span className="text-gray-300">{r.decision}</span>
                    </div>
                    <div>
                      <span className="text-gray-500">{t(lang, 'decisionReason')}:</span>{' '}
                      <span className="text-gray-300">{r.decision_reason || '—'}</span>
                    </div>
                    {r.reason && (
                      <div>
                        <span className="text-gray-500">{t(lang, 'reason')}:</span>{' '}
                        <span className="text-gray-300">{r.reason}</span>
                      </div>
                    )}
                    {r.target_resource && (
                      <div>
                        <span className="text-gray-500">{t(lang, 'targetResource')}:</span>{' '}
                        <span className="text-gray-300">{r.target_resource}</span>
                      </div>
                    )}
                    {r.ai_summary && (
                      <div className="col-span-2">
                        <span className="text-gray-500">{t(lang, 'aiSummary')}:</span>{' '}
                        <span className="text-gray-300 italic">{r.ai_summary}</span>
                      </div>
                    )}
                    {r.error_message && (
                      <div className="col-span-2">
                        <span className="text-gray-500">{t(lang, 'errorMsg')}:</span>{' '}
                        <span className="text-red-400">{r.error_message}</span>
                      </div>
                    )}
                    <div>
                      <span className="text-gray-500">ID:</span>{' '}
                      <span className="text-gray-500 font-mono">{r.id}</span>
                    </div>
                  </div>

                  {/* Timeline (replay) */}
                  <h4 className="text-xs font-semibold text-gray-400 uppercase mb-3">{t(lang, 'timeline')}</h4>

                  {replayLoading && <p className="text-gray-500 text-xs">{t(lang, 'loading')}</p>}

                  {!replayLoading && replay && replay.timeline && (
                    <div className="relative ml-2">
                      <div className="absolute left-2 top-0 bottom-0 w-px bg-gray-800" />
                      {replay.timeline.map((e, i) => (
                        <div key={i} className="relative pl-8 pb-3">
                          <div className={`absolute left-0 top-1 w-4 h-4 rounded-full border-2 ${dotColors[e.event] || 'bg-gray-600'}`} />
                          <div className={`border-l-2 pl-4 py-1 ${eventColors[e.event] || 'border-gray-600'}`}>
                            <div className="flex items-center gap-2 text-xs text-gray-500">
                              <span className="font-mono">{e.event}</span>
                              <span>{e.actor}</span>
                              <span className="ml-auto">{new Date(e.at).toLocaleTimeString()}</span>
                            </div>
                            <p className="text-sm text-gray-300 mt-0.5">{e.summary}</p>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}

                  {!replayLoading && (!replay || !replay.timeline) && (
                    <p className="text-gray-600 text-xs">{t(lang, 'noTimeline')}</p>
                  )}

                  {/* Attestation */}
                  {attestation && (
                    <div className="mt-4 pt-3 border-t border-gray-800">
                      <h4 className="text-xs font-semibold text-gray-400 uppercase mb-2">{t(lang, 'attestation')}</h4>
                      <div className="bg-gray-800/50 rounded p-3 text-xs space-y-1">
                        <div>
                          <span className="text-gray-500">{t(lang, 'attester')}:</span>{' '}
                          <span className="text-blue-400 font-mono">{attestation.attester}</span>
                        </div>
                        <div>
                          <span className="text-gray-500">{t(lang, 'attestStatus')}:</span>{' '}
                          <span className={attestation.status === 'success' ? 'text-green-400' : 'text-red-400'}>{attestation.status}</span>
                        </div>
                        {attestation.provider_refs && Object.keys(attestation.provider_refs).length > 0 && (
                          <div>
                            <span className="text-gray-500">{t(lang, 'providerRefs')}:</span>{' '}
                            <span className="text-gray-300 font-mono">{JSON.stringify(attestation.provider_refs)}</span>
                          </div>
                        )}
                        <div>
                          <span className="text-gray-500">{t(lang, 'attestSignature')}:</span>{' '}
                          <span className="text-gray-500 font-mono text-[10px]">{attestation.signature.slice(0, 32)}...</span>
                        </div>
                      </div>
                    </div>
                  )}

                  {/* Evidence Pack download */}
                  <div className="mt-4 pt-3 border-t border-gray-800">
                    <button
                      onClick={(e) => {
                        e.stopPropagation()
                        fetchEvidence(r.id)
                          .then((pack) => {
                            const blob = new Blob([JSON.stringify(pack, null, 2)], { type: 'application/json' })
                            const url = URL.createObjectURL(blob)
                            const a = document.createElement('a')
                            a.href = url
                            a.download = `evidence-${r.id.slice(0, 8)}.json`
                            a.click()
                            URL.revokeObjectURL(url)
                          })
                          .catch((err) => setError(err.message))
                      }}
                      className="px-3 py-1.5 bg-indigo-900 text-indigo-300 text-xs rounded hover:bg-indigo-800 font-medium"
                    >
                      {t(lang, 'downloadEvidence')}
                    </button>
                  </div>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
