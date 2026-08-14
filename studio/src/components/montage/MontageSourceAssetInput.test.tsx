import { useState } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { uploadToOSS } from '@/lib/direct-upload'
import type { MontageAsset } from '@/types'
import { MontageSourceAssetInput } from './MontageSourceAssetInput'

vi.mock('@/lib/direct-upload', () => ({
  uploadToOSS: vi.fn(),
}))

function ControlledAssets({
  initialValue = [],
  onValueChange,
  onUploadingChange,
}: {
  initialValue?: MontageAsset[]
  onValueChange?: (value: MontageAsset[]) => void
  onUploadingChange?: (uploading: boolean) => void
}) {
  const [value, setValue] = useState(initialValue)
  return (
    <MontageSourceAssetInput
      value={value}
      onChange={(next) => {
        setValue(next)
        onValueChange?.(next)
      }}
      onUploadingChange={onUploadingChange}
    />
  )
}

describe('MontageSourceAssetInput', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it.each([
    { fileName: 'source.png', contentType: 'image/png', expectedType: 'image_url' as const },
    { fileName: 'source.mp4', contentType: 'video/mp4', expectedType: 'video_url' as const },
    { fileName: 'source.m4a', contentType: 'audio/mp4', expectedType: 'audio_url' as const },
  ])('maps $contentType uploads to $expectedType and preserves saved asset metadata', async ({ fileName, contentType, expectedType }) => {
    const previewUrl = `/${fileName}`
    vi.mocked(uploadToOSS).mockResolvedValue({
      uploadSessionId: 'session-source',
      uploadId: 'upload-source',
      key: `uploads/${fileName}`,
      previewUrl,
      publicUrl: previewUrl,
      contentType,
      size: 42,
    })
    const onValueChange = vi.fn()
    const onUploadingChange = vi.fn()
    const existing: MontageAsset = {
      type: 'document_url',
      task_file_id: 'task-file-1',
      file_name: 'brief.pdf',
      mime_type: 'application/pdf',
      file_size: 21,
    }

    render(
      <ControlledAssets
        initialValue={[existing]}
        onValueChange={onValueChange}
        onUploadingChange={onUploadingChange}
      />,
    )

    fireEvent.change(screen.getByLabelText('添加参考素材'), {
      target: { files: [new File(['source'], fileName, { type: contentType })] },
    })

    await waitFor(() => expect(onValueChange).toHaveBeenLastCalledWith([
      existing,
      {
        type: expectedType,
        url: previewUrl,
        file_name: fileName,
        mime_type: contentType,
        file_size: 42,
      },
    ]))
    expect(uploadToOSS).toHaveBeenCalledWith(expect.objectContaining({
      purpose: 'montage_asset',
      file: expect.objectContaining({ name: fileName }),
    }))
    expect(onUploadingChange).toHaveBeenCalledWith(true)
    await waitFor(() => expect(onUploadingChange).toHaveBeenLastCalledWith(false))
  })

  it('removes a saved asset without leaking adapter metadata', () => {
    const onValueChange = vi.fn()
    render(
      <ControlledAssets
        initialValue={[{
          type: 'text',
          text: '必须保留产品名称',
          file_name: '文案要求',
        }]}
        onValueChange={onValueChange}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '删除 文案要求' }))
    expect(onValueChange).toHaveBeenLastCalledWith([])
  })
})
