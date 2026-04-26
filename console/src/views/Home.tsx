import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  approveApproval,
  executeCompanionTask,
  fetchCompanionMemory,
  fetchCompanionTask,
  fetchCompanionTasks,
  fetchPendingApprovals,
  fetchRequest,
  rejectApproval,
  retryCompanionTask,
  syncCompanionTaskFromReview,
} from '../api'
import { t, relativeTime } from '../i18n'
import RiskBadge from '../components/RiskBadge'
import StatusBadge from '../components/StatusBadge'

type TaskRow = {
  id: string
  title: string
  goal?: string
  status: string
  review_status?: string
  updated_at: string
}

type TaskDetail = {
  task: TaskRow & {
    created_by?: string
    review_last_checked_at?: string
    review_sync_error?: string
  }
  review_sync?: {
    review_request_id: string
    last_review_status?: string
    last_checked_at?: string
    next_check_at?: string
  }
  execution_plan?: {
    operation: string
  }
  execution_state?: {
    retryable?: boolean
    retry_count?: number
    last_error?: string
    last_execution_status?: string
    verification_result?: {
      status?: string
      summary?: string
    }
  }
  linked_review_requests?: Array<{
    request?: {
      id?: string
      status?: string
    }
  }>
}

type MemoryEntry = {
  id: string
  kind: string
  content_text: string
  payload_json?: Record<string, unknown>
  updated_at: string
}

type ApprovalItem = {
  id: string
  request_id: string
  expires_at: string
  break_glass?: boolean
  current_approvals?: number
  required_approvals?: number
  decisions?: Array<{
    approver_id?: string
    action?: string
    note?: string
  }>
}

type ReviewRequest = {
  id: string
  action_type?: string
  target_resource?: string
  target_system?: string
  risk_level?: string
  status?: string
  decision?: string
  requester_id?: string
  requester_type?: string
  params?: Record<string, unknown>
}

type EnrichedApproval = {
  approval: ApprovalItem
  request: ReviewRequest | null
}

type TaskMemoryProjection = {
  summary?: MemoryEntry
  facts?: MemoryEntry
}

type ActionKind = 'sync' | 'execute' | 'retry'
type ApprovalDecision = 'approve' | 'reject'

const sectionTone = {
  approval: 'from-amber-500/10 via-amber-500/5 to-transparent border-amber-500/20',
  execute: 'from-emerald-500/10 via-emerald-500/5 to-transparent border-emerald-500/20',
  waiting: 'from-sky-500/10 via-sky-500/5 to-transparent border-sky-500/20',
  failed: 'from-rose-500/10 via-rose-500/5 to-transparent border-rose-500/20',
  recent: 'from-indigo-500/10 via-indigo-500/5 to-transparent border-indigo-500/20',
}

function formatDateTime(value?: string | null) {
  if (!value) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString()
}

function formatRelative(value?: string | null, lang = 'en') {
  if (!value) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return relativeTime(lang, value)
}

function linkedTaskId(request: ReviewRequest | null | undefined) {
  const nexus = request?.params?.nexus as Record<string, unknown> | undefined
  const taskId = nexus?.task_id
  return typeof taskId === 'string' && taskId ? taskId : null
}

function factsNextStep(memory?: TaskMemoryProjection) {
  const nextStep = memory?.facts?.payload_json?.next_step
  return typeof nextStep === 'string' && nextStep ? nextStep : null
}

function taskSummary(task: TaskRow, memory?: TaskMemoryProjection) {
  if (memory?.summary?.content_text) {
    return memory.summary.content_text
  }
  if (task.goal) {
    return task.goal
  }
  return task.title
}

function taskReviewRequestId(detail?: TaskDetail) {
  return (
    detail?.review_sync?.review_request_id ||
    detail?.linked_review_requests?.find((item) => item.request?.id)?.request?.id ||
    null
  )
}

function canExecuteTask(task: TaskRow) {
  return task.status === 'waiting_for_input'
}

function canSyncTask(task: TaskRow) {
  return task.status === 'waiting_for_approval'
}

function canRetryTask(task: TaskRow, detail?: TaskDetail) {
  return task.status === 'failed' && Boolean(detail?.execution_state?.retryable)
}

function StatCard({
  label,
  value,
  accent,
}: {
  label: string
  value: number
  accent: string
}) {
  return (
    <div className="rounded-2xl border border-gray-800 bg-gray-950/70 p-4 shadow-[0_0_0_1px_rgba(255,255,255,0.02)]">
      <p className="text-[11px] uppercase tracking-[0.24em] text-gray-500">{label}</p>
      <p className={`mt-2 text-3xl font-semibold ${accent}`}>{value}</p>
    </div>
  )
}

function ActionButton({
  label,
  onClick,
  busy,
  disabled,
  tone,
}: {
  label: string
  onClick: () => void
  busy?: boolean
  disabled?: boolean
  tone?: 'neutral' | 'info' | 'success' | 'danger'
}) {
  const cls = {
    neutral: 'border-gray-700 bg-gray-900 text-gray-200 hover:border-gray-500',
    info: 'border-sky-800 bg-sky-950/50 text-sky-200 hover:border-sky-600',
    success: 'border-emerald-800 bg-emerald-950/50 text-emerald-200 hover:border-emerald-600',
    danger: 'border-rose-800 bg-rose-950/50 text-rose-200 hover:border-rose-600',
  }
  return (
    <button
      type="button"
      disabled={busy || disabled}
      onClick={onClick}
      className={`rounded-full border px-3 py-1.5 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${cls[tone || 'neutral']}`}
    >
      {busy ? '…' : label}
    </button>
  )
}

function SectionCard({
  title,
  subtitle,
  tone,
  action,
  children,
}: {
  title: string
  subtitle: string
  tone: keyof typeof sectionTone
  action?: ReactNode
  children: ReactNode
}) {
  return (
    <section
      className={`rounded-3xl border bg-gradient-to-br p-5 ${sectionTone[tone]} shadow-[0_18px_60px_rgba(0,0,0,0.28)]`}
    >
      <div className="mb-4 flex items-start justify-between gap-4">
        <div>
          <h3 className="text-lg font-semibold text-white">{title}</h3>
          <p className="mt-1 text-sm text-gray-400">{subtitle}</p>
        </div>
        {action}
      </div>
      {children}
    </section>
  )
}

function ApprovalDecisionCard({
  approval,
  request,
  taskDetail,
  taskMemory,
  lang,
  busyApprove,
  busyReject,
  onViewTask,
  onViewReplay,
  onDecision,
}: {
  approval: ApprovalItem
  request: ReviewRequest | null
  taskDetail?: TaskDetail
  taskMemory?: TaskMemoryProjection
  lang: string
  busyApprove?: boolean
  busyReject?: boolean
  onViewTask: (taskId: string) => void
  onViewReplay: (requestId: string) => void
  onDecision: (decision: ApprovalDecision, note: string) => Promise<void>
}) {
  const [mode, setMode] = useState<ApprovalDecision | null>(null)
  const [note, setNote] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [cardError, setCardError] = useState<string | null>(null)
  const taskId = linkedTaskId(request)
  const expectedWord = mode === 'approve' ? 'APPROVE' : 'REJECT'
  const summary =
    taskId && taskMemory
      ? taskSummary(
          {
            id: taskId,
            title: request?.action_type || approval.request_id,
            goal: request?.target_resource,
            status: taskDetail?.task.status || 'waiting_for_approval',
            review_status: request?.status,
            updated_at: taskDetail?.task.updated_at || '',
          },
          taskMemory,
        )
      : null
  const nextStep = taskId ? factsNextStep(taskMemory) : null
  const isSubmitting = Boolean(mode === 'approve' ? busyApprove : busyReject)
  const isValid = Boolean(mode) && note.trim().length >= 3 && confirmation === expectedWord

  const start = (decision: ApprovalDecision) => {
    setMode(decision)
    setConfirmation('')
    setCardError(null)
  }

  const cancel = () => {
    setMode(null)
    setNote('')
    setConfirmation('')
    setCardError(null)
  }

  const submit = async () => {
    if (!mode || !isValid) return
    setCardError(null)
    try {
      await onDecision(mode, note.trim())
      cancel()
    } catch (e) {
      setCardError(e instanceof Error ? e.message : 'decision failed')
    }
  }

  return (
    <div className="rounded-2xl border border-gray-800 bg-gray-950/65 p-4 shadow-[0_0_0_1px_rgba(255,255,255,0.02)]">
      <div className="flex flex-wrap items-center gap-2">
        <p className="text-sm font-semibold text-white">{request?.action_type || approval.request_id}</p>
        {request?.risk_level && <RiskBadge level={request.risk_level} />}
        {request?.status && <StatusBadge status={request.status} />}
        {approval.break_glass && (
          <span className="rounded-full border border-red-700/70 bg-red-950/60 px-2 py-0.5 text-[11px] font-medium uppercase tracking-[0.16em] text-red-200">
            {t(lang, 'breakGlass')} {approval.current_approvals || 0}/{approval.required_approvals || 0}
          </span>
        )}
      </div>

      <p className="mt-2 text-sm text-gray-300">
        {request?.target_resource || request?.target_system || t(lang, 'approvalInbox')}
      </p>
      {summary && <p className="mt-2 text-sm leading-6 text-gray-400">{summary}</p>}
      {nextStep && (
        <p className="mt-2 text-xs uppercase tracking-[0.2em] text-gray-500">
          {t(lang, 'nextStep')}: <span className="normal-case tracking-normal text-gray-300">{nextStep}</span>
        </p>
      )}
      <div className="mt-3 flex flex-wrap items-center gap-3 text-xs text-gray-500">
        <span>{request?.requester_id || '—'}{request?.requester_type ? ` (${request.requester_type})` : ''}</span>
        <span>{formatRelative(approval.expires_at, lang)}</span>
        {taskId && <span className="font-mono text-[11px] text-gray-600">{taskId}</span>}
      </div>

      {approval.break_glass && approval.decisions && approval.decisions.length > 0 && (
        <div className="mt-3 space-y-1">
          {approval.decisions.map((decision, index) => (
            <div
              key={`${decision.approver_id || 'decision'}-${index}`}
              className={`rounded-lg px-2 py-1 text-xs ${
                decision.action === 'approve'
                  ? 'bg-emerald-950/50 text-emerald-300'
                  : 'bg-rose-950/50 text-rose-300'
              }`}
            >
              {decision.approver_id}: {decision.action} {decision.note ? `— ${decision.note}` : ''}
            </div>
          ))}
        </div>
      )}

      {!mode && (
        <div className="mt-4 flex flex-wrap gap-2">
          <ActionButton label={t(lang, 'approve')} onClick={() => start('approve')} busy={busyApprove} tone="success" />
          <ActionButton label={t(lang, 'reject')} onClick={() => start('reject')} busy={busyReject} tone="danger" />
          {taskId && <ActionButton label={t(lang, 'openTask')} onClick={() => onViewTask(taskId)} />}
          {request?.id && <ActionButton label={t(lang, 'openReplay')} onClick={() => onViewReplay(request.id)} tone="info" />}
        </div>
      )}

      {mode && (
        <div className={`mt-4 rounded-2xl border p-4 ${
          mode === 'approve'
            ? 'border-emerald-700/40 bg-emerald-950/20'
            : 'border-rose-700/40 bg-rose-950/20'
        }`}>
          <p className="text-sm text-gray-300">{t(lang, 'noteRequired')}</p>
          <textarea
            rows={3}
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder={t(lang, 'notePlaceholder')}
            className="mt-3 w-full rounded-xl border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white"
          />
          <p className="mt-3 text-xs text-gray-400">
            {t(lang, 'typeToConfirm')} <span className={`font-mono font-bold ${mode === 'approve' ? 'text-emerald-300' : 'text-rose-300'}`}>{expectedWord}</span>
          </p>
          <input
            value={confirmation}
            onChange={(e) => setConfirmation(e.target.value)}
            placeholder={expectedWord}
            className="mt-2 w-full rounded-xl border border-gray-700 bg-gray-900 px-3 py-2 text-sm font-mono text-white"
          />
          {cardError && <p className="mt-3 text-xs text-rose-300">{cardError}</p>}
          <div className="mt-4 flex flex-wrap gap-2">
            <ActionButton
              label={mode === 'approve' ? t(lang, 'confirmApprove') : t(lang, 'confirmReject')}
              onClick={submit}
              busy={isSubmitting}
              disabled={!isValid}
              tone={mode === 'approve' ? 'success' : 'danger'}
            />
            <button
              type="button"
              onClick={cancel}
              className="rounded-full border border-gray-700 bg-gray-900 px-3 py-1.5 text-xs font-medium text-gray-300 transition-colors hover:border-gray-500"
            >
              {t(lang, 'cancel')}
            </button>
          </div>
          {!isValid && (
            <p className="mt-3 text-[11px] uppercase tracking-[0.16em] text-gray-500">
              {t(lang, 'homeApprovalConfirmationHint')}
            </p>
          )}
        </div>
      )}
    </div>
  )
}

function TaskCard({
  task,
  detail,
  memory,
  lang,
  busy,
  onViewTask,
  onViewReplay,
  onSync,
  onExecute,
  onRetry,
}: {
  task: TaskRow
  detail?: TaskDetail
  memory?: TaskMemoryProjection
  lang: string
  busy?: boolean
  onViewTask: (taskId: string) => void
  onViewReplay: (requestId: string) => void
  onSync: (taskId: string) => void
  onExecute: (taskId: string) => void
  onRetry: (taskId: string) => void
}) {
  const reviewRequestId = taskReviewRequestId(detail)
  const nextStep = factsNextStep(memory)
  const lastError = detail?.execution_state?.last_error || detail?.task.review_sync_error

  return (
    <div className="rounded-2xl border border-gray-800 bg-gray-950/65 p-4">
      <div className="flex flex-wrap items-center gap-2">
        <p className="text-sm font-semibold text-white">{task.title}</p>
        <span className="text-gray-600">•</span>
        <StatusBadge status={task.status} />
        {task.review_status && <StatusBadge status={task.review_status} />}
      </div>
      <p className="mt-2 text-sm leading-6 text-gray-300">{taskSummary(task, memory)}</p>
      {nextStep && (
        <p className="mt-2 text-xs uppercase tracking-[0.2em] text-gray-500">
          {t(lang, 'nextStep')}: <span className="text-gray-300 normal-case tracking-normal">{nextStep}</span>
        </p>
      )}
      {detail?.execution_plan?.operation && (
        <p className="mt-2 text-xs text-gray-500">
          {t(lang, 'operation')}: <span className="text-gray-300">{detail.execution_plan.operation}</span>
        </p>
      )}
      {lastError && (
        <p className="mt-2 text-xs text-rose-300">
          {t(lang, 'errorMsg')}: {lastError}
        </p>
      )}
      <div className="mt-4 flex flex-wrap gap-2">
        <ActionButton label={t(lang, 'openTask')} onClick={() => onViewTask(task.id)} busy={busy} />
        {reviewRequestId && (
          <ActionButton
            label={t(lang, 'openReplay')}
            onClick={() => onViewReplay(reviewRequestId)}
            busy={busy}
            tone="info"
          />
        )}
        {canSyncTask(task) && (
          <ActionButton
            label={t(lang, 'syncFromReview')}
            onClick={() => onSync(task.id)}
            busy={busy}
            tone="info"
          />
        )}
        {canExecuteTask(task) && (
          <ActionButton
            label={t(lang, 'executeTask')}
            onClick={() => onExecute(task.id)}
            busy={busy}
            tone="success"
          />
        )}
        {canRetryTask(task, detail) && (
          <ActionButton
            label={t(lang, 'retryTask')}
            onClick={() => onRetry(task.id)}
            busy={busy}
            tone="danger"
          />
        )}
      </div>
      <div className="mt-3 flex items-center justify-between text-xs text-gray-500">
        <span>{t(lang, 'updated')}: {formatDateTime(task.updated_at)}</span>
        {detail?.review_sync?.next_check_at && <span>{formatRelative(detail.review_sync.next_check_at, lang)}</span>}
      </div>
    </div>
  )
}

export default function Home({
  lang,
  onViewTask = (_taskId: string) => {},
  onViewReplay = (_requestId: string) => {},
  onViewInbox = () => {},
}: {
  lang: string
  onViewTask?: (taskId: string) => void
  onViewReplay?: (requestId: string) => void
  onViewInbox?: () => void
}) {
  const [tasks, setTasks] = useState<TaskRow[]>([])
  const [approvals, setApprovals] = useState<EnrichedApproval[]>([])
  const [taskDetails, setTaskDetails] = useState<Record<string, TaskDetail>>({})
  const [taskMemory, setTaskMemory] = useState<Record<string, TaskMemoryProjection>>({})
  const [loading, setLoading] = useState(true)
  const [message, setMessage] = useState<{ type: 'ok' | 'err'; text: string } | null>(null)
  const [busyAction, setBusyAction] = useState<Record<string, boolean>>({})

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [taskRes, approvalRes] = await Promise.all([fetchCompanionTasks(), fetchPendingApprovals()])
      const taskRows = [...((taskRes.data || []) as TaskRow[])].sort(
        (a, b) => new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
      )
      setTasks(taskRows)

      const pendingApprovals = ((approvalRes.data || []) as ApprovalItem[]).slice(0, 6)
      const approvalItems = await Promise.all(
        pendingApprovals.map(async (approval) => {
          try {
            const request = (await fetchRequest(approval.request_id)) as ReviewRequest
            return { approval, request }
          } catch {
            return { approval, request: null }
          }
        }),
      )
      setApprovals(approvalItems)
      const linkedApprovalTaskIds = approvalItems
        .map(({ request }) => linkedTaskId(request))
        .filter((value): value is string => Boolean(value))

      const candidateTasks = [
        ...taskRows.filter((task) => task.status === 'waiting_for_input').slice(0, 4),
        ...taskRows.filter((task) => task.status === 'waiting_for_approval').slice(0, 4),
        ...taskRows.filter((task) => task.status === 'failed').slice(0, 4),
        ...taskRows.slice(0, 6),
      ]
      const candidateIds = Array.from(new Set([...candidateTasks.map((task) => task.id), ...linkedApprovalTaskIds]))

      const [detailResults, memoryResults] = await Promise.all([
        Promise.allSettled(candidateIds.map(async (id) => [id, (await fetchCompanionTask(id)) as TaskDetail] as const)),
        Promise.allSettled(
          candidateIds.map(async (id) => [id, ((await fetchCompanionMemory('task', id)).entries || []) as MemoryEntry[]] as const),
        ),
      ])

      const nextDetails: Record<string, TaskDetail> = {}
      for (const result of detailResults) {
        if (result.status === 'fulfilled') {
          const [id, detail] = result.value
          nextDetails[id] = detail
        }
      }
      setTaskDetails(nextDetails)

      const nextMemory: Record<string, TaskMemoryProjection> = {}
      for (const result of memoryResults) {
        if (result.status === 'fulfilled') {
          const [id, entries] = result.value
          nextMemory[id] = {
            summary: entries.find((entry) => entry.kind === 'task_summary'),
            facts: entries.find((entry) => entry.kind === 'task_facts'),
          }
        }
      }
      setTaskMemory(nextMemory)
      setMessage(null)
    } catch (e) {
      const text = e instanceof Error ? e.message : 'failed to load home'
      setMessage({ type: 'err', text })
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  useEffect(() => {
    const id = setInterval(() => {
      load()
    }, 30000)
    return () => clearInterval(id)
  }, [load])

  const runTaskAction = async (taskId: string, action: ActionKind) => {
    const key = `${action}:${taskId}`
    setBusyAction((current) => ({ ...current, [key]: true }))
    try {
      if (action === 'sync') {
        await syncCompanionTaskFromReview(taskId)
      } else if (action === 'execute') {
        await executeCompanionTask(taskId)
      } else {
        await retryCompanionTask(taskId)
      }
      setMessage({ type: 'ok', text: t(lang, `homeAction_${action}_done`) })
      await load()
    } catch (e) {
      const text = e instanceof Error ? e.message : `failed to ${action} task`
      setMessage({ type: 'err', text })
    } finally {
      setBusyAction((current) => ({ ...current, [key]: false }))
    }
  }

  const runApprovalDecision = async (approval: ApprovalItem, request: ReviewRequest | null, decision: ApprovalDecision, note: string) => {
    const key = `${decision}:${approval.id}`
    const taskId = linkedTaskId(request)
    setBusyAction((current) => ({ ...current, [key]: true }))
    try {
      if (decision === 'approve') {
        await approveApproval(approval.id, note)
      } else {
        await rejectApproval(approval.id, note)
      }
      if (taskId) {
        try {
          await syncCompanionTaskFromReview(taskId)
        } catch {
          // fallback to normal refresh; Review may still be settling.
        }
      }
      setMessage({ type: 'ok', text: t(lang, `homeAction_${decision}_done`) })
      await load()
    } catch (e) {
      throw (e instanceof Error ? e : new Error(`failed to ${decision} approval`))
    } finally {
      setBusyAction((current) => ({ ...current, [key]: false }))
    }
  }

  const waitingForApproval = useMemo(
    () => tasks.filter((task) => task.status === 'waiting_for_approval').slice(0, 4),
    [tasks],
  )
  const readyToExecute = useMemo(
    () => tasks.filter((task) => task.status === 'waiting_for_input').slice(0, 4),
    [tasks],
  )
  const failedTasks = useMemo(() => tasks.filter((task) => task.status === 'failed').slice(0, 4), [tasks])
  const recentTasks = useMemo(() => tasks.slice(0, 6), [tasks])
  const activeTasks = useMemo(
    () => tasks.filter((task) => !['done', 'failed'].includes(task.status)).length,
    [tasks],
  )

  if (loading && tasks.length === 0) {
    return <p className="text-gray-500">{t(lang, 'loading')}</p>
  }

  return (
    <div className="space-y-6">
      <section className="relative overflow-hidden rounded-[2rem] border border-gray-800 bg-gray-950 px-6 py-6 shadow-[0_24px_80px_rgba(0,0,0,0.38)]">
        <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_top_left,rgba(14,165,233,0.12),transparent_28%),radial-gradient(circle_at_top_right,rgba(251,191,36,0.12),transparent_24%),radial-gradient(circle_at_bottom_left,rgba(16,185,129,0.12),transparent_22%),radial-gradient(circle_at_bottom_right,rgba(244,63,94,0.10),transparent_22%)]" />
        <div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-gray-700 to-transparent" />
        <div className="relative">
          <div className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
            <div className="max-w-2xl">
              <p className="text-[11px] uppercase tracking-[0.34em] text-gray-500">{t(lang, 'homeEyebrow')}</p>
              <h2 className="mt-3 text-3xl font-semibold tracking-tight text-white">{t(lang, 'homeTitle')}</h2>
              <p className="mt-2 text-sm leading-6 text-gray-400">{t(lang, 'homeSubtitle')}</p>
            </div>
            <button
              type="button"
              onClick={load}
              className="rounded-full border border-gray-700 bg-gray-900/80 px-4 py-2 text-sm font-medium text-gray-200 transition-colors hover:border-gray-500"
            >
              {t(lang, 'refresh')}
            </button>
          </div>

          <div className="mt-6 grid grid-cols-2 gap-3 xl:grid-cols-4">
            <StatCard label={t(lang, 'homePendingApprovals')} value={approvals.length} accent="text-amber-300" />
            <StatCard label={t(lang, 'homeReadyToExecute')} value={readyToExecute.length} accent="text-emerald-300" />
            <StatCard label={t(lang, 'homeFailedTasks')} value={failedTasks.length} accent="text-rose-300" />
            <StatCard label={t(lang, 'homeActiveTasks')} value={activeTasks} accent="text-sky-300" />
          </div>
        </div>
      </section>

      {message && (
        <div
          className={`rounded-2xl border px-4 py-3 text-sm ${
            message.type === 'ok'
              ? 'border-emerald-800 bg-emerald-950/40 text-emerald-200'
              : 'border-rose-800 bg-rose-950/40 text-rose-200'
          }`}
        >
          {message.text}
        </div>
      )}

      <div className="grid gap-6 xl:grid-cols-2">
        <SectionCard
          title={t(lang, 'homePendingApprovals')}
          subtitle={t(lang, 'homePendingApprovalsSubtitle')}
          tone="approval"
          action={
            <ActionButton
              label={t(lang, 'openInbox')}
              onClick={onViewInbox}
              tone="info"
            />
          }
        >
          <div className="space-y-3">
            {approvals.length === 0 && <p className="text-sm text-gray-500">{t(lang, 'noPendingApprovals')}</p>}
            {approvals.map(({ approval, request }) => {
              const taskId = linkedTaskId(request)
              return (
                <ApprovalDecisionCard
                  key={approval.id}
                  approval={approval}
                  request={request}
                  taskDetail={taskId ? taskDetails[taskId] : undefined}
                  taskMemory={taskId ? taskMemory[taskId] : undefined}
                  lang={lang}
                  busyApprove={Boolean(busyAction[`approve:${approval.id}`])}
                  busyReject={Boolean(busyAction[`reject:${approval.id}`])}
                  onViewTask={onViewTask}
                  onViewReplay={onViewReplay}
                  onDecision={(decision, note) => runApprovalDecision(approval, request, decision, note)}
                />
              )
            })}
          </div>
        </SectionCard>

        <SectionCard
          title={t(lang, 'homeReadyToExecute')}
          subtitle={t(lang, 'homeReadyToExecuteSubtitle')}
          tone="execute"
        >
          <div className="space-y-3">
            {readyToExecute.length === 0 && <p className="text-sm text-gray-500">{t(lang, 'homeEmptyExecute')}</p>}
            {readyToExecute.map((task) => (
              <TaskCard
                key={task.id}
                task={task}
                detail={taskDetails[task.id]}
                memory={taskMemory[task.id]}
                lang={lang}
                busy={Boolean(busyAction[`execute:${task.id}`])}
                onViewTask={onViewTask}
                onViewReplay={onViewReplay}
                onSync={(id) => runTaskAction(id, 'sync')}
                onExecute={(id) => runTaskAction(id, 'execute')}
                onRetry={(id) => runTaskAction(id, 'retry')}
              />
            ))}
          </div>
        </SectionCard>

        <SectionCard
          title={t(lang, 'homeWaitingApproval')}
          subtitle={t(lang, 'homeWaitingApprovalSubtitle')}
          tone="waiting"
        >
          <div className="space-y-3">
            {waitingForApproval.length === 0 && <p className="text-sm text-gray-500">{t(lang, 'homeEmptyWaiting')}</p>}
            {waitingForApproval.map((task) => (
              <TaskCard
                key={task.id}
                task={task}
                detail={taskDetails[task.id]}
                memory={taskMemory[task.id]}
                lang={lang}
                busy={Boolean(busyAction[`sync:${task.id}`])}
                onViewTask={onViewTask}
                onViewReplay={onViewReplay}
                onSync={(id) => runTaskAction(id, 'sync')}
                onExecute={(id) => runTaskAction(id, 'execute')}
                onRetry={(id) => runTaskAction(id, 'retry')}
              />
            ))}
          </div>
        </SectionCard>

        <SectionCard
          title={t(lang, 'homeFailedTasks')}
          subtitle={t(lang, 'homeFailedTasksSubtitle')}
          tone="failed"
        >
          <div className="space-y-3">
            {failedTasks.length === 0 && <p className="text-sm text-gray-500">{t(lang, 'homeEmptyFailed')}</p>}
            {failedTasks.map((task) => (
              <TaskCard
                key={task.id}
                task={task}
                detail={taskDetails[task.id]}
                memory={taskMemory[task.id]}
                lang={lang}
                busy={Boolean(busyAction[`retry:${task.id}`])}
                onViewTask={onViewTask}
                onViewReplay={onViewReplay}
                onSync={(id) => runTaskAction(id, 'sync')}
                onExecute={(id) => runTaskAction(id, 'execute')}
                onRetry={(id) => runTaskAction(id, 'retry')}
              />
            ))}
          </div>
        </SectionCard>
      </div>

      <SectionCard
        title={t(lang, 'homeRecentActivity')}
        subtitle={t(lang, 'homeRecentActivitySubtitle')}
        tone="recent"
      >
        <div className="space-y-3">
          {recentTasks.length === 0 && <p className="text-sm text-gray-500">{t(lang, 'noTasks')}</p>}
          {recentTasks.map((task) => {
            const memory = taskMemory[task.id]
            const detail = taskDetails[task.id]
            const nextStep = factsNextStep(memory)
            return (
              <div key={task.id} className="flex flex-col gap-3 rounded-2xl border border-gray-800 bg-gray-950/60 p-4 lg:flex-row lg:items-start lg:justify-between">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="text-sm font-semibold text-white">{task.title}</p>
                    <StatusBadge status={task.status} />
                    {task.review_status && <StatusBadge status={task.review_status} />}
                  </div>
                  <p className="mt-2 text-sm text-gray-300">{taskSummary(task, memory)}</p>
                  <div className="mt-2 flex flex-wrap gap-4 text-xs text-gray-500">
                    <span>{t(lang, 'updated')}: {formatDateTime(task.updated_at)}</span>
                    {nextStep && (
                      <span>
                        {t(lang, 'nextStep')}: <span className="text-gray-300">{nextStep}</span>
                      </span>
                    )}
                    {detail?.execution_state?.verification_result?.summary && (
                      <span>
                        {t(lang, 'verificationSummary')}: <span className="text-gray-300">{detail.execution_state.verification_result.summary}</span>
                      </span>
                    )}
                  </div>
                </div>
                <div className="flex shrink-0 flex-wrap gap-2">
                  <ActionButton label={t(lang, 'openTask')} onClick={() => onViewTask(task.id)} />
                  {taskReviewRequestId(detail) && (
                    <ActionButton
                      label={t(lang, 'openReplay')}
                      onClick={() => onViewReplay(taskReviewRequestId(detail) as string)}
                      tone="info"
                    />
                  )}
                </div>
              </div>
            )
          })}
        </div>
      </SectionCard>
    </div>
  )
}
