import type { CreditBalance, Plan, Project, SignInStatus, Task, TaskType } from '@/types'

export type CommandCenterSignalKind = 'success' | 'risk' | 'running' | 'waiting' | 'publishing' | 'neutral'

export interface CommandCenterReadiness {
  projectsReady: boolean
  localExecutorKnown: boolean
  modelConfigKnown: boolean
  apiKeysKnown: boolean
  publishingReady: boolean
}

export interface CommandCenterCreditRisk {
  level: 'ok' | 'low' | 'critical' | 'unknown'
  balance: number | null
  signedInToday: boolean | null
}

export interface CommandCenterSignals {
  now: Date
  tasks: Task[]
  projects: Project[]
  plans: Plan[]
  runningTasks: Task[]
  failedTasks: Task[]
  pendingApprovalTasks: Task[]
  recentCompletedTasks: Task[]
  upcomingPlans: Plan[]
  creditRisk: CommandCenterCreditRisk
  readiness: CommandCenterReadiness
}

export interface BuildCommandCenterSignalsInput {
  now?: Date
  tasks?: Task[]
  projects?: Project[]
  plans?: Plan[]
  creditsBalance?: CreditBalance | null
  signInStatus?: SignInStatus | null
  localExecutorKnown?: boolean
  modelConfigKnown?: boolean
  apiKeysKnown?: boolean
}

export interface NextBestAction {
  id:
    | 'recover-failed-task'
    | 'review-publish-approval'
    | 'create-first-project'
    | 'claim-daily-credits'
    | 'review-credits'
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
const taskTypes = new Set<TaskType>(['seednote', 'article', 'ecommerce', 'video'])
const creationIntents = new Set(['new', 'retry', 'schedule'])

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
  const safeReturnTo = returnTo?.startsWith('/') ? returnTo : '/tasks'
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
  const balance = input.creditsBalance?.balance ?? null
  const signedInToday = input.signInStatus?.signed_in_today ?? null

  const runningTasks = tasks
    .filter((task) => task.status === 'running' || task.status === 'pending')
    .sort(byNewestCreatedAt)
  const failedTasks = tasks.filter((task) => task.status === 'failed').sort(byNewestCreatedAt)
  const pendingApprovalTasks = tasks
    .filter((task) => task.publish_approval_state === 'pending')
    .sort(byNewestCreatedAt)
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

  const creditRisk: CommandCenterCreditRisk = {
    balance,
    signedInToday,
    level:
      balance === null
        ? 'unknown'
        : balance <= CRITICAL_CREDIT_THRESHOLD
          ? 'critical'
          : balance < LOW_CREDIT_THRESHOLD
            ? 'low'
            : 'ok',
  }

  return {
    now,
    tasks,
    projects,
    plans,
    runningTasks,
    failedTasks,
    pendingApprovalTasks,
    recentCompletedTasks,
    upcomingPlans,
    creditRisk,
    readiness: {
      projectsReady: projects.length > 0,
      localExecutorKnown: input.localExecutorKnown ?? true,
      modelConfigKnown: input.modelConfigKnown ?? true,
      apiKeysKnown: input.apiKeysKnown ?? true,
      publishingReady: projects.some((project) => project.config.enable_publishing),
    },
  }
}

export function buildNextBestActions(signals: CommandCenterSignals): NextBestAction[] {
  const actions: NextBestAction[] = []
  const failedTask = signals.failedTasks[0]
  const approvalTask = signals.pendingApprovalTasks[0]
  const upcomingPlan = signals.upcomingPlans[0]
  const defaultProject = signals.projects[0]

  if (failedTask) {
    actions.push({
      id: 'recover-failed-task',
      label: '恢复失败任务',
      description: failedTask.title || failedTask.prompt || '查看失败原因并重新推进',
      href: `/tasks/${failedTask.id}`,
      kind: 'risk',
    })
  }

  if (approvalTask) {
    actions.push({
      id: 'review-publish-approval',
      label: '处理发布审批',
      description: approvalTask.title || approvalTask.prompt || '确认是否推送到发布渠道',
      href: `/tasks/${approvalTask.id}`,
      kind: 'publishing',
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

  if (signals.creditRisk.level === 'critical' || signals.creditRisk.level === 'low') {
    actions.push({
      id: signals.creditRisk.signedInToday === false ? 'claim-daily-credits' : 'review-credits',
      label: signals.creditRisk.signedInToday === false ? '签到补充积分' : '查看积分风险',
      description:
        signals.creditRisk.balance === null
          ? '检查积分账户是否可用'
          : `当前余额 ${signals.creditRisk.balance.toLocaleString()}，建议先确认消耗能力`,
      href: signals.creditRisk.signedInToday === false ? '/' : '/credits',
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

  if (!signals.readiness.modelConfigKnown || !signals.readiness.apiKeysKnown) {
    actions.push({
      id: 'connect-settings',
      label: '检查接入设置',
      description: '确认模型、密钥和本地执行环境是否准备好',
      href: '/settings',
      kind: 'neutral',
    })
  }

  return actions.slice(0, 5)
}
