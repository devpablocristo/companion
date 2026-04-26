const colors = {
  high: 'bg-red-500/20 text-red-400 border-red-500/30',
  medium: 'bg-yellow-500/20 text-yellow-400 border-yellow-500/30',
  low: 'bg-green-500/20 text-green-400 border-green-500/30',
}

export default function RiskBadge({ level }) {
  const cls = colors[level] || colors.low
  return (
    <span className={`px-2 py-0.5 rounded text-xs font-semibold uppercase border ${cls}`}>
      {level}
    </span>
  )
}
