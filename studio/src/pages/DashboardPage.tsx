import { useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useSubmitLock } from '@/hooks/useSubmitLock'
import { Link, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import {
  AlertTriangle,
  ArrowRight,
  CalendarClock,
  CheckCircle2,
  Coins,
  Copy,
  Inbox,
  PlayCircle,
  Plus,
  Send,
  Settings,
  type LucideIcon,
} from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import QueryErrorState from '@/components/QueryErrorState'
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
import { renderPlatformIcon } from '@/lib/PlatformIcon'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/common/button'
import PageHeader from '@/components/layout/PageHeader'
import StatsCardSkeleton from '@/components/StatsCardSkeleton'
import EmptyState from '@/components/EmptyState'
import { getLocalExecutorStatus, isDesktop } from '@/lib/tauri'
import { buildCommandCenterSignals, buildNextBestActions, createTaskHref, hasUsableModelConfig, projectsReturnHref, type CommandCenterSignalKind, type ReadinessStatus } from '@/lib/command-center'
import { cn } from '@/lib/utils'

const CHART_COLORS = [
  'var(--chart-1)',
  'var(--chart-2)',
  'var(--chart-3)',
  'var(--chart-4)',
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

  const { data: pricing } = useQuery({
    queryKey: ['credits', 'pricing'],
    queryFn: () => api.credits.pricing(),
  })

  const dailySignInCredits = pricing?.income.daily_sign_in ?? 1024

  const signInMutation = useMutation({
    mutationFn: () => api.credits.signIn(),
    onSuccess: () => {
      toast.success(`签到成功，积分 +${dailySignInCredits}`)
      queryClient.invalidateQueries({ queryKey: queryKeys.credits.all })
      queryClient.invalidateQueries({ queryKey: queryKeys.tasks.all })
      queryClient.invalidateQueries({ queryKey: queryKeys.plans.all })
    },
    onError: () => {
      toast.error('签到失败，请重试')
    },
  })

  const { data: plansData, isLoading: plansLoading, isError: plansError, refetch: refetchPlans } = useQuery({
    queryKey: ['plans', 'dashboard'],
    queryFn: () => api.plans.list({ limit: 100 }),
  })

  const { data: tasksData, isLoading: tasksLoading, isError: tasksError, refetch: refetchTasks } = useQuery({
    queryKey: ['tasks', 'dashboard'],
    queryFn: () => api.tasks.list({ limit: 100 }),
  })

  const { data: projects = [], isLoading: projectsLoading, isError: projectsError, refetch: refetchProjects } = useQuery({
    queryKey: ['projects', 'dashboard', 'active'],
    queryFn: () => api.projects.list({ status: 'active' }),
    staleTime: 60_000,
  })
  const desktopMode = isDesktop()
  const { data: apiKeys = [], isLoading: apiKeysLoading } = useQuery({
    queryKey: queryKeys.apiKeys.all,
    queryFn: async () => {
      const data = await api.apiKeys.list()
      return data.items || []
    },
  })
  const { data: modelConfig, isLoading: modelConfigLoading } = useQuery({
    queryKey: queryKeys.modelConfig.all,
    queryFn: () => api.modelConfig.get(),
  })
  const { data: localExecutorStatus, isLoading: localExecutorLoading } = useQuery({
    queryKey: ['dashboard', 'local-executor-status'],
    queryFn: getLocalExecutorStatus,
    enabled: desktopMode,
    staleTime: 30_000,
  })

  const plans = plansData?.items ?? []
  const tasks = tasksData?.items ?? []

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

  const commandSignals = useMemo(() => buildCommandCenterSignals({
    tasks,
    plans,
    projects,
    creditsBalance,
    signInStatus,
    apiKeysReady: apiKeysLoading ? null : apiKeys.length > 0,
    modelConfigReady: modelConfigLoading ? null : hasUsableModelConfig(modelConfig),
    localExecutorReady: desktopMode
      ? localExecutorLoading
        ? null
        : Boolean(localExecutorStatus?.available)
      : true,
  }), [
    tasks,
    plans,
    projects,
    creditsBalance,
    signInStatus,
    apiKeysLoading,
    apiKeys.length,
    modelConfigLoading,
    modelConfig,
    desktopMode,
    localExecutorLoading,
    localExecutorStatus,
  ])
  const nextBestActions = useMemo(() => buildNextBestActions(commandSignals), [commandSignals])
  const defaultCreateHref = commandSignals.readiness.projectsReady
    ? createTaskHref({
      type: commandSignals.projects[0]?.platform,
      projectId: commandSignals.projects[0]?.id,
      intent: 'new',
    })
    : projectsReturnHref({ type: 'seednote', intent: 'new' })

  const isLoading = plansLoading || tasksLoading || projectsLoading
  const hasError = plansError || tasksError || projectsError

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="今日指挥中心"
        description={`${user?.nickname ? `${user.nickname}，` : ''}这里集中显示今天最值得处理的创作动作。`}
      >
        <Link to={defaultCreateHref}>
          <Button>
            <Plus />
            创建任务
          </Button>
        </Link>
      </PageHeader>

      <section className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(320px,0.65fr)]">
        <Card>
          <CardHeader>
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <CardTitle>今日创作态势</CardTitle>
                <p className="mt-1 text-sm text-muted-foreground">
                  运行、异常、审批、排期和积分风险集中在这里，不用再翻多个列表。
                </p>
              </div>
              <Badge variant={commandSignals.creditRisk.level === 'ok' ? 'secondary' : 'destructive'}>
                积分 {commandSignals.creditRisk.balance?.toLocaleString() ?? '待检查'}
              </Badge>
            </div>
          </CardHeader>
          <CardContent>
            {hasError ? (
              <QueryErrorState onRetry={() => { refetchPlans(); refetchTasks(); refetchProjects() }} />
            ) : isLoading ? (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5">
                {Array.from({ length: 5 }).map((_, i) => <StatsCardSkeleton key={i} />)}
              </div>
            ) : (
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5">
                <SignalTile
                  icon={PlayCircle}
                  label="运行中"
                  value={commandSignals.runningTasks.length}
                  description="正在推进"
                  kind="running"
                />
                <SignalTile
                  icon={AlertTriangle}
                  label="失败待恢复"
                  value={commandSignals.failedTasks.length}
                  description="优先修复"
                  kind={commandSignals.failedTasks.length > 0 ? 'risk' : 'neutral'}
                />
                <SignalTile
                  icon={Send}
                  label="待发布确认"
                  value={commandSignals.pendingApprovalTasks.length}
                  description="人工把关"
                  kind="publishing"
                />
                <SignalTile
                  icon={CalendarClock}
                  label="即将触发"
                  value={commandSignals.upcomingPlans.length}
                  description="7 天内计划"
                  kind="waiting"
                />
                <SignalTile
                  icon={Coins}
                  label="积分风险"
                  value={commandSignals.creditRisk.level === 'ok' ? '正常' : '注意'}
                  description={commandSignals.creditRisk.balance?.toLocaleString() ?? '待检查'}
                  kind={commandSignals.creditRisk.level === 'ok' ? 'success' : 'risk'}
                />
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>下一步建议</CardTitle>
            <p className="text-sm text-muted-foreground">按恢复、审批、补积分、创建的顺序排序。</p>
          </CardHeader>
          <CardContent>
            <div className="flex flex-col gap-2">
              {nextBestActions.length === 0 ? (
                <div className="rounded-lg border border-border bg-muted/40 p-4">
                  <p className="text-sm font-medium text-foreground">今天没有阻塞项</p>
                  <p className="mt-1 text-sm text-muted-foreground">可以继续创建新任务或查看经营洞察。</p>
                </div>
              ) : (
                nextBestActions.map((action) => (
                  <Link
                    key={action.id}
                    to={action.href}
                    className="group flex items-start justify-between gap-3 rounded-lg border border-border bg-background p-3 transition-colors hover:border-primary/30 hover:bg-accent"
                  >
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <ActionDot kind={action.kind} />
                        <p className="truncate text-sm font-medium text-foreground">{action.label}</p>
                      </div>
                      <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{action.description}</p>
                    </div>
                    <ArrowRight className="mt-0.5 size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-primary" />
                  </Link>
                ))
              )}
            </div>
          </CardContent>
        </Card>
      </section>

      <section className="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,1fr)_360px]">
        <Card>
          <CardHeader>
            <CardTitle>接入就绪</CardTitle>
            <p className="text-sm text-muted-foreground">把创建、执行、发布链路拆成可检查状态。</p>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5">
              {[
                commandSignals.readiness.checks.projects,
                commandSignals.readiness.checks.apiKeys,
                commandSignals.readiness.checks.modelConfig,
                commandSignals.readiness.checks.localExecutor,
                commandSignals.readiness.checks.publishing,
              ].map((item) => (
                <ReadinessItem
                  key={item.label}
                  status={item.status}
                  label={item.label}
                  description={item.description}
                  href={item.href}
                />
              ))}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>积分与邀请</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center justify-between gap-4">
              <div>
                <p className="text-sm text-muted-foreground">积分余额</p>
                <p className="mt-1 text-3xl font-bold text-foreground">
                  {(creditsBalance?.balance ?? 0).toLocaleString()}
                </p>
              </div>
              <Button
                size="sm"
                onClick={() => { void submit(async () => signInMutation.mutateAsync()).catch(() => {}) }}
                disabled={(signInStatus?.signed_in_today ?? false) || signInMutation.isPending}
                loading={signInMutation.isPending}
              >
                {signInStatus?.signed_in_today ? '已签到' : `签到 +${dailySignInCredits}`}
              </Button>
            </div>
            <div className="mt-4 rounded-lg border border-border bg-muted/30 p-3">
              <div className="flex items-center justify-between gap-3">
                <div className="min-w-0">
                  <p className="text-sm text-muted-foreground">我的邀请码</p>
                  <p className="mt-1 truncate font-mono text-lg font-semibold tracking-widest text-foreground">
                    {user?.invite_code || '暂无'}
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={!user?.invite_code}
                  onClick={() => {
                    const link = `${window.location.origin}/register?invite=${user!.invite_code}`
                    navigator.clipboard.writeText(link)
                    toast.success('邀请链接已复制')
                  }}
                >
                  <Copy />
                  复制
                </Button>
              </div>
              <p className="mt-2 text-xs text-muted-foreground">
                已邀请 {user?.invite_count ?? 0} / {user?.max_invites ?? 3} 人
              </p>
            </div>
          </CardContent>
        </Card>
      </section>

      <section className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>经营洞察：任务趋势</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="mb-3 text-xs text-muted-foreground">基于最近 100 条任务样本生成</p>
            {trendData.length > 0 ? (
              <ResponsiveContainer width="100%" height={220}>
                <LineChart data={trendData}>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-border" />
                  <XAxis dataKey="date" tick={{ fontSize: 12 }} stroke="var(--muted-foreground)" interval="preserveStartEnd" />
                  <YAxis allowDecimals={false} tick={{ fontSize: 12 }} stroke="var(--muted-foreground)" />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: 'var(--background)',
                      color: 'var(--foreground)',
                      border: '1px solid var(--border)',
                      borderRadius: 'var(--radius)',
                      fontSize: '12px',
                    }}
                  />
                  <Line type="monotone" dataKey="count" name="任务数" stroke={CHART_COLORS[0]} strokeWidth={2} dot={false} activeDot={{ r: 4, fill: CHART_COLORS[0] }} />
                </LineChart>
              </ResponsiveContainer>
            ) : (
              <div className="flex h-[220px] items-center justify-center text-sm text-muted-foreground">暂无数据</div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>经营洞察：状态分布</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="mb-3 text-xs text-muted-foreground">完成、失败和取消任务的占比</p>
            {statusData.length > 0 ? (
              <div className="flex items-center justify-center">
                <ResponsiveContainer width="100%" height={220}>
                  <PieChart>
                    <Pie data={statusData} cx="50%" cy="50%" innerRadius={58} outerRadius={86} paddingAngle={4} dataKey="value" nameKey="name">
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
              <div className="flex h-[220px] items-center justify-center text-sm text-muted-foreground">暂无数据</div>
            )}
            {statusData.length > 0 && (
              <div className="mt-2 flex flex-wrap items-center justify-center gap-4">
                {statusData.map((entry, index) => (
                  <div key={entry.name} className="flex items-center gap-1.5 text-xs">
                    <span className="inline-block size-2.5 rounded-full" style={{ backgroundColor: CHART_COLORS[index % CHART_COLORS.length] }} />
                    <span className="text-muted-foreground">{entry.name}</span>
                    <span className="font-medium text-foreground">{entry.value}</span>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </section>

      <Card>
        <CardHeader className="border-b border-border">
          <CardTitle>最近任务流</CardTitle>
          <div className="col-start-2 row-start-1 self-center">
            <Link to="/tasks" className="text-sm text-primary hover:text-primary/80">
              查看全部
            </Link>
          </div>
        </CardHeader>
        <div className="divide-y divide-border">
        {hasError ? (
          <div className="p-4">
            <QueryErrorState onRetry={() => { refetchPlans(); refetchTasks(); refetchProjects() }} />
          </div>
        ) : isLoading ? (
          <div className="divide-y divide-border">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="flex items-center justify-between px-4 py-3">
                <div className="flex flex-1 flex-col gap-1.5">
                  <Skeleton className="h-4 w-2/3" />
                  <Skeleton className="h-3 w-40" />
                </div>
                <Skeleton className="h-5 w-16" />
              </div>
            ))}
          </div>
        ) : recentTasks.length === 0 ? (
          <EmptyState
            icon={Inbox}
            title="还没有任务"
            description="创建你的第一个任务开始创作。"
            action={{
              label: commandSignals.readiness.projectsReady ? '创建任务' : '创建项目',
              onClick: () => navigate(defaultCreateHref),
            }}
          />
        ) : (
          recentTasks.map((task) => (
            <Link
              key={task.id}
              to={`/tasks/${task.id}`}
              className="flex items-center justify-between gap-3 px-4 py-3 transition-colors hover:bg-accent"
            >
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium text-foreground">{task.title || task.prompt || contentTypeLabel[task.type] + ' 任务'}</p>
                <div className="mt-1 flex flex-wrap items-center gap-2">
                  <span className="text-xs text-muted-foreground">{formatDateTimeCN(task.created_at)}</span>
                  <span className="text-xs text-muted-foreground">|</span>
                  <span className="flex items-center gap-1 text-xs text-muted-foreground">
                    {renderPlatformIcon(task.type)}
                    {contentTypeLabel[task.type]}
                  </span>
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
    </div>
  )
}

function SignalTile({
  icon: Icon,
  label,
  value,
  description,
  kind,
}: {
  icon: LucideIcon
  label: string
  value: number | string
  description: string
  kind: CommandCenterSignalKind
}) {
  return (
    <div className={cn(
      'rounded-lg border bg-background p-3',
      kind === 'risk' && 'border-destructive/30 bg-destructive/5',
      kind === 'running' && 'border-primary/30 bg-primary/5',
      kind === 'publishing' && 'border-ring/30 bg-accent/40',
      kind === 'waiting' && 'border-border bg-muted/40',
      kind === 'success' && 'border-border bg-muted/30',
    )}>
      <div className="flex items-center justify-between gap-2">
        <Icon className="size-4 text-muted-foreground" />
        <span className="text-2xl font-semibold tracking-tight text-foreground">{value}</span>
      </div>
      <p className="mt-2 text-sm font-medium text-foreground">{label}</p>
      <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>
    </div>
  )
}

function ActionDot({ kind }: { kind: CommandCenterSignalKind }) {
  return (
    <span className={cn(
      'size-2 rounded-full bg-muted-foreground',
      kind === 'risk' && 'bg-destructive',
      kind === 'running' && 'bg-primary',
      kind === 'publishing' && 'bg-ring',
      kind === 'waiting' && 'bg-muted-foreground',
      kind === 'success' && 'bg-primary',
    )} />
  )
}

function ReadinessItem({
  status,
  label,
  description,
  href,
}: {
  status: ReadinessStatus
  label: string
  description: string
  href: string
}) {
  const ready = status === 'ready'
  const unknown = status === 'unknown'
  return (
    <Link
      to={href}
      className="flex items-start gap-3 rounded-lg border border-border bg-background p-3 transition-colors hover:border-primary/30 hover:bg-accent"
    >
      <span className={cn(
        'mt-0.5 flex size-6 items-center justify-center rounded-full bg-muted text-muted-foreground',
        ready && 'bg-primary/10 text-primary',
        !ready && !unknown && 'bg-destructive/10 text-destructive',
      )}>
        {ready ? <CheckCircle2 className="size-3.5" /> : <Settings className="size-3.5" />}
      </span>
      <span className="min-w-0">
        <span className="block truncate text-sm font-medium text-foreground">{label}</span>
        <span className="mt-0.5 block line-clamp-2 text-xs text-muted-foreground">{description}</span>
      </span>
    </Link>
  )
}
