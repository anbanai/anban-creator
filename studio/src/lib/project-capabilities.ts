import type { Project } from '@/types'

export const profileLabAgents = [
  { id: 'wechat-article', label: '公众号文章', detail: '深度文章与图文内容' },
  { id: 'wechat-picture', label: '公众号贴图', detail: '图文卡片与视觉表达' },
  { id: 'seednote', label: '种草笔记', detail: '笔记文案与多页内容' },
  { id: 'montage', label: '视频生成', detail: '从创意到完整视频' },
  { id: 'video-edit', label: '全自动视频剪辑', detail: '使用上传素材自动剪辑并交付编辑证据' },
  { id: 'hypit', label: '视频复刻', detail: '基于参考视频拆解复刻' },
  { id: 'local-life', label: '素材混剪', detail: '本地素材批量混剪视频' },
  { id: 'whiteboard-animation', label: '白板视频', detail: '讲解型动画内容' },
] as const

export const creationObjectLabels: Record<string, string> = {
  ...Object.fromEntries(profileLabAgents.map((agent) => [agent.id, agent.label])),
  moments: '朋友圈', ecommerce: '电商出图',
}

/** An explicit empty collection must never fall back to legacy capabilities. */
export function projectAgentIDs(project: Project): string[] {
  if (project.agent_ids != null) return project.agent_ids
  if (project.platform === 'wechat') return ['wechat-article']
  return project.platform ? [project.platform] : []
}

export function supportsProfileLab(project: Project): boolean {
  if (project.agent_ids != null) return project.agent_ids.every((id) => profileLabAgents.some((agent) => agent.id === id))
  return ['wechat', 'seednote', 'montage', 'hypit', 'whiteboard-animation', 'video-edit'].includes(project.platform)
}
