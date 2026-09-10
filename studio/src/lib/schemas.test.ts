import { describe, it, expect } from 'vitest'
import {
  loginSchema,
  registerSchema,
  createTaskSchema as rawCreateTaskSchema,
  planSchema as rawPlanSchema,
  projectSchema,
  normalizeImageRatio,
} from './schemas'

function withExecutionProfile(input: unknown) {
  return {
    execution_profile: 'effective' as const,
    ...(input as Record<string, unknown>),
  }
}

const createTaskSchema = {
  parse: (input: unknown) => rawCreateTaskSchema.parse(withExecutionProfile(input)),
  safeParse: (input: unknown) => rawCreateTaskSchema.safeParse(withExecutionProfile(input)),
}

const planSchema = {
  parse: (input: unknown) => rawPlanSchema.parse(withExecutionProfile(input)),
  safeParse: (input: unknown) => rawPlanSchema.safeParse(withExecutionProfile(input)),
}

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
  it('accepts only the current execution profile IDs', () => {
    const base = { project_id: 'ch-1', type: 'article', prompt: '测试主题' }

    for (const executionProfile of ['effective', 'balanced', 'quality']) {
      expect(rawCreateTaskSchema.safeParse({ ...base, execution_profile: executionProfile }).success).toBe(true)
    }
    for (const executionProfile of ['cost_effective', 'maximum_quality']) {
      expect(rawCreateTaskSchema.safeParse({ ...base, execution_profile: executionProfile }).success).toBe(false)
    }
  })

  it('requires a selected execution profile', () => {
    const base = { project_id: 'ch-1', type: 'article', prompt: '测试主题' }
    expect(rawCreateTaskSchema.safeParse(base).success).toBe(false)
    expect(rawCreateTaskSchema.safeParse({ ...base, execution_profile: '' }).success).toBe(false)
  })

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

  it('rejects viral analysis without a seednote project', () => {
    const result = createTaskSchema.safeParse({
      project_id: '',
      type: 'viral_analysis',
      prompt: '帮我拆解这篇 http://xhslink.com/a1b2c3',
    })
    expect(result.success).toBe(false)
  })

  it('accepts viral analysis with a seednote project and note URL', () => {
    const result = createTaskSchema.safeParse({
      project_id: 'seednote-project',
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
    for (const type of ['seednote', 'article', 'moments', 'viral_analysis'] as const) {
      expect(createTaskSchema.safeParse({
        project_id: type === 'viral_analysis' ? 'seednote-project' : 'ch-1',
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
    expect(result.image_ratio).toBe('auto')
  })

  it('defaults task input attachments to an empty snapshot', () => {
    const result = createTaskSchema.parse({
      project_id: 'seednote-1',
      type: 'seednote',
      prompt: '测试',
    })

    expect(result.input_attachments).toEqual([])
  })

  it('accepts up to 16 Seednote images with 1000 Unicode code points per instruction', () => {
    const result = createTaskSchema.safeParse({
      project_id: 'seednote-1',
      type: 'seednote',
      prompt: '测试',
      input_attachments: Array.from({ length: 16 }, (_, index) => ({
        type: 'image',
        url: `/reference-${index + 1}.png`,
        instruction: '😀'.repeat(1000),
      })),
    })

    expect(result.success).toBe(true)
  })

  it('rejects more than 16 Seednote reference images', () => {
    const result = createTaskSchema.safeParse({
      project_id: 'seednote-1',
      type: 'seednote',
      prompt: '测试',
      input_attachments: Array.from({ length: 17 }, (_, index) => ({
        type: 'image',
        url: `/reference-${index + 1}.png`,
      })),
    })

    expect(result.success).toBe(false)
  })

  it('accepts non-image Seednote attachments', () => {
    const result = createTaskSchema.safeParse({
      project_id: 'seednote-1',
      type: 'seednote',
      prompt: '测试',
      input_attachments: [{ type: 'document', url: '/brief.pdf' }],
    })

    expect(result.success).toBe(true)
  })

  it('rejects a reference instruction over 1000 Unicode code points', () => {
    const result = createTaskSchema.safeParse({
      project_id: 'seednote-1',
      type: 'seednote',
      prompt: '测试',
      input_attachments: [{
        type: 'image',
        url: '/reference.png',
        instruction: '😀'.repeat(1001),
      }],
    })

    expect(result.success).toBe(false)
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

  it('strips retired strong-goal fields from legacy form data', () => {
    const result = createTaskSchema.parse({
      project_id: 'ch-1',
      type: 'article',
      prompt: '测试',
      goal_mode: true,
      goal: '字数 ≥ 1500',
    })
    expect(result).not.toHaveProperty('goal_mode')
    expect(result).not.toHaveProperty('goal')
  })

  it('accepts moments tasks without a separate image-mode field', () => {
    const result = createTaskSchema.parse({
      project_id: 'moments-1',
      type: 'moments',
      prompt: '把成交复盘写成朋友圈',
      image_ratio: '3:4',
    })

    expect(result.type).toBe('moments')
    expect(result.image_ratio).toBe('3:4')
    expect('moments_with_image' in result).toBe(false)
  })

  it('accepts montage task input without exposing execution target choice', () => {
    const result = createTaskSchema.safeParse({
      project_id: 'project-montage',
      type: 'montage',
      image_ratio: '9:16',
      montage_input: {
        brief: '做一条新品发布短片',
        pipeline_key: 'default',
        preferences: {
          aspect_ratio: '16:9',
          duration_seconds: 30,
        },
      },
    })

    expect(result.success).toBe(true)
    if (result.success) {
      expect(result.data).not.toHaveProperty('execution_target')
      expect(result.data.image_ratio).toBe('9:16')
      expect(result.data.montage_input?.preferences).not.toHaveProperty('aspect_ratio')
    }
  })

  it('requires brief for montage tasks', () => {
    const result = createTaskSchema.safeParse({
      project_id: 'project-montage',
      type: 'montage',
      montage_input: {
        brief: '',
      },
    })

    expect(result.success).toBe(false)
  })
})

describe('normalizeImageRatio', () => {
  it('keeps supported ratios and maps unknown stored values to smart mode', () => {
    expect(normalizeImageRatio('3:4')).toBe('3:4')
    expect(normalizeImageRatio('9:16')).toBe('9:16')
    expect(normalizeImageRatio('21:9')).toBe('auto')
    expect(normalizeImageRatio('900x383')).toBe('auto')
    expect(normalizeImageRatio(undefined)).toBe('auto')
  })
})

describe('planSchema', () => {
  it('requires a selected execution profile', () => {
    const base = { type: 'article', cron_expr: '0 9 * * 1' }
    expect(rawPlanSchema.safeParse(base).success).toBe(false)
    expect(rawPlanSchema.safeParse({ ...base, execution_profile: '' }).success).toBe(false)
  })

  it('defaults plan input attachments to an empty snapshot', () => {
    const result = planSchema.parse({
      type: 'seednote',
      cron_expr: '0 9 * * 1',
    })

    expect(result.input_attachments).toEqual([])
  })

  it('accepts up to 16 image references for Seednote plans', () => {
    const result = planSchema.safeParse({
      type: 'seednote',
      cron_expr: '0 9 * * 1',
      input_attachments: Array.from({ length: 16 }, (_, index) => ({
        type: 'image',
        url: `/plan-reference-${index + 1}.png`,
      })),
    })

    expect(result.success).toBe(true)
  })

  it('rejects more than 16 attachments and accepts non-image Seednote plan materials', () => {
    expect(planSchema.safeParse({
      type: 'seednote',
      cron_expr: '0 9 * * 1',
      input_attachments: Array.from({ length: 17 }, (_, index) => ({
        type: 'image',
        url: `/plan-reference-${index + 1}.png`,
      })),
    }).success).toBe(false)

    expect(planSchema.safeParse({
      type: 'seednote',
      cron_expr: '0 9 * * 1',
      input_attachments: [{ type: 'document', url: '/brief.pdf' }],
    }).success).toBe(true)
  })

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

  it('does not accept moments plans in V1', () => {
    const result = planSchema.safeParse({
      project_id: 'moments-1',
      type: 'moments',
      cron_expr: '0 9 * * *',
      prompt: '每日朋友圈',
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

  it('accepts montage plans', () => {
    const result = planSchema.safeParse({
      project_id: 'project-montage',
      type: 'montage',
      cron_expr: '0 10 * * *',
      montage_input: {
        brief: '每天生成一条品牌短片',
      },
    })

    expect(result.success).toBe(true)
  })
})

describe('projectSchema', () => {
  it('accepts article platform without wechat_app_id when publishing disabled', () => {
    expect(projectSchema.safeParse({
      platform: 'article',
      wechat_publish_mode: 'disabled',
    }).success).toBe(true)
  })

  it('accepts article platform with wechat_app_id when publishing enabled', () => {
    expect(projectSchema.safeParse({
      platform: 'article',
      wechat_publish_mode: 'manual',
      wechat_app_id: 'wx123',
    }).success).toBe(true)
  })

  it('rejects article platform without wechat_app_id when publishing enabled', () => {
    const result = projectSchema.safeParse({
      platform: 'article',
      wechat_publish_mode: 'api_confirmed',
    })
    expect(result.success).toBe(false)
  })

  it('accepts seednote platform without wechat_app_id', () => {
    expect(projectSchema.safeParse({
      platform: 'seednote',
    }).success).toBe(true)
  })

  it('accepts moments platform without publishing credentials', () => {
    expect(projectSchema.safeParse({
      platform: 'moments',
      name: '朋友圈项目',
      instructions: '私域成交内容',
      image_ratio: '3:4',
    }).success).toBe(true)
  })

  it('accepts montage project defaults', () => {
    const result = projectSchema.safeParse({
      platform: 'montage',
      name: 'Montage 项目',
      image_ratio: '9:16',
      montage_defaults: {
        default_pipeline: 'social-short',
        preferences: {
          aspect_ratio: '16:9',
          duration_seconds: 30,
        },
        delivery_targets: ['final_video'],
      },
    })
    expect(result.success).toBe(true)
    if (result.success) {
      expect(result.data.image_ratio).toBe('9:16')
      expect(result.data.montage_defaults?.preferences).not.toHaveProperty('aspect_ratio')
    }
  })

  it('accepts all optional fields', () => {
    const result = projectSchema.safeParse({
      platform: 'article',
      wechat_publish_mode: 'manual',
      wechat_app_id: 'wx123',
      wechat_secret: 'secret',
      name: '项目名称',
      keywords: '测试',
      instructions: '定位',
      style: 'casual-science',
      theme: 'autumn-warm',
      author: '作者',
    })
    expect(result.success).toBe(true)
  })
})
