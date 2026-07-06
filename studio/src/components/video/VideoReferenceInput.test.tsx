import { useState } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { VideoReferenceInput } from './VideoReferenceInput'
import type { VideoReferenceAsset } from '@/types'
import { http } from '@/lib/http-client'
import { uploadToOSS } from '@/lib/direct-upload'

vi.mock('@/lib/http-client', () => ({
  http: {
    get: vi.fn(),
  },
}))

vi.mock('@/lib/direct-upload', () => ({
  uploadToOSS: vi.fn(),
}))

describe('VideoReferenceInput', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders previews for image, video, audio, and text references', () => {
    const refs: VideoReferenceAsset[] = [
      {
        type: 'image_url',
        url: 'https://cdn.example.com/cup.png',
        file_name: 'cup.png',
        reference_role: 'product appearance',
      },
      {
        type: 'video_url',
        url: 'https://cdn.example.com/motion.mp4',
        file_name: 'motion.mp4',
        reference_role: 'action',
      },
      {
        type: 'audio_url',
        url: 'https://cdn.example.com/bgm.mp3',
        file_name: 'bgm.mp3',
        reference_role: 'voice tone',
      },
      {
        type: 'text',
        text: '品牌杯身必须保持银色金属质感，禁止变成卡通杯。',
        reference_role: 'subject identity',
      },
      {
        type: 'image_url',
        url: 'https://cdn.example.com/style.png',
        file_name: 'style.png',
        reference_role: 'style',
      },
    ]

    const { container } = render(<VideoReferenceInput value={refs} onChange={vi.fn()} />)

    expect(screen.getByRole('img', { name: 'cup.png' })).toHaveAttribute('src', 'https://cdn.example.com/cup.png')
    expect(screen.getByRole('img', { name: 'style.png' })).toHaveAttribute('src', 'https://cdn.example.com/style.png')
    expect(container.querySelector('video source')).toHaveAttribute('src', 'https://cdn.example.com/motion.mp4')
    expect(container.querySelector('audio source')).toHaveAttribute('src', 'https://cdn.example.com/bgm.mp3')
    expect(screen.getAllByText('品牌杯身必须保持银色金属质感，禁止变成卡通杯。').length).toBeGreaterThan(0)
    expect(screen.getByText('用途：风格参考')).toBeInTheDocument()
    expect(screen.getAllByLabelText('移除参考素材')).toHaveLength(5)
  })

  it('uses authenticated blob previews for internal image, video, and audio references', async () => {
    const createObjectURL = vi.spyOn(URL, 'createObjectURL')
      .mockReturnValueOnce('blob:image-ref')
      .mockReturnValueOnce('blob:video-ref')
      .mockReturnValueOnce('blob:audio-ref')
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    vi.mocked(http.get).mockResolvedValue({ data: new Blob(['x']) })

    render(<VideoReferenceInput value={[
      { type: 'image_url', url: '/api/v1/files/uploads/video-references/u/cup.png', file_name: 'cup.png' },
      { type: 'video_url', url: '/files/uploads/video-references/u/motion.mp4', file_name: 'motion.mp4', mime_type: 'video/mp4' },
      { type: 'audio_url', url: '/files/uploads/video-references/u/bgm.mp3', file_name: 'bgm.mp3', mime_type: 'audio/mpeg' },
    ]} onChange={vi.fn()} />)

    const image = await screen.findByRole('img', { name: 'cup.png' })
    expect(image).toHaveAttribute('src', 'blob:image-ref')
    await waitFor(() => {
      expect(screen.getByLabelText('motion.mp4').querySelector('source')).toHaveAttribute('src', 'blob:video-ref')
      expect(screen.getByLabelText('bgm.mp3').querySelector('source')).toHaveAttribute('src', 'blob:audio-ref')
    })
    expect(http.get).toHaveBeenCalledWith('/files/uploads/video-references/u/cup.png', { responseType: 'blob' })
    expect(http.get).toHaveBeenCalledWith('/files/uploads/video-references/u/motion.mp4', { responseType: 'blob' })
    expect(http.get).toHaveBeenCalledWith('/files/uploads/video-references/u/bgm.mp3', { responseType: 'blob' })
    createObjectURL.mockRestore()
    revokeObjectURL.mockRestore()
  })

  it('uploads multiple media files, keeps successful items, and reports per-file failures', async () => {
    const onChange = vi.fn()
    vi.mocked(uploadToOSS)
      .mockResolvedValueOnce({ uploadId: '1', key: 'k1', publicUrl: '/api/v1/files/uploads/video-references/u/cup.png', contentType: 'image/png', size: 123 })
      .mockRejectedValueOnce(new Error('OSS 上传失败，请重试'))

    render(<VideoReferenceInput value={[]} onChange={onChange} />)

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(fileInput, {
      target: {
        files: [
          new File(['image'], 'cup.png', { type: 'image/png' }),
          new File(['video'], 'bad.mp4', { type: 'video/mp4' }),
        ],
      },
    })

    await waitFor(() => {
      expect(onChange).toHaveBeenCalledWith([expect.objectContaining({
        type: 'image_url',
        url: '/api/v1/files/uploads/video-references/u/cup.png',
        file_name: 'cup.png',
      })])
    })
    expect(await screen.findByText('bad.mp4：OSS 上传失败，请重试')).toBeInTheDocument()
  })

  it('shows upload progress for the active media file', async () => {
    const onChange = vi.fn()
    let resolveUpload: (value: unknown) => void = () => {}
    vi.mocked(uploadToOSS).mockImplementation(({ onProgress }) => {
      onProgress?.(25)
      return new Promise((resolve) => {
        resolveUpload = resolve
      }) as any
    })

    render(<VideoReferenceInput value={[]} onChange={onChange} />)

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(fileInput, {
      target: {
        files: [new File(['image'], 'cup.png', { type: 'image/png' })],
      },
    })

    expect(await screen.findByText('cup.png')).toBeInTheDocument()
    expect(await screen.findByText('25%')).toBeInTheDocument()

    resolveUpload({ uploadId: '1', key: 'k1', publicUrl: '/api/v1/files/uploads/video-references/u/cup.png', contentType: 'image/png', size: 123 })
    await waitFor(() => expect(onChange).toHaveBeenCalled())
  })

  it('uses an explicit text constraint action instead of an ambiguous plus button', () => {
    const onChange = vi.fn()
    render(<VideoReferenceInput value={[]} onChange={onChange} />)

    expect(screen.getByRole('button', { name: '添加文本约束' })).toBeDisabled()
    fireEvent.change(screen.getByPlaceholderText('例如：杯身必须保持银色金属质感，禁止变成卡通杯。'), {
      target: { value: '保持真实手持感' },
    })
    fireEvent.click(screen.getByRole('button', { name: '添加文本约束' }))

    expect(onChange).toHaveBeenCalledWith([{
      type: 'text',
      text: '保持真实手持感',
      reference_role: 'subject identity',
    }])
  })

  it('edits explicit reference transfer rules for each asset', () => {
    const onChange = vi.fn()
    function Harness() {
      const [refs, setRefs] = useState<VideoReferenceAsset[]>([{
        type: 'video_url',
        url: 'https://cdn.example.com/motion.mp4',
        file_name: 'motion.mp4',
        reference_role: 'camera movement',
      }])
      return <VideoReferenceInput value={refs} onChange={(next) => {
        setRefs(next)
        onChange(next)
      }} />
    }
    render(<Harness />)

    fireEvent.change(screen.getByLabelText('控制什么'), {
      target: { value: '运镜, 节奏' },
    })
    fireEvent.change(screen.getByLabelText('可变什么'), {
      target: { value: '人物, 场景' },
    })
    fireEvent.change(screen.getByLabelText('不传递什么'), {
      target: { value: '原 logo, 原人物' },
    })

    expect(onChange).toHaveBeenLastCalledWith([expect.objectContaining({
      must_keep: ['运镜', '节奏'],
      can_change: ['人物', '场景'],
      must_not_transfer: ['原 logo', '原人物'],
    })])
    expect(screen.getByText('视频素材默认只参考运镜、节奏、动作，不复制人物、场景、logo。')).toBeInTheDocument()
  })
})
