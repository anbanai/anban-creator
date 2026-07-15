import { Fragment } from 'react'
import { Separator } from '@/components/ui/separator'
import { contentTypeLabel } from '@/lib/labels'
import {
  videoCreativeTypeLabel,
  videoModelDisplayName,
  videoPurposeLabel,
  videoReferenceRoleLabel,
} from '@/lib/video-display'
import { isVideoCreator, isVideoEditor, isVideoPlatform } from '@/lib/video-platforms'
import { cn } from '@/lib/utils'
import type {
  Project,
  Task,
  VideoInput,
  VideoPricingBreakdown,
  VideoReferenceAsset,
  VideoTaskConfig,
} from '@/types'

export interface TaskConfigurationDetailsProps {
  task: Task
  project?: Project
}

interface DetailProps {
  label: string
  value: string
  wide?: boolean
}

interface VideoSnapshotProps {
  task: Task
  input?: VideoInput
  config?: VideoTaskConfig
}

interface SnapshotDetailsProps extends TaskConfigurationDetailsProps {
  hasSnapshot: boolean
}

type VideoSegment = NonNullable<VideoTaskConfig['segments']>[number]
type VideoPricingSegment = NonNullable<VideoPricingBreakdown['segments']>[number]

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

function VideoReferenceList({
  emptyLabel,
  references,
}: {
  emptyLabel: string
  references: VideoReferenceAsset[]
}) {
  if (references.length === 0) {
    return <p className="text-xs text-muted-foreground">{emptyLabel}</p>
  }

  return (
    <div className="flex flex-col border-y border-border">
      {references.map((reference, index) => (
        <Fragment key={`${reference.type}-${reference.url || reference.text}-${index}`}>
          {index > 0 ? <Separator /> : null}
          <div className="flex min-w-0 flex-col gap-1 py-2 text-xs">
            <p className="break-words text-foreground">
              {videoReferenceRoleLabel(reference.reference_role)} · {reference.file_name || reference.text || reference.url || '—'}
            </p>
            {typeof reference.input_duration_seconds === 'number' ? (
              <p className="text-muted-foreground">
                输入时长 {reference.input_duration_seconds}s
              </p>
            ) : null}
            <dl className="grid gap-x-3 gap-y-2 pt-1 sm:grid-cols-2">
              <Detail label="参考角色" value={videoReferenceRoleLabel(reference.reference_role)} />
              <Detail label="参考类型" value={reference.type} />
              {reference.file_name !== undefined ? (
                <Detail label="文件名" value={reference.file_name || '—'} />
              ) : null}
              {reference.url !== undefined ? (
                <Detail label="URL" value={reference.url || '—'} wide />
              ) : null}
              {reference.text !== undefined ? (
                <Detail label="文本" value={reference.text || '—'} wide />
              ) : null}
              {reference.task_file_id !== undefined ? (
                <Detail label="任务文件 ID" value={reference.task_file_id || '—'} />
              ) : null}
              {reference.mime_type !== undefined ? (
                <Detail label="MIME" value={reference.mime_type || '—'} />
              ) : null}
              {typeof reference.file_size === 'number' ? (
                <Detail label="文件大小" value={`${reference.file_size.toLocaleString()} bytes`} />
              ) : null}
              {reference.must_keep !== undefined ? (
                <Detail label="必须保留" value={reference.must_keep.join('、') || '—'} wide />
              ) : null}
              {reference.can_change !== undefined ? (
                <Detail label="允许变化" value={reference.can_change.join('、') || '—'} wide />
              ) : null}
              {reference.must_not_transfer !== undefined ? (
                <Detail
                  label="禁止迁移"
                  value={reference.must_not_transfer.join('、') || '—'}
                  wide
                />
              ) : null}
            </dl>
          </div>
        </Fragment>
      ))}
    </div>
  )
}

function hasResolvedVideoConfig(config?: VideoTaskConfig): boolean {
  return Boolean(config && Object.values(config).some((value) => value !== undefined))
}

function configuredBoolean(value: boolean | undefined): string {
  if (value === undefined) return '—'
  return value ? '启用' : '关闭'
}

function VideoSegmentDetails({ segment }: { segment: VideoSegment }) {
  const model = videoModelDisplayName(segment.model_key || segment.model) || '—'
  const credits = typeof segment.estimated_credits === 'number'
    ? `${segment.estimated_credits.toLocaleString()} 积分`
    : '积分 —'

  return (
    <div className="flex flex-col gap-1 py-2 text-xs">
      <p className="font-medium text-foreground">
        #{segment.index} · {segment.start_second}–{segment.end_second}s · 时长 {segment.duration}s
      </p>
      <p className="break-words text-muted-foreground">
        {model} · {segment.resolution || '—'} · {segment.ratio || '—'} · {credits}
      </p>
      {segment.prompt ? (
        <p className="whitespace-pre-wrap text-foreground">{segment.prompt}</p>
      ) : null}
    </div>
  )
}

function VideoSegmentList({ segments }: { segments: VideoSegment[] }) {
  if (segments.length === 0) return null

  return (
    <div className="flex flex-col gap-2">
      <p className="text-xs text-muted-foreground">分段计划</p>
      <div className="flex flex-col border-y border-border">
        {segments.map((segment, index) => (
          <Fragment key={`${segment.index}-${segment.start_second}-${segment.end_second}`}>
            {index > 0 ? <Separator /> : null}
            <VideoSegmentDetails segment={segment} />
          </Fragment>
        ))}
      </div>
    </div>
  )
}

function VideoPricingSegmentDetails({ segment }: { segment: VideoPricingSegment }) {
  return (
    <p className="py-2 text-xs text-foreground">
      #{segment.index} · {segment.seconds}s · {segment.cny.toLocaleString()} CNY · {segment.credits.toLocaleString()} 积分
    </p>
  )
}

function VideoPricingDetails({ pricing }: { pricing?: VideoPricingBreakdown }) {
  if (!pricing) return null

  return (
    <div className="flex flex-col gap-3">
      <p className="text-xs font-medium text-foreground">计价明细</p>
      <dl className="grid gap-3 sm:grid-cols-2">
        <Detail label="计价金额" value={`${pricing.cny.toLocaleString()} CNY`} />
        <Detail label="每元积分" value={pricing.credits_per_cny.toLocaleString()} />
        <Detail
          label="积分倍率"
          value={typeof pricing.credit_multiplier === 'number'
            ? pricing.credit_multiplier.toLocaleString()
            : '—'}
        />
        <Detail
          label="档位倍率"
          value={typeof pricing.tier_multiplier === 'number'
            ? pricing.tier_multiplier.toLocaleString()
            : '—'}
        />
        <Detail
          label="用户倍率"
          value={typeof pricing.user_multiplier === 'number'
            ? pricing.user_multiplier.toLocaleString()
            : '—'}
        />
        <Detail label="输入视频" value={configuredBoolean(pricing.input_video)} />
        <Detail
          label="计价输入时长"
          value={typeof pricing.input_seconds === 'number' ? `${pricing.input_seconds}s` : '—'}
        />
        <Detail label="计价输出时长" value={`${pricing.output_seconds}s`} />
        <Detail
          label="计价分段数"
          value={typeof pricing.segment_count === 'number'
            ? pricing.segment_count.toLocaleString()
            : '—'}
        />
      </dl>
      {pricing.segments && pricing.segments.length > 0 ? (
        <div className="flex flex-col gap-2">
          <p className="text-xs text-muted-foreground">计价分段</p>
          <div className="flex flex-col border-y border-border">
            {pricing.segments.map((segment, index) => (
              <Fragment key={`${segment.index}-${segment.seconds}-${index}`}>
                {index > 0 ? <Separator /> : null}
                <VideoPricingSegmentDetails segment={segment} />
              </Fragment>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  )
}

function VideoSnapshot({ task, input, config }: VideoSnapshotProps) {
  const references = input?.references ?? []
  const constraints = input?.hard_constraints
  const resolvedReferences = config?.references ?? []
  const pricing = config?.pricing_breakdown
  const resolvedModel = config?.model_key || config?.model || pricing?.model_key
  const resolvedResolution = config?.resolution || pricing?.resolution || '—'
  const resolvedRatio = config?.ratio || pricing?.ratio || '—'
  const resolvedDuration = config?.target_duration_seconds
    ?? config?.duration
    ?? pricing?.output_seconds
  const resolvedSpec = [
    resolvedResolution,
    resolvedRatio,
    typeof resolvedDuration === 'number' ? `${resolvedDuration}s` : '—',
  ].join(' · ')

  return (
    <div className="flex flex-col gap-5">
      <section aria-labelledby="video-user-input" className="flex flex-col gap-3">
        <h3 id="video-user-input" className="text-sm font-semibold">用户输入</h3>
        <div className="flex flex-col gap-1">
          <p className="text-xs text-muted-foreground">创作要求</p>
          <p className="whitespace-pre-wrap text-sm leading-6 text-foreground">
            {input?.brief?.trim() || task.prompt || '—'}
          </p>
        </div>
        <dl className="grid gap-3 sm:grid-cols-3">
          <Detail label="比例硬约束" value={constraints?.ratio || '由 Agent 判断'} />
          <Detail
            label="时长硬约束"
            value={typeof constraints?.duration === 'number'
              ? `${constraints.duration}s`
              : '由 Agent 判断'}
          />
          <Detail
            label="水印硬约束"
            value={typeof constraints?.watermark === 'boolean'
              ? (constraints.watermark ? '加水印' : '不加水印')
              : '由 Agent 判断'}
          />
        </dl>
        <div className="flex flex-col gap-2">
          <p className="text-xs text-muted-foreground">参考素材</p>
          <VideoReferenceList emptyLabel="未提供参考素材" references={references} />
        </div>
      </section>

      {hasResolvedVideoConfig(config) ? (
        <>
          <Separator />
          <section aria-labelledby="video-resolved-config" className="flex flex-col gap-3">
            <h3 id="video-resolved-config" className="text-sm font-semibold">Agent 解析结果</h3>
            <dl className="grid gap-3 sm:grid-cols-2">
              <Detail
                label="视频模型"
                value={videoModelDisplayName(resolvedModel) || '—'}
              />
              <Detail label="规格" value={resolvedSpec} />
              <Detail label="创意类型" value={videoCreativeTypeLabel(config?.creative_type)} />
              <Detail label="商业目标" value={videoPurposeLabel(config?.purpose)} />
              <Detail label="人物 / 主体" value={config?.subject_profile?.trim() || '—'} wide />
              <Detail label="目标受众" value={config?.audience?.trim() || '—'} wide />
              <Detail label="核心信息" value={config?.single_message?.trim() || '—'} wide />
              <Detail
                label="估算积分"
                value={(task.video_estimated_credits ?? config?.estimated_credits ?? 0).toLocaleString()}
              />
              <Detail
                label="积分消耗"
                value={(task.video_credits_charged ?? task.credits_charged ?? 0).toLocaleString()}
              />
            </dl>
            <dl className="grid gap-3 sm:grid-cols-2">
              <Detail label="场景" value={config?.scenario_key || '—'} />
              <Detail label="制作模式" value={config?.production_mode || '—'} />
              <Detail label="目标时长来源" value={config?.target_duration_source || '—'} />
              <Detail
                label="目标时长说明"
                value={config?.target_duration_reason || '—'}
                wide
              />
              <Detail
                label="最短分段"
                value={typeof config?.segment_min_duration_seconds === 'number'
                  ? `${config.segment_min_duration_seconds}s`
                  : '—'}
              />
              <Detail
                label="最长分段"
                value={typeof config?.segment_max_duration_seconds === 'number'
                  ? `${config.segment_max_duration_seconds}s`
                  : '—'}
              />
              <Detail label="水印" value={configuredBoolean(config?.watermark)} />
              <Detail label="预检" value={configuredBoolean(config?.preflight)} />
              <Detail
                label="返修预算"
                value={typeof config?.retake_budget === 'number'
                  ? config.retake_budget.toLocaleString()
                  : '—'}
              />
              <Detail
                label="交付目标"
                value={config?.delivery_targets?.join('、') || '—'}
                wide
              />
            </dl>
            <VideoSegmentList segments={config?.segments ?? []} />
            <VideoPricingDetails pricing={pricing} />
            <div className="flex flex-col gap-2">
              <p className="text-xs text-muted-foreground">执行参考素材</p>
              <VideoReferenceList emptyLabel="未解析参考素材" references={resolvedReferences} />
            </div>
          </section>
        </>
      ) : null}
    </div>
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
  const videoInput = isVideoCreator(task.type)
    ? task.video_creator_input
    : isVideoEditor(task.type)
      ? task.video_editor_input
      : undefined
  const videoConfig = isVideoCreator(task.type)
    ? task.video_creator_config
    : isVideoEditor(task.type)
      ? task.video_editor_config
      : undefined

  return (
    <div className="flex flex-col gap-5">
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
      {isVideoPlatform(task.type) ? (
        <>
          <Separator />
          <VideoSnapshot input={videoInput} config={videoConfig} task={task} />
        </>
      ) : null}
    </div>
  )
}

export default TaskConfigurationDetails
