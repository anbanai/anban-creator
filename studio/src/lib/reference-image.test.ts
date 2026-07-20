import { describe, expect, it } from 'vitest'

import {
  referenceSelectionFromUpload,
  referenceSelectionFromValue,
} from '@/lib/reference-image'

describe('reference image identity', () => {
  it('submits a session without its preview URL', () => {
    const upload = {
      uploadSessionId: 'session-1',
      previewUrl: 'blob:preview',
    }

    const selection = referenceSelectionFromUpload(upload)

    expect(selection).toEqual({ upload_session_id: 'session-1' })
    expect(JSON.stringify(selection)).not.toContain('preview')
  })

  it('submits an existing asset by asset id', () => {
    const selection = referenceSelectionFromValue({
      asset_id: 'asset-1',
      file_name: 'ref.png',
      content_type: 'image/png',
      size: 3,
      download_url: 'https://signed.example/ref.png',
      download_expires_at: '2026-07-17T09:15:00Z',
    })

    expect(selection).toEqual({ asset_id: 'asset-1' })
    expect(JSON.stringify(selection)).not.toContain('signed.example')
  })

  it('preserves a pending session discriminator and supports clearing', () => {
    expect(referenceSelectionFromValue({ upload_session_id: 'session-2' }))
      .toEqual({ upload_session_id: 'session-2' })
    expect(referenceSelectionFromValue(null)).toBeNull()
  })
})
