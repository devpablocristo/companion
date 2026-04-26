import { useState, useEffect } from 'react'
import { fetchReplay, fetchRequest, fetchRequests } from '../api'
import { t } from '../i18n'
import StatusBadge from '../components/StatusBadge'

const eventColors = {
  received: 'border-gray-500', evaluated: 'border-gray-500',
  allowed: 'border-green-500', approved: 'border-green-500', executed: 'border-green-500',
  denied: 'border-red-500', rejected: 'border-red-500', execution_failed: 'border-red-500',
  sent_to_approval: 'border-yellow-500', expired: 'border-gray-600', cancelled: 'border-gray-600',
}
const dotColors = {
  received: 'bg-gray-500', evaluated: 'bg-gray-500',
  allowed: 'bg-green-500', approved: 'bg-green-500', executed: 'bg-green-500',
  denied: 'bg-red-500', rejected: 'bg-red-500', execution_failed: 'bg-red-500',
  sent_to_approval: 'bg-yellow-500', expired: 'bg-gray-600', cancelled: 'bg-gray-600',
}

function linkedTaskId(request: any) {
  const taskId = request?.params?.nexus?.task_id
  return typeof taskId === 'string' && taskId ? taskId : null
}

export default function Replay({
  lang,
  requestId,
  onViewTask = (_taskId: string) => {},
}: {
  lang: any
  requestId?: string | null
  onViewTask?: (taskId: string) => void
}) {
  const [replay, setReplay] = useState<any>(null)
  const [requests, setRequests] = useState<any[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(requestId ?? null)
  const [requestDetail, setRequestDetail] = useState<any>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  useEffect(() => {
    setSelectedId(requestId ?? null)
    setReplay(null)
    setRequestDetail(null)
    if (!requestId) {
      setError(null)
    }
  }, [requestId])

  useEffect(() => {
    if (!selectedId) fetchRequests({ limit: 20 }).then((r) => setRequests(r.data || [])).catch(() => {})
  }, [selectedId])

  useEffect(() => {
    if (!selectedId) return
    setLoading(true)
    fetchReplay(selectedId)
      .then((r) => { setReplay(r); setError(null) })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
    fetchRequest(selectedId)
      .then((r) => setRequestDetail(r))
      .catch(() => setRequestDetail(null))
  }, [selectedId])

  const taskId = linkedTaskId(requestDetail)

  if (!selectedId) {
    return (
      <div>
        <h2 className="text-xl font-bold mb-4">{t(lang, 'selectRequest')}</h2>
        {requests.length === 0 && <p className="text-gray-500">{t(lang, 'noRequests')}</p>}
        <div className="space-y-2">
          {requests.map((r) => (
            <button key={r.id} onClick={() => setSelectedId(String(r.id))}
              className="w-full text-left bg-gray-900 border border-gray-800 rounded p-3 hover:bg-gray-800 transition-colors">
              <div className="flex items-center gap-3">
                <StatusBadge status={r.status} />
                <span className="font-medium">{r.action_type}</span>
                <span className="text-gray-500">{r.target_resource}</span>
                <span className="ml-auto text-gray-600 text-xs">{r.requester_id}</span>
              </div>
            </button>
          ))}
        </div>
      </div>
    )
  }

  if (loading) return <p className="text-gray-500">{t(lang, 'loadingReplay')}</p>
  if (error) return <p className="text-red-400">{error}</p>
  if (!replay) return null

  return (
    <div>
      <div className="flex items-center gap-3 mb-6">
        <button onClick={() => { setSelectedId(null); setReplay(null); setRequestDetail(null) }} className="text-gray-400 hover:text-white text-sm">&larr; {t(lang, 'back')}</button>
        <h2 className="text-xl font-bold">{t(lang, 'replayTitle')}</h2>
        {taskId && (
          <button
            onClick={() => onViewTask(taskId)}
            className="ml-auto px-3 py-1.5 rounded bg-indigo-900 text-indigo-200 text-sm hover:bg-indigo-800"
          >
            {t(lang, 'openTask')}
          </button>
        )}
      </div>
      <div className="bg-gray-900 border border-gray-800 rounded-lg p-4 mb-6">
        <div className="grid grid-cols-2 gap-2 text-sm">
          <div><span className="text-gray-500">{t(lang, 'request')}:</span> {replay.request_id}</div>
          <div><span className="text-gray-500">{t(lang, 'requester')}:</span> {replay.requester?.ID || replay.requester?.id} ({replay.requester?.Type || replay.requester?.type})</div>
          <div><span className="text-gray-500">{t(lang, 'action')}:</span> {replay.action_type}</div>
          <div><span className="text-gray-500">{t(lang, 'target')}:</span> {replay.target}</div>
          <div><span className="text-gray-500">{t(lang, 'status')}:</span> <StatusBadge status={replay.final_status} /></div>
          {taskId && <div><span className="text-gray-500">{t(lang, 'linkedTask')}:</span> <span className="font-mono text-xs">{taskId}</span></div>}
          {replay.duration_total && <div><span className="text-gray-500">{t(lang, 'duration')}:</span> {replay.duration_total}</div>}
        </div>
      </div>
      <h3 className="text-sm font-semibold text-gray-400 uppercase mb-3">{t(lang, 'timeline')}</h3>
      <div className="relative ml-4">
        <div className="absolute left-2 top-0 bottom-0 w-px bg-gray-800" />
        {(replay.timeline || []).map((e, i) => (
          <div key={i} className="relative pl-8 pb-4">
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
    </div>
  )
}
