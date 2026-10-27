import type { AssistantInteractionCommitResult, AssistantInteractionDraftReceipt, AssistantInteractionSnapshot } from '@/lib/api/assistant'

export type AssistantInteractionStatus = 'connecting' | 'reconnecting' | 'needs_confirmation' | 'input_retryable' | 'command_uncertain' | 'draft_retryable' | 'restart_required' | 'completed'

export interface AssistantInteractionState {
  interactionId?: string
  generation?: number
  status: AssistantInteractionStatus
  availableActions: string[]
  composer: string
  messages: Array<{ id: string; role: 'user' | 'assistant'; text: string }>
  drafts: AssistantInteractionDraftReceipt[]
  errorCode?: string
  taskId?: string
}

export type AssistantInteractionAction =
  | { type: 'snapshot'; snapshot: AssistantInteractionSnapshot }
  | { type: 'composer'; value: string }
  | { type: 'message'; message: { id: string; role: 'user' | 'assistant'; text: string } }
  | { type: 'draft_ready'; draft: AssistantInteractionDraftReceipt }
  | { type: 'generation_changed'; generation: number }
  | { type: 'error'; code: string }
  | { type: 'clear_error' }
  | { type: 'commit_result'; result: AssistantInteractionCommitResult }
  | { type: 'restart' }

export const initialAssistantInteractionState: AssistantInteractionState = {
  status: 'connecting',
  availableActions: [],
  composer: '',
  messages: [],
  drafts: [],
}

export function assistantInteractionReducer(state: AssistantInteractionState, action: AssistantInteractionAction): AssistantInteractionState {
  switch (action.type) {
    case 'snapshot': {
      const snapshot = action.snapshot
      const readyDraft = (snapshot.drafts ?? []).some((draft) => draft.ready && draft.confirmation_state !== 'consumed')
      const status = snapshot.recovery_error_code === 'assistant_commit_reconciliation_required'
        ? 'restart_required'
        : snapshot.lifecycle === 'completed' ? 'completed' : readyDraft || snapshot.lifecycle === 'needs_confirmation' ? 'needs_confirmation' : snapshot.lifecycle === 'reconnecting' ? 'reconnecting' : 'connecting'
      return { ...state, interactionId: snapshot.interaction_id, generation: snapshot.generation, status, availableActions: snapshot.available_actions ?? [], drafts: snapshot.drafts ?? [], errorCode: snapshot.recovery_error_code, taskId: snapshot.commit_result?.task_id }
    }
    case 'composer': return { ...state, composer: action.value }
    case 'message': return { ...state, messages: [...state.messages, action.message], status: 'connecting', errorCode: undefined }
    case 'draft_ready': {
      const drafts = [...state.drafts.filter((draft) => draft.draft_id !== action.draft.draft_id && draft.mode_id !== action.draft.mode_id), action.draft]
      return { ...state, drafts, status: action.draft.ready ? 'needs_confirmation' : state.status, errorCode: undefined }
    }
    case 'generation_changed': return { ...state, generation: action.generation, status: 'reconnecting', availableActions: ['reload_snapshot'], errorCode: 'assistant_interaction_generation_changed' }
    case 'error': {
      const status: AssistantInteractionStatus = action.code === 'assistant_draft_saved_retryable' || action.code === 'assistant_draft_revision_conflict' ? 'draft_retryable' : action.code === 'assistant_command_outcome_unknown' ? 'command_uncertain' : action.code === 'assistant_interaction_restart_required' || action.code === 'assistant_commit_reconciliation_required' ? 'restart_required' : action.code === 'assistant_interaction_reconnecting' || action.code === 'assistant_interaction_generation_changed' ? 'reconnecting' : action.code === 'assistant_commit_already_applied' ? 'completed' : 'input_retryable'
      const availableActions = action.code === 'assistant_commit_reconciliation_required' ? ['view_task', 'reconcile_commit'] : action.code === 'assistant_command_outcome_unknown' ? ['retry_with_new_attempt', 'restart'] : action.code === 'assistant_interaction_generation_changed' || action.code === 'assistant_interaction_reconnecting' ? ['reload_snapshot'] : action.code === 'assistant_draft_saved_retryable' || action.code === 'assistant_draft_revision_conflict' ? ['retry_stage', 'restore_draft'] : state.availableActions
      return { ...state, status, availableActions, errorCode: action.code }
    }
    case 'clear_error': return { ...state, status: 'connecting', availableActions: [], errorCode: undefined }
    case 'commit_result': return { ...state, status: action.result.status === 'already_created' || action.result.status === 'created' ? 'completed' : action.result.status === 'needs_reconciliation' ? 'restart_required' : state.status, taskId: action.result.task_id }
    case 'restart': return { ...initialAssistantInteractionState, status: 'connecting' }
    default: return state
  }
}

export function assistantInteractionRecoveryLabel(state: AssistantInteractionState): string {
  switch (state.status) {
    case 'reconnecting': return '正在恢复连接'
    case 'command_uncertain': return '上一条操作的结果无法确认，重新提交可能重复执行'
    case 'needs_confirmation': return '计划已准备好，请确认'
    case 'input_retryable': return '刚才的消息未送出，内容已保留'
    case 'draft_retryable': return state.errorCode === 'assistant_draft_revision_conflict' ? '计划版本已更新，请恢复最新计划' : '计划已保留，可以重试暂存'
    case 'restart_required': return state.errorCode === 'assistant_commit_reconciliation_required' ? '创建结果需要核对，请查看任务状态' : '连接已结束，计划仍然保留'
    case 'completed': return '已创建任务'
    default: return ''
  }
}
