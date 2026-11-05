import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { AssistantRecoveryBanner } from './AssistantRecoveryBanner'
import { assistantInteractionReducer, initialAssistantInteractionState } from '@/lib/assistant-interaction-state'

it('offers snapshot reload for stale versions without suggesting a staging retry', () => {
  const restore = vi.fn()
  const state = assistantInteractionReducer(initialAssistantInteractionState, { type: 'error', code: 'assistant_draft_revision_conflict' })
  render(<AssistantRecoveryBanner state={state} onRetry={vi.fn()} onRestoreDraft={restore} />)
  fireEvent.click(screen.getByRole('button', { name: '恢复最新计划' }))
  expect(restore).toHaveBeenCalledOnce()
  expect(screen.queryByRole('button', { name: '重试暂存' })).not.toBeInTheDocument()
})

it('offers reconciliation without an unsupported restart', () => {
  const state = assistantInteractionReducer(initialAssistantInteractionState, { type: 'error', code: 'assistant_commit_reconciliation_required' })
  render(<AssistantRecoveryBanner state={state} onRestoreDraft={vi.fn()} onRestart={vi.fn()} />)
  expect(screen.getByRole('button', { name: '核对结果' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '重新开始' })).not.toBeInTheDocument()
})

it('offers only a homepage restart for an ended conversation', () => {
  const state = assistantInteractionReducer(initialAssistantInteractionState, { type: 'snapshot', snapshot: {
    interaction_id: 'old', mode_id: 'creation', lifecycle: 'abandoned', generation: 1,
    runtime_status: 'closed', expires_at: '', last_activity_at: '', available_actions: ['restart'], drafts: [],
  } })
  render(<AssistantRecoveryBanner state={state} onRestoreDraft={vi.fn()} onRestart={vi.fn()} />)
  expect(screen.getByText('当前对话已结束，请重新开始')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '重新开始' })).toBeEnabled()
  expect(screen.queryByRole('button', { name: '恢复计划' })).not.toBeInTheDocument()
})

it('does not invent an unsent message for a context operation failure', () => {
  const state = assistantInteractionReducer(initialAssistantInteractionState, { type: 'error', code: 'assistant_workspace_context_mismatch' })
  render(<AssistantRecoveryBanner state={state} onRetry={vi.fn()} />)
  expect(screen.queryByText('刚才的消息未送出，内容已保留')).not.toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '重试发送' })).not.toBeInTheDocument()
})
