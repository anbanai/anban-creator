import type { AgentExecutionProfileID, BillingCatalog, Project, Task, TaskType } from '@/types'
import { projectsReturnHref } from '@/lib/command-center'
import { taskCostFor } from '@/lib/pricing'
import { workflowReadinessLabel } from '@/lib/workflow-readiness'

export interface DashboardBlocker {
  id: 'no-project' | 'api-key'
  message: string
  actionLabel: string
  actionHref: string
  blocking: boolean
}

export function buildDashboardBlocker({
  projectsLoading,
  projectsError,
  activeProjectCount,
  apiKeysReady,
}: {
  projectsLoading: boolean
  projectsError: boolean
  activeProjectCount: number
  apiKeysReady?: boolean | null
}): DashboardBlocker | null {
  if (projectsLoading || projectsError) return null
  if (activeProjectCount === 0) {
    return {
      id: 'no-project',
      message: '先创建一个项目，再开始创作。',
      actionLabel: '创建项目',
      actionHref: projectsReturnHref({ type: 'seednote', intent: 'new' }),
      blocking: true,
    }
  }
  if (apiKeysReady === false) {
    return {
      id: 'api-key',
      message: '平台密钥未就绪，先补齐接入能力。',
      actionLabel: '去设置',
      actionHref: '/settings#api-key-settings',
      blocking: true,
    }
  }
  return null
}

export interface ProjectCreationDefaults {
  type: TaskType
  imageRatio: string
  imageCapabilityKey: string
  selectedModules: Record<string, number>
  targetPlatform: string
}

export function getProjectCreationDefaults(project?: Project | null): ProjectCreationDefaults {
  const type = (project?.platform || 'seednote') as TaskType
  return {
    type,
    imageRatio: project?.image_ratio || 'auto',
    imageCapabilityKey: project?.ecommerce_defaults?.image_capability_key || '',
    selectedModules: project?.ecommerce_defaults?.default_selected_modules || {},
    targetPlatform: project?.ecommerce_defaults?.target_platform || '',
  }
}

export interface CreationCostPreview {
  priceAvailable: boolean
  baseCost: number
  listBaseCost: number
  discountPerTask: number
  billableQuantity: number
  totalCost: number
  remaining: number
  insufficient: boolean
}

export function taskCreationCostPreview({
  catalog,
  type,
  quantity,
  balance,
  executionProfile,
}: {
  catalog?: BillingCatalog
  type: string
  quantity: number
  balance: number
  executionProfile?: AgentExecutionProfileID
}): CreationCostPreview {
  const isEcommerce = type === 'ecommerce'
  const sku = catalog?.skus.find((item) => item.charge_policy === 'task_admission'
    && item.operation === `task.${type}`
    && (executionProfile === undefined || item.execution_profile === executionProfile))
  const resolvedPrice = taskCostFor(catalog, type, executionProfile)
  const priceAvailable = resolvedPrice !== undefined
  const baseCost = resolvedPrice ?? 0
  const listBaseCost = sku?.list_price_credits ?? baseCost
  const discountPerTask = sku?.discount_credits ?? Math.max(0, listBaseCost - baseCost)
  const billableQuantity = isEcommerce ? 1 : quantity
  const totalCost = baseCost * billableQuantity
  const remaining = balance - totalCost
  return {
    priceAvailable,
    baseCost,
    listBaseCost,
    discountPerTask,
    billableQuantity,
    totalCost,
    remaining,
    insufficient: remaining < 0,
  }
}

export interface TaskActionSignal {
  label: string
  hint: string
  tone: 'risk' | 'publishing' | 'running' | 'success' | 'neutral'
}

export function taskFailureMessage(task: Pick<Task, 'error_message'>): string | null {
  return task.error_message?.trim() || null
}

export function taskActionSignal(task: Task): TaskActionSignal {
  if (task.status === 'failed') {
    const failureMessage = taskFailureMessage(task)
    return failureMessage
      ? { label: '查看失败原因', hint: '进入详情后可继续执行或克隆', tone: 'risk' }
      : { label: '查看任务状态', hint: '未返回失败详情', tone: 'risk' }
  }
  if (task.status === 'running' || task.status === 'pending') {
    return {
      label: task.status === 'running' ? '查看运行进度' : '等待执行',
      hint: `${task.progress ?? 0}%`,
      tone: 'running',
    }
  }
  const readiness = workflowReadinessLabel(task.workflow_status)
  if (task.status === 'completed') {
    return {
      label: readiness || '查看产物',
      hint: '可查看、下载或复用',
      tone: 'success',
    }
  }
  return { label: '查看详情', hint: '进入任务详情', tone: 'neutral' }
}

export interface SettingsReadinessListItem {
  id: 'execution' | 'api-key' | 'publishing' | 'account-security'
  title: string
  status: string
  impact: string
  actionLabel: string
  href: string
  ready: boolean
}

export function buildSettingsReadinessItems({
  isDesktopApp,
  apiKeyCount,
  hasPassword,
}: {
  isDesktopApp: boolean
  apiKeyCount: number
  hasPassword: boolean
}): SettingsReadinessListItem[] {
  return [
    {
      id: 'execution',
      title: '执行环境',
      status: isDesktopApp ? '桌面执行器可检查' : '浏览器模式',
      impact: isDesktopApp ? '影响本地 ffmpeg、任务认领和本机运行。' : '当前任务默认走云端执行。',
      actionLabel: isDesktopApp ? '检查执行器' : '查看执行说明',
      href: '#execution-settings',
      ready: true,
    },
    {
      id: 'api-key',
      title: '平台密钥',
      status: apiKeyCount > 0 ? `${apiKeyCount} 个平台密钥` : '需要创建密钥',
      impact: '影响 Agent 接入与平台能力调用。',
      actionLabel: apiKeyCount > 0 ? '检查密钥' : '创建密钥',
      href: '#api-key-settings',
      ready: apiKeyCount > 0,
    },
    {
      id: 'publishing',
      title: '发布渠道',
      status: '需要检查',
      impact: '影响公众号草稿箱、通知和发布审批。',
      actionLabel: '检查发布',
      href: '#publishing-settings',
      ready: false,
    },
    {
      id: 'account-security',
      title: '账号安全',
      status: hasPassword ? '已设置密码' : '需要设置密码',
      impact: '影响账号资料、配额和登录凭证安全。',
      actionLabel: hasPassword ? '查看账号' : '设置密码',
      href: '#account-security-settings',
      ready: hasPassword,
    },
  ]
}
