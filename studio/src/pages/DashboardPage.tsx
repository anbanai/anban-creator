import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { toast } from 'react-hot-toast'
import { useAuth } from '@/contexts/AuthContext'
import { api } from '@/lib/api'
import { taskStatusLabel, contentTypeLabel, formatDateTimeCN } from '@/lib/labels'
import { Card, CardBody } from '@/components/ui/Card'
import Badge from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'

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
  const queryClient = useQueryClient()

  const { data: creditsBalance } = useQuery({
    queryKey: ['credits', 'balance'],
    queryFn: () => api.credits.balance(),
  })

  const { data: signInStatus } = useQuery({
    queryKey: ['credits', 'signInStatus'],
    queryFn: () => api.credits.signInStatus(),
  })

  const signInMutation = useMutation({
    mutationFn: () => api.credits.signIn(),
    onSuccess: () => {
      toast.success('签到成功，积分 +1024')
      queryClient.invalidateQueries({ queryKey: ['credits'] })
    },
    onError: () => {
      toast.error('签到失败，请重试')
    },
  })

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
          <h1 className="text-2xl font-bold text-foreground">
            欢迎{user?.nickname ? `，${user.nickname}` : ''}
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">以下是你的内容工作区概览。</p>
        </div>
        <Link
          to="/tasks?create=true"
          className="inline-flex items-center gap-2 rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
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
              <p className="text-sm text-muted-foreground">{stat.label}</p>
              <p className="mt-1 text-3xl font-bold text-foreground">{stat.value}</p>
              <p className="mt-1 text-xs text-muted-foreground">{stat.description}</p>
            </CardBody>
          </Card>
        ))}
      </div>

      {/* Credits card */}
      <Card>
        <CardBody>
          <div className="flex items-center justify-between">
            <div>
              <p className="text-sm text-muted-foreground">积分余额</p>
              <p className="mt-1 text-3xl font-bold text-foreground">
                {(creditsBalance?.balance ?? 0).toLocaleString()}
              </p>
            </div>
            <Button
              onClick={() => signInMutation.mutate()}
              disabled={signInStatus?.signed_in_today ?? false || signInMutation.isPending}
              loading={signInMutation.isPending}
            >
              {signInStatus?.signed_in_today ? '已签到' : '签到 +1024'}
            </Button>
          </div>
        </CardBody>
      </Card>

      {/* Recent tasks */}
      <Card>
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <h2 className="font-semibold text-foreground">最近任务</h2>
          <Link to="/tasks" className="text-sm text-primary hover:text-primary/80">
            查看全部
          </Link>
        </div>
        <div className="divide-y divide-border">
          {recentTasks.length === 0 ? (
            <div className="py-8 text-center text-sm text-muted-foreground">
              还没有任务。创建你的第一个任务开始创作。
            </div>
          ) : (
            recentTasks.map((task) => (
              <Link
                key={task.id}
                to={`/tasks/${task.id}`}
                className="flex items-center justify-between px-4 py-3 transition-colors hover:bg-accent"
              >
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-foreground">{task.topic}</p>
                  <div className="mt-1 flex items-center gap-2">
                    <span className="text-xs text-muted-foreground">
                      {formatDateTimeCN(task.created_at)}
                    </span>
                    <span className="text-xs text-muted-foreground">|</span>
                    <span className="text-xs text-muted-foreground">{contentTypeLabel[task.type]}</span>
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
        <h2 className="mb-3 text-lg font-semibold text-foreground">快捷操作</h2>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <Link
            to="/tasks?create=true"
            className="rounded-xl border border-border bg-card p-4 transition-colors hover:border-foreground/20 hover:bg-accent"
          >
            <p className="font-medium text-foreground">新建任务</p>
            <p className="mt-1 text-sm text-muted-foreground">创建内容任务</p>
          </Link>
          <Link
            to="/plans"
            className="rounded-xl border border-border bg-card p-4 transition-colors hover:border-foreground/20 hover:bg-accent"
          >
            <p className="font-medium text-foreground">新建计划</p>
            <p className="mt-1 text-sm text-muted-foreground">规划新的内容排期</p>
          </Link>
          <Link
            to="/timeline"
            className="rounded-xl border border-border bg-card p-4 transition-colors hover:border-foreground/20 hover:bg-accent"
          >
            <p className="font-medium text-foreground">查看时间线</p>
            <p className="mt-1 text-sm text-muted-foreground">查看已排期内容</p>
          </Link>
        </div>
      </div>
    </div>
  )
}
