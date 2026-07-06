import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProjectsPage from './ProjectsPage'
import { api } from '@/lib/api'
import { render } from '@/test/test-utils'

const { errorMock } = vi.hoisted(() => ({ errorMock: vi.fn() }))

vi.mock('sonner', () => ({ toast: { error: errorMock, success: vi.fn() } }))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  const { mockPlatformConfigs, mockProjectDetail, mockProjects } =
    await vi.importActual<typeof import('@/test/mocks/handlers')>('@/test/mocks/handlers')
  return {
    ...actual,
    api: {
      ...actual.api,
      projects: {
        ...actual.api.projects,
        list: vi.fn().mockResolvedValue(mockProjects),
        stats: vi.fn().mockResolvedValue({ 'ch-1': mockProjectDetail.stats }),
        platformConfigs: vi.fn().mockResolvedValue(mockPlatformConfigs),
        delete: vi.fn(),
      },
      imageModels: {
        ...actual.api.imageModels,
        list: vi.fn().mockResolvedValue({ items: [], tier: 'pro' }),
      },
      video: {
        ...actual.api.video,
        models: vi.fn().mockResolvedValue({ items: [] }),
      },
    },
  }
})

describe('ProjectsPage deletion feedback', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.history.pushState({}, '', '/projects')
  })

  it('shows the server archive guidance when deleting a project fails with associated work', async () => {
    vi.mocked(api.projects.delete).mockRejectedValueOnce({
      response: {
        data: {
          msg: 'cannot delete project with 13 associated tasks; archive it instead',
        },
      },
    })

    render(<ProjectsPage />)

    fireEvent.click(await screen.findByRole('button', { name: '删除项目' }))
    fireEvent.click(await screen.findByRole('button', { name: '删除' }))

    await waitFor(() => {
      expect(errorMock).toHaveBeenCalledWith('cannot delete project with 13 associated tasks; archive it instead')
    })
  })
})
