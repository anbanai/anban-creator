import { lazy, Suspense, useMemo, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { Plus, Loader2, Inbox, CalendarPlus, Clock, CreditCard, Copy } from 'lucide-react'
import { useAuth } from '@/contexts/AuthContext'
import { api } from '@/lib/api'
import { queryKeys } from '@/lib/query-keys'
import { taskStatusLabel, contentTypeLabel, formatDateTimeCN, statusBadgeVariant } from '@/lib/labels'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/Card'
import Badge from '@/components/ui/Badge'
import { Button } from '@/components/ui/Button'
import PageHeader from '@/components/layout/PageHeader'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import StatsCard from '@/components/StatsCard'
import EmptyState from '@/components/EmptyState'

const ChartsSection = lazy(() => import('./dashboard/ChartsSection'))

export default function DashboardPage() {
  const { user } = useAuth()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [rechargeOpen, setRechargeOpen] = useState(false)

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
              onClick={() => signInMutation.mutate()}
              disabled={(signInStatus?.signed_in_today ?? false) || signInMutation.isPending}
              loading={signInMutation.isPending}
            >
              {signInStatus?.signed_in_today ? '已签到' : '签到 +1024'}
            </Button>
            <Button variant="outline" onClick={() => setRechargeOpen(true)}>
              <CreditCard className="mr-1.5 h-4 w-4" />
              充值
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* Invite card */}
      {user?.invite_code && (
        <Card>
          <CardContent>
            <div className="flex items-center justify-between py-1">
              <div>
                <p className="text-sm text-muted-foreground">我的邀请码</p>
                <p className="mt-1 font-mono text-2xl font-bold tracking-widest text-foreground">
                  {user.invite_code}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  已邀请 {user.invite_count ?? 0} / {user.max_invites ?? 3} 人
                </p>
              </div>
              <Button
                variant="outline"
                onClick={() => {
                  const link = `${window.location.origin}/register?invite=${user.invite_code}`
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
      )}

      {/* Charts */}
      <Suspense fallback={<div className="grid grid-cols-1 gap-4 lg:grid-cols-2"><div className="flex h-[280px] items-center justify-center"><Loader2 className="h-6 w-6 animate-spin text-muted-foreground" /></div></div>}>
        <ChartsSection trendData={trendData} statusData={statusData} />
      </Suspense>

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

      {/* Recharge dialog */}
      <Dialog open={rechargeOpen} onOpenChange={setRechargeOpen}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>充值积分</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col items-center space-y-4 py-2">
            <img
              src="https://placehold.co/200x200?text=QR"
              alt="企微客服二维码"
              className="rounded-lg border border-border"
              width={200}
              height={200}
            />
            <p className="text-sm text-muted-foreground">扫码联系客服充值</p>
            <div className="w-full space-y-2">
              {[
                { tier: '基础', price: '10', credits: '10,000' },
                { tier: '标准', price: '50', credits: '55,000' },
                { tier: '专业', price: '100', credits: '120,000' },
              ].map(({ tier, price, credits }) => (
                <div key={tier} className="flex items-center justify-between rounded-lg border border-border px-3 py-2 text-sm">
                  <span className="font-medium">{tier}</span>
                  <span>
                    <span className="text-foreground">{price} 元</span>
                    <span className="mx-2 text-muted-foreground">=</span>
                    <span className="text-primary font-semibold">{credits} 积分</span>
                  </span>
                </div>
              ))}
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
