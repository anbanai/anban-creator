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
    execution_profile: 'effective',
    billing_price_credits: 0,
    created_at: '2026-07-22T00:00:00Z',
    started_at: '2026-07-22T00:00:01Z',
    completed_at: '2026-07-22T00:01:00Z',
    ...overrides,
  }
}

describe('task form mapping', () => {
  it('maps replication defaults and new remix brief without mutating source', () => {
    const defaults = createTaskFormDefaults(project({ platform: 'hypit', hypit_defaults: { preferences: { aspect_ratio: '16:9', language: '中文' } } }))
    expect(defaults.hypit_input?.preferences).toEqual({ aspect_ratio: '16:9', language: '中文' })
    const source = task({ type: 'hypit', hypit_input: { brief: 'original', reference: { type: 'video_url', url: 'https://example.com/ref.mp4' }, preferences: { duration_seconds: 0 } } })
    const clone = cloneTaskFormDefaults(source)
    expect(clone.hypit_input?.preferences?.duration_seconds).toBeUndefined()
    expect(source.hypit_input?.preferences?.duration_seconds).toBe(0)
    clone.prompt = 'replace the product'
    const request = taskFormValuesToRequest(clone)
    expect(request.hypit_input?.brief).toBe('replace the product')
    expect(request.hypit_input?.reference?.url).toBe('https://example.com/ref.mp4')
    expect(source.hypit_input?.brief).toBe('original')
  })

  /* Agent Pack extension snapshots remain separate from typed business fields. */
  it('preserves agent_input through clone defaults and request mapping', () => {
    const defaults = cloneTaskFormDefaults(task({ agent_input: {} }))
    expect(defaults.agent_input).toEqual({})
    expect(taskFormValuesToRequest(defaults).agent_input).toEqual({})
  })

  it('keeps viral_analysis when selecting a seednote project', () => {
    const seednoteProject = project({ id: 'seednote-project', platform: 'seednote' })
    const current = {
      ...createTaskFormDefaults(seednoteProject),
      type: 'viral_analysis' as const,
      prompt: 'https://www.xiaohongshu.com/explore/note-1',
    }

    const result = switchTaskFormDefaults(current, seednoteProject)

    expect(result.type).toBe('viral_analysis')
    expect(result.project_id).toBe(seednoteProject.id)
  })

  it('does not send image options for viral analysis tasks', () => {
    const values = {
      ...createTaskFormDefaults(project()),
      type: 'viral_analysis' as const,
      execution_profile: 'effective' as const,
      prompt: 'https://www.xiaohongshu.com/explore/note-1',
      image_ratio: '3:4' as const,
      image_capability_key: 'image-model',
      skip_reference_image: true,
      reference_image: { asset_id: '11111111-1111-4111-8111-111111111111' } as const,
      watermark: true,
    }

    expect(taskFormValuesToRequest(values)).toEqual({
      type: 'viral_analysis',
      execution_profile: 'effective',
      prompt: 'https://www.xiaohongshu.com/explore/note-1',
      project_id: 'project-1',
      quantity: 1,
      input_attachments: [],
      agent_input: {},
    })
  })

  it('requires an execution profile in defaults and request payloads', () => {
    const values = createTaskFormDefaults(project())
    expect(values.execution_profile).toBe('')

    values.execution_profile = 'balanced'
    expect(taskFormValuesToRequest(values)).toMatchObject({ execution_profile: 'balanced' })
  })

  it('preserves the source execution profile when cloning', () => {
    const defaults = cloneTaskFormDefaults(task({ execution_profile: 'quality' }))
    expect(defaults.execution_profile).toBe('quality')
    expect(taskFormValuesToRequest(defaults).execution_profile).toBe('quality')
  })

  it('creates complete defaults from the selected project without sharing project config objects', () => {
    const source = project({
      id: 'ecommerce-project',
      platform: 'ecommerce',
      image_ratio: '1:1',
      ecommerce_defaults: {
        image_capability_key: 'model-ecommerce',
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
      image_capability_key: 'model-ecommerce',
      watermark: false,
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
        image_capability_key: 'project-model',
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
      image_ratio: '1:1',
      image_capability_key: 'project-model',
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

  it('preserves ecommerce inputs but applies destination defaults between ecommerce projects', () => {
    const current = {
      ...createTaskFormDefaults(project({
        id: 'source-ecommerce',
        platform: 'ecommerce',
        image_ratio: '1:1',
        ecommerce_defaults: {
          image_capability_key: 'source-capability',
          default_selected_modules: { hero: 1 },
          target_platform: 'tmall',
        },
      })),
      prompt: 'Keep ecommerce prompt',
      quantity: 3,
      input_attachments: [{ type: 'document' as const, key: 'brief' }],
      watermark: true,
      product_photos: ['oss://product.png'],
      selected_modules: { hero: 3, detail: 2 },
      target_platform: 'tmall',
      selling_points: 'Lightweight and durable',
      language: 'zh-CN',
    }
    const destination = project({
      id: 'destination-ecommerce',
      platform: 'ecommerce',
      image_ratio: '4:3',
      ecommerce_defaults: {
        image_capability_key: 'destination-capability',
        default_selected_modules: { destination_hero: 1 },
        target_platform: 'douyin',
      },
    })

    const switched = switchTaskFormDefaults(current, destination)

    expect(switched).toMatchObject({
      project_id: 'destination-ecommerce',
      type: 'ecommerce',
      prompt: 'Keep ecommerce prompt',
      quantity: 3,
      input_attachments: [{ type: 'document', key: 'brief' }],
      watermark: true,
      image_ratio: '4:3',
      image_capability_key: 'destination-capability',
      product_photos: ['oss://product.png'],
      selected_modules: { destination_hero: 1 },
      target_platform: 'douyin',
      selling_points: 'Lightweight and durable',
      language: 'zh-CN',
    })
  })

  it('preserves article image toggles but applies destination image defaults', () => {
    const current = {
      ...createTaskFormDefaults(project({
        id: 'source-article',
        platform: 'article',
        image_ratio: '1:1',
        ecommerce_defaults: { image_capability_key: 'source-capability' },
      })),
      article_with_cover: false,
      article_with_content_images: false,
    }
    const destination = project({
      id: 'destination-article',
      platform: 'article',
      image_ratio: '16:9',
      ecommerce_defaults: { image_capability_key: 'destination-capability' },
    })

    expect(switchTaskFormDefaults(current, destination)).toMatchObject({
      project_id: 'destination-article',
      type: 'article',
      image_ratio: '16:9',
      image_capability_key: 'destination-capability',
      article_with_cover: false,
      article_with_content_images: false,
    })
  })

  it('preserves seednote image toggles but applies destination image defaults', () => {
    const current = {
      ...createTaskFormDefaults(project({
        id: 'source-seednote',
        platform: 'seednote',
        image_ratio: '1:1',
        ecommerce_defaults: { image_capability_key: 'source-capability' },
      })),
      has_content_image: false,
      has_tail_image: true,
    }
    const destination = project({
      id: 'destination-seednote',
      platform: 'seednote',
      image_ratio: '3:4',
      ecommerce_defaults: { image_capability_key: 'destination-capability' },
    })

    expect(switchTaskFormDefaults(current, destination)).toMatchObject({
      project_id: 'destination-seednote',
      type: 'seednote',
      image_ratio: '3:4',
      image_capability_key: 'destination-capability',
      has_content_image: false,
      has_tail_image: true,
    })
  })

  it('preserves Montage inputs but applies destination pipeline defaults', () => {
    const current = {
      ...createTaskFormDefaults(project({
        id: 'source-montage',
        platform: 'montage',
        image_ratio: '1:1',
        ecommerce_defaults: { image_capability_key: 'source-capability' },
      })),
      prompt: 'Keep montage brief',
      montage_input: {
        brief: 'Keep montage brief',
        pipeline_key: 'custom-pipeline',
        source_assets: [{ type: 'video_url' as const, url: 'oss://source.mp4' }],
        preferences: { duration_seconds: 37, style: 'kinetic' },
        delivery_targets: ['douyin'],
        advanced: { render: { fps: 60 } },
      },
    }
    const destination = project({
      id: 'destination-montage',
      platform: 'montage',
      image_ratio: '16:9',
      ecommerce_defaults: { image_capability_key: 'destination-capability' },
      montage_defaults: {
        default_pipeline: 'destination-default',
        preferences: { duration_seconds: 15, style: 'documentary' },
        delivery_targets: ['wechat'],
      },
    })

    const switched = switchTaskFormDefaults(current, destination)

    expect(switched).toMatchObject({
      project_id: 'destination-montage',
      type: 'montage',
      prompt: 'Keep montage brief',
      image_ratio: '16:9',
      image_capability_key: 'destination-capability',
      montage_input: {
        brief: 'Keep montage brief',
        pipeline_key: 'destination-default',
        source_assets: [{ type: 'video_url', url: 'oss://source.mp4' }],
        preferences: { duration_seconds: 15, style: 'documentary' },
        delivery_targets: ['wechat'],
        advanced: { render: { fps: 60 } },
      },
    })
  })

  it('switches into Montage with the current prompt as the editable video brief', () => {
    const montageProject = project({
      id: 'montage-project',
      platform: 'montage',
      montage_defaults: {
        default_pipeline: 'social-short',
        preferences: { duration_seconds: 45 },
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
        preferences: { duration_seconds: 45 },
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
        preferences: { duration_seconds: 45, style: 'clean' },
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
        preferences: { duration_seconds: 45, style: 'clean' },
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
    ['montage', { montage_input: { brief: 'Launch video', pipeline_key: 'launch', source_assets: [{ type: 'image_url', url: 'oss://asset.png' }], preferences: { duration_seconds: 30 }, delivery_targets: ['douyin'], advanced: { render: { fps: 30 } } } }],
  ] as const)('clones complete %s task creation defaults', (type, platformFields) => {
    const source = task({
      type,
      project_id: `${type}-project`,
      prompt: `${type} prompt`,
      image_ratio: type === 'montage' ? '9:16' : '16:9',
      image_capability_key: type === 'montage' ? 'montage-model' : `${type}-model`,
      skip_reference_image: false,
      watermark: false,
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
        preferences: { duration_seconds: 30 },
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
      image_ratio: type === 'montage' ? '9:16' : '16:9',
      image_capability_key: type === 'montage' ? 'montage-model' : `${type}-model`,
      skip_reference_image: false,
      execution_profile: 'effective',
      watermark: false,
      has_content_image: false,
      has_tail_image: false,
      article_with_cover: false,
      article_with_content_images: false,
      reference_image: type === 'article' ? null : { asset_id: 'asset-1' },
      input_attachments: [
        ...(type === 'article' ? [] : [{ type: 'image' as const, asset_id: 'asset-1', file_name: 'reference.png', content_type: 'image/png', size: 10 }]),
        { type: 'document', key: 'keep', role: 'brief', instruction: 'use this' },
      ],
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

  it('preserves and serializes top-level Montage image settings without mutating form values', () => {
    const values = cloneTaskFormDefaults(task({
      type: 'montage',
      image_ratio: '16:9',
      image_capability_key: 'retired-capability',
      watermark: false,
      has_content_image: false,
      has_tail_image: false,
      article_with_cover: false,
      article_with_content_images: false,
      ecommerce: { selected_modules: { hero: 1 }, product_photos: ['oss://product.png'] },
      montage_input: { advanced: { render: { fps: 30 } } },
    }))
    expect(values.image_ratio).toBe('16:9')
    expect(values.image_capability_key).toBe('retired-capability')
    values.quantity = 3

    const request = taskFormValuesToRequest(values)

    expect(request).toMatchObject({
      type: 'montage',
      project_id: 'project-1',
      quantity: 3,
      image_ratio: '16:9',
      image_capability_key: 'retired-capability',
      watermark: false,
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
      },
      expected: {
        prompt: 'Seednote prompt',
        has_content_image: false,
        has_tail_image: false,
      },
      omitted: ['article_with_cover', 'article_with_content_images', 'product_photos', 'selected_modules', 'target_platform', 'selling_points', 'language', 'montage_input'],
    },
    {
      name: 'article omits empty prompt and inactive platform fields while retaining explicit false article settings',
      values: {
        type: 'article' as const,
        prompt: '   ',
        has_content_image: false,
        has_tail_image: false,
        article_with_cover: false,
        article_with_content_images: false,
        product_photos: ['oss://stale.png'],
        selected_modules: { stale: 1 },
        montage_input: { brief: 'stale', source_assets: [], delivery_targets: [] },
      },
      expected: {
        prompt: undefined,
        article_with_cover: false,
        article_with_content_images: false,
      },
      omitted: ['has_content_image', 'has_tail_image', 'product_photos', 'selected_modules', 'target_platform', 'selling_points', 'language', 'montage_input'],
    },
    {
      name: 'ecommerce gates non-ecommerce values and keeps only active delivery modules',
      values: {
        type: 'ecommerce' as const,
        prompt: '  Product launch  ',
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
      },
      expected: {
        prompt: 'Product launch',
        product_photos: ['oss://product.png'],
        selected_modules: { hero: 2, ignored: 0 },
        target_platform: 'tmall',
        selling_points: 'Light and portable',
        language: 'zh-CN',
      },
      omitted: ['has_content_image', 'has_tail_image', 'article_with_cover', 'article_with_content_images', 'montage_input'],
    },
    {
      name: '视频生成 normalizes nested submission input and omits execution target',
      values: {
        type: 'montage' as const,
        prompt: '  Launch video  ',
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
          preferences: { duration_seconds: 30, style: '  clean  ', music_prompt: '  upbeat  ' },
          delivery_targets: ['douyin'],
        },
      },
      expected: {
        prompt: 'Launch video',
        montage_input: {
          brief: 'Launch video',
          pipeline_key: 'social-short',
          source_assets: [{ type: 'image_url', url: 'oss://source.png' }],
          preferences: { duration_seconds: 30, style: 'clean', music_prompt: 'upbeat' },
          delivery_targets: ['douyin'],
        },
      },
      omitted: ['has_content_image', 'has_tail_image', 'article_with_cover', 'article_with_content_images', 'product_photos', 'selected_modules', 'target_platform', 'selling_points', 'language', 'execution_target'],
    },
  ])('$name', ({ values, expected, omitted }) => {
    const formValues: TaskFormDefaults = {
      ...createTaskFormDefaults(project({ id: `${values.type}-project`, platform: values.type as Project['platform'] })),
      ...(values as unknown as Partial<TaskFormDefaults>),
      execution_profile: 'effective',
      quantity: 1,
      image_ratio: 'auto',
      image_capability_key: '',
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
    expect(request.image_ratio).toBe('auto')
    expect(request.image_capability_key).toBeUndefined()
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

  it('keeps project portraits optional unless the cover requirement is selected', () => {
    const values = createTaskFormDefaults(project({ platform: 'article' }))
    expect(values.article_cover_use_portrait).toBe(false)
    expect(taskFormValuesToRequest(values)).toMatchObject({ article_cover_use_portrait: false })
    expect(taskFormValuesToRequest({ ...values, article_with_cover: false })).toMatchObject({ article_cover_use_portrait: false })
  })

  it('submits an explicit project portrait requirement for article covers', () => {
    const values = createTaskFormDefaults(project({ platform: 'article' }))
    expect(taskFormValuesToRequest({ ...values, article_cover_use_portrait: true })).toMatchObject({
      article_cover_use_portrait: true,
    })
  })

  it('keeps a cloned article portrait out of general input attachments', () => {
    const cloned = cloneTaskFormDefaults(task({
      type: 'article',
      article_with_cover: true,
      project_snapshot: { portrait_reference_image_asset_id: '11111111-1111-4111-8111-111111111111' },
      input_attachments: [{ type: 'document', key: 'brief', role: 'brief' }],
      reference_image: {
        asset_id: '11111111-1111-4111-8111-111111111111',
        file_name: 'portrait.png',
        content_type: 'image/png',
        size: 8,
        download_url: 'https://cdn.example/portrait.png',
        download_expires_at: '2026-09-13T12:00:00Z',
      },
    }))

    expect(cloned.reference_image).toEqual({ asset_id: '11111111-1111-4111-8111-111111111111' })
    expect(cloned.input_attachments).toEqual([{ type: 'document', key: 'brief', role: 'brief' }])
  })

  it('preserves the required portrait setting when cloning an article task', () => {
    const cloned = cloneTaskFormDefaults(task({ type: 'article', article_cover_use_portrait: true }))
    expect(cloned.article_cover_use_portrait).toBe(true)
  })

  it('clears the required portrait option when switching to an article project without a portrait', () => {
    const current = { ...createTaskFormDefaults(project({ platform: 'article' })), article_cover_use_portrait: true }
    const switched = switchTaskFormDefaults(current, project({
      id: 'project-without-portrait',
      platform: 'article',
      portrait_reference_image: null,
    }))
    expect(switched.article_cover_use_portrait).toBe(false)
  })
})
