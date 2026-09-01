import type { BillingWallet, Plan, Project, Task, TaskType } from '@/types'

export type CommandCenterSignalKind = 'success' | 'risk' | 'running' | 'waiting' | 'publishing' | 'neutral'

export type ReadinessStatus = 'ready' | 'not_ready' | 'unknown'

export interface CommandCenterReadinessCheck {
  status: ReadinessStatus
  label: string
  description: string
  href: string
}

export interface CommandCenterReadiness {
  projectsReady: boolean
  localExecutorKnown: boolean
  apiKeysKnown: boolean
  publishingReady: boolean
  checks: {
    projects: CommandCenterReadinessCheck
    apiKeys: CommandCenterReadinessCheck
    localExecutor: CommandCenterReadinessCheck
    publishing: CommandCenterReadinessCheck
  }
}

export interface CommandCenterWalletRisk {
  level: 'ok' | 'low' | 'critical' | 'unknown'
  balance: number | null
  debt: number | null
}

export interface CommandCenterSignals {
  now: Date
  tasks: Task[]
  projects: Project[]
  plans: Plan[]
  runningTasks: Task[]
  failedTasks: Task[]
  recentCompletedTasks: Task[]
  upcomingPlans: Plan[]
  walletRisk: CommandCenterWalletRisk
  readiness: CommandCenterReadiness
}

export interface BuildCommandCenterSignalsInput {
  now?: Date
  tasks?: Task[]
  projects?: Project[]
  plans?: Plan[]
  billingWallet?: BillingWallet | null
  localExecutorKnown?: boolean
  apiKeysKnown?: boolean
  localExecutorReady?: boolean | null
  apiKeysReady?: boolean | null
}

export interface NextBestAction {
  id:
    | 'recover-failed-task'
    | 'create-first-project'
    | 'review-wallet'
    | 'review-upcoming-plan'
    | 'create-task'
    | 'connect-settings'
  label: string
  description: string
  href: string
  kind: CommandCenterSignalKind
}

const UPCOMING_WINDOW_MS = 7 * 24 * 60 * 60 * 1000
const LOW_CREDIT_THRESHOLD = 200
const CRITICAL_CREDIT_THRESHOLD = 50
const taskTypes = new Set<TaskType>(['seednote', 'article', 'moments', 'viral_analysis', 'ecommerce', 'montage'])
const creationIntents = new Set(['new', 'retry', 'schedule'])

function readinessStatus(ready?: boolean | null, legacyKnown?: boolean): ReadinessStatus {
  if (ready === true) return 'ready'
  if (ready === false) return 'not_ready'
  if (legacyKnown === true) return 'ready'
  if (legacyKnown === false) return 'unknown'
  return 'unknown'
}

export interface CreationIntent {
  shouldCreate: boolean
  type?: TaskType
  projectId?: string
  intent?: 'new' | 'retry' | 'schedule'
}

function byNewestCreatedAt(a: Task, b: Task) {
  return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
}

function byNextRunAt(a: Plan, b: Plan) {
  return new Date(a.next_run_at).getTime() - new Date(b.next_run_at).getTime()
}

export function createTaskHref({
  type,
  projectId,
  intent,
}: {
  type?: TaskType
  projectId?: string
  intent?: 'new' | 'retry' | 'schedule'
} = {}) {
  const params = new URLSearchParams()
  params.set('create', 'true')
  if (type) params.set('type', type)
  if (projectId) params.set('project_id', projectId)
  if (intent) params.set('intent', intent)
  return `/tasks?${params.toString()}`
}

export function projectsReturnHref({
  type = 'seednote',
  intent,
}: {
  type?: TaskType
  intent?: 'new' | 'retry' | 'schedule'
} = {}) {
  const params = new URLSearchParams()
  params.set('return_to', '/tasks')
  params.set('create', 'true')
  params.set('type', type)
  if (intent) params.set('intent', intent)
  return `/projects?${params.toString()}`
}

function safeTasksReturnHref(returnTo?: string | null) {
  return returnTo === '/tasks' || returnTo?.startsWith('/tasks?') ? returnTo : '/tasks'
}

export function projectCreatedReturnHref({
  returnTo,
  type,
  projectId,
  intent,
}: {
  returnTo?: string | null
  type?: TaskType
  projectId: string
  intent?: 'new' | 'retry' | 'schedule'
}) {
  const safeReturnTo = safeTasksReturnHref(returnTo)
  const [path, query = ''] = safeReturnTo.split('?')
  const params = new URLSearchParams(query)
  params.set('create', 'true')
  if (type) params.set('type', type)
  params.set('project_id', projectId)
  if (intent) params.set('intent', intent)
  return `${path}?${params.toString()}`
}

export function parseCreationIntent(params: URLSearchParams): CreationIntent {
  const rawType = params.get('type') || undefined
  const rawIntent = params.get('intent') || undefined
  const projectId = params.get('project_id') || undefined

  return {
    shouldCreate: params.get('create') === 'true',
    type: rawType && taskTypes.has(rawType as TaskType) ? (rawType as TaskType) : undefined,
    projectId,
    intent: rawIntent && creationIntents.has(rawIntent) ? (rawIntent as CreationIntent['intent']) : undefined,
  }
}

export function buildCommandCenterSignals(input: BuildCommandCenterSignalsInput): CommandCenterSignals {
  const now = input.now ?? new Date()
  const tasks = input.tasks ?? []
  const projects = (input.projects ?? []).filter((project) => project.status !== 'archived')
  const plans = input.plans ?? []
  const balance = input.billingWallet?.balance ?? null
  const debt = input.billingWallet?.debt ?? null

  const runningTasks = tasks
    .filter((task) => task.status === 'running' || task.status === 'pending')
    .sort(byNewestCreatedAt)
  const failedTasks = tasks.filter((task) => task.status === 'failed').sort(byNewestCreatedAt)
  const recentCompletedTasks = tasks
    .filter((task) => task.status === 'completed')
    .sort(byNewestCreatedAt)
    .slice(0, 5)

  const upcomingPlans = plans
    .filter((plan) => {
      if (plan.status !== 'active' || !plan.next_run_at) return false
      const nextRun = new Date(plan.next_run_at).getTime()
      const diff = nextRun - now.getTime()
      return diff >= 0 && diff <= UPCOMING_WINDOW_MS
    })
    .sort(byNextRunAt)

  const walletRisk: CommandCenterWalletRisk = {
    balance,
    debt,
    level:
      balance === null || debt === null
        ? 'unknown'
        : debt > 0 || balance <= CRITICAL_CREDIT_THRESHOLD
          ? 'critical'
          : balance < LOW_CREDIT_THRESHOLD
            ? 'low'
            : 'ok',
  }

  const publishableProjects = projects.filter(
    (project) => project.platform === 'article' && (project.config.wechat_publish_mode ?? 'manual') !== 'disabled',
  )
  const projectStatus: ReadinessStatus = projects.length > 0 ? 'ready' : 'not_ready'
  const publishingStatus: ReadinessStatus =
    projects.length === 0 ? 'unknown' : publishableProjects.length > 0 ? 'ready' : 'not_ready'
  const apiKeysStatus = readinessStatus(input.apiKeysReady, input.apiKeysKnown)
  const localExecutorStatus = readinessStatus(input.localExecutorReady, input.localExecutorKnown)
  const checks: CommandCenterReadiness['checks'] = {
    projects: {
      status: projectStatus,
      label: '项目定位',
      description: projects.length > 0 ? `${projects.length} 个项目可用` : '先创建账号/项目',
      href: projects.length > 0 ? '/projects' : projectsReturnHref({ type: 'seednote', intent: 'new' }),
    },
    apiKeys: {
      status: apiKeysStatus,
      label: '平台密钥',
      description:
        apiKeysStatus === 'ready'
          ? '密钥可用于 Agent 接入'
          : apiKeysStatus === 'not_ready'
            ? '需要创建平台密钥'
            : '正在检查密钥状态',
      href: '/settings',
    },
    localExecutor: {
      status: localExecutorStatus,
      label: '执行环境',
      description:
        localExecutorStatus === 'ready'
          ? '执行环境可用'
          : localExecutorStatus === 'not_ready'
            ? '本地执行器未就绪，可走云端'
            : '正在检查桌面执行状态',
      href: '/settings',
    },
    publishing: {
      status: publishingStatus,
      label: '发布能力',
      description:
        publishingStatus === 'ready'
          ? `${publishableProjects.length} 个项目已配置公众号发布流程`
          : publishingStatus === 'not_ready'
            ? '需要检查发布配置'
            : '创建项目后检查发布配置',
      href: '/settings',
    },
  }

  return {
    now,
    tasks,
    projects,
    plans,
    runningTasks,
    failedTasks,
    recentCompletedTasks,
    upcomingPlans,
    walletRisk,
    readiness: {
      projectsReady: checks.projects.status === 'ready',
      localExecutorKnown: checks.localExecutor.status !== 'unknown',
      apiKeysKnown: checks.apiKeys.status === 'ready',
      publishingReady: checks.publishing.status === 'ready',
      checks,
    },
  }
}

export function buildNextBestActions(signals: CommandCenterSignals): NextBestAction[] {
  const actions: NextBestAction[] = []
  const failedTask = signals.failedTasks[0]
  const upcomingPlan = signals.upcomingPlans[0]
  const defaultProject = signals.projects[0]
  const setupNeedsAttention = signals.readiness.checks.apiKeys.status !== 'ready'

  if (failedTask) {
    actions.push({
      id: 'recover-failed-task',
      label: '恢复失败任务',
      description: failedTask.title || failedTask.prompt || '查看失败原因并重新推进',
      href: `/tasks/${failedTask.id}`,
      kind: 'risk',
    })
  }

  if (!signals.readiness.projectsReady) {
    actions.push({
      id: 'create-first-project',
      label: '创建第一个项目',
      description: '先建立账号定位，再让任务自动继承风格和配置',
      href: projectsReturnHref({ type: 'seednote' }),
      kind: 'neutral',
    })
  }

  if (setupNeedsAttention && signals.readiness.projectsReady) {
    actions.push({
      id: 'connect-settings',
      label: '检查接入设置',
      description: '确认平台密钥和接入状态后再创建任务',
      href: '/settings',
      kind: 'neutral',
    })
  }

  if (signals.walletRisk.level === 'critical' || signals.walletRisk.level === 'low') {
    actions.push({
      id: 'review-wallet',
      label: signals.walletRisk.debt && signals.walletRisk.debt > 0 ? '补齐钱包欠费' : '查看钱包余额',
      description:
        signals.walletRisk.debt && signals.walletRisk.debt > 0
          ? `当前欠费 ${signals.walletRisk.debt.toLocaleString()}，新任务暂不可创建`
          : signals.walletRisk.balance === null
            ? '检查钱包是否可用'
            : `当前余额 ${signals.walletRisk.balance.toLocaleString()}，建议充值后再创建任务`,
      href: '/billing',
      kind: 'risk',
    })
  }

  if (upcomingPlan) {
    actions.push({
      id: 'review-upcoming-plan',
      label: '检查下一次排期',
      description: upcomingPlan.title || upcomingPlan.prompt || '确认即将触发的自动计划',
      href: `/plans?highlight=${upcomingPlan.id}`,
      kind: 'waiting',
    })
  }

  if (signals.readiness.projectsReady) {
    actions.push({
      id: 'create-task',
      label: signals.tasks.length === 0 ? '创建首个任务' : '新建创作任务',
      description: defaultProject ? `使用「${defaultProject.name}」继续产出` : '开始新的内容任务',
      href: createTaskHref({ type: defaultProject?.platform, projectId: defaultProject?.id, intent: 'new' }),
      kind: 'running',
    })
  }

  return actions.slice(0, 5)
}
