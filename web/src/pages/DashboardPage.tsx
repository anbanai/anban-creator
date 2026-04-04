import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useAuth } from '@/contexts/AuthContext'
import { api } from '@/lib/api'
import { taskStatusLabel, contentTypeLabel, formatDateTimeCN } from '@/lib/labels'
import { Card, CardBody } from '@/components/ui/Card'
import Badge from '@/components/ui/Badge'

function statusBadgeVariant(status: string) {
  switch (status) {
    case 'completed': return 'success'
    case 'failed': return 'danger'
    case 'running': return 'warning'
    case 'cancelled': return 'neutral'
    default: return 'neutral'
  }
}

export default function DashboardPage() {
  const { user } = useAuth()

  const { data: plansData } = useQuery({
    queryKey: ['plans', 'dashboard'],
    queryFn: () => api.plans.list({ limit: 100 }),
  })

  const { data: tasksData } = useQuery({
    queryKey: ['tasks', 'dashboard'],
    queryFn: () => api.tasks.list({ limit: 100 }),
  })

  const plans = plansData?.items ?? []
  const tasks = tasksData?.items ?? []

  const activePlans = plans.filter((p) => p.status === 'active').length
  const todayTasks = tasks.filter((t) => {
    const created = new Date(t.created_at).toDateString()
    return created === new Date().toDateString()
  })
  const completedToday = todayTasks.filter((t) => t.status === 'completed').length
  const failedToday = todayTasks.filter((t) => t.status === 'failed').length
  const totalToday = todayTasks.length
  const successRate = totalToday > 0
    ? Math.round((completedToday / (completedToday + failedToday || 1)) * 100)
    : 0

  const recentTasks = tasks
    .slice()
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    .slice(0, 5)

  const stats = [
    { label: '活跃计划', value: activePlans, description: '运行中的调度' },
    { label: '今日任务', value: totalToday, description: `${completedToday} 已完成, ${failedToday} 失败` },
    { label: '成功率', value: `${successRate}%`, description: '仅今日' },
    { label: '总任务数', value: tasks.length, description: '全部时间' },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-100">
            欢迎{user?.nickname ? `，${user.nickname}` : ''}
          </h1>
          <p className="mt-1 text-sm text-gray-400">以下是你的内容工作区概览。</p>
        </div>
        <Link
          to="/tasks?create=true"
          className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-700"
        >
          <svg className="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 4v16m8-8H4" />
          </svg>
          创建任务
        </Link>
      </div>

      {/* Stats cards */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {stats.map((stat) => (
          <Card key={stat.label}>
            <CardBody>
              <p className="text-sm text-gray-400">{stat.label}</p>
              <p className="mt-1 text-3xl font-bold text-gray-100">{stat.value}</p>
              <p className="mt-1 text-xs text-gray-500">{stat.description}</p>
            </CardBody>
          </Card>
        ))}
      </div>

      {/* Recent tasks */}
      <Card>
        <div className="flex items-center justify-between border-b border-gray-700 px-4 py-3">
          <h2 className="font-semibold text-gray-100">最近任务</h2>
          <Link to="/tasks" className="text-sm text-blue-400 hover:text-blue-300">
            查看全部
          </Link>
        </div>
        <div className="divide-y divide-gray-700">
          {recentTasks.length === 0 ? (
            <div className="py-8 text-center text-sm text-gray-500">
              还没有任务。创建你的第一个任务开始创作。
            </div>
          ) : (
            recentTasks.map((task) => (
              <Link
                key={task.id}
                to={`/tasks/${task.id}`}
                className="flex items-center justify-between px-4 py-3 transition-colors hover:bg-gray-750"
              >
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-gray-100">{task.topic}</p>
                  <div className="mt-1 flex items-center gap-2">
                    <span className="text-xs text-gray-500">
                      {formatDateTimeCN(task.created_at)}
                    </span>
                    <span className="text-xs text-gray-600">|</span>
                    <span className="text-xs text-gray-500">{contentTypeLabel[task.type]}</span>
                  </div>
                </div>
                <Badge variant={statusBadgeVariant(task.status)}>
                  {taskStatusLabel[task.status]}
                </Badge>
              </Link>
            ))
          )}
        </div>
      </Card>

      {/* Quick actions */}
      <div>
        <h2 className="mb-3 text-lg font-semibold text-gray-100">快捷操作</h2>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <Link
            to="/tasks?create=true"
            className="rounded-xl border border-gray-700 bg-gray-800 p-4 transition-colors hover:border-gray-600 hover:bg-gray-750"
          >
            <p className="font-medium text-gray-100">新建任务</p>
            <p className="mt-1 text-sm text-gray-400">创建内容任务</p>
          </Link>
          <Link
            to="/plans"
            className="rounded-xl border border-gray-700 bg-gray-800 p-4 transition-colors hover:border-gray-600 hover:bg-gray-750"
          >
            <p className="font-medium text-gray-100">新建计划</p>
            <p className="mt-1 text-sm text-gray-400">规划新的内容排期</p>
          </Link>
          <Link
            to="/timeline"
            className="rounded-xl border border-gray-700 bg-gray-800 p-4 transition-colors hover:border-gray-600 hover:bg-gray-750"
          >
            <p className="font-medium text-gray-100">查看时间线</p>
            <p className="mt-1 text-sm text-gray-400">查看已排期内容</p>
          </Link>
        </div>
      </div>
    </div>
  )
}
