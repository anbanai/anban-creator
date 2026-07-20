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

  it('maps uploads to Montage assets and preserves saved asset metadata', async () => {
    vi.mocked(uploadToOSS).mockResolvedValue({
      uploadSessionId: 'session-source',
      uploadId: 'upload-source',
      key: 'uploads/source.mp4',
      previewUrl: '/source.mp4',
      publicUrl: '/source.mp4',
      contentType: 'video/mp4',
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
      target: { files: [new File(['video'], 'source.mp4', { type: 'video/mp4' })] },
    })

    await waitFor(() => expect(onValueChange).toHaveBeenLastCalledWith([
      existing,
      {
        type: 'video_url',
        url: '/source.mp4',
        file_name: 'source.mp4',
        mime_type: 'video/mp4',
        file_size: 42,
      },
    ]))
    expect(uploadToOSS).toHaveBeenCalledWith(expect.objectContaining({
      purpose: 'montage_asset',
      file: expect.objectContaining({ name: 'source.mp4' }),
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
