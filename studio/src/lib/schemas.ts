import { z } from "zod"

export const loginSchema = z.object({
  email: z.string().min(1, "邮箱不能为空").email("请输入有效的邮箱地址"),
  password: z.string().min(1, "密码不能为空"),
})
export type LoginFormValues = z.infer<typeof loginSchema>

export const registerSchema = z.object({
  email: z.string().min(1, "邮箱不能为空").email("请输入有效的邮箱地址"),
  password: z.string().min(8, "密码至少 8 个字符"),
  nickname: z.string().optional(),
})
export type RegisterFormValues = z.infer<typeof registerSchema>

export const createTaskSchema = z.object({
  channel_id: z.string().optional(),
  type: z.enum(["rednote", "article", "xls"]),
  topic: z.string().min(1, "主题不能为空").max(200, "主题不能超过 200 个字符"),
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
  platform: z.enum(["article", "xls", "rednote"]),
  name: z.string().min(1, "频道名称不能为空").max(100, "名称不能超过 100 个字符"),
  description: z.string().max(500, "简介不能超过 500 个字符").optional(),
  avatar_url: z.string().url("请输入有效的 URL").or(z.literal("")).optional(),
  wechat_app_id: z.string().optional(),
  wechat_secret: z.string().optional(),
  keywords: z.string().max(200, "关键词不能超过 200 个字符").optional(),
  positioning: z.string().max(300, "定位不能超过 300 个字符").optional(),
  style: z.string().max(100, "写作风格不能超过 100 个字符").optional(),
  theme: z.string().max(100, "主题不能超过 100 个字符").optional(),
  author: z.string().max(50, "作者名不能超过 50 个字符").optional(),
})
export type ChannelFormValues = z.infer<typeof channelSchema>
