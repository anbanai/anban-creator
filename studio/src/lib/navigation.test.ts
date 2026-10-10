import { Sparkles } from 'lucide-react'
import { describe, expect, it } from 'vitest'

import {
  adminNavItems,
  allNavItems,
  mvpNavItems,
  workspaceNavItems,
  visibleNavItems,
} from './navigation'

describe('navigation IA', () => {
  it('defines the MVP navigation in approved order', () => {
    expect(mvpNavItems.map((item) => item.label)).toEqual([
      'AI助手',
      '新手引导',
      '项目',
      '任务',
      '计划',
      '钱包',
      '插件',
      '设置',
      '内容分析',
      '热点雷达',
    ])
    expect(mvpNavItems[0]?.icon).toBe(Sparkles)
    expect(mvpNavItems.map((item) => item.to)).not.toContain('/timeline')
    expect(mvpNavItems.map((item) => item.to)).not.toContain('/usage')
    expect(workspaceNavItems.map((item) => item.to)).toEqual([
      '/',
      '/projects/new/interview',
      '/projects',
      '/tasks',
      '/plans',
      '/timeline',
      '/billing',
      '/plugins',
      '/settings',
      '/content-analytics',
      '/trends',
    ])
    expect(workspaceNavItems.find((item) => item.to === '/timeline')?.adminOnly).toBe(true)
  })

  it('defines administrator navigation in approved order', () => {
    expect(adminNavItems.map((item) => item.label)).toEqual([
      '模板库',
      '种草笔记账号',
    ])
    expect(adminNavItems.every((item) => item.adminOnly)).toBe(true)
  })

  it('filters the combined navigation by administrator access', () => {
    expect(allNavItems.map((item) => item.to)).toEqual([
      '/',
      '/projects/new/interview',
      '/projects',
      '/tasks',
      '/plans',
      '/timeline',
      '/billing',
      '/plugins',
      '/settings',
      '/content-analytics',
      '/trends',
      '/templates',
      '/admin/seednote',
    ])
    expect(visibleNavItems(allNavItems, false)).toEqual(mvpNavItems)
    expect(visibleNavItems(allNavItems, true)).toEqual([
      ...workspaceNavItems,
      ...adminNavItems,
    ])
  })
})
