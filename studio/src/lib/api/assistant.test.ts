import { afterEach, expect, it, vi } from 'vitest'
import { http } from '@/lib/http-client'
import { assistantApi } from './assistant'

vi.mock('@/lib/http-client', () => ({
  http: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
  unwrap: async (request: Promise<{ data: { data: unknown } }>) => (await request).data.data,
}))

afterEach(() => vi.clearAllMocks())

it('loads completed works by page', async () => {
  vi.mocked(http.get).mockResolvedValueOnce({ data: { data: { items: [{ id: 'work-26' }], has_more: false } } })

  const page = await assistantApi.works('project-1', { created_at: '2026-10-19T00:00:00Z', id: 'work-27' }, 25)

  expect(http.get).toHaveBeenNthCalledWith(1, '/assistant/projects/project-1/works', { params: { before_created_at: '2026-10-19T00:00:00Z', before_id: 'work-27', limit: 25 } })
  expect(page.items[0]?.id).toBe('work-26')
})

it('creates and commits from a durable interaction draft receipt', async () => {
  vi.mocked(http.post)
    .mockResolvedValueOnce({ data: { data: { interaction_id: 'interaction-1', generation: 3, lifecycle: 'needs_confirmation', available_actions: [], drafts: [] } } })
    .mockResolvedValueOnce({ data: { data: { status: 'already_created', task_id: 'task-1' } } })

  const interaction = await assistantApi.createInteraction({ mode_id: 'creation' }, 'interaction-key')
  const result = await assistantApi.commitInteraction(interaction.interaction_id, { draft_id: 'draft-1', revision: 2, idempotency_key: 'commit-1' })

  expect(http.post).toHaveBeenNthCalledWith(1, '/assistant/interactions', { mode_id: 'creation' }, { headers: { 'Idempotency-Key': 'interaction-key' } })
  expect(http.post).toHaveBeenNthCalledWith(2, '/assistant/interactions/interaction-1/commit', { draft_id: 'draft-1', revision: 2, idempotency_key: 'commit-1' }, { timeout: 15 * 60 * 1000 })
  expect(result.status).toBe('already_created')
})

it('commits the exact durable receipt without resolving an old session or another draft', async () => {
  vi.mocked(http.post).mockResolvedValue({ data: { data: { status: 'created', task_id: 'task-1' } } })

  const result = await assistantApi.workspaceCommit('interaction-1', 'creation', {
    draft_id: 'draft-7',
    revision: 4,
    idempotency_key: 'commit-4',
  })

  expect(http.get).not.toHaveBeenCalled()
  expect(http.post).toHaveBeenCalledWith('/assistant/interactions/interaction-1/commit', {
    draft_id: 'draft-7',
    revision: 4,
    idempotency_key: 'commit-4',
  }, { timeout: 15 * 60 * 1000 })
  expect(result.task_id).toBe('task-1')
})

it('answers an interaction using the interaction-scoped answer endpoint', async () => {
  vi.mocked(http.post).mockResolvedValue({ data: { data: { accepted: true } } })

  await assistantApi.answerInteractionQuestion('interaction-1', 'call-1', {
    mode_id: 'creation',
    generation: 3,
    attempt_id: 'answer-1',
    answers: [{ id: 'choice', selected: ['yes'] }],
  })

  expect(http.post).toHaveBeenCalledWith('/assistant/interactions/interaction-1/answers', {
    call_id: 'call-1',
    mode_id: 'creation',
    generation: 3,
    attempt_id: 'answer-1',
    answers: [{ id: 'choice', selected: ['yes'] }],
  }, { timeout: 15 * 60 * 1000 })
})

it('requests runtime restoration before continuing from a durable draft', async () => {
  vi.mocked(http.post).mockResolvedValue({ data: { data: { interaction_id: 'interaction-1', generation: 4, lifecycle: 'active', available_actions: [], drafts: [] } } })

  await assistantApi.recoverInteraction('interaction-1', {
    generation: 4,
    recovery_context: {
      version: 1,
      interaction_id: 'interaction-1',
      mode_id: 'profile',
      messages: [{ role: 'user', text: '之前确定了乌龙茶品鉴装的方向' }],
      questions: [],
    },
    continue_conversation: true,
  })

  expect(http.post).toHaveBeenCalledWith('/assistant/interactions/interaction-1/recover', expect.objectContaining({
    generation: 4,
    continue_conversation: true,
    recovery_context: expect.objectContaining({ interaction_id: 'interaction-1' }),
  }), { timeout: 15 * 60 * 1000 })
})
