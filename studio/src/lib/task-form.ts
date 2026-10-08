import type { AgentExecutionProfileID, CreateTaskRequest, HypitDefaults, Project, ReferenceImageSelection, Task, TaskType } from '@/types'
import type { CreateTaskFormValues } from '@/lib/schemas'
import { buildHypitInputForSubmit, initialHypitInput } from '@/lib/hypit-form'
import { buildMontageInputForSubmit, initialMontageInput } from '@/lib/montage-form'
import { supportsPortraitCover } from '@/lib/portrait-cover'
import { defaultTaskImageRatio, getProjectCreationDefaults } from '@/lib/studio-ux'

export interface TaskFormDefaults extends CreateTaskFormValues {
  quantity: number
  watermark: boolean
  has_content_image: boolean
  has_tail_image: boolean
  article_with_cover: boolean
  article_with_content_images: boolean
  cover_use_portrait: boolean
}

function taskIdentity(type: TaskType): { agent_id: string; channel: NonNullable<CreateTaskRequest['channel']>; task_kind: string } {
  switch (type) {
    case 'seednote':
    case 'viral_analysis':
      return { agent_id: 'seednote', channel: 'seednote', task_kind: type === 'viral_analysis' ? 'viral_analysis' : 'content_generation' }
    case 'wechat-picture':
      return { agent_id: 'wechat-picture', channel: 'wechat-picture', task_kind: 'content_generation' }
    case 'whiteboard-animation':
      return { agent_id: 'whiteboard-animation', channel: 'whiteboard-animation', task_kind: 'whiteboard-animation' }
    case 'wechat-article':
      return { agent_id: 'wechat-article', channel: 'wechat-article', task_kind: 'content_generation' }
    default:
      return { agent_id: type, channel: type, task_kind: type }
  }
}

function cloneValue<T>(value: T): T {
  if (Array.isArray(value)) {
    return value.map((item) => cloneValue(item)) as T
  }
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value as Record<string, unknown>).map(([key, item]) => [key, cloneValue(item)]),
    ) as T
  }
  return value
}

function projectMontageInput(project?: Project | null, brief = ''): TaskFormDefaults['montage_input'] {
  if (project?.platform !== 'montage') return undefined
  return cloneValue(initialMontageInput(brief, undefined, project.montage_defaults))
}

export function createTaskFormDefaults(project?: Project | null): TaskFormDefaults {
  const defaults = getProjectCreationDefaults(project)

  return {
    project_id: project?.id ?? '',
    execution_profile: '',
    type: defaults.type,
    topic: undefined,
    prompt: '',
    quantity: 1,
    image_ratio: defaultTaskImageRatio(defaults.type, project) as TaskFormDefaults['image_ratio'],
    image_capability_key: defaults.imageCapabilityKey,
    skip_reference_image: false,
    reference_image: null,
    input_attachments: [],
    agent_input: {},
    watermark: false,
    has_content_image: true,
    has_tail_image: false,
    article_with_cover: true,
    article_with_content_images: true,
    cover_use_portrait: false,
    product_photos: [],
    selected_modules: cloneValue(defaults.selectedModules),
    target_platform: defaults.targetPlatform,
    selling_points: '',
    language: '',
    hypit_input: project?.platform === 'hypit' ? initialHypitInput('', undefined, project.hypit_defaults) : undefined,
    montage_input: projectMontageInput(project),
  }
}

// Project changes replace project-owned defaults while keeping user-authored input.
export function switchTaskFormDefaults(
  current: TaskFormDefaults,
  project?: Project | null,
): TaskFormDefaults {
  if (project && ((project.platform === 'wechat' && (current.type === 'wechat-article' || current.type === 'wechat-picture')) || current.type === project.platform || (current.type === 'viral_analysis'))) {
    const defaults = createTaskFormDefaults(project)
    const switched = {
      ...cloneValue(current),
      project_id: project.id,
      image_ratio: defaultTaskImageRatio(current.type, project) as TaskFormDefaults['image_ratio'],
      image_capability_key: defaults.image_capability_key,
      cover_use_portrait: current.cover_use_portrait && Boolean(project.portrait_reference_image),
    }

    if (current.type === 'ecommerce') {
      switched.selected_modules = cloneValue(defaults.selected_modules)
      switched.target_platform = defaults.target_platform
    }

    if (current.type === 'hypit') switched.hypit_input = initialHypitInput(current.prompt, { ...current.hypit_input, preferences: project.hypit_defaults?.preferences }, project.hypit_defaults)
    if (current.type === 'montage') {
      const montageDefaults = projectMontageInput(project, current.prompt)
      if (montageDefaults) {
        switched.montage_input = {
          ...montageDefaults,
          brief: current.montage_input?.brief ?? current.prompt,
          source_assets: cloneValue(current.montage_input?.source_assets ?? []),
          advanced: cloneValue(current.montage_input?.advanced),
        }
      }
    }

    return switched
  }

  const defaults = createTaskFormDefaults(project)
  return {
    ...defaults,
    execution_profile: current.execution_profile,
    topic: current.topic,
    prompt: current.prompt,
    quantity: current.quantity,
    skip_reference_image: current.skip_reference_image,
    reference_image: cloneValue(current.reference_image),
    input_attachments: cloneValue(current.input_attachments),
    watermark: current.watermark,
    hypit_input: project?.platform === 'hypit' ? initialHypitInput(current.prompt, undefined, project.hypit_defaults) : undefined,
    montage_input: projectMontageInput(project, current.prompt),
  }
}

function taskReferenceSelection(task: Task): ReferenceImageSelection | null {
  return task.reference_image ? { asset_id: task.reference_image.asset_id } : null
}

export function cloneTaskFormDefaults(task: Task): TaskFormDefaults {
  const ecommerce = task.ecommerce
  const agentInput = cloneValue(task.agent_input ?? {})
  if (task.type === 'wechat-picture' && agentInput.picture_image_count != null && agentInput.picture_image_count_mode == null) {
    agentInput.picture_image_count_mode = 'exact'
  }
  const directReference = task.reference_image
  const articleUsesPortrait = task.type === 'wechat-article'
    && task.article_with_cover !== false
    && task.cover_use_portrait === true
    && (directReference == null || task.project_snapshot?.portrait_reference_image_asset_id === undefined || directReference.asset_id === task.project_snapshot.portrait_reference_image_asset_id)
  const clonedAttachments = (task.input_attachments ?? [])
    .filter((attachment) => attachment.role !== 'resume_latest' && attachment.role !== 'resume_file')
    .map((attachment) => cloneValue(attachment))
  if (task.type !== 'wechat-article' && directReference && directReference.asset_id !== task.project_snapshot?.reference_image_asset_id) {
    const withoutDuplicate = clonedAttachments.filter((attachment) => attachment.asset_id !== directReference.asset_id)
    clonedAttachments.splice(0, clonedAttachments.length, {
      type: 'image', asset_id: directReference.asset_id, file_name: directReference.file_name,
      content_type: directReference.content_type, size: directReference.size,
    }, ...withoutDuplicate)
  }

  return {
    project_id: task.project_id,
    execution_profile: task.execution_profile,
    type: task.type,
    topic: task.topic,
    prompt: task.type === 'hypit' ? task.hypit_input?.brief || task.prompt : task.prompt,
    quantity: 1,
    image_ratio: (task.image_ratio || 'auto') as TaskFormDefaults['image_ratio'],
    image_capability_key: task.image_capability_key ?? '',
    skip_reference_image: task.skip_reference_image ?? false,
    reference_image: task.type === 'wechat-article' && !articleUsesPortrait ? null : taskReferenceSelection(task),
    input_attachments: clonedAttachments,
    agent_input: agentInput,
    watermark: task.watermark ?? false,
    has_content_image: task.has_content_image ?? true,
    has_tail_image: task.has_tail_image ?? false,
    article_with_cover: task.article_with_cover ?? true,
    article_with_content_images: task.article_with_content_images ?? true,
    cover_use_portrait: !!task.cover_use_portrait && supportsPortraitCover(task.type) && (task.type !== 'wechat-article' || task.article_with_cover !== false),
    product_photos: cloneValue(ecommerce?.product_photos ?? []),
    selected_modules: cloneValue(ecommerce?.selected_modules ?? {}),
    target_platform: ecommerce?.target_platform ?? '',
    selling_points: ecommerce?.selling_points ?? '',
    language: ecommerce?.language ?? '',
    hypit_input: task.hypit_input ? cloneValue(initialHypitInput(task.prompt, task.hypit_input)) : undefined,
    montage_input: task.montage_input
      ? cloneValue(initialMontageInput(task.prompt, task.montage_input))
      : undefined,
  }
}

export function taskFormValuesToRequest(values: TaskFormDefaults, hypitDefaults?: HypitDefaults): CreateTaskRequest {
  const prompt = values.prompt?.trim() || undefined
  const identity = taskIdentity(values.type)
  if (values.type === 'viral_analysis') {
    return {
      ...identity,
      type: values.type,
      execution_profile: values.execution_profile as AgentExecutionProfileID,
      prompt,
      project_id: values.project_id,
      quantity: values.quantity,
      input_attachments: [],
      agent_input: cloneValue(values.agent_input),
    }
  }
  const sellingPoints = values.selling_points?.trim() || undefined
  const hasActiveModules = Object.values(values.selected_modules ?? {}).some((quantity) => quantity >= 1)
  return {
    ...identity,
    type: values.type,
    execution_profile: values.execution_profile as AgentExecutionProfileID,
    topic: values.topic,
    prompt,
    project_id: values.project_id,
    quantity: values.quantity,
    image_ratio: values.image_ratio,
    image_capability_key: values.image_capability_key || undefined,
    skip_reference_image: values.skip_reference_image,
    ...(values.skip_reference_image ? { reference_image: null } : {}),
    input_attachments: cloneValue(values.input_attachments),
    agent_input: cloneValue(values.agent_input),
    watermark: values.watermark,
    ...(supportsPortraitCover(values.type) ? { cover_use_portrait: values.cover_use_portrait && (values.type !== 'wechat-article' || values.article_with_cover) } : {}),
    ...(values.type === 'seednote'
      ? {
          has_content_image: values.has_content_image,
          has_tail_image: values.has_tail_image,
        }
      : {}),
    ...(values.type === 'wechat-article'
      ? {
          article_with_cover: values.article_with_cover,
          article_with_content_images: values.article_with_content_images,
        }
      : {}),
    ...(values.type === 'wechat-picture'
      ? { agent_input: { ...cloneValue(values.agent_input), picture_image_count: Number((values.agent_input as Record<string, unknown>)?.picture_image_count ?? 5), picture_image_count_mode: (values.agent_input as Record<string, unknown>)?.picture_image_count_mode === 'exact' ? 'exact' : 'up_to', picture_publish_draft: (values.agent_input as Record<string, unknown>)?.picture_publish_draft !== false } }
      : {}),
    ...(values.type === 'ecommerce'
      ? {
          ...(values.product_photos?.length ? { product_photos: cloneValue(values.product_photos) } : {}),
          ...(hasActiveModules ? { selected_modules: cloneValue(values.selected_modules) } : {}),
          ...(values.target_platform ? { target_platform: values.target_platform } : {}),
          ...(sellingPoints ? { selling_points: sellingPoints } : {}),
          ...(values.language ? { language: values.language } : {}),
        }
      : {}),
    ...(values.type === 'hypit' ? { hypit_input: buildHypitInputForSubmit(prompt ?? '', cloneValue(values.hypit_input), hypitDefaults) } : {}),
    ...(values.type === 'montage'
      ? { montage_input: cloneValue(buildMontageInputForSubmit(values.prompt, values.montage_input)) }
      : {}),
  }
}
