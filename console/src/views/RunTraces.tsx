import { useEffect, useState } from 'react'
import { fetchCompanionRunTrace, fetchCompanionRunTracesByOrg } from '../api'

interface IdentityChain {
  initiating_user?: string
  tenant?: string
  product_surface?: string
  companion_principal?: string
  capability_principal?: string
  approval_actor?: string
}

interface ToolTrace {
  name: string
  tool_call_id?: string
  allowed: boolean
  decision_reason?: string
  duration_ms: number
  error?: string
}

interface GuardrailEvent {
  type: string
  target?: string
  reason: string
}

interface RunTrace {
  run_id: string
  org_id: string
  user_id: string
  task_id?: string | null
  product_surface: string
  intent: string
  autonomy_level: string
  identity_chain: IdentityChain
  guardrail_events?: GuardrailEvent[]
  tool_calls?: ToolTrace[]
  started_at: string
  completed_at?: string
  error?: string
}

function formatDuration(start: string, end?: string) {
  if (!end) return '—'
  const ms = new Date(end).getTime() - new Date(start).getTime()
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

export default function RunTraces({ runId }: { runId?: string | null }) {
  const [traces, setTraces] = useState<RunTrace[]>([])
  const [selected, setSelected] = useState<RunTrace | null>(null)
  const [selectedId, setSelectedId] = useState<string | null>(runId ?? null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (selectedId) return
    fetchCompanionRunTracesByOrg(50)
      .then((r: { traces?: RunTrace[] }) => setTraces(r.traces || []))
      .catch((e: Error) => setError(e.message))
  }, [selectedId])

  useEffect(() => {
    if (!selectedId) {
      setSelected(null)
      return
    }
    setLoading(true)
    fetchCompanionRunTrace(selectedId)
      .then((r: RunTrace) => {
        setSelected(r)
        setError(null)
      })
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [selectedId])

  if (!selectedId) {
    return (
      <div>
        <h2 className="text-xl font-bold mb-4">Run Traces</h2>
        {error && <p className="text-red-400 mb-4">{error}</p>}
        {traces.length === 0 && <p className="text-gray-500">No run traces yet.</p>}
        <div className="space-y-2">
          {traces.map((tr) => (
            <button
              key={tr.run_id}
              onClick={() => setSelectedId(tr.run_id)}
              className="w-full text-left bg-gray-900 border border-gray-800 rounded p-3 hover:bg-gray-800 transition-colors"
            >
              <div className="flex items-center gap-3 text-sm">
                <span className="font-mono text-xs text-gray-500">{tr.run_id.slice(0, 8)}</span>
                <span className="font-medium">{tr.intent || 'general.assist'}</span>
                <span className="text-gray-500">{tr.product_surface}</span>
                <span className="text-gray-600">A:{tr.autonomy_level}</span>
                <span className="ml-auto text-gray-600 text-xs">{new Date(tr.started_at).toLocaleString()}</span>
              </div>
              {tr.error && <p className="text-red-400 text-xs mt-1">err: {tr.error}</p>}
            </button>
          ))}
        </div>
      </div>
    )
  }

  if (loading) return <p className="text-gray-500">Loading...</p>
  if (error) return <p className="text-red-400">{error}</p>
  if (!selected) return null

  return (
    <div>
      <div className="flex items-center gap-3 mb-6">
        <button onClick={() => { setSelectedId(null); setSelected(null) }} className="text-gray-400 hover:text-white text-sm">&larr; Back</button>
        <h2 className="text-xl font-bold">Run Trace</h2>
      </div>
      <div className="bg-gray-900 border border-gray-800 rounded-lg p-4 mb-6">
        <div className="grid grid-cols-2 gap-2 text-sm">
          <div><span className="text-gray-500">Run:</span> <span className="font-mono text-xs">{selected.run_id}</span></div>
          <div><span className="text-gray-500">Started:</span> {new Date(selected.started_at).toLocaleString()}</div>
          <div><span className="text-gray-500">Intent:</span> {selected.intent}</div>
          <div><span className="text-gray-500">Surface:</span> {selected.product_surface}</div>
          <div><span className="text-gray-500">Autonomy:</span> {selected.autonomy_level}</div>
          <div><span className="text-gray-500">Duration:</span> {formatDuration(selected.started_at, selected.completed_at)}</div>
          {selected.task_id && <div><span className="text-gray-500">Task:</span> <span className="font-mono text-xs">{selected.task_id}</span></div>}
          {selected.error && <div className="col-span-2"><span className="text-red-400">Error:</span> {selected.error}</div>}
        </div>
      </div>

      <h3 className="text-sm font-semibold text-gray-400 uppercase mb-2">Identity Chain</h3>
      <div className="bg-gray-900 border border-gray-800 rounded p-3 mb-6 text-xs font-mono space-y-1">
        <div><span className="text-gray-500">initiating_user:</span> {selected.identity_chain.initiating_user || '—'}</div>
        <div><span className="text-gray-500">tenant:</span> {selected.identity_chain.tenant || '—'}</div>
        <div><span className="text-gray-500">product_surface:</span> {selected.identity_chain.product_surface || '—'}</div>
        <div><span className="text-gray-500">companion_principal:</span> {selected.identity_chain.companion_principal || '—'}</div>
        <div><span className="text-gray-500">capability_principal:</span> {selected.identity_chain.capability_principal || '—'}</div>
        <div><span className="text-gray-500">approval_actor:</span> {selected.identity_chain.approval_actor || '—'}</div>
      </div>

      {selected.guardrail_events && selected.guardrail_events.length > 0 && (
        <>
          <h3 className="text-sm font-semibold text-gray-400 uppercase mb-2">Guardrail Events</h3>
          <div className="space-y-2 mb-6">
            {selected.guardrail_events.map((e, i) => (
              <div key={i} className="bg-yellow-900/20 border border-yellow-700/40 rounded p-2 text-xs">
                <span className="font-mono text-yellow-300">{e.type}</span>
                {e.target && <span className="text-gray-400 ml-2">on {e.target}</span>}
                <p className="text-gray-300 mt-1">{e.reason}</p>
              </div>
            ))}
          </div>
        </>
      )}

      <h3 className="text-sm font-semibold text-gray-400 uppercase mb-2">Tool Calls</h3>
      {(!selected.tool_calls || selected.tool_calls.length === 0) ? (
        <p className="text-gray-500 text-sm">No tool calls in this run.</p>
      ) : (
        <div className="space-y-2">
          {selected.tool_calls.map((tc, i) => (
            <div key={i} className={`border rounded p-2 text-xs ${tc.allowed ? 'border-green-700/40 bg-green-900/10' : 'border-red-700/40 bg-red-900/10'}`}>
              <div className="flex items-center gap-3">
                <span className="font-mono">{tc.name}</span>
                <span className={tc.allowed ? 'text-green-400' : 'text-red-400'}>
                  {tc.allowed ? 'allowed' : 'rejected'}
                </span>
                <span className="ml-auto text-gray-500">{tc.duration_ms}ms</span>
              </div>
              {tc.decision_reason && <p className="text-gray-400 mt-1">{tc.decision_reason}</p>}
              {tc.error && <p className="text-red-400 mt-1">err: {tc.error}</p>}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
