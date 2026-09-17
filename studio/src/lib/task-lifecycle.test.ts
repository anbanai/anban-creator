import { describe, expect, it } from 'vitest'

import { lifecycleStageStateLabel, shouldStreamTaskLifecycle, taskStageSummary } from './task-lifecycle'
import type { TaskLifecycle } from '@/types'

describe('task lifecycle helpers', () => {
  it('treats an empty lifecycle snapshot as the planning state', () => {
    const lifecycle = {
      version: 0,
      revision: 0,
      updated_at: '0001-01-01T00:00:00Z',
      stages: null,
    } as unknown as TaskLifecycle

    expect(taskStageSummary({ status: 'running', lifecycle })).toEqual({
      title: '正在制定执行计划',
      state: 'active',
    })
  })

  it('keeps lifecycle streaming after creative completion while publication is unresolved', () => {
    expect(shouldStreamTaskLifecycle({
      status: 'completed',
      lifecycle: {
        version: 1,
        revision: 3,
        updated_at: '2026-09-17T08:00:00Z',
        stages: [
          { id: 'writing', title: '完成创作', source: 'agent', kind: 'work', state: 'complete' },
          { id: 'system_publication', title: '正式发布', source: 'server', kind: 'publication', state: 'pending' },
        ],
      },
    })).toBe(true)
    expect(shouldStreamTaskLifecycle({
      status: 'completed',
      lifecycle: {
        version: 1,
        revision: 4,
        updated_at: '2026-09-17T08:01:00Z',
        stages: [
          { id: 'writing', title: '完成创作', source: 'agent', kind: 'work', state: 'complete' },
          { id: 'system_publication', title: '正式发布', source: 'server', kind: 'publication', state: 'complete' },
        ],
      },
    })).toBe(false)
  })

  it('reports the cancelled execution point instead of a later skipped stage', () => {
    expect(taskStageSummary({
      status: 'cancelled',
      lifecycle: {
        version: 1,
        revision: 5,
        updated_at: '2026-09-17T08:00:00Z',
        stages: [
          { id: 'research', title: '研究素材', source: 'agent', kind: 'work', state: 'complete' },
          { id: 'writing', title: '撰写内容', source: 'agent', kind: 'work', state: 'cancelled' },
          { id: 'review', title: '质量复核', source: 'agent', kind: 'work', state: 'skipped' },
        ],
      },
    })).toEqual({ title: '撰写内容', state: 'cancelled' })
    expect(lifecycleStageStateLabel('cancelled')).toBe('已取消')
    expect(lifecycleStageStateLabel('skipped')).toBe('已跳过')
  })

  it('keeps the last work stage visible when final delivery fails after work completes', () => {
    expect(taskStageSummary({
      status: 'failed',
      lifecycle: {
        version: 1,
        revision: 6,
        updated_at: '2026-09-17T08:00:00Z',
        stages: [
          { id: 'writing', title: '完成创作', source: 'agent', kind: 'work', state: 'complete' },
          { id: 'system_draft', title: '创建公众号草稿', source: 'server', kind: 'draft', state: 'skipped' },
          { id: 'system_publication', title: '正式发布', source: 'server', kind: 'publication', state: 'skipped' },
        ],
      },
    })).toEqual({ title: '完成创作', state: 'complete' })
  })
})
