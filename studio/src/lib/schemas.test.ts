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
      topic: '测试主题',
    }).success).toBe(true)
  })

  it('rejects missing channel_id', () => {
    const result = createTaskSchema.safeParse({
      channel_id: '',
      type: 'article',
      topic: '测试主题',
    })
    expect(result.success).toBe(false)
  })

  it('rejects empty topic', () => {
    const result = createTaskSchema.safeParse({
      channel_id: 'ch-1',
      type: 'article',
      topic: '',
    })
    expect(result.success).toBe(false)
  })

  it('rejects topic exceeding max length', () => {
    const result = createTaskSchema.safeParse({
      channel_id: 'ch-1',
      type: 'article',
      topic: 'a'.repeat(201),
    })
    expect(result.success).toBe(false)
  })

  it('accepts all valid content types', () => {
    for (const type of ['rednote', 'article', 'xls'] as const) {
      expect(createTaskSchema.safeParse({
        channel_id: 'ch-1',
        type,
        topic: '测试',
      }).success).toBe(true)
    }
  })

  it('applies default values for quantity and image_ratio', () => {
    const result = createTaskSchema.parse({
      channel_id: 'ch-1',
      type: 'article',
      topic: '测试',
    })
    expect(result.quantity).toBe(1)
    expect(result.image_ratio).toBe('')
  })
})

describe('planSchema', () => {
  it('accepts valid plan data', () => {
    expect(planSchema.safeParse({
      type: 'article',
      title: '测试计划',
      cron_expr: '0 9 * * 1',
    }).success).toBe(true)
  })

  it('rejects empty title', () => {
    const result = planSchema.safeParse({
      type: 'article',
      title: '',
      cron_expr: '0 9 * * 1',
    })
    expect(result.success).toBe(false)
  })

  it('rejects empty cron expression', () => {
    const result = planSchema.safeParse({
      type: 'article',
      title: '测试计划',
      cron_expr: '',
    })
    expect(result.success).toBe(false)
  })

  it('accepts optional fields', () => {
    const result = planSchema.safeParse({
      type: 'rednote',
      title: '测试',
      cron_expr: '0 9 * * 1',
      description: '描述',
      topic_hint: '主题方向',
      channel_id: 'ch-1',
    })
    expect(result.success).toBe(true)
  })
})

describe('channelSchema', () => {
  it('accepts valid channel data for article platform with wechat_app_id', () => {
    expect(channelSchema.safeParse({
      platform: 'article',
      wechat_app_id: 'wx123',
    }).success).toBe(true)
  })

  it('rejects article platform without wechat_app_id', () => {
    const result = channelSchema.safeParse({
      platform: 'article',
    })
    expect(result.success).toBe(false)
  })

  it('rejects xls platform without wechat_app_id', () => {
    const result = channelSchema.safeParse({
      platform: 'xls',
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
      wechat_app_id: 'wx123',
      wechat_secret: 'secret',
      name: '频道名称',
      keywords: '测试',
      positioning: '定位',
      style: 'casual-science',
      theme: 'autumn-warm',
      author: '作者',
      max_concurrent_tasks: 5,
    })
    expect(result.success).toBe(true)
  })
})
