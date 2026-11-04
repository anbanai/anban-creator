import type { Project } from '@/types'
import { projectAgentIDs } from './project-capabilities'
import { PROMPT_MAX_LENGTH } from './schemas'

// Leave room for the selected product-image brief in the shared task schema.
export const MOBILE_BRIEF_MAX_LENGTH = PROMPT_MAX_LENGTH - 200

export const mobileCreations = [
  { id: 'wechat-article', title: '公众号文章', subtitle: '把想法，写成好文章', mark: '01', tone: 'sage' },
  { id: 'seednote', title: '小红书图文', subtitle: '有表达，也有好看的图', mark: '02', tone: 'rose' },
  { id: 'video-edit', title: '短视频剪辑', subtitle: '上传素材，剪出完整作品', mark: '03', tone: 'blue' },
  { id: 'ecommerce', title: '电商图片', subtitle: '主图 · 详情 · 白底 · SKU', mark: '04', tone: 'amber' },
] as const
export type MobileCreationType = typeof mobileCreations[number]['id']
export const productImageChoices = [
  { id: 'hero', label: '商品主图', description: '突出产品与核心卖点', modules: { main_images: 1 }, brief: '制作商品头图，突出产品主体与核心卖点。' },
  { id: 'detail', label: '详情图', description: '把产品价值讲清楚', modules: { detail_page: 5 }, brief: '制作商品详情图，按产品信息组织清晰的卖点与细节。' },
  { id: 'white', label: '白底图', description: '干净背景，保留真实细节', modules: { main_images: 1 }, brief: '制作纯白背景商品图，只保留真实产品，不添加文字、道具或虚构细节。' },
  { id: 'sku', label: 'SKU 图', description: '区分颜色、规格与款式', modules: { sku_images: 1 }, brief: '制作商品 SKU 图，按提供的颜色、规格或款式区分，保持构图一致，不虚构规格。' },
] as const
export function mobileCreationType(value: string | null): MobileCreationType | undefined {
  return mobileCreations.find(item => item.id === value)?.id
}
export function canUseMobileCreation(project: Project, type: MobileCreationType) {
  return projectAgentIDs(project).includes(type)
}
export const newMobilePersonaState = { modeId: 'profile', projectId: '', initialMessage: '', startNew: true } as const
export interface MobileTaskDraft {
  projectId: string
  type: 'video-edit' | 'ecommerce'
  prompt: string
  selectedModules?: Record<string, number>
}

export function parseMobileTaskDraft(state: unknown, projectId?: string, type?: string): MobileTaskDraft | undefined {
  if (!state || typeof state !== 'object' || !('mobileDraft' in state)) return
  const draft = state.mobileDraft
  if (!draft || typeof draft !== 'object' || !('projectId' in draft) || !('type' in draft) || !('prompt' in draft)) return
  if (draft.projectId !== projectId || draft.type !== type || (type !== 'ecommerce' && type !== 'video-edit') || typeof draft.prompt !== 'string' || draft.prompt.length > PROMPT_MAX_LENGTH) return
  const selectedModules: Record<string, number> = {}
  if (type === 'ecommerce' && 'selectedModules' in draft && draft.selectedModules && typeof draft.selectedModules === 'object') {
    for (const [key, count] of Object.entries(draft.selectedModules)) {
      if (['main_images', 'detail_page', 'sku_images'].includes(key) && Number.isInteger(count) && count >= 1 && count <= (key === 'main_images' ? 10 : 20)) selectedModules[key] = count
    }
  }
  return { projectId: projectId!, type, prompt: draft.prompt, ...(Object.keys(selectedModules).length ? { selectedModules } : {}) }
}

/** Only carries a draft into existing creation flows. Never submits a task. */
export function mobileCreationHandoff(projectId: string, type: MobileCreationType, brief: string, imageChoice = 'hero') {
  if (type === 'wechat-article' || type === 'seednote') return {
    to: '/', state: { modeId: 'creation', projectId, creationType: type, initialMessage: brief, startNew: true },
  }
  const choice = productImageChoices.find(item => item.id === imageChoice) ?? productImageChoices[0]
  return {
    to: `/tasks/new?project_id=${encodeURIComponent(projectId)}&type=${type}`,
    state: { mobileDraft: {
      projectId, type,
      prompt: type === 'ecommerce' ? [brief.trim(), choice.brief].filter(Boolean).join('\n\n') : brief,
      ...(type === 'ecommerce' ? { selectedModules: choice.modules } : {}),
    } },
  }
}
