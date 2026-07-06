import { describe, expect, it } from 'vitest'

import {
  allNavItems,
  assetItems,
  automationItems,
  businessItems,
  connectSettingItems,
  creationItems,
  todayItems,
} from './navigation'

describe('navigation IA', () => {
  it('groups Studio navigation by creator outcomes', () => {
    expect(todayItems.map((item) => item.label)).toEqual(['今日'])
    expect(creationItems.map((item) => item.label)).toEqual(['项目', '任务', '设计师'])
    expect(automationItems.map((item) => item.label)).toEqual(['计划', '时间轴'])
    expect(assetItems.map((item) => item.label)).toEqual(['模板库'])
    expect(businessItems.map((item) => item.label)).toEqual(['积分', '用量'])
    expect(connectSettingItems.map((item) => item.label)).toEqual(['Claude Code', 'OpenClaw', 'Codex', '设置'])
  })

  it('keeps route compatibility while renaming the default workspace to today', () => {
    expect(allNavItems.map((item) => item.to)).toContain('/')
    expect(allNavItems.find((item) => item.to === '/')?.label).toBe('今日')
    expect(allNavItems.map((item) => item.to)).toEqual(expect.arrayContaining([
      '/projects',
      '/plans',
      '/tasks',
      '/timeline',
      '/settings',
    ]))
  })
})
