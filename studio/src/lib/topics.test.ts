import { describe, expect, it } from 'vitest'
import { topicFilters, topicUsage, topicCreationHref, topicOutputHref, topicTaskTypes } from './topics'
import { taskFormValuesToRequest, createTaskFormDefaults } from './task-form'
import type { ContentAsset } from '@/types'
const asset: ContentAsset = { asset_id: '4b073aec-e9cf-4f18-b8b8-93b04d07aa93', user_id: 'u', title: '选题标题', core_question: '从什么角度写', status: 'candidate', lifecycle: 'evergreen', created_at: '', updated_at: '', project_uses: [{ project_id: 'a', project_name: '甲', status: 'used' }, { project_id: 'b', project_name: '乙', status: 'unused' }], outputs: [{ task_id: 's', project_id: 'b', type: 'content-script', title: '脚本', status: 'completed' }] }
describe('topic library contracts', () => {
  it('keeps all personas as default and validates pagination', () => {
    expect(topicFilters(new URLSearchParams())).toMatchObject({ project_id: undefined, page: 1, offset: 0 })
    expect(topicFilters(new URLSearchParams('page=-5&usage=invalid'))).toMatchObject({ page: 1, usage: undefined })
    expect(topicFilters(new URLSearchParams('project_id=b&q=测试&usage=unused&page=3'))).toMatchObject({ project_id: 'b', q: '测试', usage: 'unused', offset: 50 })
  })
  it('does not treat a completed script as persona usage', () => {
    expect(topicUsage(asset, 'a')).toBe('used')
    expect(topicUsage(asset, 'b')).toBe('unused')
    expect(topicUsage(asset)).toBe('used')
  })
  it('preserves canonical asset UUID and prompt through the existing creation form', () => {
    const url = new URL(topicCreationHref(asset, 'b'), 'https://studio.test')
    expect(url.pathname).toBe('/tasks/new')
    expect(url.searchParams.get('asset_id')).toBe(asset.asset_id)
    expect(url.searchParams.get('project_id')).toBe('b')
    expect(url.searchParams.get('prompt')).toBe('选题标题\n从什么角度写')
    const request = taskFormValuesToRequest({ ...createTaskFormDefaults(), project_id: 'b', asset_id: asset.asset_id, prompt: url.searchParams.get('prompt')! })
    expect(request).toMatchObject({ asset_id: asset.asset_id, project_id: 'b', prompt: '选题标题\n从什么角度写' })
  })
  it('preserves library context when opening a script output', () => {
    const context = new URLSearchParams('q=keyword&project_id=a&usage=unused&page=2')
    const href = topicOutputHref(asset.asset_id, { job_id: 'script-1', project_id: 'a', type: 'topic-script', title: '脚本', status: 'completed' }, context)
    const url = new URL(href!, 'https://studio.test')
    expect(url.searchParams.get('q')).toBe('keyword')
    expect(url.searchParams.get('project_id')).toBe('a')
    expect(url.searchParams.get('usage')).toBe('unused')
    expect(url.searchParams.get('page')).toBe('2')
    expect(url.searchParams.get('asset_id')).toBe(asset.asset_id)
    expect(url.searchParams.get('script_id')).toBe('script-1')
  })
  it('limits topic-bound task creation to task types admitted by the server', () => {
    expect(topicTaskTypes(['wechat-article', 'montage', 'video-edit', 'seednote'])).toEqual(['wechat-article', 'seednote'])
    expect(topicTaskTypes(['hypit', 'montage'])).toEqual([])
  })
})
