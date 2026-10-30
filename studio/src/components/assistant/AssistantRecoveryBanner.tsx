import { AlertTriangle, CheckCircle2, Loader2, RefreshCw } from 'lucide-react'
import type { AssistantInteractionState } from '@/lib/assistant-interaction-state'
import { assistantInteractionRecoveryLabel } from '@/lib/assistant-interaction-state'
import { Button } from '@/components/ui/button'

export function AssistantRecoveryBanner({ state, onRetry, onRetryUncertain, onRestoreDraft, onRestart, onViewTask }: { state: AssistantInteractionState; onRetry?: () => void; onRetryUncertain?: () => void; onRestoreDraft?: () => void; onRestart?: () => void; onViewTask?: () => void }) {
  const label = assistantInteractionRecoveryLabel(state)
  if (!label) return null
  const busy = state.status === 'reconnecting' || state.status === 'connecting'
  return <div role="status" className="flex items-center gap-3 border-b bg-muted/30 px-4 py-2 text-sm">
    {busy ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : state.status === 'completed' ? <CheckCircle2 className="size-4 text-emerald-600" aria-hidden="true" /> : <AlertTriangle className="size-4 text-amber-600" aria-hidden="true" />}
    <span className="flex-1">{label}</span>
    {state.status === 'input_retryable' && onRetry ? <Button size="sm" variant="outline" onClick={onRetry}><RefreshCw className="mr-1 size-4" />重试发送</Button> : null}
    {state.status === 'command_uncertain' && onRetryUncertain ? <Button size="sm" variant="outline" onClick={onRetryUncertain}><RefreshCw className="mr-1 size-4" />仍然重试</Button> : null}
    {state.status === 'command_uncertain' && onRestart ? <Button size="sm" variant="ghost" onClick={onRestart}>重新开始</Button> : null}
    {state.status === 'draft_retryable' && state.availableActions.includes('reload_snapshot') && onRestoreDraft ? <Button size="sm" variant="outline" onClick={onRestoreDraft}><RefreshCw className="mr-1 size-4" />恢复最新计划</Button> : null}
    {state.status === 'draft_retryable' && state.availableActions.includes('retry_stage') && onRetry ? <Button size="sm" variant="outline" onClick={onRetry}><RefreshCw className="mr-1 size-4" />重试暂存</Button> : null}
    {state.status === 'restart_required' && onRestoreDraft ? <Button size="sm" onClick={onRestoreDraft}>{state.errorCode === 'assistant_commit_reconciliation_required' ? '核对结果' : '恢复计划'}</Button> : null}
    {state.status === 'restart_required' && state.errorCode !== 'assistant_commit_reconciliation_required' && onRestart ? <Button size="sm" variant="ghost" onClick={onRestart}>重新开始</Button> : null}
    {state.status === 'completed' && onViewTask ? <Button size="sm" onClick={onViewTask}>查看任务</Button> : null}
  </div>
}
