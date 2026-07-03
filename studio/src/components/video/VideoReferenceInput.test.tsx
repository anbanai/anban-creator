import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { VideoReferenceInput } from './VideoReferenceInput'
import type { VideoReferenceAsset } from '@/types'

describe('VideoReferenceInput', () => {
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
    ]

    const { container } = render(<VideoReferenceInput value={refs} onChange={vi.fn()} />)

    expect(screen.getByRole('img', { name: 'cup.png' })).toHaveAttribute('src', 'https://cdn.example.com/cup.png')
    expect(container.querySelector('video source')).toHaveAttribute('src', 'https://cdn.example.com/motion.mp4')
    expect(container.querySelector('audio source')).toHaveAttribute('src', 'https://cdn.example.com/bgm.mp3')
    expect(screen.getAllByText('品牌杯身必须保持银色金属质感，禁止变成卡通杯。').length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText('移除参考素材')).toHaveLength(4)
  })
})
