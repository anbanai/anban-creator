import type { AgentExecutionProfileID, CreateTaskRequest, Project, ReferenceImageSelection, Task } from '@/types'
import type { CreateTaskFormValues } from '@/lib/schemas'
import { buildMontageInputForSubmit, initialMontageInput } from '@/lib/montage-form'
import { getProjectCreationDefaults } from '@/lib/studio-ux'

export interface TaskFormDefaults extends CreateTaskFormValues {
  quantity: number
  watermark: boolean
  goal: string
  goal_mode: boolean
  has_content_image: boolean
  has_tail_image: boolean
  article_with_cover: boolean
  article_with_content_images: boolean
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
    image_ratio: defaults.imageRatio as TaskFormDefaults['image_ratio'],
    image_capability_key: defaults.imageCapabilityKey,
    skip_reference_image: false,
    reference_image: null,
    input_attachments: [],
    agent_input: {},
    watermark: false,
    goal: '',
    goal_mode: false,
    has_content_image: true,
    has_tail_image: false,
    article_with_cover: true,
    article_with_content_images: true,
    product_photos: [],
    selected_modules: cloneValue(defaults.selectedModules),
    target_platform: defaults.targetPlatform,
    selling_points: '',
    language: '',
    montage_input: projectMontageInput(project),
  }
}

// Project changes replace project-owned defaults while keeping user-authored input.
export function switchTaskFormDefaults(
  current: TaskFormDefaults,
  project?: Project | null,
): TaskFormDefaults {
  if (project && (current.type === project.platform || (current.type === 'viral_analysis' && project.platform === 'seednote'))) {
    const defaults = createTaskFormDefaults(project)
    const switched = {
      ...cloneValue(current),
      project_id: project.id,
      image_ratio: defaults.image_ratio,
      image_capability_key: defaults.image_capability_key,
    }

    if (current.type === 'ecommerce') {
      switched.selected_modules = cloneValue(defaults.selected_modules)
      switched.target_platform = defaults.target_platform
    }

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
    goal: current.goal,
    goal_mode: current.goal_mode,
    montage_input: projectMontageInput(project, current.prompt),
  }
}

function taskReferenceSelection(task: Task): ReferenceImageSelection | null {
  return task.reference_image ? { asset_id: task.reference_image.asset_id } : null
}

export function cloneTaskFormDefaults(task: Task): TaskFormDefaults {
  const ecommerce = task.ecommerce
  const clonedAttachments = (task.input_attachments ?? [])
    .filter((attachment) => attachment.role !== 'resume_latest' && attachment.role !== 'resume_file')
    .map((attachment) => cloneValue(attachment))
  const directReference = task.reference_image
  if (directReference && directReference.asset_id !== task.project_snapshot?.reference_image_asset_id) {
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
    prompt: task.prompt,
    quantity: 1,
    image_ratio: (task.image_ratio ?? '') as TaskFormDefaults['image_ratio'],
    image_capability_key: task.image_capability_key ?? '',
    skip_reference_image: task.skip_reference_image ?? false,
    reference_image: taskReferenceSelection(task),
    input_attachments: clonedAttachments,
    agent_input: cloneValue(task.agent_input ?? {}),
    watermark: task.watermark ?? false,
    goal: task.goal ?? '',
    goal_mode: task.goal_mode ?? false,
    has_content_image: task.has_content_image ?? true,
    has_tail_image: task.has_tail_image ?? false,
    article_with_cover: task.article_with_cover ?? true,
    article_with_content_images: task.article_with_content_images ?? true,
    product_photos: cloneValue(ecommerce?.product_photos ?? []),
    selected_modules: cloneValue(ecommerce?.selected_modules ?? {}),
    target_platform: ecommerce?.target_platform ?? '',
    selling_points: ecommerce?.selling_points ?? '',
    language: ecommerce?.language ?? '',
    montage_input: task.montage_input
      ? cloneValue(initialMontageInput(task.prompt, task.montage_input))
      : undefined,
  }
}

export function taskFormValuesToRequest(values: TaskFormDefaults): CreateTaskRequest {
  const prompt = values.prompt?.trim() || undefined
  if (values.type === 'viral_analysis') {
    return {
      type: values.type,
      execution_profile: values.execution_profile as AgentExecutionProfileID,
      prompt,
      project_id: values.project_id,
      quantity: values.quantity,
      input_attachments: [],
      agent_input: cloneValue(values.agent_input),
    }
  }
  const goal = values.goal?.trim() || undefined
  const sellingPoints = values.selling_points?.trim() || undefined
  const hasActiveModules = Object.values(values.selected_modules ?? {}).some((quantity) => quantity >= 1)
  return {
    type: values.type,
    execution_profile: values.execution_profile as AgentExecutionProfileID,
    topic: values.topic,
    prompt,
    project_id: values.project_id,
    quantity: values.quantity,
    image_ratio: values.image_ratio || undefined,
    image_capability_key: values.image_capability_key || undefined,
    skip_reference_image: values.skip_reference_image,
    ...(values.skip_reference_image ? { reference_image: null } : {}),
    input_attachments: cloneValue(values.input_attachments),
    agent_input: cloneValue(values.agent_input),
    watermark: values.watermark,
    ...(values.type !== 'ecommerce'
      ? {
          goal_mode: values.goal_mode,
          ...(values.goal_mode && goal ? { goal } : {}),
        }
      : {}),
    ...(values.type === 'seednote'
      ? {
          has_content_image: values.has_content_image,
          has_tail_image: values.has_tail_image,
        }
      : {}),
    ...(values.type === 'article'
      ? {
          article_with_cover: values.article_with_cover,
          article_with_content_images: values.article_with_content_images,
        }
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
    ...(values.type === 'montage'
      ? { montage_input: cloneValue(buildMontageInputForSubmit(values.prompt, values.montage_input)) }
      : {}),
  }
}
