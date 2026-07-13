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

  it('offers strict remake roles as optional choices while defaulting to Agent judgment', () => {
    render(<VideoReferenceInput value={[{
      type: 'video_url',
      url: 'https://cdn.example.com/reference.mp4',
      file_name: 'reference.mp4',
      input_duration_seconds: 45,
    }]} onChange={vi.fn()} />)

    expect(screen.getByText('用途：由 Agent 判断')).toBeInTheDocument()
    expect(screen.getByText('输入时长 45.0 秒')).toBeInTheDocument()

    fireEvent.click(screen.getAllByRole('combobox')[0])

    expect(screen.getAllByText('完整复刻参考').length).toBeGreaterThan(1)
    expect(screen.getByText('段子/时间轴结构')).toBeInTheDocument()
  })

  it('uploads multiple media files, keeps successful items, and reports per-file failures', async () => {
    const onChange = vi.fn()
    vi.mocked(uploadToOSS)
      .mockResolvedValueOnce({
        uploadId: 'upload-1',
        key: 'uploads/pending/user/upload-1/cup.png',
        publicUrl: 'https://anbancreator.oss-cn-chengdu.aliyuncs.com/uploads/pending/user/upload-1/cup.png',
        contentType: 'image/png',
        size: 123,
      })
      .mockRejectedValueOnce(new Error('OSS 上传失败，请重试'))

    render(<VideoReferenceInput value={[]} onChange={onChange} />)

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(fileInput, {
      target: {
        files: [
          new File(['image'], 'cup.png', { type: 'image/png' }),
          new File(['audio'], 'bad.mp3', { type: 'audio/mpeg' }),
        ],
      },
    })

    await waitFor(() => {
      expect(onChange).toHaveBeenCalledWith([expect.objectContaining({
        type: 'image_url',
        url: 'https://anbancreator.oss-cn-chengdu.aliyuncs.com/uploads/pending/user/upload-1/cup.png',
        file_name: 'cup.png',
      })])
    })
    expect(await screen.findByText('bad.mp3：OSS 上传失败，请重试')).toBeInTheDocument()
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

    resolveUpload({
      uploadId: 'upload-1',
      key: 'uploads/pending/user/upload-1/cup.png',
      publicUrl: 'https://anbancreator.oss-cn-chengdu.aliyuncs.com/uploads/pending/user/upload-1/cup.png',
      contentType: 'image/png',
      size: 123,
    })
    await waitFor(() => expect(onChange).toHaveBeenCalled())
  })

  it('stores measured duration for uploaded local video references', async () => {
    const onChange = vi.fn()
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:local-video')
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    const createElement = vi.spyOn(document, 'createElement')
    const originalCreateElement = createElement.getMockImplementation()
    createElement.mockImplementation((tagName: string, options?: ElementCreationOptions) => {
      const element = originalCreateElement
        ? originalCreateElement(tagName, options)
        : Document.prototype.createElement.call(document, tagName, options)
      if (tagName === 'video') {
        Object.defineProperty(element, 'duration', { value: 12.5, configurable: true })
        Object.defineProperty(element, 'load', {
          value: vi.fn(() => {
            setTimeout(() => {
              ;(element as HTMLVideoElement).onloadedmetadata?.(new Event('loadedmetadata'))
            }, 0)
          }),
          configurable: true,
        })
      }
      return element
    })
    vi.mocked(uploadToOSS).mockResolvedValueOnce({
      uploadId: 'upload-video',
      key: 'uploads/pending/user/upload-video/clip.mp4',
      publicUrl: 'https://anbancreator.oss-cn-chengdu.aliyuncs.com/uploads/pending/user/upload-video/clip.mp4',
      contentType: 'video/mp4',
      size: 123,
    })

    render(<VideoReferenceInput value={[]} onChange={onChange} />)

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(fileInput, {
      target: {
        files: [new File(['video'], 'clip.mp4', { type: 'video/mp4' })],
      },
    })

    await waitFor(() => {
      expect(onChange).toHaveBeenCalledWith([expect.objectContaining({
        type: 'video_url',
        input_duration_seconds: 12.5,
      })])
    })
    createObjectURL.mockRestore()
    revokeObjectURL.mockRestore()
    createElement.mockRestore()
  })

  it('rejects a local video before upload when browser metadata cannot be read', async () => {
    const onChange = vi.fn()
    const createObjectURL = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:unreadable-video')
    const revokeObjectURL = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    const createElement = vi.spyOn(document, 'createElement')
    const originalCreateElement = createElement.getMockImplementation()
    createElement.mockImplementation((tagName: string, options?: ElementCreationOptions) => {
      const element = originalCreateElement
        ? originalCreateElement(tagName, options)
        : Document.prototype.createElement.call(document, tagName, options)
      if (tagName === 'video') {
        Object.defineProperty(element, 'load', {
          value: vi.fn(() => {
            setTimeout(() => {
              ;(element as HTMLVideoElement).onerror?.(new Event('error'))
            }, 0)
          }),
          configurable: true,
        })
      }
      return element
    })

    render(<VideoReferenceInput value={[]} onChange={onChange} />)

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    fireEvent.change(fileInput, {
      target: {
        files: [new File(['video'], 'unreadable.mp4', { type: 'video/mp4' })],
      },
    })

    expect(await screen.findByText('unreadable.mp4：无法读取视频时长，请转换为浏览器支持的 MP4、MOV 或 WebM 后重试。')).toBeInTheDocument()
    expect(uploadToOSS).not.toHaveBeenCalled()
    expect(onChange).not.toHaveBeenCalled()
    createObjectURL.mockRestore()
    revokeObjectURL.mockRestore()
    createElement.mockRestore()
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
      reference_role: undefined,
    }])
  })

  it('writes selected media reference role onto the same reference item', async () => {
    const onChange = vi.fn()
    render(<VideoReferenceInput value={[{
      type: 'image_url',
      url: 'https://cdn.example.com/person.png',
      file_name: 'person.png',
    }]} onChange={onChange} />)

    fireEvent.click(screen.getAllByRole('combobox')[1])
    const option = await screen.findByRole('option', { name: '主体不变' })
    fireEvent.pointerDown(option)
    fireEvent.mouseDown(option)
    fireEvent.pointerUp(option)
    fireEvent.mouseUp(option)
    fireEvent.click(option)

    await waitFor(() => {
      expect(onChange).toHaveBeenCalledWith([expect.objectContaining({
        type: 'image_url',
        url: 'https://cdn.example.com/person.png',
        reference_role: 'subject identity',
      })])
    })
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
    expect(screen.getByText('视频素材默认约束运镜、节奏、动作；当要求同款/复刻/完全一样时，会作为段子结构和时间轴参考，但主体、产品、场景仍以你的其他素材和文字为准。')).toBeInTheDocument()
  })
})
