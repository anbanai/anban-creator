import { describe, expect, it } from 'vitest'

import { prepareReusableInputAttachments } from './input-attachment-submit'

describe('prepareReusableInputAttachments', () => {
  it('preserves finalized asset-backed attachments without requiring a URL or upload key', () => {
    const attachment = {
      type: 'image' as const,
      asset_id: '22222222-2222-4222-8222-222222222222',
      file_name: 'reference.png',
      content_type: 'image/png',
      size: 9,
      instruction: '保留主体构图',
    }

    expect(prepareReusableInputAttachments([attachment])).toEqual({
      attachments: [attachment],
    })
  })
})
