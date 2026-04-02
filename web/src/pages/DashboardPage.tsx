import { useAuth } from '@/contexts/AuthContext'

export default function DashboardPage() {
  const { user } = useAuth()

  const stats = [
    { label: 'Total Tasks', value: '--', description: 'Content tasks' },
    { label: 'Plans', value: '--', description: 'Content plans' },
    { label: 'Published', value: '--', description: 'Published articles' },
    { label: 'Scheduled', value: '--', description: 'Upcoming content' },
  ]

  const actions = [
    { label: 'New Task', description: 'Create a content task', href: '/tasks' },
    { label: 'New Plan', description: 'Plan new content', href: '/plans' },
    { label: 'View Timeline', description: 'See scheduled content', href: '/timeline' },
  ]

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-100">
          Welcome{user?.nickname ? `, ${user.nickname}` : ''}
        </h1>
        <p className="mt-1 text-sm text-gray-400">Here is an overview of your content workspace.</p>
      </div>

      {/* Stats cards */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {stats.map((stat) => (
          <div
            key={stat.label}
            className="rounded-xl border border-gray-700 bg-gray-800 p-4"
          >
            <p className="text-sm text-gray-400">{stat.label}</p>
            <p className="mt-1 text-3xl font-bold text-gray-100">{stat.value}</p>
            <p className="mt-1 text-xs text-gray-500">{stat.description}</p>
          </div>
        ))}
      </div>

      {/* Quick actions */}
      <div>
        <h2 className="mb-3 text-lg font-semibold text-gray-100">Quick Actions</h2>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          {actions.map((action) => (
            <a
              key={action.label}
              href={action.href}
              className="rounded-xl border border-gray-700 bg-gray-800 p-4 transition-colors hover:border-gray-600 hover:bg-gray-750"
            >
              <p className="font-medium text-gray-100">{action.label}</p>
              <p className="mt-1 text-sm text-gray-400">{action.description}</p>
            </a>
          ))}
        </div>
      </div>
    </div>
  )
}
