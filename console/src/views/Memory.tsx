import { useState } from 'react'
import { deleteCompanionMemory, fetchCompanionMemory } from '../api'
import { t, relativeTime } from '../i18n'

interface MemoryEntry {
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
  expires_at: string | null
}

const kindLabels: Record<string, Record<string, string>> = {
  en: { task_summary: 'Task Summary', task_facts: 'Task Facts', playbook_snippet: 'Playbook', user_preference: 'Preference' },
  es: { task_summary: 'Resumen de tarea', task_facts: 'Hechos de tarea', playbook_snippet: 'Playbook', user_preference: 'Preferencia' }
}

export default function Memory({ lang }: { lang: string }) {
  const [entries, setEntries] = useState<MemoryEntry[]>([])
  const [scopeType, setScopeType] = useState('org')
  const [scopeId, setScopeId] = useState('')
  const [kindFilter, setKindFilter] = useState('')
  const [loading, setLoading] = useState(false)

  const load = async () => {
    if (!scopeId) return
    setLoading(true)
    const data = await fetchCompanionMemory(scopeType, scopeId, kindFilter || undefined)
    setEntries(data.entries || [])
    setLoading(false)
  }

  const handleDelete = async (id: string) => {
    try {
      await deleteCompanionMemory(id)
      setEntries(prev => prev.filter(e => e.id !== id))
    } catch {
      // keep current list; caller can retry
    }
  }

  return (
    <div>
      <h2 className="text-xl font-bold text-white mb-4">{t(lang, 'memory')}</h2>

      <div className="flex gap-3 mb-4 items-end">
        <div>
          <label className="block text-xs text-gray-400 mb-1">Scope</label>
          <select value={scopeType} onChange={e => setScopeType(e.target.value)}
            className="bg-gray-800 text-white rounded px-3 py-2 text-sm border border-gray-700">
            <option value="org">Org</option>
            <option value="task">Task</option>
            <option value="user">User</option>
          </select>
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1">Scope ID</label>
          <input value={scopeId} onChange={e => setScopeId(e.target.value)}
            placeholder="UUID or ID"
            className="bg-gray-800 text-white rounded px-3 py-2 text-sm border border-gray-700 w-64" />
        </div>
        <div>
          <label className="block text-xs text-gray-400 mb-1">Kind</label>
          <select value={kindFilter} onChange={e => setKindFilter(e.target.value)}
            className="bg-gray-800 text-white rounded px-3 py-2 text-sm border border-gray-700">
            <option value="">{t(lang, 'memAll')}</option>
            <option value="task_summary">task_summary</option>
            <option value="task_facts">task_facts</option>
            <option value="playbook_snippet">playbook_snippet</option>
            <option value="user_preference">user_preference</option>
          </select>
        </div>
        <button onClick={load}
          className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded text-sm font-medium">
          {t(lang, 'memSearch')}
        </button>
      </div>

      {loading && <p className="text-gray-400 text-sm">{t(lang, 'loading')}</p>}

      {!loading && entries.length === 0 && scopeId && (
        <p className="text-gray-500 text-sm">{t(lang, 'memNoEntries')}</p>
      )}

      <div className="space-y-3">
        {entries.map(e => (
          <div key={e.id} className="bg-gray-800 rounded-lg p-4 border border-gray-700">
            <div className="flex items-center justify-between mb-2">
              <div className="flex items-center gap-2">
                <span className="text-xs bg-gray-700 text-gray-300 px-2 py-0.5 rounded">
                  {(kindLabels[lang] || kindLabels.en)[e.kind] || e.kind}
                </span>
                <span className="text-sm font-medium text-white">{e.key}</span>
                <span className="text-xs text-gray-500">v{e.version}</span>
              </div>
              <div className="flex items-center gap-3">
                <span className="text-xs text-gray-500">{relativeTime(lang, e.updated_at)}</span>
                {e.expires_at && (
                  <span className="text-xs text-yellow-500">
                    {t(lang, 'memExpires')}: {new Date(e.expires_at).toLocaleDateString()}
                  </span>
                )}
                <button onClick={() => handleDelete(e.id)}
                  className="text-xs text-red-400 hover:text-red-300">
                  {t(lang, 'memDelete')}
                </button>
              </div>
            </div>
            {e.content_text && (
              <p className="text-sm text-gray-300 mb-2">{e.content_text}</p>
            )}
            {Object.keys(e.payload_json).length > 0 && (
              <pre className="text-xs text-gray-400 bg-gray-900 rounded p-2 overflow-x-auto">
                {JSON.stringify(e.payload_json, null, 2)}
              </pre>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
