import { z } from 'zod'

export const facetKeys = ['identity', 'audience', 'style', 'platforms', 'preferences', 'experience'] as const
const message = z.object({
  id: z.string().regex(/^[a-zA-Z0-9_-]{1,64}$/),
  role: z.enum(['user', 'assistant']),
  text: z.string().trim().min(1).max(6000),
}).strict()
export const portraitChatRequest = z.object({ messages: z.array(message).min(1).max(60) }).strict().superRefine(({ messages }, ctx) => {
  if (messages[messages.length - 1]?.role !== 'user') ctx.addIssue({ code: 'custom', message: '最后一条必须是用户输入。' })
  if (new Set(messages.map(item => item.id)).size !== messages.length) ctx.addIssue({ code: 'custom', message: '消息编号重复。' })
  if (messages.reduce((size, item) => size + item.text.length, 0) > 40000) ctx.addIssue({ code: 'custom', message: '对话过长，请保存画像后重新开始。' })
})
export type PortraitChatMessage = z.infer<typeof message>

const fact = z.object({
  text: z.string().trim().min(1).max(500),
  certainty: z.enum(['stated', 'inferred']),
  // A brief can combine facts collected across many turns (topic, format,
  // pain point, CTA, platform...). Bound by the conversation budget, not four facts.
  evidence: z.array(z.object({ messageId: z.string().min(1).max(64), quote: z.string().trim().min(1).max(300) }).strict()).min(1).max(60),
}).strict()
export const portraitCandidateSchema = z.object({
  reply: z.string().trim().min(1).max(1800),
  name: z.string().trim().min(1).max(80).nullable(),
  summary: z.string().trim().min(1).max(300).nullable(),
  facets: z.object({
    identity: fact.nullable(), audience: fact.nullable(), style: fact.nullable(),
    platforms: fact.nullable(), preferences: fact.nullable(), experience: fact.nullable(),
  }).strict(),
  creationIdea: fact.nullable(),
}).strict()
export type PortraitCandidate = z.infer<typeof portraitCandidateSchema>

/** Evidence must quote a real user message; assistant suggestions are not facts. */
export function validatePortraitCandidate(value: unknown, messages: PortraitChatMessage[]): PortraitCandidate {
  const candidate = portraitCandidateSchema.parse(value)
  const userMessages = new Map(messages.filter(item => item.role === 'user').map(item => [item.id, item.text]))
  for (const item of [...Object.values(candidate.facets), candidate.creationIdea]) {
    if (!item) continue
    for (const evidence of item.evidence) {
      if (!userMessages.get(evidence.messageId)?.includes(evidence.quote)) throw new Error('画像引用了不存在的用户依据。')
    }
  }
  if (!candidate.facets.identity && (candidate.name || candidate.summary)) throw new Error('缺少身份依据。')
  if (candidate.name && ![...userMessages.values()].some(text => text.includes(candidate.name!))) throw new Error('名称没有用户依据。')
  return candidate
}

export function canConfirmPortrait(candidate: PortraitCandidate | null): boolean {
  return !!(candidate?.facets.identity && candidate.facets.audience && candidate.facets.platforms)
}
