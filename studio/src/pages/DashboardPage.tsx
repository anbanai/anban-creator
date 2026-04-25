import { useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { Link, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { Plus, Loader2, Inbox, CalendarPlus, Clock, Copy } from 'lucide-react'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
} from 'recharts'
import { useAuth } from '@/contexts/AuthContext'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { taskStatusLabel, contentTypeLabel, formatDateTimeCN, statusBadgeVariant } from '@/lib/labels'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/Card'
import Badge from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import PageHeader from '@/components/layout/PageHeader'
import StatsCard from '@/components/StatsCard'
import EmptyState from '@/components/EmptyState'

const CHART_COLORS = [
  'hsl(270, 60%, 55%)',
  'hsl(170, 50%, 55%)',
  'hsl(60, 60%, 55%)',
  'hsl(330, 50%, 55%)',
]

export default function DashboardPage() {
  const { user } = useAuth()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { submit } = useSubmitLock()

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
      queryClient.invalidateQueries({ queryKey: queryKeys.credits.all })
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all })
      queryClient.invalidateQueries({ queryKey: queryKeys.plans.all })
    },
    onError: () => {
      toast.error('签到失败，请重试')
    },
  })

  const { data: plansData, isLoading: plansLoading } = useQuery({
    queryKey: ['plans', 'dashboard'],
    queryFn: () => api.plans.list({ limit: 100 }),
  })

  const { data: tasksData, isLoading: tasksLoading } = useQuery({
    queryKey: ['tasks', 'dashboard'],
    queryFn: () => api.tasks.list({ limit: 100 }),
  })

  const plans = plansData?.items ?? []
  const tasks = tasksData?.items ?? []

  const activePlans = useMemo(() => plans.filter((p) => p.status === 'active').length, [plans])

  const todayTasks = useMemo(() => tasks.filter((t) => {
    const created = new Date(t.created_at).toDateString()
    return created === new Date().toDateString()
  }), [tasks])

  const completedToday = useMemo(() => todayTasks.filter((t) => t.status === 'completed').length, [todayTasks])
  const failedToday = useMemo(() => todayTasks.filter((t) => t.status === 'failed').length, [todayTasks])
  const totalToday = todayTasks.length
  const successRate = totalToday > 0
    ? Math.round((completedToday / (completedToday + failedToday || 1)) * 100)
    : 0

  const recentTasks = useMemo(
    () =>
      tasks
        .slice()
        .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
        .slice(0, 5),
    [tasks]
  )

  // Aggregate tasks by date for the last 30 days
  const trendData = useMemo(() => {
    const now = new Date()
    const days: Record<string, number> = {}
    for (let i = 29; i >= 0; i--) {
      const d = new Date(now)
      d.setDate(d.getDate() - i)
      const key = d.toDateString()
      days[key] = 0
    }
    tasks.forEach((t) => {
      const key = new Date(t.created_at).toDateString()
      if (key in days) {
        days[key]++
      }
    })
    return Object.entries(days).map(([dateStr, count]) => {
      const d = new Date(dateStr)
      return {
        date: `${d.getMonth() + 1}/${d.getDate()}`,
        count,
      }
    })
  }, [tasks])

  // Status distribution for pie chart
  const statusData = useMemo(() => {
    const counts: Record<string, number> = { completed: 0, failed: 0, cancelled: 0 }
    tasks.forEach((t) => {
      if (t.status in counts) counts[t.status]++
    })
    return [
      { name: '已完成', value: counts.completed },
      { name: '失败', value: counts.failed },
      { name: '已取消', value: counts.cancelled },
    ].filter((d) => d.value > 0)
  }, [tasks])

  const stats = useMemo(() => [
    { title: '活跃计划', value: activePlans, description: '运行中的调度' },
    { title: '今日任务', value: totalToday, description: `${completedToday} 已完成, ${failedToday} 失败` },
    { title: '成功率', value: `${successRate}%`, description: '仅今日' },
    { title: '总任务数', value: tasks.length, description: '全部时间' },
  ], [activePlans, totalToday, completedToday, failedToday, successRate, tasks.length])

  const isLoading = plansLoading || tasksLoading

  return (
    <div className="space-y-6">
      <PageHeader
        title={`欢迎${user?.nickname ? `，${user.nickname}` : ''}`}
        description="以下是你的内容工作区概览。"
      >
        <Link to="/tasks?create=true">
          <Button>
            <Plus className="mr-2 h-4 w-4" />
            创建任务
          </Button>
        </Link>
      </PageHeader>

      {/* Stats cards */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {stats.map((stat) => (
          <StatsCard
            key={stat.title}
            title={stat.title}
            value={stat.value}
            description={stat.description}
          />
        ))}
      </div>

      {/* Credits card */}
      <Card>
        <CardContent>
          <div className="flex items-center justify-between py-1">
            <div>
              <p className="text-sm text-muted-foreground">积分余额</p>
              <p className="mt-1 text-3xl font-bold text-foreground">
                {(creditsBalance?.balance ?? 0).toLocaleString()}
              </p>
            </div>
            <Button
              size="sm"
              onClick={() => submit(async () => signInMutation.mutateAsync())}
              disabled={(signInStatus?.signed_in_today ?? false) || signInMutation.isPending}
              loading={signInMutation.isPending}
            >
              {signInStatus?.signed_in_today ? '已签到' : '签到 +1024'}
            </Button>
          </div>
          <Link to="/credits" className="mt-2 block text-right text-sm text-muted-foreground hover:text-primary">
            查看明细 &rarr;
          </Link>
        </CardContent>
      </Card>

      {/* Invite card */}
      <Card>
        <CardContent>
          <div className="flex items-center justify-between py-1">
            <div>
              <p className="text-sm text-muted-foreground">我的邀请码</p>
              <p className="mt-1 font-mono text-2xl font-bold tracking-widest text-foreground">
                {user?.invite_code}
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                已邀请 {user?.invite_count ?? 0} / {user?.max_invites ?? 3} 人
              </p>
            </div>
            <Button
              variant="outline"
              disabled={!user?.invite_code}
              onClick={() => {
                const link = `${window.location.origin}/register?invite=${user!.invite_code}`
                navigator.clipboard.writeText(link)
                toast.success('邀请链接已复制')
              }}
            >
              <Copy className="mr-1.5 h-4 w-4" />
              复制邀请链接
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* Charts */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {/* Task trend line chart */}
        <Card>
          <CardHeader>
            <CardTitle>任务趋势（近30天）</CardTitle>
          </CardHeader>
          <CardContent>
            {trendData.length > 0 ? (
              <ResponsiveContainer width="100%" height={240}>
                <LineChart data={trendData}>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                  <XAxis
                    dataKey="date"
                    tick={{ fontSize: 12 }}
                    stroke="var(--muted-foreground)"
                    interval="preserveStartEnd"
                  />
                  <YAxis
                    allowDecimals={false}
                    tick={{ fontSize: 12 }}
                    stroke="var(--muted-foreground)"
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: 'var(--background)',
                      color: 'var(--foreground)',
                      border: '1px solid var(--border)',
                      borderRadius: 'var(--radius)',
                      fontSize: '12px',
                    }}
                  />
                  <Line
                    type="monotone"
                    dataKey="count"
                    name="任务数"
                    stroke={CHART_COLORS[0]}
                    strokeWidth={2}
                    dot={false}
                    activeDot={{ r: 4, fill: CHART_COLORS[0] }}
                  />
                </LineChart>
              </ResponsiveContainer>
            ) : (
              <div className="flex h-[240px] items-center justify-center text-sm text-muted-foreground">
                暂无数据
              </div>
            )}
          </CardContent>
        </Card>

        {/* Status distribution donut chart */}
        <Card>
          <CardHeader>
            <CardTitle>状态分布</CardTitle>
          </CardHeader>
          <CardContent>
            {statusData.length > 0 ? (
              <div className="flex items-center justify-center">
                <ResponsiveContainer width="100%" height={240}>
                  <PieChart>
                    <Pie
                      data={statusData}
                      cx="50%"
                      cy="50%"
                      innerRadius={60}
                      outerRadius={90}
                      paddingAngle={4}
                      dataKey="value"
                      nameKey="name"
                    >
                      {statusData.map((_, index) => (
                        <Cell key={`cell-${index}`} fill={CHART_COLORS[index % CHART_COLORS.length]} />
                      ))}
                    </Pie>
                    <Tooltip
                      contentStyle={{
                        backgroundColor: 'var(--background)',
                        color: 'var(--foreground)',
                        border: '1px solid var(--border)',
                        borderRadius: 'var(--radius)',
                        fontSize: '12px',
                      }}
                    />
                  </PieChart>
                </ResponsiveContainer>
              </div>
            ) : (
              <div className="flex h-[240px] items-center justify-center text-sm text-muted-foreground">
                暂无数据
              </div>
            )}
            {statusData.length > 0 && (
              <div className="mt-2 flex items-center justify-center gap-4">
                {statusData.map((entry, index) => (
                  <div key={entry.name} className="flex items-center gap-1.5 text-xs">
                    <span
                      className="inline-block h-2.5 w-2.5 rounded-full"
                      style={{ backgroundColor: CHART_COLORS[index % CHART_COLORS.length] }}
                    />
                    <span className="text-muted-foreground">{entry.name}</span>
                    <span className="font-medium text-foreground">{entry.value}</span>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Recent tasks */}
      <Card>
        <CardHeader className="border-b border-border">
          <CardTitle>最近任务</CardTitle>
          <div className="col-start-2 row-start-1 self-center">
            <Link to="/tasks" className="text-sm text-primary hover:text-primary/80">
              查看全部
            </Link>
          </div>
        </CardHeader>
        <div className="divide-y divide-border">
          {isLoading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : recentTasks.length === 0 ? (
            <EmptyState
              icon={Inbox}
              title="还没有任务"
              description="创建你的第一个任务开始创作。"
              action={{
                label: '创建任务',
                onClick: () => navigate('/tasks?create=true'),
              }}
            />
          ) : (
            recentTasks.map((task) => (
              <Link
                key={task.id}
                to={`/tasks/${task.id}`}
                className="flex items-center justify-between px-4 py-3 transition-colors hover:bg-accent"
              >
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-foreground">{task.prompt || contentTypeLabel[task.type] + ' 任务'}</p>
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
            className="flex items-start gap-3 rounded-xl border border-border bg-card p-4 transition-all duration-200 hover:border-primary/30 hover:bg-accent hover:shadow-sm active:scale-[0.98]"
          >
            <Plus className="mt-0.5 h-5 w-5 shrink-0 text-primary" />
            <div>
              <p className="font-medium text-foreground">新建任务</p>
              <p className="mt-0.5 text-sm text-muted-foreground">创建内容任务</p>
            </div>
          </Link>
          <Link
            to="/plans"
            className="flex items-start gap-3 rounded-xl border border-border bg-card p-4 transition-all duration-200 hover:border-primary/30 hover:bg-accent hover:shadow-sm active:scale-[0.98]"
          >
            <CalendarPlus className="mt-0.5 h-5 w-5 shrink-0 text-primary" />
            <div>
              <p className="font-medium text-foreground">新建计划</p>
              <p className="mt-0.5 text-sm text-muted-foreground">规划新的内容排期</p>
            </div>
          </Link>
          <Link
            to="/timeline"
            className="flex items-start gap-3 rounded-xl border border-border bg-card p-4 transition-all duration-200 hover:border-primary/30 hover:bg-accent hover:shadow-sm active:scale-[0.98]"
          >
            <Clock className="mt-0.5 h-5 w-5 shrink-0 text-primary" />
            <div>
              <p className="font-medium text-foreground">查看时间轴</p>
              <p className="mt-0.5 text-sm text-muted-foreground">查看已排期内容</p>
            </div>
          </Link>
        </div>
      </div>

    </div>
  )
}
