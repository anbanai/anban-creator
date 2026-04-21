import { describe, it, expect } from 'vitest'
import { getApiErrorMessage } from './http-client'

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
})
