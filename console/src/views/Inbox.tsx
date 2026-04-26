import { useState, useEffect } from 'react'
import { fetchPendingApprovals, fetchRequest, approveApproval, rejectApproval } from '../api'
import { t } from '../i18n'
import RiskBadge from '../components/RiskBadge'

function timeRemaining(expiresAt, lang) {
  const diff = new Date(expiresAt).getTime() - Date.now()
  if (diff <= 0) return t(lang, 'expired')
  const min = Math.floor(diff / 60000)
  if (min < 60) return `${min}min ${t(lang, 'timeLeft')}`
  return `${Math.floor(min / 60)}h ${min % 60}min ${t(lang, 'timeLeft')}`
}

const riskBorder = {
  high: 'border-red-500/40',
  medium: 'border-yellow-500/40',
  low: 'border-gray-800',
}

function linkedTaskId(request) {
  const taskId = request?.params?.nexus?.task_id
  return typeof taskId === 'string' && taskId ? taskId : null
}

function ApprovalCard({ approval, request, lang, onDone, onViewReplay, onViewTask }) {
  const [expanded, setExpanded] = useState(false)
  const [note, setNote] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [action, setAction] = useState(null) // 'approve' | 'reject'
  const [submitting, setSubmitting] = useState(false)
  const [cardError, setCardError] = useState(null)

  const risk = request?.risk_level || 'low'
  const expectedWord = action === 'approve' ? 'APPROVE' : 'REJECT'
  const isValid = note.trim().length >= 3 && confirmation === expectedWord

  const startAction = (act) => {
    setAction(act)
    setExpanded(true)
    setConfirmation('')
    setCardError(null)
  }

  const cancelAction = () => {
    setAction(null)
    setExpanded(false)
    setNote('')
    setConfirmation('')
    setCardError(null)
  }

  const taskId = linkedTaskId(request)

  const submit = async () => {
    if (!isValid) return
    setSubmitting(true)
    setCardError(null)
    try {
      if (action === 'approve') {
        await approveApproval(approval.id, note.trim())
      } else {
        await rejectApproval(approval.id, note.trim())
      }
      onDone(approval.id)
    } catch (e) {
      setCardError(e.message)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className={`bg-gray-900 border rounded-lg p-4 ${riskBorder[risk] || riskBorder.low}`}>
      <div className="flex items-center gap-3 mb-2">
        <RiskBadge level={risk} />
        <span className="font-medium">{request?.action_type || '—'}</span>
        <span className="text-gray-500">{request?.target_resource || ''}</span>
        <span className="text-gray-600 text-sm">{request?.target_system || ''}</span>
        {approval.break_glass && (
          <span className="px-2 py-0.5 bg-red-900 text-red-300 border border-red-700 rounded text-xs font-medium">
            {t(lang, 'breakGlass')} ({approval.current_approvals}/{approval.required_approvals})
          </span>
        )}
        <span className="ml-auto text-gray-500 text-xs">{timeRemaining(approval.expires_at, lang)}</span>
      </div>
      {/* Decisiones parciales de break-glass */}
      {approval.break_glass && approval.decisions && approval.decisions.length > 0 && (
        <div className="mb-2 space-y-1">
          {approval.decisions.map((d, i) => (
            <div key={i} className={`text-xs px-2 py-1 rounded ${d.action === 'approve' ? 'bg-green-900/30 text-green-400' : 'bg-red-900/30 text-red-400'}`}>
              {d.approver_id}: {d.action} — {d.note}
            </div>
          ))}
        </div>
      )}
      {request?.ai_summary && (
        <p className="text-gray-300 text-sm mb-2 line-clamp-2">{request.ai_summary}</p>
      )}
      <div className="flex items-center gap-2 text-xs text-gray-500 mb-3">
        <span>{request?.requester_id || '—'} ({request?.requester_type || ''})</span>
        {request?.decision_reason && <span>| {request.decision_reason}</span>}
      </div>

      {/* Botones iniciales */}
      {!expanded && (
        <div className="flex gap-2">
          <button onClick={() => startAction('approve')}
            className="px-3 py-1.5 bg-green-600 hover:bg-green-500 text-white text-sm rounded font-medium transition-colors">
            {t(lang, 'approve')}
          </button>
          <button onClick={() => startAction('reject')}
            className="px-3 py-1.5 bg-red-600 hover:bg-red-500 text-white text-sm rounded font-medium transition-colors">
            {t(lang, 'reject')}
          </button>
          {request && (
            <button onClick={() => onViewReplay(request.id)}
              className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded font-medium transition-colors">
              {t(lang, 'details')}
            </button>
          )}
          {taskId && (
            <button onClick={() => onViewTask(taskId)}
              className="px-3 py-1.5 bg-indigo-900 hover:bg-indigo-800 text-indigo-200 text-sm rounded font-medium transition-colors">
              {t(lang, 'openTask')}
            </button>
          )}
        </div>
      )}

      {/* Panel de confirmación */}
      {expanded && (
        <div className={`mt-3 p-3 rounded border ${action === 'approve' ? 'border-green-500/30 bg-green-500/5' : 'border-red-500/30 bg-red-500/5'}`}>
          <p className="text-sm text-gray-300 mb-2">
            {t(lang, 'noteRequired')}
          </p>
          <textarea
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder={t(lang, 'notePlaceholder')}
            rows={2}
            className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white mb-2 resize-none"
          />
          <p className="text-xs text-gray-400 mb-1">
            {t(lang, 'typeToConfirm')} <span className={`font-mono font-bold ${action === 'approve' ? 'text-green-400' : 'text-red-400'}`}>{expectedWord}</span>
          </p>
          <input
            value={confirmation}
            onChange={(e) => setConfirmation(e.target.value)}
            className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-2 text-sm text-white font-mono mb-3"
            placeholder={expectedWord}
          />
          {cardError && <p className="text-red-400 text-xs mb-2">{cardError}</p>}
          <div className="flex gap-2">
            <button
              onClick={submit}
              disabled={!isValid || submitting}
              className={`px-4 py-1.5 text-white text-sm rounded font-medium transition-colors ${
                isValid && !submitting
                  ? action === 'approve' ? 'bg-green-600 hover:bg-green-500' : 'bg-red-600 hover:bg-red-500'
                  : 'bg-gray-700 text-gray-500 cursor-not-allowed'
              }`}
            >
              {submitting ? '...' : action === 'approve' ? t(lang, 'confirmApprove') : t(lang, 'confirmReject')}
            </button>
            <button
              onClick={cancelAction}
              className="px-4 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded font-medium transition-colors"
            >
              {t(lang, 'cancel')}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

export default function Inbox({
  lang,
  onViewReplay = (_requestId: string) => {},
  onViewTask = (_taskId: string) => {},
}: {
  lang: any
  onViewReplay?: (requestId: string) => void
  onViewTask?: (taskId: string) => void
}) {
  const [items, setItems] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  const load = async () => {
    try {
      setLoading(true)
      const res = await fetchPendingApprovals()
      const approvals = (res.data || []).slice(0, 20) // máx 20 para no saturar
      const enriched = []
      for (const a of approvals) {
        try {
          const req = await fetchRequest(a.request_id)
          enriched.push({ approval: a, request: req })
        } catch {
          enriched.push({ approval: a, request: null })
        }
      }
      setItems(enriched)
      setError(null)
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])
  useEffect(() => {
    const id = setInterval(load, 30000)
    return () => clearInterval(id)
  }, [])

  const handleDone = (approvalId) => {
    setItems((prev) => prev.filter((i) => i.approval.id !== approvalId))
  }

  if (loading && items.length === 0) return <p className="text-gray-500">{t(lang, 'loading')}</p>
  if (error) return <p className="text-red-400">{error}</p>

  return (
    <div>
      <h2 className="text-xl font-bold mb-4">
        {t(lang, 'approvalInbox')} <span className="text-gray-500 text-sm font-normal ml-2">{items.length} {t(lang, 'pending')}</span>
      </h2>
      {items.length === 0 && <p className="text-gray-500">{t(lang, 'noPendingApprovals')}</p>}
      <div className="space-y-3">
        {items.map(({ approval, request }) => (
          <ApprovalCard
            key={approval.id}
            approval={approval}
            request={request}
            lang={lang}
            onDone={handleDone}
            onViewReplay={onViewReplay}
            onViewTask={onViewTask}
          />
        ))}
      </div>
    </div>
  )
}
