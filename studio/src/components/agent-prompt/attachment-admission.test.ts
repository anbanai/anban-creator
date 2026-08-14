import { describe, expect, it } from 'vitest'

import type { PromptAttachment } from '@/types/input-attachment'
import {
  AttachmentRejectionReason,
  GENERAL_AGENT_ATTACHMENT_POLICY,
  admitPromptAttachments,
  classifyPromptAttachment,
} from './attachment-admission'

const MB = 1024 * 1024

function fileOf(
  name: string,
  type: string,
  size = 1,
  lastModified = 1,
) {
  const file = new File(['x'], name, { type, lastModified })
  Object.defineProperty(file, 'size', { value: size })
  return file
}

function currentAttachment(file: File): PromptAttachment {
  return {
    id: `current-${file.name}`,
    type: classifyPromptAttachment(file) ?? 'document',
    file,
    fileName: file.name,
    contentType: file.type,
    size: file.size,
    lastModified: file.lastModified,
    status: 'queued',
    progress: 0,
  }
}

describe('classifyPromptAttachment', () => {
  it.each([
    ['photo', 'image/png', 'image'],
    ['voice.m4a', '', 'audio'],
    ['clip.mov', 'application/octet-stream', 'video'],
    ['report.pdf', 'application/octet-stream', 'document'],
    ['notes.md', '', 'text'],
    ['data.json', 'application/json', 'document'],
    ['README', 'text/plain', 'text'],
  ] as const)('classifies %s from MIME and safe extension fallback', (name, mime, expected) => {
    expect(classifyPromptAttachment(fileOf(name, mime))).toBe(expected)
  })

  it.each([
    ['report.pdf', 'image/png'],
    ['photo.png', 'application/pdf'],
    ['payload.png', 'image/svg+xml'],
    ['photo.jpg', 'image/png'],
    ['notes.txt', 'application/x-msdownload'],
    ['payload.exe', 'image/png'],
    ['payload.exe', 'application/octet-stream'],
  ] as const)('rejects mismatched or unsupported metadata for %s (%s)', (name, mime) => {
    expect(classifyPromptAttachment(fileOf(name, mime))).toBeNull()
  })
})

describe('admitPromptAttachments', () => {
  it('defines the shared prompt policy as five files with server-aligned size limits', () => {
    expect(GENERAL_AGENT_ATTACHMENT_POLICY).toEqual({
      allowedTypes: ['image', 'audio', 'video', 'document', 'text'],
      maxCount: 5,
      maxBytes: {
        image: 50 * MB,
        audio: 50 * MB,
        video: 50 * MB,
        document: 25 * MB,
        text: 25 * MB,
      },
    })
  })
  const allTypes = ['image', 'audio', 'video', 'document', 'text'] as const

  it.each([
    ['media boundary', fileOf('photo.png', 'image/png', 50 * MB), true],
    ['media over boundary', fileOf('photo.png', 'image/png', 50 * MB + 1), false],
    ['document boundary', fileOf('report.pdf', 'application/pdf', 25 * MB), true],
    ['document over boundary', fileOf('report.pdf', 'application/pdf', 25 * MB + 1), false],
    ['text boundary', fileOf('notes.txt', 'text/plain', 25 * MB), true],
    ['text over boundary', fileOf('notes.txt', 'text/plain', 25 * MB + 1), false],
  ] as const)('enforces the exact size limit for %s', (_label, file, accepted) => {
    const result = admitPromptAttachments([], [file], {
      allowedTypes: allTypes,
      maxCount: 10,
    })

    expect(result.accepted).toHaveLength(accepted ? 1 : 0)
    expect(result.rejected).toEqual(accepted ? [] : [{
      file,
      reason: AttachmentRejectionReason.TooLarge,
    }])
  })

  it('deduplicates against current and earlier incoming files while preserving order', () => {
    const existing = fileOf('existing.png', 'image/png', 4, 10)
    const first = fileOf('first.pdf', 'application/pdf', 5, 20)
    const duplicateCurrent = fileOf('existing.png', 'image/png', 4, 10)
    const duplicateIncoming = fileOf('first.pdf', 'application/pdf', 5, 20)
    const second = fileOf('second.txt', 'text/plain', 6, 30)

    const result = admitPromptAttachments(
      [currentAttachment(existing)],
      [first, duplicateCurrent, duplicateIncoming, second],
      { allowedTypes: allTypes, maxCount: 10 },
    )

    expect(result.accepted.map(({ file }) => file.name)).toEqual(['first.pdf', 'second.txt'])
    expect(result.rejected).toEqual([
      { file: duplicateCurrent, reason: AttachmentRejectionReason.Duplicate },
      { file: duplicateIncoming, reason: AttachmentRejectionReason.Duplicate },
    ])
    expect(result.accepted.length + result.rejected.length).toBe(4)
  })

  it('reports unsupported, disallowed, and capacity rejections without silent drops', () => {
    const current = currentAttachment(fileOf('current.png', 'image/png'))
    const unsupported = fileOf('archive.zip', 'application/zip')
    const disallowed = fileOf('voice.mp3', 'audio/mpeg')
    const accepted = fileOf('first.png', 'image/png')
    const overCapacity = fileOf('second.png', 'image/png')

    const result = admitPromptAttachments(
      [current],
      [unsupported, disallowed, accepted, overCapacity],
      { allowedTypes: ['image'], maxCount: 2 },
    )

    expect(result.accepted.map(({ file }) => file)).toEqual([accepted])
    expect(result.rejected).toEqual([
      { file: unsupported, reason: AttachmentRejectionReason.UnsupportedType },
      { file: disallowed, reason: AttachmentRejectionReason.UnsupportedType },
      { file: overCapacity, reason: AttachmentRejectionReason.Capacity },
    ])
    expect(result.accepted.length + result.rejected.length).toBe(4)
  })

  it('applies per-type max byte overrides at the exact boundary', () => {
    const atImageLimit = fileOf('allowed.png', 'image/png', 10 * MB)
    const overImageLimit = fileOf('rejected.png', 'image/png', 10 * MB + 1)

    const result = admitPromptAttachments([], [atImageLimit, overImageLimit], {
      allowedTypes: ['image'],
      maxCount: 2,
      maxBytes: { image: 10 * MB },
    })

    expect(result.accepted).toEqual([{ file: atImageLimit, type: 'image' }])
    expect(result.rejected).toEqual([{
      file: overImageLimit,
      reason: AttachmentRejectionReason.TooLarge,
    }])
  })
})
