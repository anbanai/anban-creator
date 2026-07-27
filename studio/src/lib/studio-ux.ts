import type { BillingCatalog, Project, ProjectStats, Task, TaskType } from '@/types'
import { projectsReturnHref } from '@/lib/command-center'
import { platformDefaultRatio } from '@/lib/labels'
import { taskCostFor } from '@/lib/pricing'
import { workflowReadinessLabel } from '@/lib/workflow-readiness'

export interface DashboardBlocker {
  id: 'no-project' | 'api-key' | 'model-config'
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
  modelConfigReady,
}: {
  projectsLoading: boolean
  projectsError: boolean
  activeProjectCount: number
  apiKeysReady?: boolean | null
  modelConfigReady?: boolean | null
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
      actionHref: '/settings#model-key-settings',
      blocking: true,
    }
  }
  if (modelConfigReady === false) {
    return {
      id: 'model-config',
      message: '模型配置未就绪，先选择可用模型。',
      actionLabel: '去设置',
      actionHref: '/settings#model-key-settings',
      blocking: true,
    }
  }
  return null
}

export interface ProjectCreationDefaults {
  type: TaskType
  imageRatio: string
  imageModelKey: string
  selectedModules: Record<string, number>
  targetPlatform: string
}

export function getProjectCreationDefaults(project?: Project | null): ProjectCreationDefaults {
  const type = (project?.platform || 'seednote') as TaskType
  return {
    type,
    imageRatio: project?.image_ratio || platformDefaultRatio[type] || '',
    imageModelKey: project?.ecommerce_defaults?.image_model_key || '',
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
}: {
  catalog?: BillingCatalog
  type: string
  quantity: number
  balance: number
}): CreationCostPreview {
  const isEcommerce = type === 'ecommerce'
  const sku = catalog?.skus.find((item) => item.charge_policy === 'task_admission' && item.operation === `task.${type}`)
  const resolvedPrice = taskCostFor(catalog, type)
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

export interface ProjectReadinessSummary {
  tone: 'ready' | 'attention' | 'archived'
  headline: string
  details: string[]
}

export function buildProjectReadinessSummary(project: Project, stats?: ProjectStats): ProjectReadinessSummary {
  if (project.status === 'archived') {
    return { tone: 'archived', headline: '项目已归档', details: ['恢复后可继续创建任务'] }
  }

  const details: string[] = []
  if (project.platform === 'article') {
    details.push(
      project.config.enable_publishing
        ? project.config.require_publish_approval
          ? '发布需审核'
          : '自动入草稿箱'
        : '发布未启用',
    )
    details.push(project.writer || project.theme || project.author ? '写作已配置' : '补写作配置')
  }
  if (project.platform === 'montage') {
    const pipeline = project.montage_defaults?.default_pipeline
    const duration = project.montage_defaults?.preferences?.duration_seconds
    details.push(pipeline ? `Pipeline ${pipeline}` : '使用系统默认 Pipeline')
    details.push(duration ? `默认 ${duration} 秒` : '使用系统默认时长')
  } else {
    details.push(project.visual_style ? '视觉已配置' : '补视觉配置')
  }
  if (project.platform === 'ecommerce') {
    details.push(project.ecommerce_defaults?.target_platform ? `投放 ${project.ecommerce_defaults.target_platform}` : '补投放平台')
  }
  if (stats && stats.total_tasks > 0) {
    details.push(`成功率 ${(stats.success_rate * 100).toFixed(0)}%`)
  }

  const needsAttention = details.some((item) => item.startsWith('补') || item.includes('未启用'))
  return {
    tone: needsAttention ? 'attention' : 'ready',
    headline: needsAttention ? '还有配置可补齐' : '创作配置已就绪',
    details,
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
  if (task.publish_approval_state === 'pending') {
    return { label: '处理发布审批', hint: '审核后放行到公众号草稿箱', tone: 'publishing' }
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
      hint: task.published ? '已标记发布' : '可下载、发布或复用',
      tone: 'success',
    }
  }
  return { label: '查看详情', hint: '进入任务详情', tone: 'neutral' }
}

export interface SettingsReadinessListItem {
  id: 'execution' | 'model-key' | 'publishing' | 'account-security'
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
      id: 'model-key',
      title: '模型与密钥',
      status: apiKeyCount > 0 ? `${apiKeyCount} 个平台密钥` : '需要创建密钥',
      impact: '影响 Agent 接入、模型调用和生成能力。',
      actionLabel: apiKeyCount > 0 ? '检查模型' : '创建密钥',
      href: '#model-key-settings',
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
