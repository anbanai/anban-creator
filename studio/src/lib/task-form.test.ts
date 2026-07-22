import { describe, expect, it } from 'vitest'
import type { Project, Task } from '@/types'
import {
  cloneTaskFormDefaults,
  createTaskFormDefaults,
  switchTaskFormDefaults,
  taskFormValuesToRequest,
} from './task-form'
import type { TaskFormDefaults } from './task-form'

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: 'project-1',
    user_id: 'user-1',
    platform: 'seednote',
    name: 'Project',
    avatar_url: '',
    profile_url: '',
    keywords: '',
    visual_style: '',
    writer: '',
    theme: '',
    author: '',
    template_id: '',
    image_ratio: '3:4',
    max_concurrent_tasks: 1,
    config: {},
    status: 'active',
    created_at: '2026-07-22T00:00:00Z',
    updated_at: '2026-07-22T00:00:00Z',
    ...overrides,
  }
}

function task(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    type: 'seednote',
    prompt: 'Original prompt',
    status: 'completed',
    project_id: 'project-1',
    published: false,
    published_at: null,
    billing_price_credits: 0,
    created_at: '2026-07-22T00:00:00Z',
    started_at: '2026-07-22T00:00:01Z',
    completed_at: '2026-07-22T00:01:00Z',
    ...overrides,
  }
}

describe('task form mapping', () => {
  it('creates complete defaults from the selected project without sharing project config objects', () => {
    const source = project({
      id: 'ecommerce-project',
      platform: 'ecommerce',
      image_ratio: '1:1',
      ecommerce_defaults: {
        image_model_key: 'model-ecommerce',
        default_selected_modules: { main_image: 2 },
        target_platform: 'tmall',
      },
    })

    const defaults = createTaskFormDefaults(source)

    expect(defaults).toMatchObject({
      project_id: 'ecommerce-project',
      type: 'ecommerce',
      quantity: 1,
      image_ratio: '1:1',
      image_model_key: 'model-ecommerce',
      watermark: false,
      goal: '',
      goal_mode: false,
      has_content_image: true,
      has_tail_image: false,
      article_with_cover: true,
      article_with_content_images: true,
      product_photos: [],
      selected_modules: { main_image: 2 },
      target_platform: 'tmall',
      montage_input: undefined,
    })

    defaults.selected_modules.main_image = 9
    expect(source.ecommerce_defaults?.default_selected_modules).toEqual({ main_image: 2 })
  })

  it('switches project by resetting type-specific fields to the new project defaults', () => {
    const ecommerceProject = project({
      id: 'ecommerce-project',
      platform: 'ecommerce',
      image_ratio: '1:1',
      ecommerce_defaults: {
        image_model_key: 'project-model',
        default_selected_modules: { detail_image: 3 },
        target_platform: 'douyin',
      },
    })

    const current = {
      ...createTaskFormDefaults(project({ platform: 'article' })),
      prompt: 'Keep this prompt',
      quantity: 3,
      input_attachments: [{ type: 'document' as const, key: 'brief' }],
      reference_image: { asset_id: 'asset-1' } as const,
      watermark: true,
      goal: 'Keep this goal',
      goal_mode: true,
      product_photos: ['oss://stale-product.png'],
      selected_modules: { stale: 9 },
      target_platform: 'stale',
      selling_points: 'stale',
      language: 'en',
      montage_input: {
        brief: 'stale video',
        source_assets: [],
        delivery_targets: [],
      },
    }

    const switched = switchTaskFormDefaults(current, ecommerceProject)

    expect(switched).toMatchObject({
      project_id: 'ecommerce-project',
      type: 'ecommerce',
      prompt: 'Keep this prompt',
      quantity: 3,
      input_attachments: [{ type: 'document', key: 'brief' }],
      reference_image: { asset_id: 'asset-1' },
      watermark: true,
      goal: 'Keep this goal',
      goal_mode: true,
      image_ratio: '1:1',
      image_model_key: 'project-model',
      product_photos: [],
      selected_modules: { detail_image: 3 },
      target_platform: 'douyin',
      selling_points: '',
      language: '',
      montage_input: undefined,
      has_content_image: true,
      has_tail_image: false,
      article_with_cover: true,
      article_with_content_images: true,
    })
  })

  it('switches into Montage with the current prompt as the editable video brief', () => {
    const montageProject = project({
      id: 'montage-project',
      platform: 'montage',
      montage_defaults: {
        default_pipeline: 'social-short',
        preferences: { aspect_ratio: '16:9', duration_seconds: 45 },
        delivery_targets: ['douyin'],
      },
    })
    const current = {
      ...createTaskFormDefaults(project({ platform: 'ecommerce' })),
      prompt: 'Keep this video brief',
      product_photos: ['oss://stale-product.png'],
      selected_modules: { stale: 1 },
      target_platform: 'stale',
      montage_input: {
        brief: 'stale',
        source_assets: [],
        delivery_targets: [],
      },
    }

    const switched = switchTaskFormDefaults(current, montageProject)

    expect(switched).toMatchObject({
      project_id: 'montage-project',
      type: 'montage',
      prompt: 'Keep this video brief',
      product_photos: [],
      selected_modules: {},
      target_platform: '',
      montage_input: {
        brief: 'Keep this video brief',
        pipeline_key: 'social-short',
        preferences: { aspect_ratio: '16:9', duration_seconds: 45 },
        delivery_targets: ['douyin'],
      },
    })

    switched.montage_input!.delivery_targets.push('xiaohongshu')
    expect(montageProject.montage_defaults?.delivery_targets).toEqual(['douyin'])
    expect(current.montage_input?.delivery_targets).toEqual([])
    expect(current.selected_modules).toEqual({ stale: 1 })
  })

  it('creates a fresh Montage form default from project Montage settings', () => {
    const source = project({
      id: 'montage-project',
      platform: 'montage',
      montage_defaults: {
        default_pipeline: 'social-short',
        preferences: { aspect_ratio: '16:9', duration_seconds: 45, style: 'clean' },
        delivery_targets: ['douyin'],
      },
    })

    const defaults = createTaskFormDefaults(source)

    expect(defaults).toMatchObject({
      project_id: 'montage-project',
      type: 'montage',
      quantity: 1,
      montage_input: {
        brief: '',
        pipeline_key: 'social-short',
        preferences: { aspect_ratio: '16:9', duration_seconds: 45, style: 'clean' },
        delivery_targets: ['douyin'],
      },
    })
    defaults.montage_input?.delivery_targets?.push('xiaohongshu')
    expect(source.montage_defaults?.delivery_targets).toEqual(['douyin'])
  })

  it.each([
    ['article', { article_with_cover: false, article_with_content_images: false }],
    ['seednote', { has_content_image: false, has_tail_image: false }],
    ['ecommerce', { selected_modules: { hero: 2 }, product_photos: ['oss://product.png'], target_platform: 'tmall', selling_points: 'Lightweight', language: 'zh-CN' }],
    ['montage', { montage_input: { brief: 'Launch video', pipeline_key: 'launch', source_assets: [{ type: 'image_url', url: 'oss://asset.png' }], preferences: { aspect_ratio: '9:16', duration_seconds: 30 }, delivery_targets: ['douyin'], advanced: { render: { fps: 30 } } } }],
  ] as const)('clones complete %s task creation defaults', (type, platformFields) => {
    const source = task({
      type,
      project_id: `${type}-project`,
      prompt: `${type} prompt`,
      image_ratio: '16:9',
      image_model_key: `${type}-model`,
      skip_reference_image: false,
      execution_target: 'local',
      watermark: false,
      goal: 'Publish-ready',
      goal_mode: false,
      has_content_image: false,
      has_tail_image: false,
      article_with_cover: false,
      article_with_content_images: false,
      input_attachments: [
        { type: 'document', key: 'keep', role: 'brief', instruction: 'use this' },
        { type: 'text', text: 'continue', role: 'resume_latest' },
        { type: 'document', key: 'resume', role: 'resume_file' },
      ],
      reference_image: {
        asset_id: 'asset-1',
        file_name: 'reference.png',
        content_type: 'image/png',
        size: 10,
        download_url: 'https://example.test/reference.png',
        download_expires_at: '2026-07-23T00:00:00Z',
      },
      ecommerce: type === 'ecommerce' ? {
        selected_modules: { hero: 2 },
        product_photos: ['oss://product.png'],
        target_platform: 'tmall',
        selling_points: 'Lightweight',
        language: 'zh-CN',
      } : undefined,
      montage_input: type === 'montage' ? {
        brief: 'Launch video',
        pipeline_key: 'launch',
        source_assets: [{ type: 'image_url', url: 'oss://asset.png' }],
        preferences: { aspect_ratio: '9:16', duration_seconds: 30 },
        delivery_targets: ['douyin'],
        advanced: { render: { fps: 30 } },
      } : undefined,
    })

    const defaults = cloneTaskFormDefaults(source)

    expect(defaults).toMatchObject({
      project_id: `${type}-project`,
      type,
      prompt: `${type} prompt`,
      quantity: 1,
      image_ratio: '16:9',
      image_model_key: `${type}-model`,
      skip_reference_image: false,
      execution_target: 'local',
      watermark: false,
      goal: 'Publish-ready',
      goal_mode: false,
      has_content_image: false,
      has_tail_image: false,
      article_with_cover: false,
      article_with_content_images: false,
      reference_image: { asset_id: 'asset-1' },
      input_attachments: [{ type: 'document', key: 'keep', role: 'brief', instruction: 'use this' }],
      ...platformFields,
    })

    expect(defaults.input_attachments).not.toBe(source.input_attachments)
    expect(defaults.input_attachments?.[0]).not.toBe(source.input_attachments?.[0])
    expect(defaults.input_attachments?.map((attachment) => attachment.role)).not.toContain('resume_latest')
    expect(defaults.input_attachments?.map((attachment) => attachment.role)).not.toContain('resume_file')
  })

  it('does not share clone attachment, ecommerce, or Montage nested values with the source task', () => {
    const source = task({
      type: 'montage',
      input_attachments: [{ type: 'document', key: 'brief', instruction: 'original' }],
      ecommerce: { selected_modules: { hero: 1 }, product_photos: ['oss://product.png'] },
      montage_input: {
        source_assets: [{ type: 'image_url', url: 'oss://source.png' }],
        preferences: { style: 'original' },
        delivery_targets: ['douyin'],
        advanced: { render: { fps: 30 } },
      },
    })

    const defaults = cloneTaskFormDefaults(source)
    defaults.input_attachments![0].instruction = 'changed'
    defaults.selected_modules.hero = 9
    defaults.product_photos![0] = 'oss://changed.png'
    defaults.montage_input!.source_assets![0].url = 'oss://changed-source.png'
    ;(defaults.montage_input!.advanced!.render as { fps: number }).fps = 60

    expect(source.input_attachments?.[0].instruction).toBe('original')
    expect(source.ecommerce?.selected_modules).toEqual({ hero: 1 })
    expect(source.ecommerce?.product_photos).toEqual(['oss://product.png'])
    expect(source.montage_input?.source_assets?.[0].url).toBe('oss://source.png')
    expect((source.montage_input?.advanced?.render as { fps: number }).fps).toBe(30)
  })

  it('normalizes optional persisted Montage collections into editable form collections', () => {
    const defaults = cloneTaskFormDefaults(task({
      type: 'montage',
      prompt: 'Video brief',
      montage_input: { brief: 'Video brief', pipeline_key: 'social-short' },
    }))

    expect(defaults.montage_input).toMatchObject({
      brief: 'Video brief',
      pipeline_key: 'social-short',
      source_assets: [],
      delivery_targets: [],
    })
  })

  it('serializes active Montage fields without mutating form values', () => {
    const values = cloneTaskFormDefaults(task({
      type: 'montage',
      watermark: false,
      goal_mode: false,
      has_content_image: false,
      has_tail_image: false,
      article_with_cover: false,
      article_with_content_images: false,
      ecommerce: { selected_modules: { hero: 1 }, product_photos: ['oss://product.png'] },
      montage_input: { advanced: { render: { fps: 30 } } },
    }))
    values.quantity = 3

    const request = taskFormValuesToRequest(values)

    expect(request).toMatchObject({
      type: 'montage',
      project_id: 'project-1',
      quantity: 3,
      watermark: false,
      goal_mode: false,
      montage_input: { advanced: { render: { fps: 30 } } },
    })
    ;(request.montage_input!.advanced!.render as { fps: number }).fps = 60
    expect((values.montage_input?.advanced?.render as { fps: number }).fps).toBe(30)
    expect(request).not.toHaveProperty('selected_modules')
    expect(request).not.toHaveProperty('product_photos')
    expect(request).not.toHaveProperty('has_content_image')
    expect(request).not.toHaveProperty('article_with_cover')
  })

  it.each([
    {
      name: 'seednote trims shared text and keeps explicit composition false values',
      values: {
        type: 'seednote' as const,
        prompt: '  Seednote prompt  ',
        goal_mode: true,
        goal: '  publish ready  ',
        has_content_image: false,
        has_tail_image: false,
        article_with_cover: false,
        article_with_content_images: false,
        product_photos: ['oss://stale.png'],
        selected_modules: { stale: 1 },
        target_platform: 'stale',
        selling_points: 'stale',
        language: 'en',
        montage_input: { brief: 'stale', source_assets: [], delivery_targets: [] },
        execution_target: 'local_claimed' as const,
      },
      expected: {
        prompt: 'Seednote prompt',
        goal_mode: true,
        goal: 'publish ready',
        has_content_image: false,
        has_tail_image: false,
        execution_target: 'local',
      },
      omitted: ['article_with_cover', 'article_with_content_images', 'product_photos', 'selected_modules', 'target_platform', 'selling_points', 'language', 'montage_input'],
    },
    {
      name: 'article omits empty prompt and inactive platform fields while retaining explicit false article settings',
      values: {
        type: 'article' as const,
        prompt: '   ',
        goal_mode: false,
        goal: '  stale goal  ',
        has_content_image: false,
        has_tail_image: false,
        article_with_cover: false,
        article_with_content_images: false,
        product_photos: ['oss://stale.png'],
        selected_modules: { stale: 1 },
        montage_input: { brief: 'stale', source_assets: [], delivery_targets: [] },
        execution_target: 'cloud' as const,
      },
      expected: {
        prompt: undefined,
        goal_mode: false,
        article_with_cover: false,
        article_with_content_images: false,
        execution_target: 'cloud',
      },
      omitted: ['goal', 'has_content_image', 'has_tail_image', 'product_photos', 'selected_modules', 'target_platform', 'selling_points', 'language', 'montage_input'],
    },
    {
      name: 'ecommerce gates non-ecommerce values and keeps only active delivery modules',
      values: {
        type: 'ecommerce' as const,
        prompt: '  Product launch  ',
        goal_mode: true,
        goal: '  stale goal  ',
        has_content_image: false,
        has_tail_image: false,
        article_with_cover: false,
        article_with_content_images: false,
        product_photos: ['oss://product.png'],
        selected_modules: { hero: 2, ignored: 0 },
        target_platform: 'tmall',
        selling_points: '  Light and portable  ',
        language: 'zh-CN',
        montage_input: { brief: 'stale', source_assets: [], delivery_targets: [] },
        execution_target: 'local' as const,
      },
      expected: {
        prompt: 'Product launch',
        product_photos: ['oss://product.png'],
        selected_modules: { hero: 2, ignored: 0 },
        target_platform: 'tmall',
        selling_points: 'Light and portable',
        language: 'zh-CN',
        execution_target: 'local',
      },
      omitted: ['goal', 'goal_mode', 'has_content_image', 'has_tail_image', 'article_with_cover', 'article_with_content_images', 'montage_input'],
    },
    {
      name: 'Montage normalizes nested submission input and omits execution target',
      values: {
        type: 'montage' as const,
        prompt: '  Launch video  ',
        goal_mode: true,
        goal: '  publish ready  ',
        has_content_image: false,
        has_tail_image: false,
        article_with_cover: false,
        article_with_content_images: false,
        product_photos: ['oss://stale.png'],
        selected_modules: { stale: 1 },
        montage_input: {
          brief: '  Launch video  ',
          pipeline_key: ' social-short ',
          source_assets: [{ type: 'image_url' as const, url: 'oss://source.png' }],
          preferences: { aspect_ratio: '9:16', duration_seconds: 30, style: '  clean  ', music_prompt: '  upbeat  ' },
          delivery_targets: ['douyin'],
        },
        execution_target: 'local_claimed' as const,
      },
      expected: {
        prompt: 'Launch video',
        goal_mode: true,
        goal: 'publish ready',
        montage_input: {
          brief: 'Launch video',
          pipeline_key: 'social-short',
          source_assets: [{ type: 'image_url', url: 'oss://source.png' }],
          preferences: { aspect_ratio: '9:16', duration_seconds: 30, style: 'clean', music_prompt: 'upbeat' },
          delivery_targets: ['douyin'],
        },
      },
      omitted: ['has_content_image', 'has_tail_image', 'article_with_cover', 'article_with_content_images', 'product_photos', 'selected_modules', 'target_platform', 'selling_points', 'language', 'execution_target'],
    },
  ])('$name', ({ values, expected, omitted }) => {
    const formValues: TaskFormDefaults = {
      ...createTaskFormDefaults(project({ id: `${values.type}-project`, platform: values.type as Project['platform'] })),
      ...(values as unknown as Partial<TaskFormDefaults>),
      quantity: 1,
      image_ratio: '',
      image_model_key: '',
      reference_image: null,
      skip_reference_image: false,
    }

    const request = taskFormValuesToRequest(formValues)

    expect(request).toMatchObject({
      type: values.type,
      project_id: `${values.type}-project`,
      quantity: 1,
      watermark: false,
      skip_reference_image: false,
      ...expected,
    })
    expect(request.image_ratio).toBeUndefined()
    expect(request.image_model_key).toBeUndefined()
    expect(request.reference_image).toBeUndefined()
    for (const key of omitted) expect(request).not.toHaveProperty(key)
  })

  it('preserves an explicit reference clearing request when cloning skips the reference image', () => {
    const values = {
      ...createTaskFormDefaults(project()),
      skip_reference_image: true,
      reference_image: null,
    }

    expect(taskFormValuesToRequest(values)).toMatchObject({
      skip_reference_image: true,
      reference_image: null,
    })
  })
})
