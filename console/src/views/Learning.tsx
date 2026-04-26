import { useState, useEffect } from 'react'
import { fetchProposals, acceptProposal, dismissProposal, triggerAnalyze } from '../api'
import { t } from '../i18n'

export default function Learning({ lang }) {
  const [proposals, setProposals] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [analyzing, setAnalyzing] = useState(false)
  const [analyzeResult, setAnalyzeResult] = useState(null)

  const load = async () => {
    try { setLoading(true); const res = await fetchProposals(); setProposals(res.data || []); setError(null) }
    catch (e) { setError(e.message) } finally { setLoading(false) }
  }

  useEffect(() => { load() }, [])

  const handleAccept = async (id) => {
    try { await acceptProposal(id); setProposals((p) => p.filter((x) => x.id !== id)) }
    catch (e) { setError(e.message) }
  }
  const handleDismiss = async (id) => {
    try { await dismissProposal(id); setProposals((p) => p.filter((x) => x.id !== id)) }
    catch (e) { setError(e.message) }
  }
  const handleAnalyze = async () => {
    try { setAnalyzing(true); setAnalyzeResult(null); const res = await triggerAnalyze(); setAnalyzeResult(`${res.proposals_created} proposals`); await load() }
    catch (e) { setError(e.message) } finally { setAnalyzing(false) }
  }

  if (loading && proposals.length === 0) return <p className="text-gray-500">{t(lang, 'loading')}</p>

  return (
    <div>
      <div className="flex items-center gap-4 mb-4">
        <h2 className="text-xl font-bold">
          {t(lang, 'learningProposals')} <span className="text-gray-500 text-sm font-normal ml-2">{proposals.length} {t(lang, 'pending')}</span>
        </h2>
        <button onClick={handleAnalyze} disabled={analyzing}
          className="ml-auto px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:bg-gray-700 text-white text-sm rounded font-medium transition-colors">
          {analyzing ? t(lang, 'analyzing') : t(lang, 'analyzeNow')}
        </button>
      </div>
      {analyzeResult && <p className="text-green-400 text-sm mb-3">{analyzeResult}</p>}
      {error && <p className="text-red-400 text-sm mb-3">{error}</p>}
      {proposals.length === 0 && <p className="text-gray-500">{t(lang, 'noProposals')}</p>}
      <div className="space-y-3">
        {proposals.map((p) => (
          <div key={p.id} className="bg-gray-900 border border-gray-800 rounded-lg p-4">
            <div className="flex items-center gap-2 mb-2">
              <span className="text-yellow-400">&#128161;</span>
              <span className="font-medium">{p.proposed_name}</span>
              <span className="ml-auto text-gray-500 text-xs">
                {t(lang, 'confidence')}: {Math.round(p.confidence * 100)}% | {p.sample_size} {t(lang, 'samples')} | {p.time_window}
              </span>
            </div>
            <p className="text-gray-300 text-sm mb-2">{p.pattern_summary}</p>
            <div className="bg-gray-950 border border-gray-800 rounded p-2 mb-3">
              <code className="text-xs text-green-400 font-mono">{p.proposed_expression}</code>
              <span className="text-gray-500 text-xs ml-2">→ {p.proposed_effect}</span>
            </div>
            <div className="flex gap-2">
              <button onClick={() => handleAccept(p.id)}
                className="px-3 py-1.5 bg-green-600 hover:bg-green-500 text-white text-sm rounded font-medium transition-colors">
                {t(lang, 'accept')}
              </button>
              <button onClick={() => handleDismiss(p.id)}
                className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 text-sm rounded font-medium transition-colors">
                {t(lang, 'dismiss')}
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
