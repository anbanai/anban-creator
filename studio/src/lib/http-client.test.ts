import { describe, it, expect } from 'vitest'
import { getApiErrorCode, getApiErrorMessage, sanitizeUserFacingErrorMessage } from './http-client'

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

  it.each([
    [40201, 'billing_debt_outstanding', '账户存在欠费，请先充值结清'],
    [40202, 'billing_insufficient_for_task', '积分余额不足，请先充值后继续'],
    [40203, 'billing_insufficient_for_standalone_operation', '积分余额不足，请先充值后继续'],
    [40401, 'billing_sku_not_found', '服务计费配置异常，请联系管理员'],
    [40402, 'billing_catalog_not_found', '服务计费配置异常，请联系管理员'],
  ])('maps billing code %s to localized text', (code, msg, expected) => {
    const error = { response: { data: { code, msg } } }

    expect(getApiErrorMessage(error, '默认错误')).toBe(expected)
    expect(getApiErrorCode(error)).toBe(code)
  })

  it.each([
    [50000, 'billing_internal'],
    [50001, 'billing_ledger_invalid'],
    [40400, 'billing_resource_not_found'],
  ])('hides internal billing error %s', (code, msg) => {
    const error = { response: { data: { code, msg } } }

    expect(getApiErrorMessage(error, '图片服务暂时不可用，请稍后重试')).toBe('图片服务暂时不可用，请稍后重试')
    expect(getApiErrorCode(error)).toBe(code)
  })

  it('hides a billing machine code in a text response', () => {
    const error = { response: { data: 'billing_ledger_invalid' } }

    expect(getApiErrorMessage(error, '图片服务暂时不可用，请稍后重试')).toBe('图片服务暂时不可用，请稍后重试')
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

  it('falls back to error when response msg is empty', () => {
    const axiosError = {
      response: {
        data: { msg: '', error: '请求失败，请稍后重试' },
      },
    }

    expect(getApiErrorMessage(axiosError, '默认错误')).toBe('请求失败，请稍后重试')
  })

  it('extracts message from standard Error', () => {
    expect(getApiErrorMessage(new Error('网络错误'), '默认')).toBe('网络错误')
  })

  it('returns fallback for unknown error type', () => {
    expect(getApiErrorMessage('string error', '默认')).toBe('默认')
  })

  it('returns fallback for an unknown response body shape', () => {
    const error = { response: { data: { detail: { reason: 'billing_ledger_invalid' } } } }

    expect(getApiErrorMessage(error, '默认错误')).toBe('默认错误')
    expect(getApiErrorCode(error)).toBeUndefined()
  })

  it('returns fallback for null', () => {
    expect(getApiErrorMessage(null, '默认')).toBe('默认')
  })

  it('handles Error with empty message', () => {
    expect(getApiErrorMessage(new Error(''), '默认')).toBe('默认')
  })

  it('hides a billing machine code wrapped in a standard Error', () => {
    expect(getApiErrorMessage(new Error('billing_ledger_invalid'), '默认错误')).toBe('默认错误')
  })

  it('returns a localized message for an Axios network error without losing the original error', () => {
    const error = Object.assign(new Error('Network Error'), { isAxiosError: true, code: 'ERR_NETWORK' })

    expect(getApiErrorMessage(error, '默认')).toBe('网络连接失败，请检查网络后重试')
    expect(error.message).toBe('Network Error')
    expect(error.code).toBe('ERR_NETWORK')
  })

  it('hides structured backend logs and provider internals from users', () => {
    const raw = '{"level":"error","error":"[OpenAI] OpenAI 图片接口返回了 URL，但下载失败: https://files.example.com/a.png\\n提示: RevisedPrompt=\\"\\"","message":"task image generation failed"}<br/>{"level":"error"}'

    const got = sanitizeUserFacingErrorMessage(raw, '图片生成失败，请稍后重试')

    expect(got).toBe('图片已生成，但保存到作品库失败，请稍后重试')
    expect(got).not.toContain('https://files.example.com')
    expect(got).not.toContain('RevisedPrompt')
    expect(got).not.toContain('{"level"')
  })

  it('describes internal image configuration failures as capability configuration', () => {
    const got = sanitizeUserFacingErrorMessage('image API key 配置异常', '图片生成失败')

    expect(got).toBe('图像能力配置异常，请联系管理员')
    expect(got).not.toContain('模型')
  })

  it('does not rewrite ordinary download errors as image save failures', () => {
    expect(sanitizeUserFacingErrorMessage('文件下载失败，请稍后重试', '默认')).toBe('文件下载失败，请稍后重试')
  })
})
