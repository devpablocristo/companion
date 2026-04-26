import { useState, useEffect, useCallback } from 'react'
import {
  fetchCompanionMemory,
  executeCompanionTask,
  fetchCompanionConnectorCapabilities,
  fetchCompanionConnectors,
  fetchCompanionTasks,
  fetchCompanionTask,
  createCompanionTask,
  proposeCompanionTask,
  retryCompanionTask,
  investigateCompanionTask,
  saveCompanionTaskExecutionPlan,
  saveCompanionMemory,
  syncCompanionTaskFromReview,
} from '../api'
import { t } from '../i18n'

type TaskRow = {
  id: string
  title: string
  status: string
  review_status?: string
  review_last_checked_at?: string
  review_sync_error?: string
  updated_at: string
}

type TaskSnapshot = {
  id: string
  title: string
  goal: string
  status: string
  created_by?: string
  review_status?: string
  review_last_checked_at?: string
  review_sync_error?: string
}

type TaskMessage = {
  id: string
  author_type?: string
  author_id?: string
  body?: string
  created_at: string
}

type TaskAction = {
  id: string
  action_type?: string
  review_request_id?: string
  error_message?: string
  created_at: string
}

type LinkedReviewRequest = {
  action_id: string
  request?: {
    id?: string
    status?: string
    decision?: string
    risk_level?: string
  }
}

type ReviewSyncState = {
  review_request_id: string
  last_review_status?: string
  last_review_http_status: number
  last_checked_at: string
  last_error?: string
  consecutive_failures: number
  next_check_at: string
}

type Artifact = {
  id: string
  kind: string
  uri: string
  payload?: Record<string, unknown>
  created_at: string
}

type ExecutionPlan = {
  connector_id: string
  operation: string
  payload?: Record<string, unknown>
  idempotency_key?: string
  created_at: string
  updated_at: string
}

type VerificationResult = {
  status: string
  summary?: string
  checked_at: string
  details?: Record<string, unknown>
}

type ExecutionState = {
  last_execution_id: string
  last_execution_status: string
  retryable: boolean
  retry_count: number
  last_error?: string
  last_attempted_at: string
  verification_result: VerificationResult
}

type MemoryEntry = {
  id: string
  kind: string
  scope_type: string
  scope_id: string
  key: string
  payload_json: Record<string, unknown>
  content_text: string
  version: number
  created_at: string
  updated_at: string
  expires_at?: string | null
}

type ConnectorOption = {
  id: string
  name: string
  kind: string
  enabled: boolean
}

type ConnectorCapability = {
  operation: string
  side_effect: boolean
  read_only: boolean
}

type ConnectorCapabilities = {
  connector_id: string
  kind: string
  capabilities: ConnectorCapability[]
}

type TaskDetail = {
  task: TaskSnapshot
  messages: TaskMessage[]
  actions: TaskAction[]
  artifacts: Artifact[]
  linked_review_requests: LinkedReviewRequest[]
  review_sync?: ReviewSyncState
  execution_plan?: ExecutionPlan
  execution_state?: ExecutionState
}

const badgeTone = {
  neutral: 'border-gray-700 bg-gray-800/70 text-gray-200',
  info: 'border-sky-700 bg-sky-950/60 text-sky-200',
  warning: 'border-amber-700 bg-amber-950/60 text-amber-200',
  success: 'border-emerald-700 bg-emerald-950/60 text-emerald-200',
  danger: 'border-rose-700 bg-rose-950/60 text-rose-200',
  muted: 'border-gray-800 bg-gray-900/80 text-gray-400',
}

function formatDateTime(value?: string | null) {
  if (!value) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString()
}

function formatStatus(status?: string | null) {
  if (!status) return '—'
  return status.split('_').join(' ')
}

function toneForStatus(status?: string | null, kind: 'task' | 'review' = 'task') {
  switch (status) {
    case 'done':
    case 'allowed':
    case 'approved':
    case 'executed':
    case 'verified':
    case 'success':
      return badgeTone.success
    case 'failed':
    case 'denied':
    case 'rejected':
    case 'expired':
    case 'cancelled':
    case 'failure':
      return badgeTone.danger
    case 'waiting_for_approval':
    case 'waiting_for_input':
    case 'pending':
    case 'pending_approval':
    case 'evaluated':
      return badgeTone.warning
    case 'investigating':
    case 'executing':
    case 'verifying':
      return badgeTone.info
    case 'new':
      return kind === 'task' ? badgeTone.neutral : badgeTone.muted
    default:
      return badgeTone.muted
  }
}

function StatusPill({
  status,
  kind = 'task',
}: {
  status?: string | null
  kind?: 'task' | 'review'
}) {
  if (!status) return null
  return (
    <span
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${toneForStatus(
        status,
        kind,
      )}`}
    >
      {formatStatus(status)}
    </span>
  )
}

export default function Tasks({
  lang,
  focusTaskId,
  onViewReplay = (_requestId: string) => {},
}: {
  lang: string
  focusTaskId?: string | null
  onViewReplay?: (requestId: string) => void
}) {
  const [list, setList] = useState<TaskRow[]>([])
  const [detail, setDetail] = useState<TaskDetail | null>(null)
  const [memoryEntries, setMemoryEntries] = useState<MemoryEntry[]>([])
  const [connectors, setConnectors] = useState<ConnectorOption[]>([])
  const [capabilities, setCapabilities] = useState<ConnectorCapabilities[]>([])
  const [loading, setLoading] = useState(true)
  const [msg, setMsg] = useState<{ type: string; text: string } | null>(null)
  const [form, setForm] = useState({ title: '', goal: '', created_by: '' })
  const [proposeNote, setProposeNote] = useState('')
  const [planForm, setPlanForm] = useState({
    connector_id: '',
    operation: '',
    payload: '{}',
    idempotency_key: '',
  })
  const [attentionFilter, setAttentionFilter] = useState('attention')
  const [summaryDraft, setSummaryDraft] = useState('')
  const [summaryDirty, setSummaryDirty] = useState(false)
  const [busy, setBusy] = useState(false)

  const loadList = useCallback(() => {
    return fetchCompanionTasks()
      .then((r: { data?: TaskRow[] }) => setList(r.data || []))
      .catch((e: Error) => setMsg({ type: 'err', text: e.message }))
  }, [])

  const loadDetail = useCallback((id: string) => {
    return fetchCompanionTask(id)
      .then((d: TaskDetail) => setDetail(d))
      .catch((e: Error) => setMsg({ type: 'err', text: e.message }))
  }, [])

  const loadTaskMemory = useCallback((id: string) => {
    return fetchCompanionMemory('task', id)
      .then((r: { entries?: MemoryEntry[] }) => setMemoryEntries(r.entries || []))
      .catch((e: Error) => setMsg({ type: 'err', text: e.message }))
  }, [])

  const loadExecutionOptions = useCallback(() => {
    return Promise.all([
      fetchCompanionConnectors().then((r: { connectors?: ConnectorOption[] }) =>
        setConnectors((r.connectors || []).filter((connector) => connector.enabled)),
      ),
      fetchCompanionConnectorCapabilities().then((r: { connectors?: ConnectorCapabilities[] }) =>
        setCapabilities(r.connectors || []),
      ),
    ]).catch((e: Error) => setMsg({ type: 'err', text: e.message }))
  }, [])

  useEffect(() => {
    setLoading(true)
    Promise.all([loadList(), loadExecutionOptions()]).finally(() => setLoading(false))
  }, [loadExecutionOptions, loadList])

  useEffect(() => {
    if (!detail?.task?.id) return
    const id = detail.task.id
    const h = setInterval(() => {
      loadDetail(id)
      loadTaskMemory(id)
      loadList()
    }, 4000)
    return () => clearInterval(h)
  }, [detail?.task?.id, loadDetail, loadList, loadTaskMemory])

  const openTask = useCallback((id: string) => {
    setMsg(null)
    return Promise.all([loadDetail(id), loadTaskMemory(id)])
  }, [loadDetail, loadTaskMemory])

  useEffect(() => {
    if (!focusTaskId) return
    openTask(focusTaskId)
  }, [focusTaskId, openTask])

  useEffect(() => {
    setSummaryDirty(false)
  }, [detail?.task?.id])

  useEffect(() => {
    if (!detail?.task?.id) {
      setMemoryEntries([])
      setSummaryDraft('')
      setSummaryDirty(false)
      return
    }
    const summaryEntry = memoryEntries.find((entry) => entry.kind === 'task_summary')
    if (!summaryDirty) {
      setSummaryDraft(summaryEntry?.content_text || '')
    }
  }, [detail?.task?.id, memoryEntries, summaryDirty])

  useEffect(() => {
    if (!detail) return
    if (detail.execution_plan) {
      setPlanForm({
        connector_id: detail.execution_plan.connector_id,
        operation: detail.execution_plan.operation,
        payload: JSON.stringify(detail.execution_plan.payload || {}, null, 2),
        idempotency_key: detail.execution_plan.idempotency_key || '',
      })
      return
    }
    setPlanForm((current) => ({
      connector_id:
        current.connector_id && connectors.some((connector) => connector.id === current.connector_id)
          ? current.connector_id
          : connectors[0]?.id || '',
      operation: '',
      payload: '{}',
      idempotency_key: '',
    }))
  }, [
    connectors,
    detail?.task.id,
    detail?.execution_plan?.connector_id,
    detail?.execution_plan?.operation,
    detail?.execution_plan?.idempotency_key,
    detail?.execution_plan?.updated_at,
  ])

  const create = () => {
    if (!form.title.trim()) return
    setBusy(true)
    setMsg(null)
    createCompanionTask({
      title: form.title.trim(),
      goal: form.goal.trim(),
      created_by: form.created_by.trim(),
    })
      .then((created: TaskSnapshot) => {
        setForm({ title: '', goal: '', created_by: '' })
        setAttentionFilter('all')
        return Promise.all([loadList(), openTask(created.id)]).then(() => undefined)
      })
      .catch((e: Error) => setMsg({ type: 'err', text: e.message }))
      .finally(() => setBusy(false))
  }

  const refreshTaskView = useCallback((id: string) => {
    return Promise.all([loadList(), loadDetail(id), loadTaskMemory(id)]).then(() => undefined)
  }, [loadDetail, loadList, loadTaskMemory])

  const propose = () => {
    if (!detail?.task?.id) return
    const taskID = detail.task.id
    setBusy(true)
    setMsg(null)
    proposeCompanionTask(taskID, { note: proposeNote })
      .then(() => {
        setProposeNote('')
      })
      .catch((e: Error) => setMsg({ type: 'err', text: e.message }))
      .finally(() => refreshTaskView(taskID).finally(() => setBusy(false)))
  }

  const investigate = () => {
    if (!detail?.task?.id) return
    const taskID = detail.task.id
    setBusy(true)
    setMsg(null)
    investigateCompanionTask(taskID, '')
      .catch((e: Error) => setMsg({ type: 'err', text: e.message }))
      .finally(() => refreshTaskView(taskID).finally(() => setBusy(false)))
  }

  const syncFromReview = () => {
    if (!detail?.task?.id) return
    const taskID = detail.task.id
    setBusy(true)
    setMsg(null)
    syncCompanionTaskFromReview(taskID)
      .catch((e: Error) => setMsg({ type: 'err', text: e.message }))
      .finally(() => refreshTaskView(taskID).finally(() => setBusy(false)))
  }

  const saveExecutionPlan = () => {
    if (!detail?.task?.id) return
    let parsedPayload: Record<string, unknown> = {}
    try {
      parsedPayload = JSON.parse(planForm.payload || '{}')
    } catch {
      setMsg({ type: 'err', text: `${t(lang, 'payload')}: invalid JSON` })
      return
    }
    if (!planForm.connector_id || !planForm.operation) {
      setMsg({ type: 'err', text: `${t(lang, 'connector')} / ${t(lang, 'operation')}: required` })
      return
    }
    const taskID = detail.task.id
    setBusy(true)
    setMsg(null)
    saveCompanionTaskExecutionPlan(taskID, {
      connector_id: planForm.connector_id,
      operation: planForm.operation,
      payload: parsedPayload,
      idempotency_key: planForm.idempotency_key || undefined,
    })
      .catch((e: Error) => setMsg({ type: 'err', text: e.message }))
      .finally(() => refreshTaskView(taskID).finally(() => setBusy(false)))
  }

  const executeTask = () => {
    if (!detail?.task?.id) return
    const taskID = detail.task.id
    setBusy(true)
    setMsg(null)
    executeCompanionTask(taskID)
      .catch((e: Error) => setMsg({ type: 'err', text: e.message }))
      .finally(() => refreshTaskView(taskID).finally(() => setBusy(false)))
  }

  const retryTask = () => {
    if (!detail?.task?.id) return
    const taskID = detail.task.id
    setBusy(true)
    setMsg(null)
    retryCompanionTask(taskID)
      .catch((e: Error) => setMsg({ type: 'err', text: e.message }))
      .finally(() => refreshTaskView(taskID).finally(() => setBusy(false)))
  }

  const saveTaskSummary = () => {
    if (!detail?.task?.id) return
    const taskID = detail.task.id
    const currentSummary = memoryEntries.find((entry) => entry.kind === 'task_summary')
    setBusy(true)
    setMsg(null)
    saveCompanionMemory({
      kind: 'task_summary',
      scope_type: 'task',
      scope_id: taskID,
      key: currentSummary?.key || 'current',
      content_text: summaryDraft.trim(),
      payload_json: currentSummary?.payload_json || {},
      version: currentSummary?.version || 0,
    })
      .then(() => {
        setSummaryDirty(false)
        setMsg({ type: 'ok', text: t(lang, 'taskSummarySaved') })
      })
      .catch((e: Error) => setMsg({ type: 'err', text: e.message }))
      .finally(() => refreshTaskView(taskID).finally(() => setBusy(false)))
  }

  const timeline = (): Array<{ kind: string; at: string; label: string; body: string }> => {
    if (!detail) return []
    const rows: Array<{ kind: string; at: string; label: string; body: string }> = []
    for (const m of detail.messages || []) {
      rows.push({
        kind: 'message',
        at: m.created_at,
        label: `${m.author_type || 'user'}:${m.author_id || ''}`,
        body: m.body || '',
      })
    }
    for (const a of detail.actions || []) {
      const rid = a.review_request_id ? ` → ${a.review_request_id}` : ''
      const err = a.error_message ? ` (${a.error_message})` : ''
      rows.push({
        kind: 'action',
        at: a.created_at,
        label: a.action_type || 'action',
        body: `${rid}${err}`,
      })
    }
    rows.sort((x, y) => new Date(x.at).getTime() - new Date(y.at).getTime())
    return rows
  }

  const selectedConnector = connectors.find((connector) => connector.id === planForm.connector_id)
  const availableCapabilities =
    capabilities.find((item) => item.kind === selectedConnector?.kind)?.capabilities || []
  const approvedForExecution =
    detail?.task.review_status === 'approved' ||
    detail?.task.review_status === 'allowed' ||
    detail?.task.review_status === 'executed'
  const canInvestigate =
    detail?.task.status === 'new' || detail?.task.status === 'investigating'
  const canPropose =
    detail?.task.status === 'new' || detail?.task.status === 'investigating'
  const canEditExecutionPlan =
    detail?.task.status !== 'done' &&
    detail?.task.status !== 'executing' &&
    detail?.task.status !== 'verifying'
  const canExecute =
    Boolean(detail?.execution_plan) &&
    Boolean(approvedForExecution) &&
    (detail?.task.status === 'waiting_for_input' || detail?.task.status === 'waiting_for_approval')
  const canRetry =
    Boolean(detail?.execution_plan) &&
    Boolean(approvedForExecution) &&
    detail?.task.status === 'failed' &&
    Boolean(detail?.execution_state?.retryable)
  const primaryReviewRequestId =
    detail?.review_sync?.review_request_id ||
    detail?.linked_review_requests?.find((item) => item.request?.id)?.request?.id
  const summaryEntry = memoryEntries.find((entry) => entry.kind === 'task_summary')
  const factsEntry = memoryEntries.find((entry) => entry.kind === 'task_facts')
  const filteredList = list.filter((row) => {
    switch (attentionFilter) {
      case 'attention':
        return ['waiting_for_approval', 'waiting_for_input', 'failed'].includes(row.status)
      case 'waiting_for_approval':
      case 'waiting_for_input':
      case 'failed':
        return row.status === attentionFilter
      default:
        return true
    }
  })
  const countForFilter = (filter: string) =>
    list.filter((row) => {
      if (filter === 'attention') {
        return ['waiting_for_approval', 'waiting_for_input', 'failed'].includes(row.status)
      }
      if (filter === 'all') {
        return true
      }
      return row.status === filter
    }).length

  if (loading) return <p className="text-gray-400">{t(lang, 'loading')}</p>

  return (
    <div className="flex flex-col lg:flex-row gap-8">
      <div className="lg:w-1/3 space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="text-xl font-bold text-white">{t(lang, 'tasksTitle')}</h2>
          <button
            type="button"
            onClick={() => loadList()}
            className="text-sm text-gray-400 hover:text-white"
          >
            {t(lang, 'refresh')}
          </button>
        </div>
        <p className="text-sm text-gray-500">{t(lang, 'tasksSubtitle')}</p>

        <div className="rounded-lg border border-gray-800 bg-gray-900/50 p-4 space-y-2">
          <p className="text-sm font-medium text-gray-300">{t(lang, 'createTask')}</p>
          <input
            className="w-full rounded bg-gray-800 border border-gray-700 px-2 py-1 text-sm text-white"
            placeholder={t(lang, 'taskTitle')}
            value={form.title}
            onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))}
          />
          <input
            className="w-full rounded bg-gray-800 border border-gray-700 px-2 py-1 text-sm text-white"
            placeholder={t(lang, 'taskGoal')}
            value={form.goal}
            onChange={(e) => setForm((f) => ({ ...f, goal: e.target.value }))}
          />
          <input
            className="w-full rounded bg-gray-800 border border-gray-700 px-2 py-1 text-sm text-white"
            placeholder={t(lang, 'taskCreatedBy')}
            value={form.created_by}
            onChange={(e) => setForm((f) => ({ ...f, created_by: e.target.value }))}
          />
          <button
            type="button"
            disabled={busy}
            onClick={create}
            className="w-full py-1.5 rounded bg-indigo-600 text-white text-sm font-medium hover:bg-indigo-500 disabled:opacity-50"
          >
            {t(lang, 'createTask')}
          </button>
        </div>

        {msg && (
          <p className={`text-sm ${msg.type === 'ok' ? 'text-emerald-300' : 'text-red-400'}`}>
            {msg.text}
          </p>
        )}

        <div className="rounded-lg border border-gray-800 bg-gray-900/40 p-3">
          <p className="text-xs font-medium uppercase tracking-wide text-gray-500 mb-2">
            {t(lang, 'taskAttention')}
          </p>
          <div className="flex flex-wrap gap-2">
            {['attention', 'waiting_for_approval', 'waiting_for_input', 'failed', 'all'].map((filter) => (
              <button
                key={filter}
                type="button"
                onClick={() => setAttentionFilter(filter)}
                className={`rounded-full border px-3 py-1 text-xs font-medium transition-colors ${
                  attentionFilter === filter
                    ? 'border-indigo-500 bg-indigo-950/50 text-indigo-200'
                    : 'border-gray-700 bg-gray-900/60 text-gray-400 hover:text-white'
                }`}
              >
                {t(lang, `taskFilter_${filter}`)} · {countForFilter(filter)}
              </button>
            ))}
          </div>
        </div>

        <ul className="space-y-1">
          {filteredList.length === 0 && (
            <li className="text-gray-500 text-sm">
              {list.length === 0 ? t(lang, 'noTasks') : t(lang, 'noTasksForFilter')}
            </li>
          )}
          {filteredList.map((row) => (
            <li key={row.id}>
              <button
                type="button"
                onClick={() => openTask(row.id)}
                className={`w-full text-left px-3 py-2 rounded text-sm border ${
                  detail?.task?.id === row.id
                    ? 'border-indigo-500 bg-indigo-950/40 text-white'
                    : 'border-gray-800 bg-gray-900/30 text-gray-300 hover:border-gray-600'
                }`}
              >
                <div className="font-medium">{row.title}</div>
                <div className="mt-1 flex flex-wrap items-center gap-2">
                  <StatusPill status={row.status} />
                  <StatusPill status={row.review_status} kind="review" />
                </div>
                <div className="mt-2 text-xs text-gray-500">
                  {t(lang, 'lastChecked')}: {formatDateTime(row.review_last_checked_at)} ·{' '}
                  {formatDateTime(row.updated_at)}
                </div>
                {row.review_sync_error && (
                  <div className="mt-1 text-xs text-rose-300">
                    {t(lang, 'syncError')}: {row.review_sync_error}
                  </div>
                )}
              </button>
            </li>
          ))}
        </ul>
      </div>

      <div className="flex-1 min-h-[320px]">
        {!detail && (
          <p className="text-gray-500 text-sm">
            {t(lang, 'details')} — {t(lang, 'selectTask')}
          </p>
        )}
        {detail && (
          <div className="space-y-4">
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setDetail(null)}
                className="text-sm text-gray-400 hover:text-white"
              >
                {t(lang, 'backToList')}
              </button>
            </div>
            <div className="rounded-lg border border-gray-800 p-4 bg-gray-900/40">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div>
                  <h3 className="text-lg font-semibold text-white mb-2">{detail.task.title}</h3>
                  <div className="flex flex-wrap items-center gap-2">
                    <StatusPill status={detail.task.status} />
                    <StatusPill status={detail.task.review_status} kind="review" />
                  </div>
                </div>
                {primaryReviewRequestId && (
                  <button
                    type="button"
                    onClick={() => onViewReplay(primaryReviewRequestId)}
                    className="px-3 py-1.5 rounded bg-indigo-900 text-sm text-indigo-200 hover:bg-indigo-800"
                  >
                    {t(lang, 'openReplay')}
                  </button>
                )}
                {detail.task.status === 'waiting_for_approval' && (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={syncFromReview}
                    className="px-3 py-1.5 rounded bg-teal-800 text-sm text-white hover:bg-teal-700 disabled:opacity-50"
                  >
                    {t(lang, 'syncFromReview')}
                  </button>
                )}
                {canExecute && (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={executeTask}
                    className="px-3 py-1.5 rounded bg-emerald-800 text-sm text-white hover:bg-emerald-700 disabled:opacity-50"
                  >
                    {t(lang, 'executeTask')}
                  </button>
                )}
                {canRetry && (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={retryTask}
                    className="px-3 py-1.5 rounded bg-rose-800 text-sm text-white hover:bg-rose-700 disabled:opacity-50"
                  >
                    {t(lang, 'retryTask')}
                  </button>
                )}
              </div>
              <dl className="grid grid-cols-1 sm:grid-cols-2 gap-2 text-sm">
                <div>
                  <dt className="text-gray-500">{t(lang, 'taskStatus')}</dt>
                  <dd className="text-gray-200">{formatStatus(detail.task.status)}</dd>
                </div>
                <div>
                  <dt className="text-gray-500">{t(lang, 'reviewStatus')}</dt>
                  <dd className="text-gray-200">{formatStatus(detail.task.review_status)}</dd>
                </div>
                <div>
                  <dt className="text-gray-500">{t(lang, 'taskCreatedBy')}</dt>
                  <dd className="text-gray-200">{detail.task.created_by || '—'}</dd>
                </div>
                <div>
                  <dt className="text-gray-500">{t(lang, 'lastChecked')}</dt>
                  <dd className="text-gray-200">{formatDateTime(detail.task.review_last_checked_at)}</dd>
                </div>
                <div className="sm:col-span-2">
                  <dt className="text-gray-500">{t(lang, 'reviewRequestId')}</dt>
                  <dd className="text-gray-200 font-mono text-xs break-all">
                    {detail.review_sync?.review_request_id || '—'}
                  </dd>
                </div>
                <div className="sm:col-span-2">
                  <dt className="text-gray-500">{t(lang, 'taskGoal')}</dt>
                  <dd className="text-gray-200">{detail.task.goal || '—'}</dd>
                </div>
                {detail.task.review_sync_error && (
                  <div className="sm:col-span-2">
                    <dt className="text-gray-500">{t(lang, 'syncError')}</dt>
                    <dd className="text-rose-300">{detail.task.review_sync_error}</dd>
                  </div>
                )}
                {detail.execution_state && (
                  <>
                    <div>
                      <dt className="text-gray-500">{t(lang, 'verificationStatus')}</dt>
                      <dd className="text-gray-200">
                        <StatusPill status={detail.execution_state.verification_result.status} />
                      </dd>
                    </div>
                    <div>
                      <dt className="text-gray-500">{t(lang, 'retryCount')}</dt>
                      <dd className="text-gray-200">{detail.execution_state.retry_count}</dd>
                    </div>
                  </>
                )}
              </dl>
              <div className="flex flex-wrap gap-2 mt-4">
                <button
                  type="button"
                  disabled={busy || !canInvestigate}
                  onClick={investigate}
                  className="px-3 py-1.5 rounded bg-gray-700 text-sm text-white hover:bg-gray-600 disabled:opacity-50"
                >
                  {t(lang, 'investigateTask')}
                </button>
                <input
                  className="flex-1 min-w-[120px] rounded bg-gray-800 border border-gray-700 px-2 py-1 text-sm text-white"
                  placeholder={t(lang, 'proposeNote')}
                  value={proposeNote}
                  onChange={(e) => setProposeNote(e.target.value)}
                />
                <button
                  type="button"
                  disabled={busy || !canPropose}
                  onClick={propose}
                  className="px-3 py-1.5 rounded bg-amber-700 text-sm text-white hover:bg-amber-600 disabled:opacity-50"
                >
                  {busy ? t(lang, 'proposeRunning') : t(lang, 'proposeToReview')}
                </button>
              </div>
            </div>

            <div className="rounded-lg border border-gray-800 p-4">
              <div className="flex items-center justify-between gap-3 mb-3">
                <h4 className="text-sm font-medium text-gray-300">{t(lang, 'executionPlan')}</h4>
                {canExecute && (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={executeTask}
                    className="px-3 py-1.5 rounded bg-emerald-800 text-sm text-white hover:bg-emerald-700 disabled:opacity-50"
                  >
                    {t(lang, 'executeTask')}
                  </button>
                )}
                {canRetry && (
                  <button
                    type="button"
                    disabled={busy}
                    onClick={retryTask}
                    className="px-3 py-1.5 rounded bg-rose-800 text-sm text-white hover:bg-rose-700 disabled:opacity-50"
                  >
                    {t(lang, 'retryTask')}
                  </button>
                )}
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs text-gray-500 mb-1">{t(lang, 'connector')}</label>
                  <select
                    value={planForm.connector_id}
                    disabled={busy || !canEditExecutionPlan}
                    onChange={(e) => setPlanForm((current) => ({ ...current, connector_id: e.target.value, operation: '' }))}
                    className="w-full rounded bg-gray-800 border border-gray-700 px-2 py-1 text-sm text-white disabled:opacity-50"
                  >
                    <option value="">{t(lang, 'connector')}</option>
                    {connectors.map((connector) => (
                      <option key={connector.id} value={connector.id}>
                        {connector.name} · {connector.kind}
                      </option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="block text-xs text-gray-500 mb-1">{t(lang, 'operation')}</label>
                  <select
                    value={planForm.operation}
                    disabled={busy || !canEditExecutionPlan || !selectedConnector}
                    onChange={(e) => setPlanForm((current) => ({ ...current, operation: e.target.value }))}
                    className="w-full rounded bg-gray-800 border border-gray-700 px-2 py-1 text-sm text-white disabled:opacity-50"
                  >
                    <option value="">{t(lang, 'operation')}</option>
                    {availableCapabilities.map((capability) => (
                      <option key={capability.operation} value={capability.operation}>
                        {capability.operation}
                      </option>
                    ))}
                    {planForm.operation &&
                      !availableCapabilities.some((capability) => capability.operation === planForm.operation) && (
                        <option value={planForm.operation}>{planForm.operation}</option>
                      )}
                  </select>
                </div>
                <div className="sm:col-span-2">
                  <label className="block text-xs text-gray-500 mb-1">{t(lang, 'idempotencyKey')}</label>
                  <input
                    value={planForm.idempotency_key}
                    disabled={busy || !canEditExecutionPlan}
                    onChange={(e) => setPlanForm((current) => ({ ...current, idempotency_key: e.target.value }))}
                    className="w-full rounded bg-gray-800 border border-gray-700 px-2 py-1 text-sm text-white disabled:opacity-50"
                  />
                </div>
                <div className="sm:col-span-2">
                  <label className="block text-xs text-gray-500 mb-1">{t(lang, 'payload')}</label>
                  <textarea
                    rows={8}
                    value={planForm.payload}
                    disabled={busy || !canEditExecutionPlan}
                    onChange={(e) => setPlanForm((current) => ({ ...current, payload: e.target.value }))}
                    className="w-full rounded bg-gray-800 border border-gray-700 px-2 py-2 text-sm text-white font-mono disabled:opacity-50"
                  />
                </div>
              </div>
              <div className="mt-3 flex flex-wrap items-center gap-3">
                <button
                  type="button"
                  disabled={busy || !canEditExecutionPlan}
                  onClick={saveExecutionPlan}
                  className="px-3 py-1.5 rounded bg-blue-700 text-sm text-white hover:bg-blue-600 disabled:opacity-50"
                >
                  {t(lang, 'saveExecutionPlan')}
                </button>
                {!approvedForExecution && (
                  <p className="text-xs text-gray-500">{t(lang, 'executionAwaitingApproval')}</p>
                )}
              </div>
              {detail.execution_plan && (
                <div className="mt-4 rounded border border-gray-800 bg-gray-950/50 p-3 text-sm">
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                    <div>
                      <dt className="text-gray-500">{t(lang, 'connector')}</dt>
                      <dd className="text-gray-200 font-mono text-xs break-all">
                        {detail.execution_plan.connector_id}
                      </dd>
                    </div>
                    <div>
                      <dt className="text-gray-500">{t(lang, 'operation')}</dt>
                      <dd className="text-gray-200">{detail.execution_plan.operation}</dd>
                    </div>
                    <div>
                      <dt className="text-gray-500">{t(lang, 'idempotencyKey')}</dt>
                      <dd className="text-gray-200">{detail.execution_plan.idempotency_key || '—'}</dd>
                    </div>
                    <div>
                      <dt className="text-gray-500">{t(lang, 'lastChecked')}</dt>
                      <dd className="text-gray-200">{formatDateTime(detail.execution_plan.updated_at)}</dd>
                    </div>
                  </div>
                </div>
              )}
              {detail.execution_state && (
                <div className="mt-4 rounded border border-gray-800 bg-gray-950/50 p-3 text-sm">
                  <div className="flex flex-wrap items-center gap-2 mb-3">
                    <span className="text-gray-500">{t(lang, 'verificationStatus')}</span>
                    <StatusPill status={detail.execution_state.verification_result.status} />
                    {detail.execution_state.retryable && (
                      <span className="text-xs text-amber-300">{t(lang, 'retryAvailable')}</span>
                    )}
                  </div>
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                    <div>
                      <dt className="text-gray-500">{t(lang, 'executionStatus')}</dt>
                      <dd className="text-gray-200">
                        <StatusPill status={detail.execution_state.last_execution_status} />
                      </dd>
                    </div>
                    <div>
                      <dt className="text-gray-500">{t(lang, 'retryCount')}</dt>
                      <dd className="text-gray-200">{detail.execution_state.retry_count}</dd>
                    </div>
                    <div>
                      <dt className="text-gray-500">{t(lang, 'lastAttemptedAt')}</dt>
                      <dd className="text-gray-200">
                        {formatDateTime(detail.execution_state.last_attempted_at)}
                      </dd>
                    </div>
                    <div>
                      <dt className="text-gray-500">{t(lang, 'verificationCheckedAt')}</dt>
                      <dd className="text-gray-200">
                        {formatDateTime(detail.execution_state.verification_result.checked_at)}
                      </dd>
                    </div>
                    <div className="sm:col-span-2">
                      <dt className="text-gray-500">{t(lang, 'verificationSummary')}</dt>
                      <dd className="text-gray-200">
                        {detail.execution_state.verification_result.summary || '—'}
                      </dd>
                    </div>
                    {detail.execution_state.last_error && (
                      <div className="sm:col-span-2">
                        <dt className="text-gray-500">{t(lang, 'errorMsg')}</dt>
                        <dd className="text-rose-300">{detail.execution_state.last_error}</dd>
                      </div>
                    )}
                  </div>
                </div>
              )}
            </div>

            {detail.review_sync && (
              <div className="rounded-lg border border-gray-800 p-4">
                <h4 className="text-sm font-medium text-gray-300 mb-3">{t(lang, 'reviewSync')}</h4>
                <dl className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-sm">
                  <div>
                    <dt className="text-gray-500">{t(lang, 'reviewRequestId')}</dt>
                    <dd className="text-gray-200 font-mono text-xs break-all">
                      {detail.review_sync.review_request_id}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-gray-500">{t(lang, 'reviewStatus')}</dt>
                    <dd className="text-gray-200">
                      {formatStatus(detail.review_sync.last_review_status)}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-gray-500">{t(lang, 'httpStatus')}</dt>
                    <dd className="text-gray-200">{detail.review_sync.last_review_http_status || '—'}</dd>
                  </div>
                  <div>
                    <dt className="text-gray-500">{t(lang, 'lastChecked')}</dt>
                    <dd className="text-gray-200">{formatDateTime(detail.review_sync.last_checked_at)}</dd>
                  </div>
                  <div>
                    <dt className="text-gray-500">{t(lang, 'nextCheckAt')}</dt>
                    <dd className="text-gray-200">{formatDateTime(detail.review_sync.next_check_at)}</dd>
                  </div>
                  <div>
                    <dt className="text-gray-500">{t(lang, 'consecutiveFailures')}</dt>
                    <dd className="text-gray-200">{detail.review_sync.consecutive_failures}</dd>
                  </div>
                  {detail.review_sync.last_error && (
                    <div className="sm:col-span-2">
                      <dt className="text-gray-500">{t(lang, 'syncError')}</dt>
                      <dd className="text-rose-300">{detail.review_sync.last_error}</dd>
                    </div>
                  )}
                </dl>
              </div>
            )}

            <div className="rounded-lg border border-gray-800 p-4">
              <div className="flex items-center justify-between gap-3 mb-3">
                <h4 className="text-sm font-medium text-gray-300">{t(lang, 'taskMemory')}</h4>
                {summaryEntry && (
                  <span className="text-xs text-gray-500">
                    v{summaryEntry.version} · {formatDateTime(summaryEntry.updated_at)}
                  </span>
                )}
              </div>
              <div className="space-y-4">
                <div>
                  <label className="block text-xs text-gray-500 mb-1">{t(lang, 'taskSummary')}</label>
                  <textarea
                    rows={4}
                    value={summaryDraft}
                    disabled={busy}
                    onChange={(e) => {
                      setSummaryDraft(e.target.value)
                      setSummaryDirty(true)
                    }}
                    className="w-full rounded bg-gray-800 border border-gray-700 px-2 py-2 text-sm text-white disabled:opacity-50"
                  />
                  <div className="mt-2 flex items-center justify-between gap-3">
                    <p className="text-xs text-gray-500">{t(lang, 'taskSummaryHelp')}</p>
                    <button
                      type="button"
                      disabled={busy || !detail.task.id}
                      onClick={saveTaskSummary}
                      className="px-3 py-1.5 rounded bg-blue-700 text-sm text-white hover:bg-blue-600 disabled:opacity-50"
                    >
                      {t(lang, 'saveSummary')}
                    </button>
                  </div>
                </div>
                <div>
                  <label className="block text-xs text-gray-500 mb-1">{t(lang, 'taskFacts')}</label>
                  {!factsEntry && <p className="text-sm text-gray-500">{t(lang, 'noTaskFacts')}</p>}
                  {factsEntry && (
                    <pre className="overflow-x-auto rounded bg-gray-950 p-3 text-xs text-gray-300">
                      {JSON.stringify(factsEntry.payload_json || {}, null, 2)}
                    </pre>
                  )}
                </div>
              </div>
            </div>

            {(detail.artifacts || []).length > 0 && (
              <div className="rounded-lg border border-gray-800 p-4">
                <h4 className="text-sm font-medium text-gray-300 mb-2">{t(lang, 'taskArtifacts')}</h4>
                <ul className="space-y-3 text-sm">
                  {detail.artifacts.map((artifact) => (
                    <li key={artifact.id} className="rounded border border-gray-800 bg-gray-950/40 p-3">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-white font-medium">{artifact.kind}</span>
                        <span className="text-gray-500 text-xs">{formatDateTime(artifact.created_at)}</span>
                      </div>
                      {artifact.uri && <div className="text-xs text-gray-400 mt-1">ref: {artifact.uri}</div>}
                      {artifact.payload && (
                        <pre className="mt-2 overflow-x-auto rounded bg-gray-950 p-2 text-xs text-gray-300">
                          {JSON.stringify(artifact.payload, null, 2)}
                        </pre>
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {(detail.linked_review_requests || []).length > 0 && (
              <div className="rounded-lg border border-gray-800 p-4">
                <h4 className="text-sm font-medium text-gray-300 mb-2">{t(lang, 'linkedReview')}</h4>
                <ul className="space-y-2 text-sm">
                  {detail.linked_review_requests.map((lr) => (
                    <li key={lr.action_id} className="text-gray-400 border-l-2 border-indigo-600 pl-2">
                      {lr.request ? (
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-white font-mono text-xs">{String(lr.request.id)}</span>
                          <span>·</span>
                          <span>
                            {formatStatus(lr.request.status)} · {formatStatus(lr.request.decision)} ·{' '}
                            {formatStatus(lr.request.risk_level)}
                          </span>
                          {lr.request.id && (
                            <button
                              type="button"
                              onClick={() => onViewReplay(String(lr.request?.id))}
                              className="rounded border border-indigo-800 px-2 py-0.5 text-xs text-indigo-200 hover:bg-indigo-950/50"
                            >
                              {t(lang, 'openReplay')}
                            </button>
                          )}
                        </div>
                      ) : (
                        <span className="text-gray-500">{t(lang, 'requestUnavailable')}</span>
                      )}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            <div className="rounded-lg border border-gray-800 p-4">
              <h4 className="text-sm font-medium text-gray-300 mb-2">{t(lang, 'taskTimeline')}</h4>
              <ul className="space-y-2 text-sm">
                {timeline().map((row, i) => (
                  <li key={i} className="border-l border-gray-700 pl-2">
                    <span className="text-gray-500 text-xs">{formatDateTime(row.at)}</span>
                    <span className="text-indigo-400 text-xs ml-2">[{row.kind}]</span>
                    <span className="text-gray-400 text-xs ml-2">{row.label}</span>
                    <div className="text-gray-200">{row.body}</div>
                  </li>
                ))}
              </ul>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
