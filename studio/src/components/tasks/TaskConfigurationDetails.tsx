import { Separator } from '@/components/ui/separator'
import { contentTypeLabel } from '@/lib/labels'
import { cn } from '@/lib/utils'
import type { Project, Task } from '@/types'
import type { AgentClaudeControls, AgentModelMatrix } from '@/types'

export interface TaskConfigurationDetailsProps {
  task: Task
  project?: Project
}

interface DetailProps {
  label: string
  value: string
  wide?: boolean
}

interface SnapshotDetailsProps extends TaskConfigurationDetailsProps {
  hasSnapshot: boolean
}

function Detail({ label, value, wide = false }: DetailProps) {
  return (
    <div className={cn('flex min-w-0 flex-col gap-1', wide && 'sm:col-span-2')}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="break-words text-sm text-foreground">{value}</dd>
    </div>
  )
}

function ArticleSnapshot({ task, project, hasSnapshot }: SnapshotDetailsProps) {
  const snapshot = task.project_snapshot
  const author = hasSnapshot
    ? snapshot?.author || '—'
    : task.overrides?.author || project?.author || '—'
  const writer = hasSnapshot
    ? snapshot?.writer || '默认'
    : task.overrides?.writer || project?.writer || '默认'
  const theme = hasSnapshot
    ? snapshot?.theme || '默认'
    : task.overrides?.theme || project?.theme || '默认'

  return (
    <section aria-labelledby="article-task-snapshot" className="flex flex-col gap-3">
      <h3 id="article-task-snapshot" className="text-sm font-semibold">文章配置</h3>
      <dl className="grid gap-3 sm:grid-cols-2">
        <Detail label="作者" value={author} />
        <Detail label="写作风格" value={writer} />
        <Detail label="排版主题" value={theme} />
      </dl>
    </section>
  )
}

function EcommerceSnapshot({ task, project, hasSnapshot }: SnapshotDetailsProps) {
  const snapshot = task.project_snapshot
  const defaults = hasSnapshot ? snapshot?.ecommerce_defaults : project?.ecommerce_defaults
  const selectedModules = task.ecommerce?.selected_modules || defaults?.default_selected_modules
  const modules = Object.entries(selectedModules || {})
    .map(([key, quantity]) => `${key} x${quantity}`)
    .join('、') || '—'

  return (
    <section aria-labelledby="ecommerce-task-snapshot" className="flex flex-col gap-3">
      <h3 id="ecommerce-task-snapshot" className="text-sm font-semibold">电商配置</h3>
      <dl className="grid gap-3 sm:grid-cols-2">
        <Detail
          label="目标平台"
          value={task.ecommerce?.target_platform || defaults?.target_platform || '—'}
        />
        <Detail label="模块数量" value={modules} />
        <Detail label="品牌简述" value={defaults?.brand_brief || '—'} wide />
      </dl>
    </section>
  )
}

const agentModelRoles: Array<[keyof AgentModelMatrix, string]> = [
  ['default', '默认'], ['opus', 'Opus'], ['fable', 'Fable'], ['sonnet', 'Sonnet'], ['haiku', 'Haiku'],
]

const claudeControlLabels: Array<[keyof AgentClaudeControls, string]> = [
  ['effort_level', '推理强度'],
  ['always_enable_effort', '始终启用推理'],
  ['max_context_tokens', '最大上下文'],
  ['max_output_tokens', '最大输出'],
  ['max_thinking_tokens', '最大思考 Token'],
  ['disable_adaptive_thinking', '关闭自适应思考'],
  ['disable_thinking', '关闭思考'],
  ['auto_compact_window', '自动压缩窗口'],
  ['autocompact_pct_override', '自动压缩阈值'],
  ['disable_1m_context', '关闭 1M 上下文'],
  ['subagent_model', '子 Agent 模型'],
  ['enable_tool_search', '启用工具搜索'],
]

function displayControlValue(value: string | number | boolean): string {
  if (typeof value === 'boolean') return value ? '开启' : '关闭'
  return typeof value === 'number' ? value.toLocaleString() : value
}

function AgentProfileDetails({ task }: { task: Task }) {
  const snapshot = task.agent_profile_snapshot
  if (!snapshot) return null
  const models = Object.values(snapshot.models)
  const uniform = models.every((value) => value === models[0])
  const configuredControls = claudeControlLabels.flatMap(([key, label]) => {
    const value = snapshot.claude[key]
    return value === undefined ? [] : [`${label}：${displayControlValue(value)}`]
  })

  return (
    <section aria-labelledby="task-agent-profile" className="flex flex-col gap-3">
      <h3 id="task-agent-profile" className="text-sm font-semibold">Agent 执行配置</h3>
      <dl className="grid gap-3 sm:grid-cols-2">
        <Detail label="档位" value={snapshot.display_name} />
        <Detail label="Provider" value={snapshot.provider} />
        {uniform ? (
          <Detail label="模型矩阵" value={`全部角色：${models[0]}`} wide />
        ) : agentModelRoles.map(([role, label]) => (
          <Detail key={role} label={`${label}模型`} value={snapshot.models[role]} />
        ))}
        {configuredControls.length ? (
          <Detail label="Claude 参数" value={configuredControls.join('；')} wide />
        ) : null}
      </dl>
    </section>
  )
}

export function TaskConfigurationDetails({ task, project }: TaskConfigurationDetailsProps) {
  const snapshot = task.project_snapshot
  const hasSnapshot = Boolean(snapshot?.platform)
  const projectName = hasSnapshot ? snapshot?.project_name || '—' : project?.name || '—'
  const visualStyle = hasSnapshot
    ? snapshot?.visual_style || '—'
    : task.overrides?.visual_style || project?.visual_style || '—'
  const imageRatio = task.image_ratio
    || (hasSnapshot ? snapshot?.image_ratio || '—' : project?.image_ratio || '—')
  const imageModel = task.image_model_key
    || (hasSnapshot
      ? snapshot?.ecommerce_defaults?.image_model_key || '—'
      : project?.ecommerce_defaults?.image_model_key || '—')
  const platform = hasSnapshot ? snapshot?.platform || task.type : project?.platform || task.type

  return (
    <div className="flex flex-col gap-5">
      {task.agent_profile_snapshot ? (
        <>
          <AgentProfileDetails task={task} />
          <Separator />
        </>
      ) : null}
      <section aria-labelledby="task-project-snapshot" className="flex flex-col gap-3">
        <h3 id="task-project-snapshot" className="text-sm font-semibold">项目快照</h3>
        <dl className="grid gap-x-4 gap-y-3 sm:grid-cols-2">
          <Detail label="项目" value={projectName} />
          <Detail label="内容类型" value={contentTypeLabel[platform] || platform} />
          <Detail label="视觉风格" value={visualStyle} wide />
          <Detail label="图片比例" value={imageRatio} />
          <Detail label="图片模型" value={imageModel} />
        </dl>
      </section>

      {task.type === 'article' ? (
        <>
          <Separator />
          <ArticleSnapshot task={task} project={project} hasSnapshot={hasSnapshot} />
        </>
      ) : null}
      {task.type === 'ecommerce' ? (
        <>
          <Separator />
          <EcommerceSnapshot task={task} project={project} hasSnapshot={hasSnapshot} />
        </>
      ) : null}
    </div>
  )
}

export default TaskConfigurationDetails
