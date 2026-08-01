import * as z from "zod"

export const PROMPT_MAX_LENGTH = 5120
export const GOAL_TEXT_MAX_LENGTH = 4000

const unicodeLength = (value: string) => Array.from(value).length

const promptSchema = z.string().refine(
  (value) => unicodeLength(value) <= PROMPT_MAX_LENGTH,
  `Prompt 不能超过 ${PROMPT_MAX_LENGTH} 个字符`,
)

const goalSchema = z.string().refine(
  (value) => unicodeLength(value) <= GOAL_TEXT_MAX_LENGTH,
  `目标条件不能超过 ${GOAL_TEXT_MAX_LENGTH} 个字符`,
)

const executionProfileSchema = z
  .enum(['effective', 'balanced', 'quality'])
  .or(z.literal(''))
  .refine((value): boolean => value !== '', '请选择执行配置')

export const imageRatioSchema = z.enum(['', '3:4', '1:1', '4:3', '16:9', '3:2', '2:3', '9:16', '21:9'])
export type ImageRatio = z.infer<typeof imageRatioSchema>

export function normalizeImageRatio(value: unknown): ImageRatio {
  const parsed = imageRatioSchema.safeParse(value)
  return parsed.success ? parsed.data : ''
}

export const agentExecutionProfileCapabilitySchema = z.object({
  id: z.enum(['effective', 'balanced', 'quality']),
  display_name: z.string().min(1),
  description: z.string(),
  provider: z.string(),
  model_name: z.string(),
  min_tier: z.enum(['free', 'pro', 'enterprise']),
  available: z.boolean(),
  unavailable_reason: z.string().optional(),
}).strict().superRefine((profile, context) => {
  if (!profile.available) return
  if (!profile.provider.trim()) {
    context.addIssue({ code: 'custom', path: ['provider'], message: '可用配置必须包含 Provider' })
  }
  if (!profile.model_name.trim()) {
    context.addIssue({ code: 'custom', path: ['model_name'], message: '可用配置必须包含模型' })
  }
})

export const agentExecutionProfileCapabilitiesSchema = z.array(agentExecutionProfileCapabilitySchema)

const referenceImageSelectionSchema = z.union([
  z.object({
    asset_id: z.string().uuid(),
    upload_session_id: z.never().optional(),
  }),
  z.object({
    upload_session_id: z.string().uuid(),
    asset_id: z.never().optional(),
  }),
])

const inputAttachmentSchema = z.object({
  type: z.enum(["image", "audio", "video", "document", "text"]),
  url: z.string().optional(),
  text: z.string().optional(),
  file_name: z.string().optional(),
  content_type: z.string().optional(),
  size: z.number().optional(),
  role: z.string().optional(),
  upload_id: z.string().optional(),
  key: z.string().optional(),
  instruction: z.string().refine(
    (value) => unicodeLength(value) <= 1000,
    "单张素材说明不能超过 1000 个字符",
  ).optional(),
})

const montageAssetSchema = z.object({
  type: z.enum(["text", "image_url", "video_url", "audio_url", "document_url"]),
  url: z.string().optional(),
  task_file_id: z.string().optional(),
  text: z.string().optional(),
  file_name: z.string().optional(),
  mime_type: z.string().optional(),
  file_size: z.number().optional(),
})

const montagePreferencesSchema = z.object({
  aspect_ratio: z.string().optional(),
  duration_seconds: z.number().int().min(1).max(600).optional(),
  style: z.string().max(1000).optional(),
  music_prompt: z.string().max(1000).optional(),
  subtitle_mode: z.string().optional(),
  voiceover_mode: z.string().optional(),
}).optional()

const montageInputSchema = z.object({
  brief: promptSchema.optional(),
  pipeline_key: z.string().max(100).optional(),
  source_assets: z.array(montageAssetSchema).default([]),
  preferences: montagePreferencesSchema,
  delivery_targets: z.array(z.string()).default([]),
  advanced: z.record(z.string(), z.unknown()).optional(),
}).optional()

export const loginSchema = z.object({
  email: z.string().min(1, "邮箱不能为空").email("请输入有效的邮箱地址"),
  password: z.string().min(1, "密码不能为空"),
})
export type LoginFormValues = z.infer<typeof loginSchema>

export const registerSchema = z.object({
  invite_code: z.string(),
  email: z.string().min(1, "邮箱不能为空").email("请输入有效的邮箱地址"),
  code: z.string().min(1, "请输入验证码"),
  password: z.string().min(8, "密码至少 8 个字符"),
  nickname: z.string().optional(),
})
export type RegisterFormValues = z.infer<typeof registerSchema>

export const createTaskSchema = z.object({
  project_id: z.string().optional().default(""),
  execution_profile: executionProfileSchema,
  type: z.enum(["seednote", "article", "moments", "viral_analysis", "ecommerce", "montage"]),
  topic: promptSchema.optional(),
  prompt: promptSchema.optional(),
  quantity: z.number().int().min(1).max(5).default(1),
  image_ratio: imageRatioSchema.default(''),
  image_capability_key: z.string().max(50).optional(),
  skip_reference_image: z.boolean().default(false),
  reference_image: referenceImageSelectionSchema.nullable().optional(),
  input_attachments: z.array(inputAttachmentSchema)
    .max(16, "最多添加 16 个附件")
    .default([]),
  watermark: z.boolean().optional(),
  goal: goalSchema.optional(),
  goal_mode: z.boolean().default(false),
  // Seednote image composition: cover always generated. Content defaults on, tail
  // defaults off — matches server column defaults and the seednote form default.
  // Non-seednote task types ignore these fields server-side.
  has_content_image: z.boolean().default(true),
  has_tail_image: z.boolean().default(false),
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable (unlike seednote, the article cover is NOT mandatory). Both default
  // true → legacy "always generate both" behavior, zero regression. Non-article
  // task types ignore these fields server-side.
  article_with_cover: z.boolean().default(true),
  article_with_content_images: z.boolean().default(true),
  // E-commerce package (server ignores for non-ecommerce). selected_modules maps
  // module key → quantity; product_photos are server-owned storage URLs
  // materialized into the agent workspace by the executor.
  product_photos: z.array(z.string()).default([]),
  selected_modules: z.record(z.string(), z.number().int().min(0)).default({}),
  target_platform: z.string().optional(),
  selling_points: z.string().max(2000, "卖点不能超过 2000 个字符").optional(),
  language: z.string().optional(),
  montage_input: montageInputSchema,
}).superRefine((data, ctx) => {
  if (!data.project_id?.trim()) {
    ctx.addIssue({
      code: "custom",
      message: "请选择项目",
      path: ["project_id"],
    })
  }

  if (data.type === "viral_analysis") {
    const prompt = data.prompt?.trim() || ""
    if (!/https?:\/\/[^\s]+/.test(prompt)) {
      ctx.addIssue({
        code: "custom",
        message: "请粘贴种草笔记链接或分享文本",
        path: ["prompt"],
      })
    }
    return
  }

  if (data.type === "ecommerce") {
    const modules = data.selected_modules ?? {}
    const activeModules = Object.entries(modules).filter(([, q]) => q >= 1)
    if (activeModules.length === 0) {
      ctx.addIssue({
        code: "custom",
        message: "请至少选择一个交付模块",
        path: ["selected_modules"],
      })
    }
    if ((data.product_photos ?? []).length === 0) {
      ctx.addIssue({
        code: "custom",
        message: "请至少上传一张产品图",
        path: ["product_photos"],
      })
    }
  }

  if (data.type === "montage") {
    const brief = data.montage_input?.brief?.trim() || ""
    if (!brief) {
      ctx.addIssue({
        code: "custom",
        message: "请填写 Montage 视频 brief",
        path: ["montage_input", "brief"],
      })
    }
  }

  if (data.goal_mode) {
    const goal = data.goal?.trim() || ""
    if (!goal) {
      ctx.addIssue({
        code: "custom",
        message: "开启强目标模式后必须填写目标条件",
        path: ["goal"],
      })
    }
  }
})
export type CreateTaskFormValues = z.infer<typeof createTaskSchema>

export const planSchema = z.object({
  project_id: z.string().optional(),
  execution_profile: executionProfileSchema,
  type: z.enum(["seednote", "article", "montage"]),
  cron_expr: z.string().min(1, "请设置排期"),
  prompt: promptSchema.optional(),
  image_capability_key: z.string().max(50).optional(),
  image_ratio: imageRatioSchema.default(''),
  skip_reference_image: z.boolean().default(false),
  reference_image: referenceImageSelectionSchema.nullable().optional(),
  input_attachments: z.array(inputAttachmentSchema)
    .max(16, "最多添加 16 个附件")
    .default([]),
  watermark: z.boolean().optional(),
  goal: goalSchema.optional(),
  goal_mode: z.boolean().default(false),
  // Seednote image composition (see createTaskSchema). Defaults match the server.
  has_content_image: z.boolean().default(true),
  has_tail_image: z.boolean().default(false),
  // Article image toggles (see createTaskSchema). Both default true; spawned
  // article tasks inherit them; non-article plans ignore them server-side.
  article_with_cover: z.boolean().default(true),
  article_with_content_images: z.boolean().default(true),
  montage_input: montageInputSchema,
}).superRefine((data, ctx) => {
  if (data.type === "montage") {
    const brief = data.montage_input?.brief?.trim() || ""
    if (!brief) {
      ctx.addIssue({
        code: "custom",
        message: "请填写 Montage 视频 brief",
        path: ["montage_input", "brief"],
      })
    }
  }

  if (data.goal_mode) {
    const goal = data.goal?.trim() || ""
    if (!goal) {
      ctx.addIssue({
        code: "custom",
        message: "开启强目标模式后必须填写目标条件",
        path: ["goal"],
      })
    }
  }
})
export type PlanFormValues = z.infer<typeof planSchema>

export const projectSchema = z.object({
  platform: z.enum(["seednote", "article", "moments", "ecommerce", "montage"]),
  name: z.string().max(100, "名称不能超过 100 个字符").optional(),
  profile_url: z.string().optional(),
  avatar_url: z.string().url("请输入有效的 URL").or(z.literal("")).optional(),
  enable_publishing: z.boolean().default(false),
  require_publish_approval: z.boolean().default(false),
  wechat_app_id: z.string().optional(),
  wechat_secret: z.string().optional(),
  keywords: z.string().max(200, "关键词不能超过 200 个字符").optional(),
  instructions: z.string().max(8192, "项目定位不能超过 8192 个字符").optional(),
  visual_style: z.string().max(1024, "视觉风格不能超过 1024 个字符").optional(),
  writer: z.string().max(100, "写作风格不能超过 100 个字符").optional(),
  theme: z.string().max(100, "主题不能超过 100 个字符").optional(),
  author: z.string().max(50, "作者名不能超过 50 个字符").optional(),
  // 一次性导入视觉模板；项目保存自己的视觉字段，不运行时绑定模板。
  template_id: z.string().optional(),
  ecommerce_default_selected_modules: z.record(z.string(), z.number().int().min(0)).default({}),
  ecommerce_target_platform: z.string().optional(),
  ecommerce_brand_brief: z.string().max(2000, "品牌 brief 不能超过 2000 个字符").optional(),
  ecommerce_image_capability_key: z.string().max(50).optional(),
  montage_defaults: z.object({
    default_pipeline: z.string().max(100).optional(),
    preferences: montagePreferencesSchema,
    asset_guidance: z.string().max(2000).optional(),
    delivery_targets: z.array(z.string()).default([]),
  }).optional(),
  reference_image: referenceImageSelectionSchema.nullable().optional(),
  image_ratio: imageRatioSchema.optional(),
}).refine((data) => {
  if (data.enable_publishing) {
    return !!data.wechat_app_id?.trim()
  }
  return true
}, {
  message: "启用自动发布时，微信 AppID 为必填项",
  path: ["wechat_app_id"],
})
export type ProjectFormValues = z.infer<typeof projectSchema>

export const changePasswordSchema = z.object({
  old_password: z.string().min(1, '请输入当前密码'),
  new_password: z.string().min(8, '新密码至少 8 个字符').max(128, '密码不能超过 128 个字符'),
  confirm_password: z.string().min(1, '请确认新密码'),
}).refine((data) => data.new_password === data.confirm_password, {
  message: '两次输入的密码不一致',
  path: ['confirm_password'],
})
export type ChangePasswordFormValues = z.infer<typeof changePasswordSchema>

export const codeLoginSchema = z.object({
  email: z.string().min(1, '邮箱不能为空').email('请输入有效的邮箱地址'),
  code: z.string().min(1, '请输入验证码'),
})
export type CodeLoginFormValues = z.infer<typeof codeLoginSchema>
