import { useState } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { uploadToOSS, type DirectUploadPurpose, type UploadToOSSResult } from '@/lib/direct-upload'
import type { InputAttachment } from '@/types/input-attachment'
import { ReferenceMaterialInput } from './ReferenceMaterialInput'

vi.mock('@/lib/direct-upload', () => ({
  uploadToOSS: vi.fn(),
}))

const imageFile = (name: string) => new File(['image'], name, { type: 'image/png' })

const uploadResult = (fileName: string, overrides: Partial<UploadToOSSResult> = {}): UploadToOSSResult => ({
  uploadId: `upload-${fileName}`,
  key: `uploads/${fileName}`,
  publicUrl: `/${fileName}`,
  contentType: fileName.endsWith('.png') ? 'image/png' : 'application/octet-stream',
  size: 5,
  ...overrides,
  uploadSessionId: overrides.uploadSessionId ?? `session-${fileName}`,
  previewUrl: overrides.previewUrl ?? `/${fileName}`,
})

const seededAttachments = (count: number): InputAttachment[] => Array.from(
  { length: count },
  (_, index) => ({
    type: 'image',
    url: `/seed-${index + 1}.png`,
    file_name: `seed-${index + 1}.png`,
  }),
)

function ControlledReferenceInput({
  initialValue = [],
  onValueChange,
  instructionMaxLength = 1000,
  uploadPurpose,
}: {
  initialValue?: InputAttachment[]
  onValueChange?: (value: InputAttachment[]) => void
  instructionMaxLength?: number
  uploadPurpose?: DirectUploadPurpose
}) {
  const [value, setValue] = useState<InputAttachment[]>(initialValue)
  return (
    <ReferenceMaterialInput
      value={value}
      onChange={(nextValue) => {
        setValue(nextValue)
        onValueChange?.(nextValue)
      }}
      allowedTypes={['image']}
      maxCount={16}
      instructionEnabled
      instructionMaxLength={instructionMaxLength}
      uploadPurpose={uploadPurpose}
    />
  )
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

describe('ReferenceMaterialInput', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('uploads multiple selected images, appends them, and renders previews', async () => {
    vi.mocked(uploadToOSS).mockImplementation(async ({ file }) => uploadResult(file.name))
    const existing: InputAttachment = {
      type: 'image',
      url: '/existing.png',
      file_name: 'existing.png',
      instruction: '保留产品外观',
    }
    const onValueChange = vi.fn()

    render(<ControlledReferenceInput initialValue={[existing]} onValueChange={onValueChange} />)

    fireEvent.change(screen.getByLabelText('添加参考素材'), {
      target: { files: [imageFile('a.png'), imageFile('b.png')] },
    })

    expect(await screen.findByAltText('a.png')).toHaveAttribute('src', '/a.png')
    expect(await screen.findByAltText('b.png')).toHaveAttribute('src', '/b.png')
    expect(screen.getByAltText('existing.png')).toHaveAttribute('src', '/existing.png')
    expect(onValueChange).toHaveBeenLastCalledWith([
      existing,
      expect.objectContaining({
        type: 'image',
        url: '/a.png',
        file_name: 'a.png',
        upload_id: 'upload-a.png',
        key: 'uploads/a.png',
        instruction: '',
      }),
      expect.objectContaining({
        type: 'image',
        url: '/b.png',
        file_name: 'b.png',
        upload_id: 'upload-b.png',
        key: 'uploads/b.png',
        instruction: '',
      }),
    ])
  })


  it('preserves the user-selected order when concurrent uploads finish out of order', async () => {
    const uploads = new Map<string, ReturnType<typeof deferred<UploadToOSSResult>>>()
    vi.mocked(uploadToOSS).mockImplementation(({ file }) => {
      const pending = deferred<UploadToOSSResult>()
      uploads.set(file.name, pending)
      return pending.promise
    })
    const onValueChange = vi.fn()

    render(<ControlledReferenceInput onValueChange={onValueChange} />)

    fireEvent.change(screen.getByLabelText('添加参考素材'), {
      target: { files: [imageFile('first.png'), imageFile('second.png')] },
    })

    uploads.get('second.png')?.resolve(uploadResult('second.png'))
    expect(await screen.findByAltText('second.png')).toBeInTheDocument()

    uploads.get('first.png')?.resolve(uploadResult('first.png'))
    expect(await screen.findByAltText('first.png')).toBeInTheDocument()

    await waitFor(() => expect(onValueChange).toHaveBeenLastCalledWith([
      expect.objectContaining({ file_name: 'first.png' }),
      expect.objectContaining({ file_name: 'second.png' }),
    ]))
  })

  it('accepts drag and drop on the upload zone', async () => {
    vi.mocked(uploadToOSS).mockResolvedValue(uploadResult('drop.png'))

    render(<ControlledReferenceInput />)

    const dropZone = screen.getByLabelText('参考素材上传区域')
    fireEvent.dragOver(dropZone)
    fireEvent.drop(dropZone, {
      dataTransfer: { files: [imageFile('drop.png')] },
    })

    expect(await screen.findByAltText('drop.png')).toHaveAttribute('src', '/drop.png')
    expect(uploadToOSS).toHaveBeenCalledWith(expect.objectContaining({
      purpose: 'ai_entry_attachment',
      file: expect.objectContaining({ name: 'drop.png' }),
    }))
  })

  it('uses the caller supplied direct-upload purpose', async () => {
    vi.mocked(uploadToOSS).mockResolvedValue(uploadResult('montage.png'))

    render(<ControlledReferenceInput uploadPurpose="montage_asset" />)

    fireEvent.change(screen.getByLabelText('添加参考素材'), {
      target: { files: [imageFile('montage.png')] },
    })

    expect(await screen.findByAltText('montage.png')).toBeInTheDocument()
    expect(uploadToOSS).toHaveBeenCalledWith(expect.objectContaining({
      purpose: 'montage_asset',
      file: expect.objectContaining({ name: 'montage.png' }),
    }))
  })

  it('edits optional instructions by Unicode code points and removes successful attachments', async () => {
    const attachment: InputAttachment = {
      type: 'image',
      url: '/a.png',
      file_name: 'a.png',
      instruction: '',
    }
    const onValueChange = vi.fn()

    render(
      <ControlledReferenceInput
        initialValue={[attachment]}
        instructionMaxLength={2}
        onValueChange={onValueChange}
      />,
    )

    const instruction = screen.getByLabelText('a.png 的说明')
    expect(instruction).toHaveAttribute(
      'placeholder',
      '可选：告诉 AI 这张图是什么，或哪些内容需要保留、忽略。留空也会自动理解。',
    )

    fireEvent.change(instruction, { target: { value: '😀好' } })
    expect(onValueChange).toHaveBeenLastCalledWith([{ ...attachment, instruction: '😀好' }])

    fireEvent.change(screen.getByLabelText('a.png 的说明'), { target: { value: '😀好A' } })
    expect(screen.getByLabelText('a.png 的说明')).toHaveValue('😀好')
    expect(screen.getByText('最多 2 个字符')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '删除 a.png' }))
    expect(onValueChange).toHaveBeenLastCalledWith([])
  })

  it('keeps instruction validation aligned after deleting an earlier attachment', () => {
    const first: InputAttachment = {
      type: 'image', url: '/first.png', file_name: 'first.png', instruction: '',
    }
    const second: InputAttachment = {
      type: 'image', url: '/second.png', file_name: 'second.png', instruction: '',
    }

    render(
      <ControlledReferenceInput
        initialValue={[first, second]}
        instructionMaxLength={1}
      />,
    )

    fireEvent.change(screen.getByLabelText('second.png 的说明'), { target: { value: '太长' } })
    expect(screen.getByText('最多 1 个字符')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '删除 first.png' }))

    expect(screen.getByLabelText('second.png 的说明')).toBeInTheDocument()
    expect(screen.getByText('最多 1 个字符')).toBeInTheDocument()
  })

  it('shows a failed upload and retries it without losing successful attachments', async () => {
    vi.mocked(uploadToOSS).mockImplementation(async ({ file }) => {
      if (file.name === 'bad.png') throw new Error('网络失败')
      return uploadResult(file.name)
    })

    render(<ControlledReferenceInput />)

    fireEvent.change(screen.getByLabelText('添加参考素材'), {
      target: { files: [imageFile('good.png'), imageFile('bad.png')] },
    })

    expect(await screen.findByAltText('good.png')).toBeInTheDocument()
    expect(await screen.findByText('网络失败')).toBeInTheDocument()

    vi.mocked(uploadToOSS).mockResolvedValueOnce(uploadResult('bad.png'))
    fireEvent.click(screen.getByRole('button', { name: '重试 bad.png' }))

    expect(await screen.findByAltText('bad.png')).toBeInTheDocument()
    expect(screen.getByAltText('good.png')).toBeInTheDocument()
  })

  it('validates count before starting an upload', () => {
    render(
      <ReferenceMaterialInput
        value={seededAttachments(16)}
        onChange={vi.fn()}
        allowedTypes={['image']}
        maxCount={16}
      />,
    )

    fireEvent.change(screen.getByLabelText('添加参考素材'), {
      target: { files: [imageFile('extra.png')] },
    })

    expect(uploadToOSS).not.toHaveBeenCalled()
    expect(screen.getByText('最多添加 16 个参考素材')).toBeInTheDocument()
  })

  it('validates unknown, disallowed, and oversized file types before upload', () => {
    const unknown = new File(['binary'], 'archive.xyz', { type: 'application/octet-stream' })
    const disallowed = new File(['audio'], 'voice.mp3', { type: 'audio/mpeg' })
    const oversizedDocument = new File(['pdf'], 'brief.pdf', { type: 'application/pdf' })
    Object.defineProperty(oversizedDocument, 'size', { value: 25 * 1024 * 1024 + 1 })

    const { rerender } = render(
      <ReferenceMaterialInput value={[]} onChange={vi.fn()} allowedTypes={['image']} />,
    )

    fireEvent.change(screen.getByLabelText('添加参考素材'), {
      target: { files: [unknown] },
    })
    expect(screen.getByText('不支持 archive.xyz 的文件类型')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('添加参考素材'), {
      target: { files: [disallowed] },
    })
    expect(screen.getByText('当前不支持添加 audio 类型的参考素材')).toBeInTheDocument()

    rerender(
      <ReferenceMaterialInput value={[]} onChange={vi.fn()} allowedTypes={['document']} />,
    )
    fireEvent.change(screen.getByLabelText('添加参考素材'), {
      target: { files: [oversizedDocument] },
    })
    expect(screen.getByText('brief.pdf 不能超过 25MB')).toBeInTheDocument()
    expect(uploadToOSS).not.toHaveBeenCalled()
  })

  it('reports uploading transitions and displays progress', async () => {
    const pendingUpload = deferred<UploadToOSSResult>()
    const onUploadingChange = vi.fn()
    vi.mocked(uploadToOSS).mockImplementation(({ onProgress }) => {
      onProgress?.(42)
      return pendingUpload.promise
    })

    render(
      <ReferenceMaterialInput
        value={[]}
        onChange={vi.fn()}
        allowedTypes={['image']}
        onUploadingChange={onUploadingChange}
      />,
    )

    fireEvent.change(screen.getByLabelText('添加参考素材'), {
      target: { files: [imageFile('pending.png')] },
    })

    await waitFor(() => expect(onUploadingChange).toHaveBeenLastCalledWith(true))
    expect(await screen.findByText('42%')).toBeInTheDocument()

    pendingUpload.resolve(uploadResult('pending.png'))
    await waitFor(() => expect(onUploadingChange).toHaveBeenLastCalledWith(false))
  })

  it('renders non-image attachments as accessible file cards', () => {
    render(
      <ReferenceMaterialInput
        value={[{
          type: 'document',
          url: '/brief.pdf',
          file_name: 'brief.pdf',
          content_type: 'application/pdf',
        }]}
        onChange={vi.fn()}
        allowedTypes={['document']}
        hint="支持品牌资料"
      />,
    )

    expect(screen.getByLabelText('brief.pdf 文件卡片')).toBeInTheDocument()
    expect(screen.getByText('支持品牌资料')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '删除 brief.pdf' })).toBeInTheDocument()
  })
})
