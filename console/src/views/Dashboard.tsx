import { useState, useEffect } from 'react'
import { fetchMetrics } from '../api'
import { t } from '../i18n'

const periods = ['7d', '14d', '30d']

function MetricCard({ label, value, color }) {
  return (
    <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
      <p className="text-gray-500 text-xs uppercase tracking-wide">{label}</p>
      <p className={`text-3xl font-bold mt-1 ${color}`}>{value}</p>
    </div>
  )
}

export default function Dashboard({ lang }) {
  const [metrics, setMetrics] = useState(null)
  const [period, setPeriod] = useState('7d')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  useEffect(() => {
    setLoading(true)
    fetchMetrics(period)
      .then((m) => { setMetrics(m); setError(null) })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [period])

  if (loading && !metrics) return <p className="text-gray-500">{t(lang, 'loading')}</p>
  if (error) return <p className="text-red-400">{error}</p>

  return (
    <div>
      <div className="flex items-center gap-4 mb-6">
        <h2 className="text-xl font-bold">{t(lang, 'dashboardTitle')}</h2>
        <div className="flex gap-1 ml-auto">
          {periods.map((p) => (
            <button key={p} onClick={() => setPeriod(p)}
              className={`px-3 py-1 rounded text-sm font-medium transition-colors ${
                period === p ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-white hover:bg-gray-800'
              }`}>
              {p}
            </button>
          ))}
        </div>
      </div>
      {metrics && (
        <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
          <MetricCard label={t(lang, 'totalRequests')} value={metrics.total_requests} color="text-white" />
          <MetricCard label={t(lang, 'allowed')} value={metrics.allowed} color="text-green-400" />
          <MetricCard label={t(lang, 'denied')} value={metrics.denied} color="text-red-400" />
          <MetricCard label={t(lang, 'pendingApproval')} value={metrics.pending_approval} color="text-yellow-400" />
          <MetricCard label={t(lang, 'approved')} value={metrics.approved} color="text-green-400" />
          <MetricCard label={t(lang, 'rejected')} value={metrics.rejected} color="text-red-400" />
        </div>
      )}
    </div>
  )
}
