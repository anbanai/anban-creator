import { afterEach, expect, it, vi } from 'vitest'
import { http } from '@/lib/http-client'
import { assistantApi } from './assistant'

vi.mock('@/lib/http-client', () => ({
  http: { get: vi.fn(), post: vi.fn(), delete: vi.fn() },
  unwrap: async (request: Promise<{ data: { data: unknown } }>) => (await request).data.data,
}))

afterEach(() => vi.clearAllMocks())

it('creates Assistant sessions with mode and selected context only', async () => {
  vi.mocked(http.post).mockResolvedValue({ data: { data: { id: 'session-1' } } })

  await assistantApi.createSession({ mode_id: 'review', project_id: 'project-1', content_id: 'work-1' }, 'request-key')

  expect(http.post).toHaveBeenCalledWith('/assistant/sessions', {
    mode_id: 'review',
    project_id: 'project-1',
    content_id: 'work-1',
  }, { headers: { 'Idempotency-Key': 'request-key' } })
  expect(vi.mocked(http.post).mock.calls[0]?.[1]).not.toHaveProperty('agent_id')
  expect(vi.mocked(http.post).mock.calls[0]?.[1]).not.toHaveProperty('preset_id')
})

it('persists a structured checkpoint and commits its server-owned id', async () => {
  vi.mocked(http.post)
    .mockResolvedValueOnce({ data: { data: { id: 'checkpoint-1' } } })
    .mockResolvedValueOnce({ data: { data: { status: 'candidate' } } })

  await assistantApi.checkpoint('session-1', {
    schema_version: '1.0',
    payload: { fields: { goal: '复盘作品表现' } },
    summary: '复盘草稿：作品 A',
    status: 'ready',
  })
  await assistantApi.commit('session-1', { checkpoint_id: 'checkpoint-1', checkpoint_digest: 'saved-payload-digest', idempotency_key: 'commit-key' })

  expect(http.post).toHaveBeenNthCalledWith(1, '/assistant/sessions/session-1/checkpoint', {
    schema_version: '1.0',
    payload: { fields: { goal: '复盘作品表现' } },
    summary: '复盘草稿：作品 A',
    status: 'ready',
  }, { timeout: 15 * 60 * 1000 })
  expect(http.post).toHaveBeenNthCalledWith(2, '/assistant/sessions/session-1/commit', {
    checkpoint_id: 'checkpoint-1', checkpoint_digest: 'saved-payload-digest',
    idempotency_key: 'commit-key',
  }, { timeout: 15 * 60 * 1000 })
})

it('loads completed works by page and retrieves only the selected scope checkpoint', async () => {
  vi.mocked(http.get)
    .mockResolvedValueOnce({ data: { data: { items: [{ id: 'work-26' }], has_more: false } } })
    .mockResolvedValueOnce({ data: { data: { id: 'checkpoint-1', summary: '草稿', status: 'draft', updatedAt: '' } } })

  const page = await assistantApi.works('project-1', { created_at: '2026-10-19T00:00:00Z', id: 'work-27' }, 25)
  const checkpoint = await assistantApi.latestCheckpoint({ mode_id: 'review', project_id: 'project-1', content_id: 'work-26' })

  expect(http.get).toHaveBeenNthCalledWith(1, '/assistant/projects/project-1/works', { params: { before_created_at: '2026-10-19T00:00:00Z', before_id: 'work-27', limit: 25 } })
  expect(http.get).toHaveBeenNthCalledWith(2, '/assistant/checkpoint', { params: { mode_id: 'review', project_id: 'project-1', content_id: 'work-26' } })
  expect(page.items[0]?.id).toBe('work-26')
  expect(checkpoint?.id).toBe('checkpoint-1')
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

it('answers an interaction using the interaction-scoped answer endpoint', async () => {
  vi.mocked(http.post).mockResolvedValue({ data: { data: { accepted: true } } })

  await assistantApi.answerInteractionQuestion('interaction-1', 'call-1', {
    generation: 3,
    attempt_id: 'answer-1',
    answers: [{ id: 'choice', selected: ['yes'] }],
  })

  expect(http.post).toHaveBeenCalledWith('/assistant/interactions/interaction-1/answers', {
    call_id: 'call-1',
    generation: 3,
    attempt_id: 'answer-1',
    answers: [{ id: 'choice', selected: ['yes'] }],
  }, { timeout: 15 * 60 * 1000 })
})
