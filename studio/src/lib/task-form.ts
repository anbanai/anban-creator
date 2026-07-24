import type { CreateTaskRequest, ExecutionTarget, Project, ReferenceImageSelection, Task } from '@/types'
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
  execution_target?: ExecutionTarget
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
    type: defaults.type,
    topic: undefined,
    prompt: '',
    quantity: 1,
    image_ratio: defaults.imageRatio as TaskFormDefaults['image_ratio'],
    image_model_key: defaults.imageModelKey,
    skip_reference_image: false,
    reference_image: null,
    input_attachments: [],
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
    execution_target: undefined,
  }
}

// Project changes replace project-owned defaults while keeping user-authored input.
export function switchTaskFormDefaults(
  current: TaskFormDefaults,
  project?: Project | null,
): TaskFormDefaults {
  if (project && current.type === project.platform) {
    const defaults = createTaskFormDefaults(project)
    const switched = {
      ...cloneValue(current),
      project_id: project.id,
      image_ratio: defaults.image_ratio,
      image_model_key: defaults.image_model_key,
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
    execution_target: current.execution_target,
  }
}

function taskReferenceSelection(task: Task): ReferenceImageSelection | null {
  return task.reference_image ? { asset_id: task.reference_image.asset_id } : null
}

export function cloneTaskFormDefaults(task: Task): TaskFormDefaults {
  const ecommerce = task.ecommerce

  return {
    project_id: task.project_id,
    type: task.type,
    topic: task.topic,
    prompt: task.prompt,
    quantity: 1,
    image_ratio: (task.image_ratio ?? '') as TaskFormDefaults['image_ratio'],
    image_model_key: task.image_model_key ?? '',
    skip_reference_image: task.skip_reference_image ?? false,
    reference_image: taskReferenceSelection(task),
    input_attachments: (task.input_attachments ?? [])
      .filter((attachment) => attachment.role !== 'resume_latest' && attachment.role !== 'resume_file')
      .map((attachment) => cloneValue(attachment)),
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
    execution_target: task.execution_target,
  }
}

export function taskFormValuesToRequest(values: TaskFormDefaults): CreateTaskRequest {
  const prompt = values.prompt?.trim() || undefined
  const goal = values.goal?.trim() || undefined
  const sellingPoints = values.selling_points?.trim() || undefined
  const hasActiveModules = Object.values(values.selected_modules ?? {}).some((quantity) => quantity >= 1)
  const referenceImage = values.reference_image
    ? cloneValue(values.reference_image)
    : values.skip_reference_image
      ? null
      : undefined
  const executionTarget = values.execution_target === 'local_claimed'
    ? 'local'
    : values.execution_target || undefined

  return {
    type: values.type,
    topic: values.topic,
    prompt,
    project_id: values.project_id,
    quantity: values.quantity,
    image_ratio: values.image_ratio || undefined,
    image_model_key: values.image_model_key || undefined,
    skip_reference_image: values.skip_reference_image,
    ...(referenceImage !== undefined ? { reference_image: referenceImage } : {}),
    input_attachments: cloneValue(values.input_attachments),
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
    ...(values.type !== 'montage' && executionTarget ? { execution_target: executionTarget } : {}),
  }
}
