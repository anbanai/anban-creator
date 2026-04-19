import { z } from "zod"

export const loginSchema = z.object({
  email: z.string().min(1, "邮箱不能为空").email("请输入有效的邮箱地址"),
  password: z.string().min(1, "密码不能为空"),
})
export type LoginFormValues = z.infer<typeof loginSchema>

export const registerSchema = z.object({
  email: z.string().min(1, "邮箱不能为空").email("请输入有效的邮箱地址"),
  code: z.string().min(1, "请输入验证码"),
  password: z.string().min(8, "密码至少 8 个字符"),
  nickname: z.string().optional(),
})
export type RegisterFormValues = z.infer<typeof registerSchema>

export const createTaskSchema = z.object({
  channel_id: z.string().min(1, "请选择频道"),
  type: z.enum(["rednote", "article", "xls"]),
  topic: z.string().min(1, "主题不能为空").max(200, "主题不能超过 200 个字符"),
  quantity: z.number().int().min(1).max(5).default(1),
})
export type CreateTaskFormValues = z.infer<typeof createTaskSchema>

export const planSchema = z.object({
  channel_id: z.string().optional(),
  type: z.enum(["rednote", "article", "xls"]),
  title: z.string().min(1, "标题不能为空").max(200, "标题不能超过 200 个字符"),
  description: z.string().max(500, "描述不能超过 500 个字符").optional(),
  cron_expr: z.string().min(1, "请设置排期"),
  topic_hint: z.string().max(200, "主题方向不能超过 200 个字符").optional(),
})
export type PlanFormValues = z.infer<typeof planSchema>

export const channelSchema = z.object({
  platform: z.enum(["rednote", "article", "xls"]),
  name: z.string().max(100, "名称不能超过 100 个字符").optional(),
  profile_url: z.string().optional(),
  avatar_url: z.string().url("请输入有效的 URL").or(z.literal("")).optional(),
  wechat_app_id: z.string().optional(),
  wechat_secret: z.string().optional(),
  keywords: z.string().max(200, "关键词不能超过 200 个字符").optional(),
  positioning: z.string().max(300, "账号定位不能超过 300 个字符").optional(),
  style: z.string().max(1024, "风格描述不能超过 1024 个字符").optional(),
  theme: z.string().max(100, "主题不能超过 100 个字符").optional(),
  author: z.string().max(50, "作者名不能超过 50 个字符").optional(),
  reference_image_url: z.string().url("请输入有效的图片 URL").or(z.literal("")).optional(),
  max_concurrent_tasks: z.number().int().min(1, "最小并发数为 1").max(100, "最大并发数为 100").optional(),
}).refine((data) => {
  // For article/xls platforms, wechat_app_id is required
  if (data.platform === 'article' || data.platform === 'xls') {
    return !!data.wechat_app_id?.trim()
  }
  return true
}, {
  message: "WeChat App ID 为必填项",
  path: ["wechat_app_id"],
})
export type ChannelFormValues = z.infer<typeof channelSchema>
