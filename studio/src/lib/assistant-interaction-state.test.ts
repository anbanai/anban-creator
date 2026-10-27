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

  it('requires an explicit new attempt when command delivery is uncertain', () => {
    const state = assistantInteractionReducer(initialAssistantInteractionState, { type: 'error', code: 'assistant_command_outcome_unknown' })
    expect(state.status).toBe('command_uncertain')
    expect(assistantInteractionRecoveryLabel(state)).toContain('可能重复执行')
  })

  it('moves to confirmation when a durable draft event arrives', () => {
    const activeSnapshot = { ...snapshot, lifecycle: 'active' as const, drafts: [] }
    const state = assistantInteractionReducer(initialAssistantInteractionState, { type: 'snapshot', snapshot: activeSnapshot })
    const next = assistantInteractionReducer(state, { type: 'draft_ready', draft: { draft_id: 'draft-1', mode_id: 'creation', revision: 1, payload: {}, payload_digest: 'digest', ready: true, confirmation_state: 'pending' } })
    expect(next.status).toBe('needs_confirmation')
    expect(next.drafts[0]?.draft_id).toBe('draft-1')
  })

  it('restores confirmation from a ready draft even when lifecycle lagged', () => {
    const state = assistantInteractionReducer(initialAssistantInteractionState, { type: 'snapshot', snapshot: { ...snapshot, lifecycle: 'active' as const, drafts: [{ draft_id: 'draft-1', mode_id: 'creation', revision: 1, payload: {}, payload_digest: 'digest', ready: true, confirmation_state: 'pending' }] } })
    expect(state.status).toBe('needs_confirmation')
  })

  it('marks duplicate commit as completed', () => {
    const state = assistantInteractionReducer(initialAssistantInteractionState, { type: 'commit_result', result: { status: 'already_created', task_id: 'task-1' } })
    expect(state.status).toBe('completed')
    expect(state.taskId).toBe('task-1')
  })

	it('restores completed commit receipt and task from a refreshed snapshot', () => {
		const state = assistantInteractionReducer(initialAssistantInteractionState, {
			type: 'snapshot',
			snapshot: {
				...snapshot,
				lifecycle: 'completed',
				available_actions: ['view_task'],
				commit_result: { status: 'created', task_id: 'task-2' },
			},
		})
		expect(state.status).toBe('completed')
		expect(state.taskId).toBe('task-2')
	})

	it('restores reconciliation state from a durable commit failure receipt', () => {
		const state = assistantInteractionReducer(initialAssistantInteractionState, {
			type: 'snapshot',
			snapshot: {
				...snapshot,
				lifecycle: 'active',
				recovery_error_code: 'assistant_commit_reconciliation_required',
				available_actions: ['view_task', 'reconcile_commit'],
			},
		})
		expect(state.status).toBe('restart_required')
		expect(state.errorCode).toBe('assistant_commit_reconciliation_required')
		expect(assistantInteractionRecoveryLabel(state)).toContain('核对')
	})

	it('restores command outcome uncertainty after a page refresh', () => {
		const state = assistantInteractionReducer(initialAssistantInteractionState, {
			type: 'snapshot',
			snapshot: {
				...snapshot,
				lifecycle: 'active',
				recovery_error_code: 'assistant_command_outcome_unknown',
				available_actions: ['retry_with_new_attempt', 'restart'],
			},
		})
		expect(state.status).toBe('command_uncertain')
		expect(assistantInteractionRecoveryLabel(state)).toContain('可能重复执行')
	})
})
