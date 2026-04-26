import { describe, it, expect } from 'vitest'
import { loginSchema, registerSchema, createTaskSchema, planSchema, channelSchema } from './schemas'

describe('loginSchema', () => {
  it('accepts valid email and password', () => {
    expect(loginSchema.safeParse({ email: 'test@example.com', password: '12345678' }).success).toBe(true)
  })

  it('rejects empty email', () => {
    const result = loginSchema.safeParse({ email: '', password: '12345678' })
    expect(result.success).toBe(false)
  })

  it('rejects invalid email format', () => {
    const result = loginSchema.safeParse({ email: 'not-an-email', password: '12345678' })
    expect(result.success).toBe(false)
  })

  it('rejects empty password', () => {
    const result = loginSchema.safeParse({ email: 'test@example.com', password: '' })
    expect(result.success).toBe(false)
  })
})

describe('registerSchema', () => {
  it('accepts valid registration data with invite code', () => {
    expect(registerSchema.safeParse({
      invite_code: 'AB2C4D6E',
      email: 'test@example.com',
      code: '123456',
      password: '12345678',
    }).success).toBe(true)
  })

  it('accepts registration without invite code (field always validates as string)', () => {
    expect(registerSchema.safeParse({
      invite_code: '',
      email: 'test@example.com',
      code: '123456',
      password: '12345678',
    }).success).toBe(true)
  })

  it('rejects short password', () => {
    const result = registerSchema.safeParse({
      invite_code: 'AB2C4D6E',
      email: 'test@example.com',
      code: '123456',
      password: 'short',
    })
    expect(result.success).toBe(false)
  })

  it('rejects missing verification code', () => {
    const result = registerSchema.safeParse({
      invite_code: 'AB2C4D6E',
      email: 'test@example.com',
      code: '',
      password: '12345678',
    })
    expect(result.success).toBe(false)
  })

  it('accepts optional nickname', () => {
    const result = registerSchema.safeParse({
      invite_code: 'AB2C4D6E',
      email: 'test@example.com',
      code: '123456',
      password: '12345678',
      nickname: '测试用户',
    })
    expect(result.success).toBe(true)
  })
})

describe('createTaskSchema', () => {
  it('accepts valid task creation data', () => {
    expect(createTaskSchema.safeParse({
      channel_id: 'ch-1',
      type: 'article',
      prompt: '测试主题',
    }).success).toBe(true)
  })

  it('rejects missing channel_id', () => {
    const result = createTaskSchema.safeParse({
      channel_id: '',
      type: 'article',
      prompt: '测试主题',
    })
    expect(result.success).toBe(false)
  })

  it('accepts optional prompt', () => {
    const result = createTaskSchema.safeParse({
      channel_id: 'ch-1',
      type: 'article',
      prompt: '',
    })
    expect(result.success).toBe(true)
  })

  it('rejects prompt exceeding max length', () => {
    const result = createTaskSchema.safeParse({
      channel_id: 'ch-1',
      type: 'article',
      prompt: 'a'.repeat(501),
    })
    expect(result.success).toBe(false)
  })

  it('accepts all valid content types', () => {
    for (const type of ['rednote', 'article', 'xls'] as const) {
      expect(createTaskSchema.safeParse({
        channel_id: 'ch-1',
        type,
        prompt: '测试',
      }).success).toBe(true)
    }
  })

  it('applies default values for quantity and image_ratio', () => {
    const result = createTaskSchema.parse({
      channel_id: 'ch-1',
      type: 'article',
      prompt: '测试',
    })
    expect(result.quantity).toBe(1)
    expect(result.image_ratio).toBe('')
  })
})

describe('planSchema', () => {
  it('accepts valid plan data', () => {
    expect(planSchema.safeParse({
      type: 'article',
      cron_expr: '0 9 * * 1',
    }).success).toBe(true)
  })

  it('accepts optional prompt', () => {
    const result = planSchema.safeParse({
      type: 'article',
      cron_expr: '0 9 * * 1',
      prompt: '写一篇护肤指南',
    })
    expect(result.success).toBe(true)
  })

  it('rejects empty cron expression', () => {
    const result = planSchema.safeParse({
      type: 'article',
      cron_expr: '',
    })
    expect(result.success).toBe(false)
  })

  it('accepts optional fields', () => {
    const result = planSchema.safeParse({
      type: 'rednote',
      cron_expr: '0 9 * * 1',
      prompt: '主题方向',
      channel_id: 'ch-1',
    })
    expect(result.success).toBe(true)
  })
})

describe('channelSchema', () => {
  it('accepts article platform without wechat_app_id when publishing disabled', () => {
    expect(channelSchema.safeParse({
      platform: 'article',
      enable_publishing: false,
    }).success).toBe(true)
  })

  it('accepts article platform with wechat_app_id when publishing enabled', () => {
    expect(channelSchema.safeParse({
      platform: 'article',
      enable_publishing: true,
      wechat_app_id: 'wx123',
    }).success).toBe(true)
  })

  it('rejects article platform without wechat_app_id when publishing enabled', () => {
    const result = channelSchema.safeParse({
      platform: 'article',
      enable_publishing: true,
    })
    expect(result.success).toBe(false)
  })

  it('accepts rednote platform without wechat_app_id', () => {
    expect(channelSchema.safeParse({
      platform: 'rednote',
    }).success).toBe(true)
  })

  it('accepts all optional fields', () => {
    const result = channelSchema.safeParse({
      platform: 'article',
      enable_publishing: true,
      wechat_app_id: 'wx123',
      wechat_secret: 'secret',
      name: '频道名称',
      keywords: '测试',
      positioning: '定位',
      style: 'casual-science',
      theme: 'autumn-warm',
      author: '作者',
    })
    expect(result.success).toBe(true)
  })
})
