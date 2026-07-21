import { Fragment } from 'react'
import { Separator } from '@/components/ui/separator'
import {
  videoCreativeTypeLabel,
  videoModelDisplayName,
  videoPurposeLabel,
  videoReferenceRoleLabel,
} from '@/lib/video-display'
import { isVideoCreator, isVideoEditor } from '@/lib/video-platforms'
import { cn } from '@/lib/utils'
import type {
  Task,
  VideoReferenceAsset,
  VideoTaskConfig,
} from '@/types'

export interface VideoTaskConfigurationDetailsProps {
  task: Task
}

interface DetailProps {
  label: string
  value: string
  wide?: boolean
}

type VideoSegment = NonNullable<VideoTaskConfig['segments']>[number]

function Detail({ label, value, wide = false }: DetailProps) {
  return (
    <div className={cn('flex min-w-0 flex-col gap-1', wide && 'sm:col-span-2')}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="break-words text-sm text-foreground">{value}</dd>
    </div>
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

  return (
    <div className="flex flex-col gap-1 py-2 text-xs">
      <p className="font-medium text-foreground">
        #{segment.index} · {segment.start_second}–{segment.end_second}s · 时长 {segment.duration}s
      </p>
      <p className="break-words text-muted-foreground">
        {model} · {segment.resolution || '—'} · {segment.ratio || '—'}
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

export function VideoTaskConfigurationDetails({ task }: VideoTaskConfigurationDetailsProps) {
  const input = isVideoCreator(task.type)
    ? task.video_creator_input
    : isVideoEditor(task.type)
      ? task.video_editor_input
      : undefined
  const config = isVideoCreator(task.type)
    ? task.video_creator_config
    : isVideoEditor(task.type)
      ? task.video_editor_config
      : undefined
  const references = input?.references ?? []
  const constraints = input?.hard_constraints
  const resolvedReferences = config?.references ?? []
  const resolvedModel = config?.model_key || config?.model
  const resolvedResolution = config?.resolution || '—'
  const resolvedRatio = config?.ratio || '—'
  const resolvedDuration = config?.target_duration_seconds
    ?? config?.duration
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
                label="任务固定价"
                value={`${task.billing_price_credits.toLocaleString()} 积分`}
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

export default VideoTaskConfigurationDetails
