import type { WorkflowStatus } from '@/types'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { parseWorkflowStatus, readinessValueLabel } from '@/lib/workflow-readiness'

interface TaskWorkflowPanelProps {
  workflow?: WorkflowStatus | string | null
}

export function parseWorkflow(workflow?: WorkflowStatus | string | null): WorkflowStatus | null {
  return parseWorkflowStatus(workflow)
}

function readinessClassName(score: number) {
  if (score >= 80) return 'border-emerald-500/30 bg-emerald-500/15 text-emerald-600 dark:text-emerald-400'
  if (score >= 60) return 'border-amber-500/30 bg-amber-500/15 text-amber-600 dark:text-amber-400'
  return 'border-destructive/30 bg-destructive/10 text-destructive'
}

export function WorkflowReviewSummary({ workflow }: TaskWorkflowPanelProps) {
  const data = parseWorkflow(workflow)
  if (!data?.review) return null

  return (
    <Card>
      <CardContent>
        <div className="flex flex-col gap-4">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h2 className="text-sm font-semibold text-foreground">发布前检查</h2>
              <p className="mt-1 text-xs text-muted-foreground">质量复盘</p>
            </div>
            <Badge variant="outline" className={readinessClassName(data.review.overall_score)}>
              {readinessValueLabel(data.review.readiness) || '未评估'}
            </Badge>
          </div>

          <div className="grid gap-4 lg:grid-cols-[120px_1fr]">
            <div className="rounded-lg border border-border bg-muted/30 p-4">
              <p className="text-xs text-muted-foreground">复盘评分</p>
              <p className="mt-1 text-4xl font-bold text-foreground">{data.review.overall_score}</p>
            </div>
            <div className="grid gap-3 sm:grid-cols-3">
              <ReviewList title="风险" items={data.review.risks} />
              <ReviewList title="下一步" items={data.review.next_actions} />
              <ReviewList title="优势" items={data.review.strengths} />
            </div>
          </div>

          {data.warnings && data.warnings.length > 0 && (
            <div className="rounded-lg border border-amber-500/20 bg-amber-500/10 px-3 py-2">
              {data.warnings.map((warning) => (
                <p key={warning.code} className="text-xs text-amber-600 dark:text-amber-400">{warning.message}</p>
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
