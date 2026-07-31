import { describe, expect, it } from 'vitest'

import type { PromptAttachment } from '@/types/input-attachment'
import { hasOrdinalMaterialReference, materialOrdinals } from './attachment-order'

const attachment = (type: PromptAttachment['type'], id: string): PromptAttachment => ({
  id,
  type,
  fileName: `${id}.dat`,
  size: 1,
  status: 'uploaded',
  progress: 100,
})
describe('attachment ordering helpers', () => {
  it('assigns global and per-type ordinals in array order', () => {
    expect(materialOrdinals([
      attachment('document', 'doc'),
      attachment('image', 'a'),
      attachment('image', 'b'),
    ])).toEqual([
      { index: 1, typeIndex: 1, label: '文档 1' },
      { index: 2, typeIndex: 1, label: '图 1' },
      { index: 3, typeIndex: 2, label: '图 2' },
    ])
  })

  it('detects ordinal material language without flagging generic prompts', () => {
    expect(hasOrdinalMaterialReference('用第一张图和附件 3')).toBe(true)
    expect(hasOrdinalMaterialReference('按品牌风格生成')).toBe(false)
    expect(hasOrdinalMaterialReference('请处理第 2 个素材')).toBe(true)
  })
})
