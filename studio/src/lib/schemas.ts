import { z } from "zod"

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
  type: z.enum(["seednote", "article", "moments", "viral_analysis", "ecommerce", "video"]),
  topic: promptSchema.optional(),
  prompt: promptSchema.optional(),
  quantity: z.number().int().min(1).max(5).default(1),
  image_ratio: z.enum(["", "3:4", "1:1", "4:3", "16:9"]).default(""),
  image_model_key: z.string().max(50).optional(),
  skip_reference_image: z.boolean().default(false),
  reference_image_url: z.string().refine(
    (val) => val === "" || val.startsWith("/") || /^https?:\/\//.test(val),
    { message: "请输入有效的图片 URL" },
  ).optional(),
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
  // module key → quantity; product_photos are /files/upload URLs materialized
  // into the agent workspace by the executor.
  product_photos: z.array(z.string()).default([]),
  selected_modules: z.record(z.string(), z.number().int().min(0)).default({}),
  target_platform: z.string().optional(),
  selling_points: z.string().max(2000, "卖点不能超过 2000 个字符").optional(),
  language: z.string().optional(),
  video_config: z.object({
    workflow: z.enum(["creator", "editor"]).default("creator"),
    scenario_key: z.string().optional(),
    production_mode: z.enum(["fast_lane", "guided", "sequence", "remake"]).optional(),
    purpose: z.enum(["planting", "ecommerce", "lead_gen", "promotion"]).optional(),
    creative_type: z.enum(["personal_ip", "high_efficiency_joke", "product_demo", "brand_promo", "custom"]).optional(),
    subject_profile: z.string().optional(),
    audience: z.string().optional(),
    single_message: z.string().optional(),
    model_key: z.string().optional(),
    resolution: z.string().optional(),
    ratio: z.string().optional(),
    duration: z.number().int().min(1).max(60).optional(),
    watermark: z.boolean().optional(),
    preflight: z.boolean().optional(),
    retake_budget: z.number().int().min(0).max(20).optional(),
    delivery_targets: z.array(z.string()).optional(),
    references: z.array(z.object({
      type: z.enum(["text", "image_url", "audio_url", "video_url"]),
      url: z.string().optional(),
      text: z.string().optional(),
      reference_role: z.string().optional(),
      must_keep: z.array(z.string()).optional(),
      can_change: z.array(z.string()).optional(),
      must_not_transfer: z.array(z.string()).optional(),
      file_name: z.string().optional(),
      mime_type: z.string().optional(),
      file_size: z.number().optional(),
      input_duration_seconds: z.number().optional(),
    })).optional(),
  }).optional(),
}).superRefine((data, ctx) => {
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

  if (!data.project_id?.trim()) {
    ctx.addIssue({
      code: "custom",
      message: "请选择项目",
      path: ["project_id"],
    })
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
  type: z.enum(["seednote", "article", "video"]),
  cron_expr: z.string().min(1, "请设置排期"),
  prompt: promptSchema.optional(),
  image_model_key: z.string().max(50).optional(),
  skip_reference_image: z.boolean().default(false),
  reference_image_url: z.string().refine(
    (val) => val === "" || val.startsWith("/") || /^https?:\/\//.test(val),
    { message: "请输入有效的图片 URL" },
  ).optional(),
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
  video_config: z.object({
    workflow: z.enum(["creator", "editor"]).default("creator"),
    scenario_key: z.string().optional(),
    production_mode: z.enum(["fast_lane", "guided", "sequence", "remake"]).optional(),
    purpose: z.enum(["planting", "ecommerce", "lead_gen", "promotion"]).optional(),
    creative_type: z.enum(["personal_ip", "high_efficiency_joke", "product_demo", "brand_promo", "custom"]).optional(),
    subject_profile: z.string().optional(),
    audience: z.string().optional(),
    single_message: z.string().optional(),
    model_key: z.string().optional(),
    resolution: z.string().optional(),
    ratio: z.string().optional(),
    duration: z.number().int().min(1).max(60).optional(),
    watermark: z.boolean().optional(),
    preflight: z.boolean().optional(),
    retake_budget: z.number().int().min(0).max(20).optional(),
    delivery_targets: z.array(z.string()).optional(),
    references: z.array(z.object({
      type: z.enum(["text", "image_url", "audio_url", "video_url"]),
      url: z.string().optional(),
      text: z.string().optional(),
      reference_role: z.string().optional(),
      must_keep: z.array(z.string()).optional(),
      can_change: z.array(z.string()).optional(),
      must_not_transfer: z.array(z.string()).optional(),
      file_name: z.string().optional(),
      mime_type: z.string().optional(),
      file_size: z.number().optional(),
      input_duration_seconds: z.number().optional(),
    })).optional(),
  }).optional(),
}).superRefine((data, ctx) => {
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
  platform: z.enum(["seednote", "article", "moments", "ecommerce", "video"]),
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
  ecommerce_image_model_key: z.string().max(50).optional(),
  video_defaults: z.object({
    purpose: z.enum(["planting", "ecommerce", "lead_gen", "promotion"]).default("planting"),
    model_key: z.string().default(""),
    resolution: z.string().min(1).default("720p"),
    ratio: z.string().min(1).default("9:16"),
    duration: z.number().int().min(1).max(600).default(15),
    watermark: z.boolean().default(false),
    preflight: z.boolean().default(true),
  }).optional(),
  video_model_policy: z.object({
    allowed_models: z.array(z.string()).default([]),
    default_model: z.string().default(""),
    allow_auto_downgrade: z.boolean().default(false),
    max_resolution: z.string().default("720p"),
    max_duration: z.number().int().min(1).max(600).default(120),
  }).optional(),
  reference_image_url: z.string().refine(
    (val) => val === "" || val.startsWith("/") || /^https?:\/\//.test(val),
    { message: "请输入有效的图片 URL" },
  ).optional(),
  image_ratio: z.enum(["", "3:4", "1:1", "4:3", "16:9"]).optional(),
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
