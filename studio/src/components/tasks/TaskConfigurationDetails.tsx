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
import type { Project, Task, VideoInput, VideoReferenceAsset, VideoTaskConfig } from '@/types'

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

function Detail({ label, value, wide = false }: DetailProps) {
  return (
    <div className={cn('flex min-w-0 flex-col gap-1', wide && 'sm:col-span-2')}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="break-words text-sm text-foreground">{value}</dd>
    </div>
  )
}

function ArticleSnapshot({ task, project }: TaskConfigurationDetailsProps) {
  const snapshot = task.project_snapshot

  return (
    <section aria-labelledby="article-task-snapshot" className="flex flex-col gap-3">
      <h3 id="article-task-snapshot" className="text-sm font-semibold">文章配置</h3>
      <dl className="grid gap-3 sm:grid-cols-2">
        <Detail label="作者" value={snapshot?.author || project?.author || '—'} />
        <Detail label="写作风格" value={snapshot?.writer || project?.writer || '默认'} />
        <Detail label="排版主题" value={snapshot?.theme || project?.theme || '默认'} />
      </dl>
    </section>
  )
}

function EcommerceSnapshot({ task, project }: TaskConfigurationDetailsProps) {
  const defaults = task.project_snapshot?.ecommerce_defaults || project?.ecommerce_defaults
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
            {reference.input_duration_seconds ? (
              <p className="text-muted-foreground">输入时长 {reference.input_duration_seconds}s</p>
            ) : null}
          </div>
        </Fragment>
      ))}
    </div>
  )
}

function hasResolvedVideoConfig(config?: VideoTaskConfig): boolean {
  return Boolean(config && (
    config.model_key
    || config.model
    || config.resolution
    || config.ratio
    || config.duration
    || config.target_duration_seconds
    || config.estimated_credits
    || config.pricing_breakdown
    || config.segments?.length
    || config.creative_type
    || config.purpose
    || config.subject_profile
    || config.audience
    || config.single_message
    || config.references?.length
  ))
}

function VideoSnapshot({ task, input, config }: VideoSnapshotProps) {
  const references = input?.references ?? []
  const constraints = input?.hard_constraints
  const resolvedReferences = config?.references ?? []
  const resolvedDuration = config?.target_duration_seconds
    || config?.pricing_breakdown?.output_seconds
    || config?.duration
  const resolvedSpec = [
    config?.resolution || '—',
    config?.ratio || '—',
    resolvedDuration ? `${resolvedDuration}s` : '—',
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
            value={constraints?.duration ? `${constraints.duration}s` : '由 Agent 判断'}
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
                value={videoModelDisplayName(config?.model_key || config?.model) || '—'}
              />
              <Detail label="规格" value={resolvedSpec} />
              <Detail label="创意类型" value={videoCreativeTypeLabel(config?.creative_type)} />
              <Detail label="商业目标" value={videoPurposeLabel(config?.purpose)} />
              <Detail label="人物 / 主体" value={config?.subject_profile?.trim() || '—'} wide />
              <Detail label="目标受众" value={config?.audience?.trim() || '—'} wide />
              <Detail label="核心信息" value={config?.single_message?.trim() || '—'} wide />
              <Detail
                label="估算积分"
                value={(task.video_estimated_credits || config?.estimated_credits || 0).toLocaleString()}
              />
              <Detail
                label="积分消耗"
                value={(task.video_credits_charged || task.credits_charged || 0).toLocaleString()}
              />
            </dl>
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
  const visualStyle = snapshot?.visual_style || project?.visual_style || '—'
  const imageRatio = snapshot?.image_ratio || project?.image_ratio || task.image_ratio || '—'
  const imageModel = task.image_model_key
    || snapshot?.ecommerce_defaults?.image_model_key
    || project?.ecommerce_defaults?.image_model_key
    || '—'
  const platform = snapshot?.platform || project?.platform || task.type
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
          <Detail label="项目" value={snapshot?.project_name || project?.name || '—'} />
          <Detail label="内容类型" value={contentTypeLabel[platform] || platform} />
          <Detail label="视觉风格" value={visualStyle} wide />
          <Detail label="图片比例" value={imageRatio} />
          <Detail label="图片模型" value={imageModel} />
        </dl>
      </section>

      {task.type === 'article' ? (
        <>
          <Separator />
          <ArticleSnapshot task={task} project={project} />
        </>
      ) : null}
      {task.type === 'ecommerce' ? (
        <>
          <Separator />
          <EcommerceSnapshot task={task} project={project} />
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
