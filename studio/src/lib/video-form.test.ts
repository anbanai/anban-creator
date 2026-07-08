import { describe, expect, it } from 'vitest'
import type { VideoInput } from '@/types'
import {
  VIDEO_PROJECT_DEFAULT_VALUE,
  buildVideoInputForSubmit,
  initialVideoInput,
  readVideoSelectValue,
  writeVideoSelectValue,
} from './video-form'

describe('video intake form helpers', () => {
  it('uses a stable select value for unset hard constraints and clears it before submit', () => {
    expect(readVideoSelectValue(undefined)).toBe(VIDEO_PROJECT_DEFAULT_VALUE)
    expect(writeVideoSelectValue(VIDEO_PROJECT_DEFAULT_VALUE)).toBeUndefined()
    expect(writeVideoSelectValue('9:16')).toBe('9:16')
  })

  it('creates initial intake values with editable references', () => {
    expect(initialVideoInput('生成一条咖啡杯短视频')).toEqual({
      brief: '生成一条咖啡杯短视频',
      references: [],
      hard_constraints: {
        ratio: undefined,
        duration: undefined,
        watermark: undefined,
      },
    })
  })

  it('submits only user intake fields and falls back to prompt as brief', () => {
    expect(buildVideoInputForSubmit('  生成一条咖啡杯短视频  ', undefined)).toEqual({
      brief: '生成一条咖啡杯短视频',
    })
  })

  it('keeps references and explicit hard constraints while dropping empty assets', () => {
    const input: VideoInput = {
      brief: '  做一个露营杯视频  ',
      references: [
        { type: 'text', text: '  杯身保持银色  ', reference_role: '' },
        { type: 'image_url', url: '' },
        { type: 'image_url', url: ' https://cdn.example.com/cup.png ', reference_role: 'product appearance' },
      ],
      hard_constraints: {
        ratio: '9:16',
        duration: 12,
        watermark: false,
      },
    }

    expect(buildVideoInputForSubmit('备用 prompt', input)).toEqual({
      brief: '做一个露营杯视频',
      references: [
        { type: 'text', text: '杯身保持银色', reference_role: undefined },
        { type: 'image_url', url: 'https://cdn.example.com/cup.png', reference_role: 'product appearance' },
      ],
      hard_constraints: {
        ratio: '9:16',
        duration: 12,
        watermark: false,
      },
    })
  })

  it('keeps task file references without public URLs', () => {
    const input: VideoInput = {
      references: [
        { type: 'video_url', task_file_id: ' task-file-1 ', reference_role: ' source footage ' },
      ],
    }

    expect(buildVideoInputForSubmit('', input)).toEqual({
      references: [
        { type: 'video_url', task_file_id: 'task-file-1', reference_role: 'source footage' },
      ],
    })
  })
})
