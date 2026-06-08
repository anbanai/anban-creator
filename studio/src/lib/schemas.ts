import { z } from "zod"

export const PROMPT_MAX_LENGTH = 5120

const unicodeLength = (value: string) => Array.from(value).length

const promptSchema = z.string().refine(
  (value) => unicodeLength(value) <= PROMPT_MAX_LENGTH,
  `Prompt 不能超过 ${PROMPT_MAX_LENGTH} 个字符`,
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
  channel_id: z.string().optional().default(""),
  type: z.enum(["seednote", "article", "xls", "viral_analysis"]),
  topic: promptSchema.optional(),
  prompt: promptSchema.optional(),
  quantity: z.number().int().min(1).max(5).default(1),
  image_ratio: z.enum(["", "3:4", "1:1", "4:3", "16:9"]).default(""),
  skip_reference_image: z.boolean().default(false),
  reference_image_url: z.string().refine(
    (val) => val === "" || val.startsWith("/") || /^https?:\/\//.test(val),
    { message: "请输入有效的图片 URL" },
  ).optional(),
  watermark: z.boolean().optional(),
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

  if (!data.channel_id?.trim()) {
    ctx.addIssue({
      code: "custom",
      message: "请选择账号",
      path: ["channel_id"],
    })
  }
})
export type CreateTaskFormValues = z.infer<typeof createTaskSchema>

export const planSchema = z.object({
  channel_id: z.string().optional(),
  type: z.enum(["seednote", "article", "xls"]),
  cron_expr: z.string().min(1, "请设置排期"),
  prompt: promptSchema.optional(),
  skip_reference_image: z.boolean().default(false),
  reference_image_url: z.string().refine(
    (val) => val === "" || val.startsWith("/") || /^https?:\/\//.test(val),
    { message: "请输入有效的图片 URL" },
  ).optional(),
  watermark: z.boolean().optional(),
})
export type PlanFormValues = z.infer<typeof planSchema>

export const channelSchema = z.object({
  platform: z.enum(["seednote", "article", "xls"]),
  name: z.string().max(100, "名称不能超过 100 个字符").optional(),
  profile_url: z.string().optional(),
  avatar_url: z.string().url("请输入有效的 URL").or(z.literal("")).optional(),
  enable_publishing: z.boolean().default(false),
  wechat_app_id: z.string().optional(),
  wechat_secret: z.string().optional(),
  keywords: z.string().max(200, "关键词不能超过 200 个字符").optional(),
  positioning: z.string().max(300, "账号定位不能超过 300 个字符").optional(),
  style: z.string().max(1024, "风格描述不能超过 1024 个字符").optional(),
  theme: z.string().max(100, "主题不能超过 100 个字符").optional(),
  author: z.string().max(50, "作者名不能超过 50 个字符").optional(),
  reference_image_url: z.string().refine(
    (val) => val === "" || val.startsWith("/") || /^https?:\/\//.test(val),
    { message: "请输入有效的图片 URL" },
  ).optional(),
  image_ratio: z.enum(["", "3:4", "1:1", "4:3", "16:9"]).optional(),
  layout: z.string().max(100).optional(),
  image_preset: z.string().max(50).optional(),
}).refine((data) => {
  if (data.enable_publishing) {
    return !!data.wechat_app_id?.trim()
  }
  return true
}, {
  message: "启用自动发布时，微信 AppID 为必填项",
  path: ["wechat_app_id"],
})
export type ChannelFormValues = z.infer<typeof channelSchema>

export const changePasswordSchema = z.object({
  old_password: z.string().min(1, '请输入当前密码'),
  new_password: z.string().min(8, '新密码至少 8 个字符').max(128, '密码不能超过 128 个字符'),
  confirm_password: z.string().min(1, '请确认新密码'),
}).refine((data) => data.new_password === data.confirm_password, {
  message: '两次输入的密码不一致',
  path: ['confirm_password'],
})
export type ChangePasswordFormValues = z.infer<typeof changePasswordSchema>
