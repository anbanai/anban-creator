import { describe, it, expect } from 'vitest'
import { getApiErrorMessage, sanitizeUserFacingErrorMessage } from './http-client'

describe('getApiErrorMessage', () => {
  it('extracts msg from Axios error with response data', () => {
    const axiosError = {
      isAxiosError: true,
      response: {
        data: { msg: '用户未找到' },
        status: 404,
      },
      message: 'Request failed with status code 404',
    }
    expect(getApiErrorMessage(axiosError, '默认错误')).toBe('用户未找到')
  })

  it('extracts msg from error with response but no nested data', () => {
    const axiosError = {
      isAxiosError: true,
      response: {
        data: 'error',
      },
    }
    expect(getApiErrorMessage(axiosError, '默认错误')).toBe('默认错误')
  })

  it('extracts message from standard Error', () => {
    expect(getApiErrorMessage(new Error('网络错误'), '默认')).toBe('网络错误')
  })

  it('returns fallback for unknown error type', () => {
    expect(getApiErrorMessage('string error', '默认')).toBe('默认')
  })

  it('returns fallback for null', () => {
    expect(getApiErrorMessage(null, '默认')).toBe('默认')
  })

  it('handles Error with empty message', () => {
    expect(getApiErrorMessage(new Error(''), '默认')).toBe('默认')
  })

  it('hides structured backend logs and provider internals from users', () => {
    const raw = '{"level":"error","error":"[OpenAI] OpenAI 图片接口返回了 URL，但下载失败: https://files.example.com/a.png\\n提示: RevisedPrompt=\\"\\"","message":"designer: image generation failed"}<br/>{"level":"error"}'

    const got = sanitizeUserFacingErrorMessage(raw, '图片生成失败，请稍后重试')

    expect(got).toBe('图片已生成，但保存到作品库失败，请稍后重试')
    expect(got).not.toContain('https://files.example.com')
    expect(got).not.toContain('RevisedPrompt')
    expect(got).not.toContain('{"level"')
  })

  it('does not rewrite ordinary download errors as designer image save failures', () => {
    expect(sanitizeUserFacingErrorMessage('文件下载失败，请稍后重试', '默认')).toBe('文件下载失败，请稍后重试')
  })
})
