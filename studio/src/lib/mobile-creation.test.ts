import { describe, expect, it } from 'vitest'
import { canUseMobileCreation, mobileCreationHandoff, mobileCreationType, parseMobileTaskDraft } from './mobile-creation'
import { mockProjects } from '@/test/mocks/handlers'

describe('mobile creation handoff', () => {
  it('does not treat a legacy platform as enabled when types are explicitly empty', () => {
    expect(canUseMobileCreation({ ...mockProjects[0], agent_ids: [] }, 'wechat-article')).toBe(false)
    expect(mobileCreationType('unknown')).toBeUndefined()
  })
  it.each(['wechat-article', 'seednote'] as const)('starts a fresh %s conversation with the original brief and identity', type => {
    expect(mobileCreationHandoff('id/with space', type, '保留原文')).toEqual({ to: '/', state: { modeId: 'creation', projectId: 'id/with space', creationType: type, initialMessage: '保留原文', startNew: true } })
  })
  it('keeps white-background instructions in the real ecommerce request draft', () => {
    const handoff = mobileCreationHandoff('p1', 'ecommerce', '红色杯子', 'white')
    const draft = parseMobileTaskDraft(handoff.state, 'p1', 'ecommerce')
    expect(draft?.prompt).toContain('红色杯子')
    expect(draft?.prompt).toContain('纯白背景')
    expect(draft?.selectedModules).toEqual({ main_images: 1 })
    expect(parseMobileTaskDraft(handoff.state, 'another-user-project', 'ecommerce')).toBeUndefined()
    expect(parseMobileTaskDraft(handoff.state, 'p1', 'video-edit')).toBeUndefined()
  })
  it('does not accept unrecognized modules or invalid image quantities from navigation state', () => {
    expect(parseMobileTaskDraft({ mobileDraft: { projectId: 'p1', type: 'ecommerce', prompt: '产品', selectedModules: { main_images: 999, sku_images: 2, arbitrary: 5 } } }, 'p1', 'ecommerce')?.selectedModules).toEqual({ sku_images: 2 })
    expect(parseMobileTaskDraft({ mobileDraft: null }, 'p1', 'ecommerce')).toBeUndefined()
  })
})
