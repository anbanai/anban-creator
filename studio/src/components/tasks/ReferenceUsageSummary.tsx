import { useEffect, useMemo, useState } from 'react'
import {
  AlertCircle,
  CheckCircle2,
  FileImage,
  Images,
  Info,
  ShieldCheck,
  TriangleAlert,
} from 'lucide-react'
import { api } from '@/lib/api'
import type { InputAttachment, ReferenceUsageSummaryData, Task, TaskFile } from '@/types'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

interface ReferenceUsageSummaryProps {
  task: Task
  files: TaskFile[]
  variant?: 'card' | 'compact'
}

const inputStatusMeta: Record<
  ReferenceUsageSummaryData['inputs'][number]['status'],
  { label: string; variant: 'secondary' | 'outline' | 'destructive'; className?: string }
> = {
  used: {
    label: '已使用',
    variant: 'secondary',
    className: 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
  },
  excluded: { label: '未采用', variant: 'outline' },
  analysis_failed: { label: '分析失败', variant: 'destructive' },
}

const verificationMeta: Record<
  ReferenceUsageSummaryData['outputs'][number]['verification']['status'],
  { label: string; variant: 'secondary' | 'outline' | 'destructive'; className?: string }
> = {
  passed: {
    label: '核验通过',
    variant: 'secondary',
    className: 'bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
  },
  warning: {
    label: '核验有提醒',
    variant: 'outline',
    className: 'border-amber-500/30 text-amber-700 dark:text-amber-300',
  },
  failed: { label: '核验失败', variant: 'destructive' },
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function isOptionalString(value: unknown): value is string | undefined {
  return value === undefined || typeof value === 'string'
}

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === 'string')
}

export function isReferenceUsageSummaryData(value: unknown): value is ReferenceUsageSummaryData {
  if (!isRecord(value) || value.version !== '1.0') return false
  if (!Array.isArray(value.inputs) || !Array.isArray(value.outputs)) return false
  if (value.warnings !== undefined && !isStringArray(value.warnings)) return false
  if (!isOptionalString(value.model_fallback_reason)) return false

  const validInputStatuses = new Set(['used', 'excluded', 'analysis_failed'])
  const validVerificationStatuses = new Set(['passed', 'warning', 'failed'])

  const inputsValid = value.inputs.every((input) => {
    if (!isRecord(input)) return false
    return isFiniteNumber(input.attachment_index)
      && isOptionalString(input.file_name)
      && isOptionalString(input.url)
      && isOptionalString(input.instruction)
      && typeof input.status === 'string'
      && validInputStatuses.has(input.status)
      && typeof input.decision_summary === 'string'
      && isFiniteNumber(input.analysis_attempts)
      && (input.warnings === undefined || isStringArray(input.warnings))
  })
  if (!inputsValid) return false

  return value.outputs.every((output) => {
    if (!isRecord(output) || !isRecord(output.verification)) return false
    if (!Array.isArray(output.references)) return false

    const referencesValid = output.references.every((reference) => (
      isRecord(reference)
      && isFiniteNumber(reference.attachment_index)
      && typeof reference.purpose === 'string'
    ))

    return typeof output.file_name === 'string'
      && referencesValid
      && isFiniteNumber(output.generation_attempts)
      && typeof output.verification.status === 'string'
      && validVerificationStatuses.has(output.verification.status)
      && typeof output.verification.summary === 'string'
      && isOptionalString(output.provider)
      && isOptionalString(output.model)
      && isOptionalString(output.selection_reason)
  })
}

function SummaryLoading({ compact }: { compact: boolean }) {
  if (compact) {
    return (
      <div aria-label="正在读取参考素材使用摘要" className="space-y-3">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
      </div>
    )
  }

  return (
    <Card aria-label="正在读取参考素材使用摘要">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Images className="size-4 text-primary" />
          参考素材使用
        </CardTitle>
        <CardDescription>正在读取 Agent 的素材选择与核验结果…</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-3 sm:grid-cols-2">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
      </CardContent>
    </Card>
  )
}

function Warnings({ warnings }: { warnings?: string[] }) {
  if (!warnings?.length) return null
  return (
    <ul className="space-y-1 text-xs text-amber-800 dark:text-amber-200">
      {warnings.map((warning, index) => (
        <li key={`${warning}-${index}`} className="flex items-start gap-1.5">
          <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
          <span className="min-w-0 break-words">{warning}</span>
        </li>
      ))}
    </ul>
  )
}

function SnapshotAttachment({ attachment, index }: { attachment: InputAttachment; index: number }) {
  const displayName = attachment.file_name || attachment.url || attachment.text || `素材 ${index + 1}`
  return (
    <div className="min-w-0 rounded-lg border border-border/70 bg-background/70 p-3">
      <div className="flex min-w-0 items-start gap-2">
        <FileImage className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 space-y-1">
          <p className="break-words text-sm font-medium text-foreground">{displayName}</p>
          {attachment.url && attachment.url !== displayName && (
            <p className="break-all text-xs text-muted-foreground">{attachment.url}</p>
          )}
          {attachment.instruction && (
            <div className="text-xs">
              <span className="text-muted-foreground">说明</span>
              <p className="mt-0.5 break-words text-foreground/80">{attachment.instruction}</p>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function SnapshotFallback({
  task,
  parseFailed,
  compact,
}: {
  task: Task
  parseFailed: boolean
  compact: boolean
}) {
  const originLabel = task.plan_id ? '计划快照' : '首次输入'
  const warning = parseFailed
    ? task.plan_id
      ? '参考使用摘要无法解析，以下仅展示计划快照。'
      : '参考使用摘要无法解析，以下仅展示首次输入快照。'
    : task.plan_id
      ? '未找到参考使用摘要，以下仅展示计划快照。'
      : '未找到参考使用摘要，以下仅展示首次输入快照。'
  const attachments = task.input_attachments ?? []

  if (compact) {
    return (
      <div className="space-y-3">
        <p className="text-xs leading-5 text-muted-foreground">
          {parseFailed
            ? '素材使用结论无法解析，仅展示任务输入。'
            : '未生成素材使用结论，仅展示任务输入。'}
        </p>
        {attachments.length > 0 ? (
          <div className="space-y-2">
            {attachments.map((attachment, index) => (
              <SnapshotAttachment
                key={`${attachment.upload_id || attachment.url || attachment.file_name || index}-${index}`}
                attachment={attachment}
                index={index}
              />
            ))}
          </div>
        ) : (
          <p className="py-3 text-center text-xs text-muted-foreground">没有参考素材。</p>
        )}
      </div>
    )
  }

  return (
    <Card>
      <CardHeader className="border-b border-border/70">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div>
            <CardTitle className="flex items-center gap-2">
              <Images className="size-4 text-primary" />
              参考素材使用
            </CardTitle>
            <CardDescription className="mt-1">摘要不可用时仍保留任务创建时的素材记录。</CardDescription>
          </div>
          <Badge variant="outline">{originLabel}</Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <Alert className="border-amber-500/30 bg-amber-500/5">
          <TriangleAlert />
          <AlertTitle>仅显示输入快照</AlertTitle>
          <AlertDescription>{warning}</AlertDescription>
        </Alert>
        <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
          <Info className="size-3.5" />
          输入快照不代表 AI 实际使用结论
        </div>
        {attachments.length > 0 ? (
          <div className="grid min-w-0 gap-2 sm:grid-cols-2">
            {attachments.map((attachment, index) => (
              <SnapshotAttachment
                key={`${attachment.upload_id || attachment.url || attachment.file_name || index}-${index}`}
                attachment={attachment}
                index={index}
              />
            ))}
          </div>
        ) : (
          <p className="rounded-lg border border-dashed border-border px-3 py-4 text-center text-xs text-muted-foreground">
            {originLabel}中没有参考素材。
          </p>
        )}
      </CardContent>
    </Card>
  )
}

function SummaryContent({
  summary,
  compact,
}: {
  summary: ReferenceUsageSummaryData
  compact: boolean
}) {
  return (
    <>
        {(summary.model_fallback_reason || summary.warnings?.length) && (
          <div className={compact ? 'grid gap-2' : 'grid gap-2 lg:grid-cols-2'}>
            {summary.model_fallback_reason && (
              <Alert className="border-sky-500/25 bg-sky-500/5">
                <Info />
                <AlertTitle>模型调整</AlertTitle>
                <AlertDescription className="break-words">
                  {summary.model_fallback_reason}
                </AlertDescription>
              </Alert>
            )}
            {summary.warnings?.length ? (
              <Alert className="border-amber-500/30 bg-amber-500/5">
                <TriangleAlert />
                <AlertTitle>执行提醒</AlertTitle>
                <AlertDescription>
                  <Warnings warnings={summary.warnings} />
                </AlertDescription>
              </Alert>
            ) : null}
          </div>
        )}

        <div className={compact ? 'grid min-w-0 gap-4' : 'grid min-w-0 gap-4 xl:grid-cols-2'}>
          <section aria-labelledby="reference-input-decisions" className="min-w-0 space-y-2">
            <div className="flex items-center justify-between gap-2">
              <h3 id="reference-input-decisions" className="text-sm font-semibold text-foreground">
                输入素材决策
              </h3>
              <span className="text-xs text-muted-foreground">{summary.inputs.length} 张</span>
            </div>
            {summary.inputs.length > 0 ? (
              <div className="space-y-2">
                {summary.inputs.map((input, index) => {
                  const status = inputStatusMeta[input.status]
                  return (
                    <article
                      key={`${input.attachment_index}-${input.file_name || input.url || index}`}
                      className="min-w-0 rounded-lg border border-border/70 bg-background/70 p-3"
                    >
                      <div className="flex min-w-0 flex-wrap items-start justify-between gap-2">
                        <div className="min-w-0">
                          <p className="break-words text-sm font-medium text-foreground">
                            #{input.attachment_index} · {input.file_name || input.url || '未命名素材'}
                          </p>
                          {input.instruction && (
                            <p className="mt-0.5 break-words text-xs text-muted-foreground">
                              说明：{input.instruction}
                            </p>
                          )}
                        </div>
                        <Badge variant={status.variant} className={status.className}>
                          {status.label}
                        </Badge>
                      </div>
                      <p className="mt-2 break-words text-sm leading-6 text-foreground/90">
                        {input.decision_summary}
                      </p>
                      <div className="mt-2 flex flex-wrap items-center gap-2">
                        <Badge variant="outline">分析 {input.analysis_attempts} 次</Badge>
                      </div>
                      {input.warnings?.length ? (
                        <div className="mt-2 rounded-md bg-amber-500/8 p-2">
                          <Warnings warnings={input.warnings} />
                        </div>
                      ) : null}
                    </article>
                  )
                })}
              </div>
            ) : (
              <p className="rounded-lg border border-dashed border-border px-3 py-4 text-center text-xs text-muted-foreground">
                没有记录输入素材决策。
              </p>
            )}
          </section>

          <section aria-labelledby="reference-output-usage" className="min-w-0 space-y-2">
            <div className="flex items-center justify-between gap-2">
              <h3 id="reference-output-usage" className="text-sm font-semibold text-foreground">
                输出图片使用情况
              </h3>
              <span className="text-xs text-muted-foreground">{summary.outputs.length} 张</span>
            </div>
            {summary.outputs.length > 0 ? (
              <div className="space-y-2">
                {summary.outputs.map((output, index) => {
                  const verification = verificationMeta[output.verification.status]
                  const providerModel = [output.provider, output.model].filter(Boolean).join(' / ')
                  return (
                    <article
                      key={`${output.file_name}-${index}`}
                      className="min-w-0 rounded-lg border border-border/70 bg-background/70 p-3"
                    >
                      <div className="flex min-w-0 flex-wrap items-start justify-between gap-2">
                        <p className="break-words text-sm font-medium text-foreground">{output.file_name}</p>
                        <Badge
                          variant={verification.variant}
                          className={verification.className}
                        >
                          {output.verification.status === 'passed'
                            ? <CheckCircle2 data-icon="inline-start" />
                            : <AlertCircle data-icon="inline-start" />}
                          {verification.label}
                        </Badge>
                      </div>

                      <div className="mt-3 space-y-2">
                        {output.references.length > 0 ? output.references.map((reference, referenceIndex) => (
                          <div
                            key={`${reference.attachment_index}-${referenceIndex}`}
                            className="flex min-w-0 items-start gap-2 rounded-md bg-muted/50 px-2.5 py-2"
                          >
                            <Badge variant="outline">#{reference.attachment_index}</Badge>
                            <p className="min-w-0 break-words text-xs leading-5 text-foreground/85">
                              {reference.purpose}
                            </p>
                          </div>
                        )) : (
                          <p className="rounded-md bg-muted/50 px-2.5 py-2 text-xs text-muted-foreground">
                            此输出未使用输入参考素材。
                          </p>
                        )}
                      </div>

                      <p className="mt-2 break-words text-xs text-muted-foreground">
                        {output.verification.summary}
                      </p>
                      <div className="mt-3 flex min-w-0 flex-wrap items-center gap-2 text-xs">
                        <Badge variant="outline">生成 {output.generation_attempts} 次</Badge>
                        {providerModel && <span className="break-all text-muted-foreground">{providerModel}</span>}
                        {output.selection_reason && (
                          <span className="min-w-0 break-all rounded-md bg-muted px-2 py-1 font-mono text-[11px] text-muted-foreground">
                            {output.selection_reason}
                          </span>
                        )}
                      </div>
                    </article>
                  )
                })}
              </div>
            ) : (
              <p className="rounded-lg border border-dashed border-border px-3 py-4 text-center text-xs text-muted-foreground">
                没有记录输出图片使用情况。
              </p>
            )}
          </section>
        </div>
    </>
  )
}

function ValidSummary({
  summary,
  compact,
}: {
  summary: ReferenceUsageSummaryData
  compact: boolean
}) {
  if (compact) {
    return (
      <div className="space-y-4">
        <SummaryContent summary={summary} compact />
      </div>
    )
  }

  return (
    <Card>
      <CardHeader className="border-b border-border/70">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div>
            <CardTitle className="flex items-center gap-2">
              <Images className="size-4 text-primary" />
              参考素材使用
            </CardTitle>
            <CardDescription className="mt-1">Agent 自动选择的输入依据、逐图用途与生成核验结果。</CardDescription>
          </div>
          <Badge
            variant="secondary"
            className="bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
          >
            <ShieldCheck data-icon="inline-start" />
            已生成决策摘要
          </Badge>
        </div>
      </CardHeader>

      <CardContent className="space-y-4">
        <SummaryContent summary={summary} compact={false} />
      </CardContent>
    </Card>
  )
}

export function ReferenceUsageSummary({
  task,
  files,
  variant = 'card',
}: ReferenceUsageSummaryProps) {
  const compact = variant === 'compact'
  const summaryFile = useMemo(
    () => files.find((file) => file.file_name === 'reference-usage-summary.json'),
    [files],
  )
  const [loaded, setLoaded] = useState<{
    taskId: string
    fileId: string
    summary: ReferenceUsageSummaryData | null
    parseFailed: boolean
  } | null>(null)

  useEffect(() => {
    let cancelled = false
    if (!summaryFile) return undefined

    void api.tasks.downloadFileBlob(task.id, summaryFile.id)
      .then((blob) => blob.text())
      .then((text) => JSON.parse(text) as unknown)
      .then((value) => {
        if (!isReferenceUsageSummaryData(value)) throw new Error('invalid summary schema')
        if (!cancelled) {
          setLoaded({
            taskId: task.id,
            fileId: summaryFile.id,
            summary: value,
            parseFailed: false,
          })
        }
      })
      .catch(() => {
        if (!cancelled) {
          setLoaded({
            taskId: task.id,
            fileId: summaryFile.id,
            summary: null,
            parseFailed: true,
          })
        }
      })

    return () => {
      cancelled = true
    }
  }, [task.id, summaryFile?.id])

  if (!summaryFile) {
    if (!(task.input_attachments?.length)) {
      if (compact) {
        return <p className="py-3 text-center text-xs text-muted-foreground">没有参考素材。</p>
      }
      return null
    }
    return <SnapshotFallback task={task} parseFailed={false} compact={compact} />
  }

  const isCurrent = loaded?.taskId === task.id && loaded.fileId === summaryFile.id
  if (!isCurrent) return <SummaryLoading compact={compact} />
  if (loaded.summary) return <ValidSummary summary={loaded.summary} compact={compact} />
  return <SnapshotFallback task={task} parseFailed={loaded.parseFailed} compact={compact} />
}

export default ReferenceUsageSummary
