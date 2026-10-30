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
