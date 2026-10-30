import { describe, expect, it } from 'vitest'
import { assistantInteractionReducer, assistantInteractionRecoveryLabel, initialAssistantInteractionState } from './assistant-interaction-state'

const snapshot = { interaction_id: 'i-1', mode_id: 'creation' as const, lifecycle: 'needs_confirmation', generation: 7, runtime_status: 'running', expires_at: '', last_activity_at: '', available_actions: ['confirm_draft'], drafts: [{ draft_id: 'd-1', mode_id: 'creation' as const, revision: 1, payload: {}, payload_digest: 'digest', ready: true, confirmation_state: 'pending' }] }

describe('assistant interaction state', () => {
  const draft = { draft_id: 'd-1', mode_id: 'creation' as const, revision: 1, payload: {}, payload_digest: 'digest', ready: true, confirmation_state: 'pending' }

  it('keeps incomplete and withdrawn snapshots editable despite stale lifecycle', () => {
    for (const receipt of [{ ...draft, ready: false }, { ...draft, confirmation_state: 'superseded' }]) {
      const state = assistantInteractionReducer(initialAssistantInteractionState, { type: 'snapshot', snapshot: { ...snapshot, available_actions: ['continue_editing', 'send_message'], drafts: [receipt] } })
      expect(state.status).toBe('connecting')
      expect(state.availableActions).toContain('send_message')
    }
  })

  it('does not revive older or withdrawn confirmation from replayed draft events', () => {
    const editing = assistantInteractionReducer(initialAssistantInteractionState, { type: 'snapshot', snapshot: { ...snapshot, lifecycle: 'active', available_actions: ['continue_editing', 'send_message'], drafts: [{ ...draft, revision: 2, ready: false, confirmation_state: 'superseded' }] } })
    for (const replay of [draft, { ...draft, revision: 2 }]) {
      const state = assistantInteractionReducer(editing, { type: 'draft_ready', draft: replay })
      expect(state.status).toBe('connecting')
      expect(state.drafts[0]?.confirmation_state).toBe('superseded')
      expect(state.availableActions).not.toContain('confirm_draft')
    }
  })

  it('converges a ready event back to editable when the server supplies a non-ready revision', () => {
    const ready = assistantInteractionReducer(initialAssistantInteractionState, { type: 'draft_ready', draft })
    expect(ready.availableActions).toContain('confirm_draft')
    const editing = assistantInteractionReducer(ready, { type: 'draft_ready', draft: { ...draft, revision: 2, ready: false } })
    expect(editing.status).toBe('connecting')
    expect(editing.availableActions).toContain('send_message')
  })

  it('keeps actionable validation distinct from saving, stale versions and reconciliation', () => {
    for (const code of ['assistant_creation_inputs_missing', 'assistant_draft_invalid', 'assistant_draft_not_ready']) {
      const state = assistantInteractionReducer(initialAssistantInteractionState, { type: 'error', code })
      expect(state.status).toBe('draft_editable')
      expect(state.availableActions).toContain('continue_editing')
      expect(assistantInteractionRecoveryLabel(state)).not.toContain('未送出')
    }
    const stale = assistantInteractionReducer(initialAssistantInteractionState, { type: 'error', code: 'assistant_draft_revision_conflict' })
    expect(stale.availableActions).toContain('reload_snapshot')
    expect(stale.availableActions).not.toContain('retry_stage')
  })

  it('preserves reconciliation controls and prevents later replay from offering another confirmation', () => {
    const ready = assistantInteractionReducer(initialAssistantInteractionState, { type: 'snapshot', snapshot })
    const uncertain = assistantInteractionReducer(ready, { type: 'commit_result', result: { status: 'needs_reconciliation' } })
    expect(uncertain.errorCode).toBe('assistant_commit_reconciliation_required')
    expect(uncertain.availableActions).toEqual(['view_task', 'reconcile_commit'])
    expect(assistantInteractionReducer(uncertain, { type: 'draft_ready', draft })).toEqual(uncertain)
  })

  it('keeps a claimed draft event in reconciliation even before a receipt is available', () => {
    const claimed = assistantInteractionReducer(initialAssistantInteractionState, { type: 'draft_ready', draft: { ...draft, ready: false, confirmation_state: 'confirmed' } })
    expect(claimed.errorCode).toBe('assistant_commit_reconciliation_required')
    expect(claimed.availableActions).toEqual(['view_task', 'reconcile_commit'])
  })

  it('restores actionable validation failures from a refreshed snapshot', () => {
    const editing = assistantInteractionReducer(initialAssistantInteractionState, { type: 'snapshot', snapshot: { ...snapshot, recovery_error_code: 'assistant_creation_inputs_missing', available_actions: ['continue_editing', 'send_message'], drafts: [{ ...draft, ready: false }] } })
    expect(editing.status).toBe('draft_editable')
    expect(assistantInteractionRecoveryLabel(editing)).toContain('待补充')
  })

  it('keeps a non-ready creation draft editable after a message or answer succeeds', () => {
    const editing = assistantInteractionReducer(initialAssistantInteractionState, { type: 'snapshot', snapshot: { ...snapshot, lifecycle: 'active', available_actions: ['continue_editing', 'send_message'], drafts: [{ ...draft, ready: false }] } })
    const accepted = assistantInteractionReducer(editing, { type: 'clear_error' })
    expect(accepted.errorCode).toBeUndefined()
    expect(accepted.availableActions).toEqual(['continue_editing', 'send_message'])
  })
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
