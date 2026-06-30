import { screen, waitFor, fireEvent } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import PlansPage from './PlansPage'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'

const { errorMock } = vi.hoisted(() => ({ errorMock: vi.fn() }))

vi.mock('sonner', () => ({ toast: { error: errorMock, success: vi.fn() } }))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  const { mockPlans, mockProjects } =
    await vi.importActual<typeof import('@/test/mocks/handlers')>('@/test/mocks/handlers')
  return {
    ...actual,
    api: {
      ...actual.api,
      plans: {
        ...actual.api.plans,
        list: vi.fn().mockResolvedValue(mockPlans),
        pause: vi.fn(),
        resume: vi.fn(),
        delete: vi.fn(),
      },
      projects: {
        ...actual.api.projects,
        list: vi.fn().mockResolvedValue(mockProjects),
      },
      credits: {
        ...actual.api.credits,
        pricing: vi.fn().mockResolvedValue({ task_costs: {} }),
        balance: vi.fn().mockResolvedValue({ balance: 0 }),
      },
    },
  }
})

describe('PlansPage — mutation failure feedback (no silent failure)', () => {
  beforeEach(() => {
    errorMock.mockClear()
  })

  it('shows an error toast when pausing a plan fails (was previously silent)', async () => {
    // pause is wired through useSubmitLock().submit(mutateAsync) with no catch —
    // before this fix, a rejection surfaced nothing to the user. Now the server's
    // reason (response.data.msg) is surfaced via getApiErrorMessage.
    vi.mocked(api.plans.pause).mockRejectedValueOnce({
      response: { data: { msg: '排期冲突，请检查现有计划' } },
    })

    render(<PlansPage />)

    // mockPlans[0] is an active plan → the 暂停 button renders.
    const pauseBtn = await screen.findByRole('button', { name: '暂停' })
    fireEvent.click(pauseBtn)

    await waitFor(() => {
      expect(errorMock).toHaveBeenCalledWith('排期冲突，请检查现有计划')
    })
  })

  it('does not offer e-commerce as a plan content type', async () => {
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '新建计划' }))
    const [, typeSelect] = screen.getAllByRole('combobox')
    fireEvent.click(typeSelect)

    expect(await screen.findByRole('option', { name: '公众号文章' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: '种草笔记' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: '电商出图' })).not.toBeInTheDocument()
  })
})
