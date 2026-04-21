import { describe, it, expect } from 'vitest'
import {
  loginSchema,
  registerSchema,
  createTaskSchema,
  planSchema,
  channelSchema,
} from '@/lib/schemas'

// --- loginSchema ---

describe('loginSchema', () => {
  it('accepts valid login data', () => {
    const result = loginSchema.safeParse({
      email: 'user@example.com',
      password: 'secret123',
    })
    expect(result.success).toBe(true)
  })

  it('rejects missing email', () => {
    const result = loginSchema.safeParse({
      password: 'secret123',
    })
    expect(result.success).toBe(false)
    if (!result.success) {
      const errors = result.error.issues.map((i) => i.path.join('.'))
      expect(errors).toContain('email')
    }
  })

  it('rejects invalid email format', () => {
    const result = loginSchema.safeParse({
      email: 'not-an-email',
      password: 'secret123',
    })
    expect(result.success).toBe(false)
  })

  it('rejects empty password', () => {
    const result = loginSchema.safeParse({
      email: 'user@example.com',
      password: '',
    })
    expect(result.success).toBe(false)
  })
})

// --- registerSchema ---

describe('registerSchema', () => {
  it('accepts valid registration data with invite code', () => {
    const result = registerSchema.safeParse({
      invite_code: 'AB2C4D6E',
      email: 'user@example.com',
      code: '123456',
      password: 'longpassword',
    })
    expect(result.success).toBe(true)
  })

  it('rejects missing invite_code', () => {
    const result = registerSchema.safeParse({
      email: 'user@example.com',
      code: '123456',
      password: 'longpassword',
    })
    expect(result.success).toBe(false)
  })

  it('rejects short password', () => {
    const result = registerSchema.safeParse({
      invite_code: 'AB2C4D6E',
      email: 'user@example.com',
      code: '123456',
      password: 'short',
    })
    expect(result.success).toBe(false)
  })

  it('rejects missing verification code', () => {
    const result = registerSchema.safeParse({
      invite_code: 'AB2C4D6E',
      email: 'user@example.com',
      code: '',
      password: 'longpassword',
    })
    expect(result.success).toBe(false)
  })

  it('accepts optional nickname', () => {
    const result = registerSchema.safeParse({
      invite_code: 'AB2C4D6E',
      email: 'user@example.com',
      code: '123456',
      password: 'longpassword',
      nickname: 'TestUser',
    })
    expect(result.success).toBe(true)
  })
})

// --- createTaskSchema ---

describe('createTaskSchema', () => {
  it('accepts valid task data with defaults', () => {
    const result = createTaskSchema.safeParse({
      channel_id: 'ch-1',
      type: 'rednote',
      topic: 'Test topic',
    })
    expect(result.success).toBe(true)
    if (result.success) {
      expect(result.data.quantity).toBe(1)
      expect(result.data.image_ratio).toBe('')
    }
  })

  it('rejects missing channel_id', () => {
    const result = createTaskSchema.safeParse({
      type: 'rednote',
      topic: 'Test topic',
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
      type: 'rednote',
      topic: 'a'.repeat(201),
    })
    expect(result.success).toBe(false)
  })

  it('accepts all valid content types', () => {
    for (const type of ['rednote', 'article', 'xls'] as const) {
      expect(createTaskSchema.safeParse({
        channel_id: 'ch-1',
        type,
        topic: 'Test',
      }).success).toBe(true)
    }
  })

  it('applies default values for quantity and image_ratio', () => {
    const result = createTaskSchema.parse({
      channel_id: 'ch-1',
      type: 'article',
      topic: 'Test',
    })
    expect(result.quantity).toBe(1)
    expect(result.image_ratio).toBe('')
  })

  it('rejects invalid type', () => {
    const result = createTaskSchema.safeParse({
      channel_id: 'ch-1',
      type: 'invalid',
      topic: 'Test topic',
    })
    expect(result.success).toBe(false)
  })
})

// --- planSchema ---

describe('planSchema', () => {
  it('accepts valid plan data', () => {
    const result = planSchema.safeParse({
      type: 'article',
      title: 'Weekly plan',
      cron_expr: '0 9 * * 1',
    })
    expect(result.success).toBe(true)
  })

  it('rejects missing title', () => {
    const result = planSchema.safeParse({
      type: 'rednote',
      title: '',
      cron_expr: '0 9 * * 1',
    })
    expect(result.success).toBe(false)
  })

  it('rejects missing cron expression', () => {
    const result = planSchema.safeParse({
      type: 'article',
      title: 'Plan title',
      cron_expr: '',
    })
    expect(result.success).toBe(false)
  })

  it('accepts optional fields', () => {
    const result = planSchema.safeParse({
      channel_id: 'ch-1',
      type: 'xls',
      title: 'Plan with all fields',
      description: 'A description',
      cron_expr: '0 8 * * 1,3,5',
      topic_hint: 'AI tips',
    })
    expect(result.success).toBe(true)
  })
})

// --- channelSchema ---

describe('channelSchema', () => {
  it('accepts valid rednote channel without wechat credentials', () => {
    const result = channelSchema.safeParse({
      platform: 'rednote',
      name: 'My channel',
    })
    expect(result.success).toBe(true)
  })

  it('rejects article channel without wechat_app_id', () => {
    const result = channelSchema.safeParse({
      platform: 'article',
      name: 'Article channel',
    })
    expect(result.success).toBe(false)
  })

  it('rejects xls channel without wechat_app_id', () => {
    const result = channelSchema.safeParse({
      platform: 'xls',
      name: 'XLS channel',
    })
    expect(result.success).toBe(false)
  })

  it('accepts article channel with wechat_app_id', () => {
    const result = channelSchema.safeParse({
      platform: 'article',
      name: 'Article channel',
      wechat_app_id: 'wx123',
    })
    expect(result.success).toBe(true)
  })

  it('accepts all optional fields', () => {
    const result = channelSchema.safeParse({
      platform: 'article',
      wechat_app_id: 'wx123',
      wechat_secret: 'secret',
      name: 'Channel name',
      keywords: 'test',
      positioning: 'positioning',
      style: 'casual-science',
      theme: 'autumn-warm',
      author: 'Author',
      max_concurrent_tasks: 5,
    })
    expect(result.success).toBe(true)
  })

  it('rejects invalid avatar URL', () => {
    const result = channelSchema.safeParse({
      platform: 'rednote',
      avatar_url: 'not-a-url',
    })
    expect(result.success).toBe(false)
  })

  it('accepts empty string for avatar_url', () => {
    const result = channelSchema.safeParse({
      platform: 'rednote',
      avatar_url: '',
    })
    expect(result.success).toBe(true)
  })

  it('accepts valid avatar URL', () => {
    const result = channelSchema.safeParse({
      platform: 'rednote',
      avatar_url: 'https://example.com/avatar.png',
    })
    expect(result.success).toBe(true)
  })
})
