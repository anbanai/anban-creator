import { afterEach, describe, expect, it, vi } from 'vitest'
import { http } from '@/lib/http-client'
import type { Task } from '@/types'
import { aiEntryApi } from './ai-entry'

describe('aiEntryApi', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('posts explicit creation parameters and unwraps the task collection', async () => {
    const tasks = [{ id: 'task-1' }, { id: 'task-2' }] as Task[]
    const post = vi.spyOn(http, 'post').mockResolvedValue({
      data: { code: 0, msg: 'success', data: { status: 'created', tasks } },
    })
    const request = {
      channel: 'studio',
      project_id: 'project-1',
      execution_profile: 'effective' as const,
      text: '写两篇文章',
      quantity: 2,
      image_ratio: '3:4',
      image_capability_key: 'professional',
    }

    const result = await aiEntryApi.submit(request)

    expect(post).toHaveBeenCalledWith('/ai-entry/submit', request)
    expect(result.tasks).toEqual(tasks)
    expect(result).not.toHaveProperty('task')
  })
})
