import { useState, useEffect } from 'react'
import { t, relativeTime } from '../i18n'
import {
  fetchCompanionConnectorCapabilities,
  fetchCompanionConnectorExecutions,
  fetchCompanionConnectors,
} from '../api'

interface Connector {
  id: string
  name: string
  kind: string
  enabled: boolean
  config: Record<string, unknown>
  created_at: string
  updated_at: string
}

interface Execution {
  id: string
  connector_id: string
  operation: string
  status: string
  external_ref: string
  result: Record<string, unknown>
  error_message: string
  duration_ms: number
  created_at: string
}

interface Capability {
  connector_id: string
  kind: string
  capabilities: { operation: string; side_effect: boolean; read_only: boolean }[]
}

const statusColors: Record<string, string> = {
  success: 'text-green-400 bg-green-900/30',
  failure: 'text-red-400 bg-red-900/30',
  partial: 'text-yellow-400 bg-yellow-900/30',
}

export default function Connectors({ lang }: { lang: string }) {
  const [connectors, setConnectors] = useState<Connector[]>([])
  const [capabilities, setCapabilities] = useState<Capability[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [executions, setExecutions] = useState<Execution[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    Promise.all([
      fetchCompanionConnectors().then((d) => d.connectors || []),
      fetchCompanionConnectorCapabilities().then((d) => d.connectors || []),
    ]).then(([conns, caps]) => {
      setConnectors(conns)
      setCapabilities(caps)
      setLoading(false)
    })
  }, [])

  const selectConnector = async (id: string) => {
    setSelectedId(id)
    const data = await fetchCompanionConnectorExecutions(id)
    setExecutions(data.executions || [])
  }

  if (loading) return <p className="text-gray-400 text-sm">{t(lang, 'loading')}</p>

  return (
    <div>
      <h2 className="text-xl font-bold text-white mb-4">{t(lang, 'connectors')}</h2>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
        {connectors.map(c => {
          const caps = capabilities.find(cap => cap.connector_id === c.id || cap.kind === c.kind)
          return (
            <div key={c.id}
              onClick={() => selectConnector(c.id)}
              className={`bg-gray-800 rounded-lg p-4 border cursor-pointer transition-colors ${
                selectedId === c.id ? 'border-blue-500' : 'border-gray-700 hover:border-gray-600'
              }`}>
              <div className="flex items-center justify-between mb-2">
                <h3 className="font-medium text-white">{c.name}</h3>
                <span className={`text-xs px-2 py-0.5 rounded ${
                  c.enabled ? 'bg-green-900/30 text-green-400' : 'bg-gray-700 text-gray-500'
                }`}>
                  {c.enabled ? t(lang, 'connActive') : t(lang, 'connInactive')}
                </span>
              </div>
              <p className="text-xs text-gray-400 mb-2">{c.kind}</p>
              {caps && (
                <div className="flex flex-wrap gap-1">
                  {caps.capabilities.map(cap => (
                    <span key={cap.operation} className="text-xs bg-gray-700 text-gray-300 px-1.5 py-0.5 rounded">
                      {cap.operation.split('.').pop()}
                      {cap.side_effect && <span className="text-yellow-500 ml-1">!</span>}
                    </span>
                  ))}
                </div>
              )}
            </div>
          )
        })}
      </div>

      {connectors.length === 0 && (
        <p className="text-gray-500 text-sm">{t(lang, 'connNoConnectors')}</p>
      )}

      {selectedId && (
        <div>
          <h3 className="text-lg font-semibold text-white mb-3">
            {t(lang, 'connRecentExecutions')}
          </h3>
          {executions.length === 0 ? (
            <p className="text-gray-500 text-sm">{t(lang, 'connNoExecutions')}</p>
          ) : (
            <div className="space-y-2">
              {executions.map(e => (
                <div key={e.id} className="bg-gray-800 rounded p-3 border border-gray-700">
                  <div className="flex items-center justify-between mb-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-white">{e.operation}</span>
                      <span className={`text-xs px-2 py-0.5 rounded ${statusColors[e.status] || 'text-gray-400'}`}>
                        {e.status}
                      </span>
                    </div>
                    <div className="flex items-center gap-3">
                      <span className="text-xs text-gray-500">{e.duration_ms}ms</span>
                      <span className="text-xs text-gray-500">{relativeTime(lang, e.created_at)}</span>
                    </div>
                  </div>
                  {e.external_ref && (
                    <p className="text-xs text-gray-400">ref: {e.external_ref}</p>
                  )}
                  {e.error_message && (
                    <p className="text-xs text-red-400 mt-1">{e.error_message}</p>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
