import { describe, expect, it } from 'vitest'
import { assistantInteractionReducer, assistantInteractionRecoveryLabel, initialAssistantInteractionState } from './assistant-interaction-state'

const snapshot = { interaction_id: 'i-1', mode_id: 'creation' as const, lifecycle: 'needs_confirmation', generation: 7, runtime_status: 'running', expires_at: '', last_activity_at: '', available_actions: ['confirm_draft'], drafts: [] }

describe('assistant interaction state', () => {
  it('restores durable snapshot without clearing composer or messages', () => {
    const state = assistantInteractionReducer({ ...initialAssistantInteractionState, composer: '保留输入', messages: [{ id: 'm-1', role: 'user', text: '需求' }] }, { type: 'snapshot', snapshot })
    expect(state.status).toBe('needs_confirmation')
    expect(state.interactionId).toBe('i-1')
    expect(state.composer).toBe('保留输入')
    expect(state.messages).toHaveLength(1)
  })

  it('maps recovery errors to executable user states', () => {
    const state = assistantInteractionReducer(initialAssistantInteractionState, { type: 'error', code: 'assistant_draft_saved_retryable' })
    expect(state.status).toBe('draft_retryable')
    expect(assistantInteractionRecoveryLabel(state)).toContain('重试暂存')
  })

  it('marks duplicate commit as completed', () => {
    const state = assistantInteractionReducer(initialAssistantInteractionState, { type: 'commit_result', result: { status: 'already_created', task_id: 'task-1' } })
    expect(state.status).toBe('completed')
    expect(state.taskId).toBe('task-1')
  })
})
