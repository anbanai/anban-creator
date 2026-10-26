import type { AssistantInteractionCommitResult, AssistantInteractionDraftReceipt, AssistantInteractionSnapshot } from '@/lib/api/assistant'

export type AssistantInteractionStatus = 'connecting' | 'reconnecting' | 'needs_confirmation' | 'input_retryable' | 'draft_retryable' | 'restart_required' | 'completed'

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
  | { type: 'generation_changed'; generation: number }
  | { type: 'error'; code: string }
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
      const status = snapshot.lifecycle === 'needs_confirmation' ? 'needs_confirmation' : snapshot.lifecycle === 'completed' ? 'completed' : snapshot.lifecycle === 'reconnecting' ? 'reconnecting' : 'connecting'
      return { ...state, interactionId: snapshot.interaction_id, generation: snapshot.generation, status, availableActions: snapshot.available_actions ?? [], drafts: snapshot.drafts ?? [], errorCode: undefined }
    }
    case 'composer': return { ...state, composer: action.value }
    case 'message': return { ...state, messages: [...state.messages, action.message], status: 'connecting', errorCode: undefined }
    case 'generation_changed': return { ...state, generation: action.generation, status: 'reconnecting', availableActions: ['reload_snapshot'], errorCode: 'assistant_interaction_generation_changed' }
    case 'error': {
      const status: AssistantInteractionStatus = action.code === 'assistant_draft_saved_retryable' ? 'draft_retryable' : action.code === 'assistant_interaction_restart_required' ? 'restart_required' : action.code === 'assistant_interaction_reconnecting' ? 'reconnecting' : action.code === 'assistant_commit_already_applied' ? 'completed' : 'input_retryable'
      return { ...state, status, errorCode: action.code }
    }
    case 'commit_result': return { ...state, status: action.result.status === 'already_created' || action.result.status === 'created' ? 'completed' : action.result.status === 'needs_reconciliation' ? 'restart_required' : state.status, taskId: action.result.task_id }
    case 'restart': return { ...initialAssistantInteractionState, status: 'connecting' }
    default: return state
  }
}

export function assistantInteractionRecoveryLabel(state: AssistantInteractionState): string {
  switch (state.status) {
    case 'reconnecting': return '正在恢复连接'
    case 'needs_confirmation': return '计划已准备好，请确认'
    case 'input_retryable': return '刚才的消息未送出，内容已保留'
    case 'draft_retryable': return '计划已保留，可以重试暂存'
    case 'restart_required': return '连接已结束，计划仍然保留'
    case 'completed': return '已创建任务'
    default: return ''
  }
}
