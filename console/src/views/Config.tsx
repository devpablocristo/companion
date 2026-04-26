import { useState, useEffect } from 'react'
import { fetchConfig, updateConfigSection, resetConfig } from '../api'
import { t } from '../i18n'

const SECTIONS = ['risk', 'approvals', 'learning', 'ai', 'general']

const SECTION_ICONS = {
  risk: '🎯', approvals: '✅', learning: '🧠', ai: '🤖', general: '⚙️',
}

export default function Config({ lang }) {
  const [config, setConfig] = useState(null)
  const [loading, setLoading] = useState(true)
  const [open, setOpen] = useState(null)
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState(null)

  const load = () => {
    setLoading(true)
    fetchConfig()
      .then((c) => setConfig(c))
      .catch((err) => setMsg({ type: 'error', text: err.message }))
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const saveSection = (section) => {
    setSaving(true)
    setMsg(null)
    updateConfigSection(section, config[section])
      .then((c) => { setConfig(c); setMsg({ type: 'ok', text: t(lang, 'configSaved') }) })
      .catch((err) => setMsg({ type: 'error', text: err.message }))
      .finally(() => setSaving(false))
  }

  const doReset = () => {
    setSaving(true)
    setMsg(null)
    resetConfig()
      .then((c) => { setConfig(c); setMsg({ type: 'ok', text: t(lang, 'configReset') }) })
      .catch((err) => setMsg({ type: 'error', text: err.message }))
      .finally(() => setSaving(false))
  }

  if (loading) return <p className="text-gray-400">{t(lang, 'loading')}</p>
  if (!config) return <p className="text-red-400">{msg?.text}</p>

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h2 className="text-xl font-bold text-white">{t(lang, 'configTitle')}</h2>
        <button onClick={doReset} className="px-3 py-1.5 bg-gray-700 text-gray-300 rounded text-sm hover:bg-gray-600">
          {t(lang, 'resetDefaults')}
        </button>
      </div>

      {msg && (
        <p className={`text-sm mb-4 ${msg.type === 'ok' ? 'text-green-400' : 'text-red-400'}`}>{msg.text}</p>
      )}

      <div className="space-y-3">
        {SECTIONS.map((s) => (
          <div key={s} className="border border-gray-800 rounded-lg overflow-hidden">
            <button
              onClick={() => setOpen(open === s ? null : s)}
              className={`w-full flex items-center gap-3 px-5 py-4 text-left transition-colors ${
                open === s ? 'bg-gray-800/60' : 'hover:bg-gray-800/30'
              }`}
            >
              <span className="text-xl">{SECTION_ICONS[s]}</span>
              <div className="flex-1">
                <span className="text-white font-medium">{t(lang, 'section_' + s)}</span>
                <p className="text-gray-500 text-xs mt-0.5">{t(lang, 'section_' + s + '_desc')}</p>
              </div>
              <span className="text-gray-600">{open === s ? '▲' : '▼'}</span>
            </button>

            {open === s && (
              <div className="border-t border-gray-800 bg-gray-900/50 px-5 py-4">
                {s === 'risk' && <RiskSection config={config} setConfig={setConfig} lang={lang} />}
                {s === 'approvals' && <ApprovalsSection config={config} setConfig={setConfig} lang={lang} />}
                {s === 'learning' && <LearningSection config={config} setConfig={setConfig} lang={lang} />}
                {s === 'ai' && <AISection config={config} setConfig={setConfig} lang={lang} />}
                {s === 'general' && <GeneralSection config={config} setConfig={setConfig} lang={lang} />}

                <div className="flex justify-end mt-4 pt-3 border-t border-gray-800">
                  <button onClick={() => saveSection(s)} disabled={saving}
                    className="px-4 py-1.5 bg-blue-600 text-white rounded text-sm hover:bg-blue-500 disabled:opacity-50">
                    {saving ? t(lang, 'saving') : t(lang, 'save')}
                  </button>
                </div>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

// --- Secciones ---

function RiskSection({ config, setConfig, lang }) {
  const r = config.risk
  const set = (field, val) => setConfig({ ...config, risk: { ...r, [field]: val } })

  return (
    <div className="space-y-4">
      <Sub title={t(lang, 'decisionThresholds')}>
        <Grid>
          <Num label={t(lang, 'thresholdAllow')} value={r.thresholds.allow} help={t(lang, 'thresholdAllowHelp')}
            onChange={(v) => set('thresholds', { ...r.thresholds, allow: v })} />
          <Num label={t(lang, 'thresholdEnhancedLog')} value={r.thresholds.enhanced_log} help={t(lang, 'thresholdEnhancedLogHelp')}
            onChange={(v) => set('thresholds', { ...r.thresholds, enhanced_log: v })} />
          <Num label={t(lang, 'thresholdRequireApproval')} value={r.thresholds.require_approval} help={t(lang, 'thresholdRequireApprovalHelp')}
            onChange={(v) => set('thresholds', { ...r.thresholds, require_approval: v })} />
          <Num label={t(lang, 'thresholdDeny')} value={r.thresholds.deny} help={t(lang, 'thresholdDenyHelp')}
            onChange={(v) => set('thresholds', { ...r.thresholds, deny: v })} />
          <Num label={t(lang, 'maxAmplification')} value={r.thresholds.max_amplification} help={t(lang, 'maxAmplificationHelp')}
            onChange={(v) => set('thresholds', { ...r.thresholds, max_amplification: v })} />
        </Grid>
      </Sub>

      <Sub title={t(lang, 'actionTypeRisk')}>
        <Tags label={t(lang, 'highRiskActions')} tags={r.action_types.high} color="red"
          onChange={(v) => set('action_types', { ...r.action_types, high: v })} />
        <Tags label={t(lang, 'mediumRiskActions')} tags={r.action_types.medium} color="yellow"
          onChange={(v) => set('action_types', { ...r.action_types, medium: v })} />
      </Sub>

      <Sub title={t(lang, 'businessHoursTitle')}>
        <Grid>
          <Num label={t(lang, 'businessStart')} value={r.business_hours.start} help={t(lang, 'businessStartHelp')}
            onChange={(v) => set('business_hours', { ...r.business_hours, start: Math.round(v) })} />
          <Num label={t(lang, 'businessEnd')} value={r.business_hours.end} help={t(lang, 'businessEndHelp')}
            onChange={(v) => set('business_hours', { ...r.business_hours, end: Math.round(v) })} />
        </Grid>
      </Sub>

      <Sub title={t(lang, 'frequencyTitle')}>
        <Grid>
          <Num label={t(lang, 'frequencyWarning')} value={r.frequency_thresholds.warning} help={t(lang, 'frequencyWarningHelp')}
            onChange={(v) => set('frequency_thresholds', { ...r.frequency_thresholds, warning: Math.round(v) })} />
          <Num label={t(lang, 'frequencyCritical')} value={r.frequency_thresholds.critical} help={t(lang, 'frequencyCriticalHelp')}
            onChange={(v) => set('frequency_thresholds', { ...r.frequency_thresholds, critical: Math.round(v) })} />
        </Grid>
      </Sub>

      <Sub title={t(lang, 'sensitiveSystemsTitle')}>
        <Tags label={t(lang, 'sensitiveSystems')} tags={r.sensitive_systems} color="purple"
          onChange={(v) => set('sensitive_systems', v)} />
      </Sub>

      <Sub title={t(lang, 'amplificationsTitle')}>
        {(r.amplifications || []).map((amp, i) => (
          <div key={i} className="flex items-center gap-3 bg-gray-800/50 rounded p-3 mb-2">
            <div className="flex-1">
              <div className="flex flex-wrap gap-1 mb-1">
                {amp.factors.map((f) => (
                  <span key={f} className="px-2 py-0.5 bg-gray-700 text-gray-300 rounded text-xs">{f}</span>
                ))}
              </div>
              <span className="text-gray-500 text-xs">{amp.reason}</span>
            </div>
            <div className="flex items-center gap-1">
              <span className="text-gray-400 text-xs">×</span>
              <input type="number" step="0.1" min="1" max="5" value={amp.multiplier}
                onChange={(e) => {
                  const amps = [...r.amplifications]
                  amps[i] = { ...amps[i], multiplier: parseFloat(e.target.value) || 1 }
                  set('amplifications', amps)
                }}
                className="w-16 bg-gray-800 border border-gray-700 rounded px-2 py-1 text-white text-sm text-center" />
            </div>
          </div>
        ))}
      </Sub>
    </div>
  )
}

function ApprovalsSection({ config, setConfig, lang }) {
  const a = config.approvals
  const set = (field, val) => setConfig({ ...config, approvals: { ...a, [field]: val } })

  return (
    <Grid>
      <Num label={t(lang, 'approvalTTL')} value={a.default_ttl_seconds}
        help={t(lang, 'approvalTTLHelp')}
        onChange={(v) => set('default_ttl_seconds', Math.round(v))} />
    </Grid>
  )
}

function LearningSection({ config, setConfig, lang }) {
  const l = config.learning
  const set = (field, val) => setConfig({ ...config, learning: { ...l, [field]: val } })

  return (
    <Grid>
      <Num label={t(lang, 'learningMinSamples')} value={l.min_samples}
        help={t(lang, 'learningMinSamplesHelp')}
        onChange={(v) => set('min_samples', Math.round(v))} />
      <Num label={t(lang, 'learningMinRate')} value={l.min_approval_rate}
        help={t(lang, 'learningMinRateHelp')}
        onChange={(v) => set('min_approval_rate', v)} />
      <Num label={t(lang, 'learningMaxRequests')} value={l.max_requests}
        help={t(lang, 'learningMaxRequestsHelp')}
        onChange={(v) => set('max_requests', Math.round(v))} />
    </Grid>
  )
}

function AISection({ config, setConfig, lang }) {
  const ai = config.ai
  const set = (field, val) => setConfig({ ...config, ai: { ...ai, [field]: val } })

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3">
        <label className="text-sm text-gray-300">{t(lang, 'aiEnabled')}</label>
        <button onClick={() => set('enabled', !ai.enabled)}
          className={`w-10 h-5 rounded-full transition-colors relative ${ai.enabled ? 'bg-blue-600' : 'bg-gray-700'}`}>
          <span className={`absolute top-0.5 w-4 h-4 rounded-full bg-white transition-transform ${ai.enabled ? 'left-5' : 'left-0.5'}`} />
        </button>
      </div>
      <Grid>
        <div>
          <label className="block text-sm text-gray-300 mb-1">{t(lang, 'aiModel')}</label>
          <input type="text" value={ai.model} onChange={(e) => set('model', e.target.value)}
            className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-white text-sm" />
          <p className="text-xs text-gray-600 mt-1">{t(lang, 'aiModelHelp')}</p>
        </div>
        <Num label={t(lang, 'aiTimeout')} value={ai.timeout_seconds}
          help={t(lang, 'aiTimeoutHelp')}
          onChange={(v) => set('timeout_seconds', Math.round(v))} />
      </Grid>
    </div>
  )
}

function GeneralSection({ config, setConfig, lang }) {
  const g = config.general
  const set = (field, val) => setConfig({ ...config, general: { ...g, [field]: val } })

  return (
    <Grid>
      <Num label={t(lang, 'defaultListLimit')} value={g.default_list_limit}
        help={t(lang, 'defaultListLimitHelp')}
        onChange={(v) => set('default_list_limit', Math.round(v))} />
      <Num label={t(lang, 'maxListLimit')} value={g.max_list_limit}
        help={t(lang, 'maxListLimitHelp')}
        onChange={(v) => set('max_list_limit', Math.round(v))} />
      <Num label={t(lang, 'maxExpressionLength')} value={g.max_expression_length}
        help={t(lang, 'maxExpressionLengthHelp')}
        onChange={(v) => set('max_expression_length', Math.round(v))} />
      <Num label={t(lang, 'maxIdempotencyKeyLength')} value={g.max_idempotency_key_length}
        help={t(lang, 'maxIdempotencyKeyLengthHelp')}
        onChange={(v) => set('max_idempotency_key_length', Math.round(v))} />
      <Num label={t(lang, 'idempotencyCacheTTL')} value={g.idempotency_cache_ttl_seconds}
        help={t(lang, 'idempotencyCacheTTLHelp')}
        onChange={(v) => set('idempotency_cache_ttl_seconds', Math.round(v))} />
      <Num label={t(lang, 'maxBodySize')} value={g.max_body_size_bytes}
        help={t(lang, 'maxBodySizeHelp')}
        onChange={(v) => set('max_body_size_bytes', Math.round(v))} />
    </Grid>
  )
}

// --- Componentes reutilizables ---

function Sub({ title, children }) {
  return (
    <div>
      <h4 className="text-xs font-semibold text-gray-500 uppercase mb-2">{title}</h4>
      {children}
    </div>
  )
}

function Grid({ children }) {
  return <div className="grid grid-cols-2 gap-3">{children}</div>
}

function Num({ label, value, onChange, help }) {
  return (
    <div>
      <label className="block text-sm text-gray-300 mb-1">{label}</label>
      <input type="number" step="any" value={value}
        onChange={(e) => onChange(parseFloat(e.target.value) || 0)}
        className="w-full bg-gray-800 border border-gray-700 rounded px-3 py-1.5 text-white text-sm" />
      {help && <p className="text-xs text-gray-600 mt-1">{help}</p>}
    </div>
  )
}

function Tags({ label, tags, onChange, color }) {
  const [input, setInput] = useState('')
  const colors = {
    red: 'bg-red-900/50 text-red-300 border-red-800',
    yellow: 'bg-yellow-900/50 text-yellow-300 border-yellow-800',
    purple: 'bg-purple-900/50 text-purple-300 border-purple-800',
  }
  const add = () => {
    const val = input.trim()
    if (val && !tags.includes(val)) onChange([...tags, val])
    setInput('')
  }
  const remove = (tag) => onChange(tags.filter((t) => t !== tag))

  return (
    <div className="mb-3">
      <label className="block text-sm text-gray-300 mb-1">{label}</label>
      <div className="flex flex-wrap gap-1 mb-2">
        {tags.map((tag) => (
          <span key={tag} className={`inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs border ${colors[color] || 'bg-gray-800 text-gray-300 border-gray-700'}`}>
            {tag}
            <button onClick={() => remove(tag)} className="hover:text-white">×</button>
          </span>
        ))}
      </div>
      <div className="flex gap-2">
        <input type="text" value={input} onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && (e.preventDefault(), add())}
          placeholder="..." className="bg-gray-800 border border-gray-700 rounded px-3 py-1 text-white text-sm flex-1" />
        <button onClick={add} className="px-3 py-1 bg-gray-700 text-gray-300 rounded text-sm hover:bg-gray-600">+</button>
      </div>
    </div>
  )
}
