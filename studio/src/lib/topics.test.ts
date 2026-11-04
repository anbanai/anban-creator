import { describe, expect, it } from 'vitest'
import { topicFilters, topicUsage, topicCreationHref } from './topics'
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
})
