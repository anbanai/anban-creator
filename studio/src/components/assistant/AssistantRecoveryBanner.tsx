import { AlertTriangle, CheckCircle2, Loader2, RefreshCw } from 'lucide-react'
import type { AssistantInteractionState } from '@/lib/assistant-interaction-state'
import { assistantInteractionRecoveryLabel } from '@/lib/assistant-interaction-state'
import { Button } from '@/components/ui/button'

export function AssistantRecoveryBanner({ state, onRetry, onRetryCommit, onRestoreDraft, onReconcileCommit, reconciling = false, onRestart, onViewTask }: { state: AssistantInteractionState; onRetry?: () => void; onRetryCommit?: () => void; onRestoreDraft?: () => void; onReconcileCommit?: () => void; reconciling?: boolean; onRestart?: () => void; onViewTask?: () => void }) {
  const label = assistantInteractionRecoveryLabel(state)
  if (!label) return null
  const busy = state.status === 'reconnecting' || state.status === 'connecting'
  return <div role="status" className="flex items-center gap-3 border-b bg-muted/30 px-4 py-2 text-sm">
    {busy ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : state.status === 'completed' ? <CheckCircle2 className="size-4 text-emerald-600" aria-hidden="true" /> : <AlertTriangle className="size-4 text-amber-600" aria-hidden="true" />}
    <span className="flex-1">{label}</span>
    {state.status === 'reconnecting' && onRetry ? <Button size="sm" variant="outline" onClick={onRetry}><RefreshCw className="mr-1 size-4" />恢复并继续</Button> : null}
    {state.status === 'input_retryable' && onRetry ? <Button size="sm" variant="outline" onClick={onRetry}><RefreshCw className="mr-1 size-4" />重试发送</Button> : null}
    {state.status === 'draft_retryable' && onRetry ? <Button size="sm" variant="outline" onClick={onRetry}><RefreshCw className="mr-1 size-4" />重试暂存</Button> : null}
    {state.status === 'needs_confirmation' && state.errorCode === 'assistant_commit_retryable' && onRetryCommit ? <Button size="sm" variant="outline" onClick={onRetryCommit}><RefreshCw className="mr-1 size-4" />重试创建</Button> : null}
    {state.status === 'restart_required' && state.errorCode === 'assistant_commit_reconciliation_required' && onReconcileCommit ? <Button size="sm" disabled={reconciling} onClick={onReconcileCommit}>{reconciling ? <Loader2 className="size-4 animate-spin" /> : null}核对结果</Button> : null}
    {state.status === 'restart_required' && state.errorCode !== 'assistant_commit_reconciliation_required' && onRestoreDraft ? <Button size="sm" onClick={onRestoreDraft}>恢复计划</Button> : null}
    {state.status === 'restart_required' && onRestart ? <Button size="sm" variant="ghost" onClick={onRestart}>重新开始</Button> : null}
    {state.status === 'completed' && onViewTask ? <Button size="sm" onClick={onViewTask}>查看任务</Button> : null}
  </div>
}
