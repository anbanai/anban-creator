import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import type { SSEEvent } from '@/lib/sse'

// Mock the sse module
vi.mock('@/lib/sse', () => ({
  streamTaskProgress: vi.fn(),
}))

// Mock @tanstack/react-query's useQueryClient
const mockInvalidateQueries = vi.fn()
vi.mock('@tanstack/react-query', () => ({
  useQueryClient: () => ({
    invalidateQueries: mockInvalidateQueries,
  }),
}))

import { useTaskSSE } from '@/hooks/useTaskSSE'
import { streamTaskProgress } from '@/lib/sse'

const mockStreamTaskProgress = vi.mocked(streamTaskProgress)

describe('useTaskSSE', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers({ shouldAdvanceTime: true })
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('returns disconnected state when disabled', () => {
    const { result } = renderHook(() =>
      useTaskSSE({ taskId: 'task-1', token: 'token', enabled: false })
    )

    expect(result.current.logs).toEqual([])
    expect(result.current.error).toBeNull()
    expect(result.current.isConnected).toBe(false)
  })

  it('returns disconnected state when no taskId', () => {
    const { result } = renderHook(() =>
      useTaskSSE({ taskId: undefined, token: 'token', enabled: true })
    )

    expect(result.current.logs).toEqual([])
    expect(result.current.error).toBeNull()
    expect(result.current.isConnected).toBe(false)
  })

  it('returns disconnected state when no token', () => {
    const { result } = renderHook(() =>
      useTaskSSE({ taskId: 'task-1', token: undefined, enabled: true })
    )

    expect(result.current.logs).toEqual([])
    expect(result.current.error).toBeNull()
    expect(result.current.isConnected).toBe(false)
  })

  it('aborts connection on unmount', async () => {
    // Create an async generator that hangs (never yields)
    async function* hangingGenerator(): AsyncGenerator<SSEEvent> {
      // Never yields - simulates a connection that stays open
      await new Promise(() => {})
    }
    mockStreamTaskProgress.mockReturnValue(hangingGenerator())

    const { unmount } = renderHook(() =>
      useTaskSSE({ taskId: 'task-1', token: 'token', enabled: true })
    )

    // The connection was attempted
    expect(mockStreamTaskProgress).toHaveBeenCalledWith(
      'task-1',
      'token',
      expect.objectContaining({ aborted: false })
    )

    unmount()

    // Allow cleanup effects to run
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
  })

  it('sets error when connection fails after retries', async () => {
    const error = new Error('SSE connection failed: 401')

    // Create a mock async generator that throws immediately
    async function* failGenerator(): AsyncGenerator<SSEEvent> {
      throw error
    }
    mockStreamTaskProgress.mockImplementation(() => failGenerator())

    const { result } = renderHook(() =>
      useTaskSSE({ taskId: 'task-1', token: 'token', enabled: true })
    )

    // Wait for retries to exhaust (3 retries with 2s, 4s, 6s delays = ~12s total)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(20000)
    })

    await waitFor(() => {
      expect(result.current.error).toBe('连接断开，正在刷新任务状态...')
    })
  })

  it('processes progress events from SSE stream', async () => {
    async function* eventGenerator(): AsyncGenerator<SSEEvent> {
      yield { event: 'progress', data: JSON.stringify({ progress: 50, message: 'Processing...' }) }
    }
    mockStreamTaskProgress.mockImplementation(() => eventGenerator())

    const { result } = renderHook(() =>
      useTaskSSE({ taskId: 'task-1', token: 'token', enabled: true })
    )

    await waitFor(() => {
      expect(result.current.logs).toContain('[50%] Processing...')
    })
  })

  it('processes string progress events from SSE stream', async () => {
    async function* eventGenerator(): AsyncGenerator<SSEEvent> {
      yield { event: 'progress', data: 'Simple progress message' }
    }
    mockStreamTaskProgress.mockImplementation(() => eventGenerator())

    const { result } = renderHook(() =>
      useTaskSSE({ taskId: 'task-1', token: 'token', enabled: true })
    )

    await waitFor(() => {
      expect(result.current.logs).toContain('Simple progress message')
    })
  })

  it('processes completed events and invalidates queries', async () => {
    async function* eventGenerator(): AsyncGenerator<SSEEvent> {
      yield { event: 'completed', data: '{}' }
    }
    mockStreamTaskProgress.mockImplementation(() => eventGenerator())

    const { result } = renderHook(() =>
      useTaskSSE({ taskId: 'task-1', token: 'token', enabled: true })
    )

    await waitFor(() => {
      expect(result.current.logs.some(l => l.includes('任务完成'))).toBe(true)
    })

    expect(mockInvalidateQueries).toHaveBeenCalledWith({ queryKey: ['task', 'task-1'] })
    expect(mockInvalidateQueries).toHaveBeenCalledWith({ queryKey: ['task-files', 'task-1'] })
  })

  it('abort() function is callable', () => {
    const { result } = renderHook(() =>
      useTaskSSE({ taskId: 'task-1', token: 'token', enabled: false })
    )

    expect(typeof result.current.abort).toBe('function')
    // Should not throw
    result.current.abort()
  })
})
