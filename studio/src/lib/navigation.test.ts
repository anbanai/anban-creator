import { Sparkles } from 'lucide-react'
import { describe, expect, it } from 'vitest'

import {
  adminNavItems,
  allNavItems,
  mvpNavItems,
  visibleNavItems,
} from './navigation'

describe('navigation IA', () => {
  it('defines the MVP navigation in approved order', () => {
    expect(mvpNavItems.map((item) => item.label)).toEqual([
      'AI助手',
      '项目',
      '任务',
      '计划',
      '钱包',
      '插件',
      '设置',
    ])
    expect(mvpNavItems[0]?.icon).toBe(Sparkles)
    expect(mvpNavItems.map((item) => item.to)).not.toContain('/timeline')
    expect(mvpNavItems.map((item) => item.to)).not.toContain('/usage')
  })

  it('defines administrator navigation in approved order', () => {
    expect(adminNavItems.map((item) => item.label)).toEqual([
      '模板库',
    ])
    expect(adminNavItems.every((item) => item.adminOnly)).toBe(true)
  })

  it('filters the combined navigation by administrator access', () => {
    expect(allNavItems.map((item) => item.to)).toEqual([
      '/',
      '/projects',
      '/tasks',
      '/plans',
      '/billing',
      '/plugins',
      '/settings',
      '/templates',
    ])
    expect(visibleNavItems(allNavItems, false)).toEqual(mvpNavItems)
    expect(visibleNavItems(allNavItems, true)).toEqual([
      ...mvpNavItems,
      ...adminNavItems,
    ])
  })
})
