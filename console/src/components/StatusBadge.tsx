const colors = {
  allowed: 'text-green-400',
  approved: 'text-green-400',
  executed: 'text-green-400',
  denied: 'text-red-400',
  rejected: 'text-red-400',
  execution_failed: 'text-red-400',
  failed: 'text-red-400',
  pending: 'text-yellow-400',
  pending_approval: 'text-yellow-400',
  expired: 'text-gray-500',
  cancelled: 'text-gray-500',
}

export default function StatusBadge({ status }) {
  return (
    <span className={`text-xs font-medium ${colors[status] || 'text-gray-400'}`}>
      {status}
    </span>
  )
}
