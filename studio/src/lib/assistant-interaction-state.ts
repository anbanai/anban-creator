import type { AssistantBlockingIssue, AssistantInteractionCommitResult, AssistantInteractionDraftReceipt, AssistantInteractionSnapshot } from '@/lib/api/assistant'

export type AssistantInteractionStatus = 'connecting' | 'reconnecting' | 'needs_confirmation' | 'input_retryable' | 'command_uncertain' | 'draft_retryable' | 'draft_editable' | 'restart_required' | 'completed'

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
  | { type: 'error'; code: string; availableActions?: string[]; blockingIssues?: AssistantBlockingIssue[] }
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

function creationDraftRegresses(current: AssistantInteractionDraftReceipt | undefined, incoming: AssistantInteractionDraftReceipt | undefined): boolean {
  if (!current) return false
  if (!incoming) return true
  if (incoming.revision !== current.revision) return incoming.revision < current.revision
  if (incoming.draft_id !== current.draft_id) return true
  if (current.confirmation_state !== 'pending' && incoming.confirmation_state !== current.confirmation_state) {
    return current.confirmation_state !== 'confirmed' || incoming.confirmation_state !== 'consumed'
  }
  return current.confirmation_state === 'pending' && incoming.confirmation_state === 'pending' && !current.ready && incoming.ready
}

export function assistantInteractionReducer(state: AssistantInteractionState, action: AssistantInteractionAction): AssistantInteractionState {
  switch (action.type) {
    case 'snapshot': {
      const snapshot = action.snapshot
      const latestDraft = (snapshot.drafts ?? []).filter((draft) => draft.mode_id === snapshot.mode_id).sort((a, b) => b.revision - a.revision)[0]
      const creationCompleted = snapshot.mode_id === 'creation' && (snapshot.lifecycle === 'completed' || latestDraft?.confirmation_state === 'consumed' || snapshot.commit_result?.status === 'created' || snapshot.commit_result?.status === 'already_created')
      if (snapshot.mode_id === 'creation' && (!state.interactionId || state.interactionId === snapshot.interaction_id) && !creationCompleted) {
        const currentDraft = state.drafts.filter((draft) => draft.mode_id === 'creation').sort((a, b) => b.revision - a.revision)[0]
        if (state.status === 'completed' || state.errorCode === 'assistant_commit_reconciliation_required' && latestDraft?.confirmation_state !== 'confirmed' || creationDraftRegresses(currentDraft, latestDraft)) return state
      }
      const readyDraft = snapshot.mode_id === 'creation'
        ? latestDraft?.ready && latestDraft.confirmation_state === 'pending' && snapshot.available_actions.includes('confirm_draft')
        : (snapshot.drafts ?? []).some((draft) => draft.ready && draft.confirmation_state !== 'consumed') || snapshot.lifecycle === 'needs_confirmation'
      const status = creationCompleted ? 'completed' : snapshot.recovery_error_code === 'assistant_command_outcome_unknown'
        ? 'command_uncertain'
        : snapshot.recovery_error_code === 'assistant_commit_reconciliation_required' || snapshot.mode_id === 'creation' && latestDraft?.confirmation_state === 'confirmed'
        ? 'restart_required'
        : snapshot.recovery_error_code && ['assistant_creation_inputs_missing', 'assistant_draft_invalid', 'assistant_draft_not_ready'].includes(snapshot.recovery_error_code)
        ? 'draft_editable'
        : snapshot.lifecycle === 'completed' ? 'completed' : readyDraft ? 'needs_confirmation' : snapshot.lifecycle === 'reconnecting' ? 'reconnecting' : 'connecting'
      return { ...state, interactionId: snapshot.interaction_id, generation: snapshot.generation, status, availableActions: snapshot.available_actions ?? [], drafts: snapshot.drafts ?? [], errorCode: snapshot.recovery_error_code ?? (status === 'restart_required' ? 'assistant_commit_reconciliation_required' : undefined), taskId: snapshot.commit_result?.task_id }
    }
    case 'composer': return { ...state, composer: action.value }
    case 'message': return { ...state, messages: [...state.messages, action.message], status: 'connecting', errorCode: undefined }
    case 'draft_ready': {
      if (action.draft.mode_id === 'creation') {
        if (state.status === 'completed' || state.errorCode === 'assistant_commit_reconciliation_required' && action.draft.confirmation_state !== 'consumed') return state
        const latest = state.drafts.filter((draft) => draft.mode_id === 'creation').sort((a, b) => b.revision - a.revision)[0]
        if (creationDraftRegresses(latest, action.draft)) return state
        const drafts = [...state.drafts.filter((draft) => draft.mode_id !== 'creation'), action.draft]
        if (action.draft.confirmation_state === 'confirmed') return { ...state, drafts, status: 'restart_required', errorCode: 'assistant_commit_reconciliation_required', availableActions: ['view_task', 'reconcile_commit'] }
        if (action.draft.confirmation_state === 'consumed') return { ...state, drafts, status: 'completed', errorCode: undefined, availableActions: ['view_task', 'restart'] }
        const ready = action.draft.ready && action.draft.confirmation_state === 'pending'
        return { ...state, drafts, status: ready ? 'needs_confirmation' : 'connecting', availableActions: ready ? ['confirm_draft', 'continue_editing'] : ['continue_editing', 'send_message'], errorCode: undefined }
      }
      const drafts = [...state.drafts.filter((draft) => draft.draft_id !== action.draft.draft_id && draft.mode_id !== action.draft.mode_id), action.draft]
      return { ...state, drafts, status: action.draft.ready ? 'needs_confirmation' : state.status, errorCode: undefined }
    }
    case 'generation_changed': return { ...state, generation: action.generation, status: 'reconnecting', availableActions: ['reload_snapshot'], errorCode: 'assistant_interaction_generation_changed' }
    case 'error': {
      if (['assistant_creation_inputs_missing', 'assistant_draft_invalid', 'assistant_draft_not_ready'].includes(action.code)) {
        return { ...state, status: 'draft_editable', availableActions: action.availableActions ?? ['continue_editing', 'send_message'], errorCode: action.code, drafts: state.drafts.map((draft) => draft.mode_id === 'creation' ? { ...draft, ready: false, blocking_issues: action.blockingIssues ?? draft.blocking_issues } : draft) }
      }
      const status: AssistantInteractionStatus = action.code === 'assistant_draft_saved_retryable' || action.code === 'assistant_draft_revision_conflict' ? 'draft_retryable' : action.code === 'assistant_command_outcome_unknown' ? 'command_uncertain' : action.code === 'assistant_interaction_restart_required' || action.code === 'assistant_commit_reconciliation_required' ? 'restart_required' : action.code === 'assistant_interaction_reconnecting' || action.code === 'assistant_interaction_generation_changed' ? 'reconnecting' : action.code === 'assistant_commit_already_applied' ? 'completed' : 'input_retryable'
      const availableActions = action.availableActions ?? (action.code === 'assistant_commit_reconciliation_required' ? ['view_task', 'reconcile_commit'] : action.code === 'assistant_command_outcome_unknown' ? ['retry_with_new_attempt', 'restart'] : action.code === 'assistant_interaction_generation_changed' || action.code === 'assistant_interaction_reconnecting' ? ['reload_snapshot'] : action.code === 'assistant_draft_revision_conflict' ? ['reload_snapshot', 'restore_draft'] : action.code === 'assistant_draft_saved_retryable' ? ['retry_stage', 'restore_draft'] : state.availableActions)
      return { ...state, status, availableActions, errorCode: action.code }
    }
    case 'clear_error': return { ...state, status: 'connecting', availableActions: state.drafts.some((draft) => draft.mode_id === 'creation' && !draft.ready && (draft.confirmation_state === 'pending' || draft.confirmation_state === 'superseded')) ? state.availableActions : [], errorCode: undefined }
    case 'commit_result': {
      if (action.result.status === 'needs_reconciliation') return { ...state, status: 'restart_required', errorCode: 'assistant_commit_reconciliation_required', availableActions: ['view_task', 'reconcile_commit'], taskId: action.result.task_id }
      if (action.result.status === 'already_created' || action.result.status === 'created') return { ...state, status: 'completed', errorCode: undefined, availableActions: ['view_task', 'restart'], drafts: state.drafts.map((draft) => draft.mode_id === 'creation' ? { ...draft, ready: false, confirmation_state: 'consumed' } : draft), taskId: action.result.task_id }
      return state
    }
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
    case 'draft_editable': return state.errorCode === 'assistant_creation_inputs_missing' ? '计划仍有必要信息待补充，可以继续讨论' : '计划尚未就绪，可以继续修改'
    case 'draft_retryable': return state.errorCode === 'assistant_draft_revision_conflict' ? '计划版本已更新，请恢复最新计划' : '计划已保留，可以重试暂存'
    case 'restart_required': return state.errorCode === 'assistant_commit_reconciliation_required' ? '创建结果需要核对，请查看任务状态' : '连接已结束，计划仍然保留'
    case 'completed': return '已创建任务'
    default: return ''
  }
}
