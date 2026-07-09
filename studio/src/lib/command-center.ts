import type { CreditBalance, Plan, Project, SignInStatus, Task, TaskType } from '@/types'

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
  modelConfigKnown: boolean
  apiKeysKnown: boolean
  publishingReady: boolean
  checks: {
    projects: CommandCenterReadinessCheck
    apiKeys: CommandCenterReadinessCheck
    modelConfig: CommandCenterReadinessCheck
    localExecutor: CommandCenterReadinessCheck
    publishing: CommandCenterReadinessCheck
  }
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
  localExecutorReady?: boolean | null
  modelConfigReady?: boolean | null
  apiKeysReady?: boolean | null
}

export interface ModelConfigLike {
  text?: {
    endpoint?: string
    api_key?: string
    model?: string
  } | null
  image?: {
    provider?: string
    endpoint?: string
    api_key?: string
    model?: string
  } | null
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
const taskTypes = new Set<TaskType>(['seednote', 'article', 'moments', 'viral_analysis', 'ecommerce', 'videocreator', 'videoeditor', 'montage'])
const creationIntents = new Set(['new', 'retry', 'schedule'])

export function hasUsableModelConfig(config?: ModelConfigLike | null) {
  return Boolean(
    config?.text?.model ||
    config?.text?.endpoint ||
    config?.text?.api_key ||
    config?.image?.provider ||
    config?.image?.model ||
    config?.image?.endpoint ||
    config?.image?.api_key,
  )
}

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

  const publishableProjects = projects.filter((project) => project.config.enable_publishing)
  const approvalProjects = publishableProjects.filter((project) => project.config.require_publish_approval)
  const projectStatus: ReadinessStatus = projects.length > 0 ? 'ready' : 'not_ready'
  const publishingStatus: ReadinessStatus =
    projects.length === 0 ? 'unknown' : publishableProjects.length > 0 ? 'ready' : 'not_ready'
  const apiKeysStatus = readinessStatus(input.apiKeysReady, input.apiKeysKnown)
  const modelConfigStatus = readinessStatus(input.modelConfigReady, input.modelConfigKnown)
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
    modelConfig: {
      status: modelConfigStatus,
      label: '模型配置',
      description:
        modelConfigStatus === 'ready'
          ? '模型策略已配置'
          : modelConfigStatus === 'not_ready'
            ? '需要配置文本或图像模型'
            : '正在检查模型配置',
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
          ? approvalProjects.length > 0
            ? `${publishableProjects.length} 个项目可发布，${approvalProjects.length} 个发布需审核`
            : `${publishableProjects.length} 个项目可发布到草稿箱`
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
    pendingApprovalTasks,
    recentCompletedTasks,
    upcomingPlans,
    creditRisk,
    readiness: {
      projectsReady: checks.projects.status === 'ready',
      localExecutorKnown: checks.localExecutor.status !== 'unknown',
      modelConfigKnown: checks.modelConfig.status === 'ready',
      apiKeysKnown: checks.apiKeys.status === 'ready',
      publishingReady: checks.publishing.status === 'ready',
      checks,
    },
  }
}

export function buildNextBestActions(signals: CommandCenterSignals): NextBestAction[] {
  const actions: NextBestAction[] = []
  const failedTask = signals.failedTasks[0]
  const approvalTask = signals.pendingApprovalTasks[0]
  const upcomingPlan = signals.upcomingPlans[0]
  const defaultProject = signals.projects[0]
  const setupNeedsAttention =
    signals.readiness.checks.apiKeys.status !== 'ready' ||
    signals.readiness.checks.modelConfig.status !== 'ready'

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

  if (setupNeedsAttention && signals.readiness.projectsReady) {
    actions.push({
      id: 'connect-settings',
      label: '检查接入设置',
      description: '确认平台密钥和模型配置后再创建任务',
      href: '/settings',
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

  return actions.slice(0, 5)
}
