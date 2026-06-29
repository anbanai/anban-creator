import { describe, it, expect } from 'vitest'
import { loginSchema, registerSchema, createTaskSchema, planSchema, projectSchema } from './schemas'

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
      project_id: 'ch-1',
      type: 'article',
      prompt: '测试主题',
    }).success).toBe(true)
  })

  it('rejects missing project_id', () => {
    const result = createTaskSchema.safeParse({
      project_id: '',
      type: 'article',
      prompt: '测试主题',
    })
    expect(result.success).toBe(false)
  })

  it('accepts viral analysis without a project when prompt contains a note URL', () => {
    const result = createTaskSchema.safeParse({
      project_id: '',
      type: 'viral_analysis',
      prompt: '帮我拆解这篇 http://xhslink.com/a1b2c3',
    })
    expect(result.success).toBe(true)
  })

  it('rejects viral analysis without a URL in prompt', () => {
    const result = createTaskSchema.safeParse({
      project_id: '',
      type: 'viral_analysis',
      prompt: '帮我拆解这篇爆款笔记',
    })
    expect(result.success).toBe(false)
  })

  it('accepts optional prompt', () => {
    const result = createTaskSchema.safeParse({
      project_id: 'ch-1',
      type: 'article',
      prompt: '',
    })
    expect(result.success).toBe(true)
  })

  it('accepts prompt at max length', () => {
    const result = createTaskSchema.safeParse({
      project_id: 'ch-1',
      type: 'article',
      prompt: 'a'.repeat(5120),
    })
    expect(result.success).toBe(true)
  })

  it('counts unicode prompt length like the API', () => {
    const result = createTaskSchema.safeParse({
      project_id: 'ch-1',
      type: 'article',
      prompt: '😀'.repeat(5120),
    })
    expect(result.success).toBe(true)
  })

  it('rejects prompt exceeding max length', () => {
    const result = createTaskSchema.safeParse({
      project_id: 'ch-1',
      type: 'article',
      prompt: 'a'.repeat(5121),
    })
    expect(result.success).toBe(false)
  })

  it('accepts all valid content types', () => {
    for (const type of ['seednote', 'article', 'viral_analysis'] as const) {
      expect(createTaskSchema.safeParse({
        project_id: type === 'viral_analysis' ? '' : 'ch-1',
        type,
        prompt: type === 'viral_analysis' ? 'https://www.xiaohongshu.com/explore/mock' : '测试',
      }).success).toBe(true)
    }
  })

  it('applies default values for quantity and image_ratio', () => {
    const result = createTaskSchema.parse({
      project_id: 'ch-1',
      type: 'article',
      prompt: '测试',
    })
    expect(result.quantity).toBe(1)
    expect(result.image_ratio).toBe('')
  })

  it('defaults article image toggles to true (legacy "always generate both")', () => {
    const result = createTaskSchema.parse({
      project_id: 'ch-1',
      type: 'article',
      prompt: '测试',
    })
    expect(result.article_with_cover).toBe(true)
    expect(result.article_with_content_images).toBe(true)
  })

  it('honors explicit article_with_cover=false', () => {
    const result = createTaskSchema.parse({
      project_id: 'ch-1',
      type: 'article',
      prompt: '测试',
      article_with_cover: false,
      article_with_content_images: false,
    })
    expect(result.article_with_cover).toBe(false)
    expect(result.article_with_content_images).toBe(false)
  })

  it('accepts goal_mode with a non-empty goal', () => {
    expect(createTaskSchema.safeParse({
      project_id: 'ch-1',
      type: 'article',
      prompt: '测试',
      goal_mode: true,
      goal: '字数 ≥ 1500',
    }).success).toBe(true)
  })

  it('rejects goal_mode=true without a goal', () => {
    const result = createTaskSchema.safeParse({
      project_id: 'ch-1',
      type: 'article',
      prompt: '测试',
      goal_mode: true,
      goal: '',
    })
    expect(result.success).toBe(false)
  })

  it('rejects goal longer than 4000 characters', () => {
    const result = createTaskSchema.safeParse({
      project_id: 'ch-1',
      type: 'article',
      prompt: '测试',
      goal_mode: true,
      goal: 'a'.repeat(4001),
    })
    expect(result.success).toBe(false)
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

  it('accepts prompt at max length', () => {
    const result = planSchema.safeParse({
      type: 'article',
      cron_expr: '0 9 * * 1',
      prompt: 'a'.repeat(5120),
    })
    expect(result.success).toBe(true)
  })

  it('counts unicode prompt length like the API', () => {
    const result = planSchema.safeParse({
      type: 'article',
      cron_expr: '0 9 * * 1',
      prompt: '😀'.repeat(5120),
    })
    expect(result.success).toBe(true)
  })

  it('rejects prompt exceeding max length', () => {
    const result = planSchema.safeParse({
      type: 'article',
      cron_expr: '0 9 * * 1',
      prompt: 'a'.repeat(5121),
    })
    expect(result.success).toBe(false)
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
      type: 'seednote',
      cron_expr: '0 9 * * 1',
      prompt: '主题方向',
      project_id: 'ch-1',
    })
    expect(result.success).toBe(true)
  })

  it('defaults article image toggles to true', () => {
    const result = planSchema.parse({
      type: 'article',
      cron_expr: '0 9 * * 1',
      prompt: '主题方向',
    })
    expect(result.article_with_cover).toBe(true)
    expect(result.article_with_content_images).toBe(true)
  })
})

describe('projectSchema', () => {
  it('accepts article platform without wechat_app_id when publishing disabled', () => {
    expect(projectSchema.safeParse({
      platform: 'article',
      enable_publishing: false,
    }).success).toBe(true)
  })

  it('accepts article platform with wechat_app_id when publishing enabled', () => {
    expect(projectSchema.safeParse({
      platform: 'article',
      enable_publishing: true,
      wechat_app_id: 'wx123',
    }).success).toBe(true)
  })

  it('rejects article platform without wechat_app_id when publishing enabled', () => {
    const result = projectSchema.safeParse({
      platform: 'article',
      enable_publishing: true,
    })
    expect(result.success).toBe(false)
  })

  it('accepts seednote platform without wechat_app_id', () => {
    expect(projectSchema.safeParse({
      platform: 'seednote',
    }).success).toBe(true)
  })

  it('accepts all optional fields', () => {
    const result = projectSchema.safeParse({
      platform: 'article',
      enable_publishing: true,
      wechat_app_id: 'wx123',
      wechat_secret: 'secret',
      name: '项目名称',
      keywords: '测试',
      positioning: '定位',
      style: 'casual-science',
      theme: 'autumn-warm',
      author: '作者',
    })
    expect(result.success).toBe(true)
  })
})
