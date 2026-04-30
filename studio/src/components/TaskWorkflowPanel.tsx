import { CheckCircle2, Circle, AlertTriangle } from 'lucide-react'
import type { WorkflowStatus } from '@/types'
import { Card, CardContent } from '@/components/ui/Card'
import { Badge } from '@/components/ui/Badge'

interface TaskWorkflowPanelProps {
  workflow?: WorkflowStatus | string | null
}

function parseWorkflow(workflow?: WorkflowStatus | string | null): WorkflowStatus | null {
  if (!workflow) return null
  if (typeof workflow !== 'string') return workflow
  try {
    return JSON.parse(workflow) as WorkflowStatus
  } catch {
    return null
  }
}

function readinessLabel(readiness?: string) {
  switch (readiness) {
    case 'ready':
      return '可发布'
    case 'ready_with_minor_edits':
      return '建议修改'
    case 'needs_revision':
      return '需重做'
    default:
      return readiness || '未评估'
  }
}

function stageIcon(status: string) {
  if (status === 'completed') return <CheckCircle2 className="h-4 w-4 text-emerald-500" />
  if (status === 'warning' || status === 'failed') return <AlertTriangle className="h-4 w-4 text-amber-500" />
  return <Circle className="h-4 w-4 text-muted-foreground" />
}

function readinessClassName(score: number) {
  if (score >= 80) return 'border-emerald-500/30 bg-emerald-500/15 text-emerald-600 dark:text-emerald-400'
  if (score >= 60) return 'border-amber-500/30 bg-amber-500/15 text-amber-600 dark:text-amber-400'
  return 'border-destructive/30 bg-destructive/10 text-destructive'
}

export default function TaskWorkflowPanel({ workflow }: TaskWorkflowPanelProps) {
  const data = parseWorkflow(workflow)
  if (!data) return null

  return (
    <Card>
      <CardContent>
        <div className="flex flex-col gap-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <h2 className="text-sm font-semibold text-foreground">创作流程</h2>
              <p className="mt-1 text-xs text-muted-foreground">阶段产物与质量复盘</p>
            </div>
            {data.review && (
              <Badge variant="outline" className={readinessClassName(data.review.overall_score)}>
                {readinessLabel(data.review.readiness)}
              </Badge>
            )}
          </div>

          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
            {data.stages.map((stage) => (
              <div key={stage.key} className="rounded-lg border border-border bg-muted/30 p-3">
                <div className="flex items-center gap-2">
                  {stageIcon(stage.status)}
                  <span className="text-sm font-medium text-foreground">{stage.label}</span>
                </div>
                {stage.artifact_paths && stage.artifact_paths.length > 0 && (
                  <p className="mt-1 truncate text-xs text-muted-foreground">{stage.artifact_paths.join(', ')}</p>
                )}
                {stage.error && (
                  <p className="mt-1 text-xs text-amber-500">{stage.error}</p>
                )}
              </div>
            ))}
          </div>

          {data.review && (
            <div className="grid gap-4 rounded-lg border border-border p-4 lg:grid-cols-[120px_1fr]">
              <div>
                <p className="text-xs text-muted-foreground">复盘评分</p>
                <p className="mt-1 text-4xl font-bold text-foreground">{data.review.overall_score}</p>
              </div>
              <div className="grid gap-3 sm:grid-cols-3">
                <ReviewList title="优势" items={data.review.strengths} />
                <ReviewList title="风险" items={data.review.risks} />
                <ReviewList title="下一步" items={data.review.next_actions} />
              </div>
            </div>
          )}

          {data.warnings && data.warnings.length > 0 && (
            <div className="space-y-1">
              {data.warnings.map((warning) => (
                <p key={warning.code} className="text-xs text-amber-500">{warning.message}</p>
              ))}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function ReviewList({ title, items }: { title: string; items?: string[] }) {
  return (
    <div>
      <p className="text-xs font-medium text-muted-foreground">{title}</p>
      {items && items.length > 0 ? (
        <ul className="mt-1 space-y-1">
          {items.slice(0, 3).map((item) => (
            <li key={item} className="text-sm text-foreground">{item}</li>
          ))}
        </ul>
      ) : (
        <p className="mt-1 text-sm text-muted-foreground">暂无</p>
      )}
    </div>
  )
}
