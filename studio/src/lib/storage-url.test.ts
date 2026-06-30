import { describe, expect, it } from 'vitest'
import { normalizeStorageUrl } from './storage-url'

describe('normalizeStorageUrl', () => {
  it('routes absolute API file URLs through the authenticated file proxy', () => {
    expect(normalizeStorageUrl('https://app.example.com/api/v1/files/uploads/references/u1/ref.png')).toBe('/files/uploads/references/u1/ref.png')
  })
})
