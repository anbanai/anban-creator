import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import OnlinePortraitDelivery from './OnlinePortraitDelivery'
import { api } from '@/lib/api'
import { portraitToProjectProfile } from '@/lib/portrait-project-handoff'
import { portraitKey } from '@/lib/portrait-online-save'
import type { PortraitCandidate } from '@/lib/portrait-chat-contract'
import type { ProjectProfile } from '@/types'

vi.mock('@/lib/api', () => ({ api: {
  auth: { me: vi.fn() },
  projects: { create: vi.fn(), getAccountProfile: vi.fn(), confirmAccountProfile: vi.fn() },
  tasks: { create: vi.fn() },
} }))

const text = '我做花艺，面向上班族，用公众号分享；第一篇讲周末插花。'
const fact = { text, certainty: 'stated' as const, evidence: [{ messageId: 'u-1', quote: text }] }
const candidate: PortraitCandidate = { reply: '请核对', name: null, summary: null, facets: { identity: fact, audience: fact, platforms: fact, style: null, preferences: null, experience: null }, creationIdea: fact }
const messages = [{ id: 'u-1', role: 'user' as const, text }]
const current = { schema_version: 1, version: 0, status: 'draft', initialization_status: 'not_started' } as ProjectProfile
const confirmed = { ...portraitToProjectProfile(candidate, current), status: 'confirmed' as const, version: 1 }

function Location() {
  const location = useLocation()
  return <output data-testid="location">{location.pathname}{location.search}</output>
}

function renderDelivery(value = candidate) {
  return render(<MemoryRouter initialEntries={['/projects']}><Routes><Route path="*" element={<><Location /><OnlinePortraitDelivery candidate={value} messages={messages} /></>} /></Routes></MemoryRouter>)
}

beforeEach(() => {
  vi.resetAllMocks()
  sessionStorage.clear()
  vi.mocked(api.auth.me).mockResolvedValue({ id: 'user-1', nickname: '测试账号' } as never)
  vi.mocked(api.projects.create).mockResolvedValue({ project: { id: 'p-1' } } as never)
  vi.mocked(api.projects.getAccountProfile).mockResolvedValue(current)
  vi.mocked(api.projects.confirmAccountProfile).mockResolvedValue(confirmed)
})

async function save() {
  await screen.findByText('当前账号：测试账号')
  fireEvent.click(screen.getByRole('button', { name: '保存当前画像并继续' }))
  await screen.findByRole('button', { name: '进入创作模式' })
}

describe('online portrait delivery', () => {
  it('saves stable portrait data and hands a one-time idea to creation mode', async () => {
    renderDelivery()
    await save()

    fireEvent.click(screen.getByRole('button', { name: '进入创作模式' }))

    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/chat?assistant_session=1&mode=creation&project_id=p-1'))
    expect(api.projects.confirmAccountProfile).toHaveBeenCalledWith('p-1', expect.not.objectContaining({ creationIdea: expect.anything() }))
    expect(sessionStorage.getItem('anban:portrait-creation-handoff:p-1')).toContain('第一篇讲周末插花')
    expect(api.tasks.create).not.toHaveBeenCalled()
  })

  it('hands off a separately entered idea when the portrait has none', async () => {
    const partial = { ...candidate, facets: { ...candidate.facets, audience: null, platforms: null }, creationIdea: null }
    const partialSaved = { ...portraitToProjectProfile(partial, current), status: 'confirmed' as const, version: 1 }
    vi.mocked(api.projects.confirmAccountProfile).mockResolvedValue(partialSaved)
    renderDelivery(partial)
    await save()

    fireEvent.change(screen.getByRole('textbox', { name: '这次想创作什么' }), { target: { value: '给养花新手写一篇插花指南' } })
    fireEvent.click(screen.getByRole('button', { name: '进入创作模式' }))

    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('mode=creation'))
    expect(sessionStorage.getItem('anban:portrait-creation-handoff:p-1')).toContain('给养花新手写一篇插花指南')
    expect(api.tasks.create).not.toHaveBeenCalled()
  })

  it('does not create a task as part of saving a portrait', async () => {
    renderDelivery()
    await save()
    expect(screen.queryByRole('button', { name: '生成第一篇作品' })).not.toBeInTheDocument()
    expect(api.tasks.create).not.toHaveBeenCalled()
  })

  it('preserves an older ambiguous task submission receipt for manual reconciliation', async () => {
    sessionStorage.setItem('anban:portrait-online:user-1', JSON.stringify({
      userId: 'user-1',
      portraitKey: portraitKey(candidate),
      phase: 'task_creating',
      projectId: 'p-1',
      profileVersion: 1,
    }))
    renderDelivery()

    expect(await screen.findByRole('alert')).toHaveTextContent('作品任务提交结果未确认')
    expect(screen.getByRole('link', { name: '打开任务列表' })).toHaveAttribute('href', '/tasks')
    expect(screen.getByRole('button', { name: '保存更新后的画像' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: '进入创作模式' })).not.toBeInTheDocument()
    expect(api.projects.confirmAccountProfile).not.toHaveBeenCalled()
    expect(api.tasks.create).not.toHaveBeenCalled()
  })
})
