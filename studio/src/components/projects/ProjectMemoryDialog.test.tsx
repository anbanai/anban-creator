import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { render } from '@/test/test-utils'
import { ProjectMemoryDialog } from './ProjectMemoryDialog'
import { projectsApi } from '@/lib/api/projects'

vi.mock('@/lib/api/projects', () => ({
  projectsApi: {
    memory: vi.fn(),
    updateMemory: vi.fn(),
    getAccountProfile: vi.fn(),
    profileAnalysis: vi.fn(),
    refreshProfile: vi.fn(),
    retryProfile: vi.fn(),
    updateProfileDimension: vi.fn(),
  },
}))

function profileProject(overrides: Record<string, unknown> = {}) {
  return {
    id: 'profile-project',
    user_id: 'user-1',
    platform: 'wechat' as const,
    agent_ids: ['wechat-article'],
    name: '公众号人设',
    avatar_url: '',
    profile_url: '',
    keywords: '',
    visual_style: '',
    writer: '',
    theme: '',
    author: '',
    image_ratio: '',
    max_concurrent_tasks: 1,
    status: 'active' as const,
    created_at: '2026-07-01T00:00:00.000Z',
    updated_at: '2026-07-01T00:00:00.000Z',
    ...overrides,
  }
}

function accountProfile(overrides: Record<string, unknown> = {}) {
  const dimension = { content: { summary: '人设定位' }, sources: ['人设资料'], evidence: [], missing_fields: [] }
  return {
    schema_version: 1,
    status: 'confirmed' as const,
    initialization_status: 'ready' as const,
    version: 3,
    dimensions: {
      identity: dimension,
      style: dimension,
      audience: dimension,
      platforms: dimension,
      preferences: dimension,
      memory: dimension,
    },
    analysis_limits: [],
    follow_up_questions: [],
    ...overrides,
  }
}

describe('ProjectMemoryDialog', () => {
  beforeEach(() => {
    vi.mocked(projectsApi.memory).mockReset()
    vi.mocked(projectsApi.updateMemory).mockReset()
    vi.mocked(projectsApi.getAccountProfile).mockReset()
    vi.mocked(projectsApi.profileAnalysis).mockReset()
    vi.mocked(projectsApi.refreshProfile).mockReset()
    vi.mocked(projectsApi.retryProfile).mockReset()
    vi.mocked(projectsApi.updateProfileDimension).mockReset()
  })

  it('loads only when opened, prioritizes MEMORY.md, switches files, and blocks images', async () => {
    vi.mocked(projectsApi.memory).mockResolvedValue({
      status: 'ready',
      updated_at: '2026-09-14T12:00:00Z',
      partial: true,
      files: [
        { path: 'MEMORY.md', content: '# Main\n![remote](https://example.com/a.png)', size_bytes: 42, modified_at: '2026-09-14T12:00:00Z', truncated: false },
        { path: 'notes/preferences.md', content: 'second file', size_bytes: 11, modified_at: '2026-09-14T11:00:00Z', truncated: true },
      ],
    })
    render(<ProjectMemoryDialog projectId="project-1" projectName="公众号人设" open={false} onOpenChange={() => {}} />)
    expect(projectsApi.memory).not.toHaveBeenCalled()

    const { rerender } = render(<ProjectMemoryDialog projectId="project-1" projectName="公众号人设" open onOpenChange={() => {}} />)
    expect(await screen.findByRole('heading', { name: 'Main' })).toBeInTheDocument()
    expect(screen.queryByRole('img')).not.toBeInTheDocument()
    expect(screen.getByText('内容已按安全限制截断')).toBeInTheDocument()
    expect(screen.getByText('MEMORY.md', { selector: 'h2' })).toBeInTheDocument()
    expect(screen.getByRole('dialog', { name: '人设记忆' })).toHaveAttribute('data-slot', 'dialog-content')
    expect(screen.getByRole('region', { name: '记忆正文' })).toBeInTheDocument()
    const readingPane = screen.getByRole('region', { name: '记忆正文' })
    readingPane.scrollTop = 400

    fireEvent.click(screen.getByRole('button', { name: 'notes/preferences.md' }))
    expect(screen.getByText('second file')).toBeInTheDocument()
    expect(readingPane.scrollTop).toBe(0)
    expect(screen.getByRole('button', { name: 'notes/preferences.md' })).toHaveAttribute('aria-current', 'true')
    expect(screen.getByText('此文件已截断')).toBeInTheDocument()
    expect(screen.getByText('notes/preferences.md', { selector: 'h2' })).toBeInTheDocument()
    rerender(<div />)
  })

  it('shows the empty state and supports refresh', async () => {
    vi.mocked(projectsApi.memory).mockResolvedValue({ status: 'empty', updated_at: null, partial: false, files: [] })
    render(<ProjectMemoryDialog projectId="project-2" projectName="空人设" open onOpenChange={() => {}} />)
    expect(await screen.findByText('还没有人设记忆')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '刷新人设记忆' }))
    expect(projectsApi.memory).toHaveBeenCalledTimes(2)
  })

  it('supports the compact file selector and close control', async () => {
    const onOpenChange = vi.fn()
    vi.mocked(projectsApi.memory).mockResolvedValue({
      status: 'ready', updated_at: null, partial: false,
      files: [
        { path: 'anban-article/MEMORY.md', content: '人设概要', size_bytes: 12, modified_at: '2026-09-22T03:45:00Z', truncated: false },
        { path: 'notes/style.md', content: '风格偏好', size_bytes: 12, modified_at: '2026-09-22T03:45:00Z', truncated: false },
      ],
    })
    render(<ProjectMemoryDialog projectId="compact" projectName="人设" open onOpenChange={onOpenChange} />)
    expect(await screen.findByText('人设概要')).toBeInTheDocument()
    fireEvent.change(screen.getByRole('combobox', { name: '选择记忆文件' }), { target: { value: 'notes/style.md' } })
    expect(screen.getByText('风格偏好')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '关闭人设记忆' }))
    expect(onOpenChange).toHaveBeenCalledWith(false, expect.anything())
  })
  it('shows a recoverable error and retries without closing the dialog', async () => {
    vi.mocked(projectsApi.memory)
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ status: 'empty', updated_at: null, partial: false, files: [] })
    render(<ProjectMemoryDialog projectId="retry" projectName="人设" open onOpenChange={() => {}} />)
    expect(await screen.findByText('无法读取人设记忆')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('还没有人设记忆')).toBeInTheDocument()
    expect(screen.getByRole('dialog', { name: '人设记忆' })).toBeInTheDocument()
    expect(projectsApi.memory).toHaveBeenCalledTimes(2)
  })

  it('edits an unprotected markdown file through the CAS API and refreshes', async () => {
    vi.mocked(projectsApi.memory)
      .mockResolvedValueOnce({
        status: 'ready', updated_at: null, partial: false,
        files: [{ path: 'notes/style.md', content: '旧风格', size_bytes: 9, modified_at: '2026-09-22T03:45:00Z', truncated: false }],
      })
      .mockResolvedValueOnce({
        status: 'ready', updated_at: null, partial: false,
        files: [{ path: 'notes/style.md', content: '新风格', size_bytes: 9, modified_at: '2026-09-22T03:46:00Z', truncated: false }],
      })
    vi.mocked(projectsApi.updateMemory).mockResolvedValue({
      status: 'ready', updated_at: null, partial: false,
      files: [{ path: 'notes/style.md', content: '新风格', size_bytes: 9, modified_at: '2026-09-22T03:46:00Z', truncated: false }],
    })

    render(<ProjectMemoryDialog projectId="editable" projectName="人设" open onOpenChange={() => {}} />)
    expect(await screen.findByText('旧风格')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    await waitFor(() => expect(screen.getByRole('textbox', { name: '编辑记忆文件' })).toBeInTheDocument())
    const editor = screen.getByRole('textbox', { name: '编辑记忆文件' })
    fireEvent.change(editor, { target: { value: '新风格' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(projectsApi.updateMemory).toHaveBeenCalledWith('editable', {
      path: 'notes/style.md', expected_content: '旧风格', content: '新风格',
    }))
    await waitFor(() => expect(projectsApi.memory).toHaveBeenCalledTimes(2))
    expect(screen.queryByRole('textbox', { name: '编辑记忆文件' })).not.toBeInTheDocument()
    expect(screen.getByText('新风格')).toBeInTheDocument()
  })

  it('does not expose editing for server-managed memory files', async () => {
    vi.mocked(projectsApi.memory).mockResolvedValue({
      status: 'ready', updated_at: null, partial: false,
      files: [{ path: 'MEMORY.md', content: 'managed', size_bytes: 7, modified_at: '2026-09-22T03:45:00Z', truncated: false }],
    })
    render(<ProjectMemoryDialog projectId="managed" projectName="人设" open onOpenChange={() => {}} />)
    expect(await screen.findByText('managed')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '编辑' })).not.toBeInTheDocument()
  })

  it('shows AGENTS.md in the memory list as a server-managed read-only file', async () => {
    vi.mocked(projectsApi.memory).mockResolvedValue({
      status: 'ready', updated_at: null, partial: false,
      files: [{ path: 'AGENTS.md', content: '# 人设规则', size_bytes: 12, modified_at: '2026-09-22T03:45:00Z', truncated: false }],
    })
    render(<ProjectMemoryDialog projectId="agents" projectName="人设" open onOpenChange={() => {}} />)
    expect(await screen.findByRole('heading', { name: '人设规则' })).toBeInTheDocument()
    expect(screen.getAllByText('只读')).toHaveLength(2)
    expect(screen.queryByRole('button', { name: '编辑' })).not.toBeInTheDocument()
  })

  it('loads the profile as part of the combined project memory page', async () => {
    vi.mocked(projectsApi.memory).mockResolvedValue({ status: 'empty', updated_at: null, partial: false, files: [] })
    vi.mocked(projectsApi.getAccountProfile).mockResolvedValue(accountProfile())
    render(<ProjectMemoryDialog projectId="profile-project" projectName="公众号人设" project={profileProject()} open onOpenChange={() => {}} />)

    expect(await screen.findByText('还没有人设记忆')).toBeInTheDocument()
    expect(await screen.findByRole('region', { name: '定位资料' })).toBeInTheDocument()
    expect(projectsApi.getAccountProfile).toHaveBeenCalledWith('profile-project')
  })

  it('renders a partial server profile with null arrays and missing dimensions', async () => {
    const incompleteProfile = {
      ...accountProfile(),
      dimensions: {
        identity: { content: null, sources: null, evidence: null, missing_fields: null },
      },
      analysis_limits: null,
      follow_up_questions: null,
    } as unknown as ReturnType<typeof accountProfile>
    vi.mocked(projectsApi.memory).mockResolvedValue({ status: 'empty', updated_at: null, partial: false, files: [] })
    vi.mocked(projectsApi.getAccountProfile).mockResolvedValue(incompleteProfile)
    render(<ProjectMemoryDialog projectId="partial-profile" projectName="公众号人设" project={profileProject()} open onOpenChange={() => {}} />)

    expect(await screen.findByRole('region', { name: '定位资料' })).toBeInTheDocument()
    expect(screen.getAllByText('还没有足够信息，可以稍后补充。')).toHaveLength(6)
  })

  it('does not show a profile tab for projects without profile support', async () => {
    vi.mocked(projectsApi.memory).mockResolvedValue({ status: 'empty', updated_at: null, partial: false, files: [] })
    render(<ProjectMemoryDialog projectId="legacy" projectName="旧人设" project={profileProject({ platform: 'ecommerce', agent_ids: null })} open onOpenChange={() => {}} onUpdateProfile={vi.fn()} />)

    expect(await screen.findByText('还没有人设记忆')).toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: '人设画像' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '对话更新画像' })).not.toBeInTheDocument()
  })

  it('retries a failed profile analysis from the profile tab', async () => {
    const failedProfile = accountProfile({ initialization_status: 'failed', analysis_task_id: 'analysis-1' })
    vi.mocked(projectsApi.memory).mockResolvedValue({ status: 'empty', updated_at: null, partial: false, files: [] })
    vi.mocked(projectsApi.getAccountProfile).mockResolvedValue(failedProfile)
    vi.mocked(projectsApi.profileAnalysis).mockResolvedValue({ status: 'failed', profile: failedProfile })
    vi.mocked(projectsApi.retryProfile).mockResolvedValue({ task: { id: 'analysis-2', status: 'pending' } as never, profile: accountProfile({ initialization_status: 'queued' }) })
    render(<ProjectMemoryDialog projectId="profile-project" projectName="公众号人设" project={profileProject()} open onOpenChange={() => {}} />)

    expect(await screen.findByText('画像分析失败')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试分析' }))

    await waitFor(() => expect(projectsApi.retryProfile).toHaveBeenCalledWith('profile-project', 3))
  })

  it('saves edited profile dimensions with the current revision', async () => {
    const currentProfile = accountProfile()
    vi.mocked(projectsApi.memory).mockResolvedValue({ status: 'empty', updated_at: null, partial: false, files: [] })
    vi.mocked(projectsApi.getAccountProfile).mockResolvedValue(currentProfile)
    vi.mocked(projectsApi.updateProfileDimension).mockResolvedValue(accountProfile({ version: 4 }))
    render(<ProjectMemoryDialog projectId="profile-project" projectName="公众号人设" project={profileProject()} open onOpenChange={() => {}} />)

    await screen.findByRole('region', { name: '定位资料' })
    fireEvent.click(screen.getByRole('button', { name: '修改定位' }))
    fireEvent.change(screen.getByRole('textbox', { name: '定位 · 概述' }), { target: { value: '新的人设定位' } })
    fireEvent.click(screen.getByRole('button', { name: '保存此维度' }))

    await waitFor(() => expect(projectsApi.updateProfileDimension).toHaveBeenCalledWith('profile-project', 'identity', 3, expect.objectContaining({ content: { summary: '新的人设定位' } })))
  })

  it('opens the conversation update flow from the combined memory page', async () => {
    const onUpdateProfile = vi.fn()
    vi.mocked(projectsApi.memory).mockResolvedValue({ status: 'empty', updated_at: null, partial: false, files: [] })
    vi.mocked(projectsApi.getAccountProfile).mockResolvedValue(accountProfile())
    render(<ProjectMemoryDialog projectId="profile-project" projectName="公众号人设" project={profileProject()} open onOpenChange={() => {}} onUpdateProfile={onUpdateProfile} />)

    const updateButton = await screen.findByRole('button', { name: '对话更新画像' })
    expect(updateButton.closest('[data-slot="dialog-header"]')).not.toBeNull()
    fireEvent.click(updateButton)

    expect(onUpdateProfile).toHaveBeenCalledOnce()
  })

})
