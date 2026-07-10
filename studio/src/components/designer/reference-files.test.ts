import { describe, expect, it } from 'vitest'
import {
  admitReferenceFiles,
  referenceFileKey,
} from './reference-files'

function createFile(
  name: string,
  type: string,
  content = 'x',
  lastModified = 1_700_000_000_000,
) {
  return new File([content], name, { lastModified, type })
}

describe('admitReferenceFiles', () => {
  it('accepts image files in stable order', () => {
    const first = createFile('first.png', 'image/png')
    const second = createFile('second.webp', 'image/webp')

    const result = admitReferenceFiles([], [first, second], 16)

    expect(result).toEqual({
      files: [first, second],
      accepted: 2,
      rejectedNonImages: 0,
      rejectedDuplicates: 0,
      rejectedOverflow: 0,
    })
  })

  it('accepts recognized image extensions with empty MIME and rejects other non-images', () => {
    const nonImage = createFile('notes.txt', 'text/plain')
    const uppercaseJpeg = createFile('photo.JPEG', '')
    const unknownExtension = createFile('asset.unknown', '')
    const missingExtension = createFile('png', '')

    const result = admitReferenceFiles(
      [],
      [nonImage, uppercaseJpeg, unknownExtension, missingExtension],
      16,
    )

    expect(result).toEqual({
      files: [uppercaseJpeg],
      accepted: 1,
      rejectedNonImages: 3,
      rejectedDuplicates: 0,
      rejectedOverflow: 0,
    })
  })

  it('deduplicates against current files and within the incoming batch using the reference file key', () => {
    const current = createFile('current.png', 'image/png', 'current', 100)
    const currentDuplicate = createFile('current.png', 'image/png', 'current', 100)
    const incoming = createFile('incoming.jpg', 'image/jpeg', 'incoming', 200)
    const incomingDuplicate = createFile('incoming.jpg', 'image/jpeg', 'incoming', 200)

    expect(referenceFileKey(current)).toBe('current.png\u00007\u0000100\u0000image/png')
    expect(referenceFileKey(currentDuplicate)).toBe(referenceFileKey(current))
    expect(referenceFileKey(incomingDuplicate)).toBe(referenceFileKey(incoming))

    const result = admitReferenceFiles(
      [current],
      [currentDuplicate, incoming, incomingDuplicate],
      16,
    )

    expect(result).toEqual({
      files: [current, incoming],
      accepted: 1,
      rejectedNonImages: 0,
      rejectedDuplicates: 2,
      rejectedOverflow: 0,
    })
  })

  it('fills only the remaining provider capacity and reports overflow', () => {
    const current = createFile('current.png', 'image/png')
    const first = createFile('first.png', 'image/png')
    const second = createFile('second.png', 'image/png')
    const overflow = createFile('overflow.png', 'image/png')

    const result = admitReferenceFiles(
      [current],
      [first, second, overflow],
      3,
    )

    expect(result).toEqual({
      files: [current, first, second],
      accepted: 2,
      rejectedNonImages: 0,
      rejectedDuplicates: 0,
      rejectedOverflow: 1,
    })
  })

  it('keeps current files unchanged and reports all incoming files as overflow when full', () => {
    const first = createFile('first.png', 'image/png')
    const second = createFile('second.png', 'image/png')
    const overflowFirst = createFile('overflow-first.png', 'image/png')
    const overflowSecond = createFile('overflow-second.png', 'image/png')
    const currentFiles = [first, second]

    const result = admitReferenceFiles(
      currentFiles,
      [overflowFirst, overflowSecond],
      2,
    )

    expect(result).toEqual({
      files: currentFiles,
      accepted: 0,
      rejectedNonImages: 0,
      rejectedDuplicates: 0,
      rejectedOverflow: 2,
    })
  })

  it('independently counts non-images, duplicates, and overflow in a mixed batch', () => {
    const current = createFile('current.png', 'image/png', 'current', 100)
    const nonImage = createFile('notes.txt', 'text/plain')
    const currentDuplicate = createFile('current.png', 'image/png', 'current', 100)
    const accepted = createFile('accepted.png', 'image/png', 'accepted', 200)
    const incomingDuplicate = createFile('accepted.png', 'image/png', 'accepted', 200)
    const overflow = createFile('overflow.png', 'image/png')

    const result = admitReferenceFiles(
      [current],
      [nonImage, currentDuplicate, accepted, incomingDuplicate, overflow],
      2,
    )

    expect(result).toEqual({
      files: [current, accepted],
      accepted: 1,
      rejectedNonImages: 1,
      rejectedDuplicates: 2,
      rejectedOverflow: 1,
    })
  })
})
