import type { Project, ProjectPlatform } from '@/types'
import { projectAgentIDs } from './project-capabilities'

// Keep these routes aligned with server/service/assistant_commit.go.
const routes = [
  { agent: 'wechat-article', platform: 'wechat', value: 'wechat-article', label: '公众号文章' },
  { agent: 'wechat-picture', platform: 'wechat', value: 'wechat-picture', label: '公众号贴图' },
  { agent: 'seednote', platform: 'seednote', value: 'seednote', label: '种草笔记' },
  { agent: 'montage', platform: 'montage', value: 'video', label: '视频生成' },
  { agent: 'video-edit', platform: 'video-edit', value: 'video-edit', label: '全自动视频剪辑' },
] as const

export function assistantCreationTypes(project?: { platform: string; agent_ids?: string[] | null }) {
  if (!project) return []
  const agents = projectAgentIDs(project as Pick<Project, 'platform' | 'agent_ids'>)
  return routes.filter(route => route.platform === project.platform && agents.includes(route.agent))
}

// The assistant currently requires both the primary platform and the selected
// Agent. Retain a compatible primary platform; initialize/repair it otherwise.
export function assistantPrimaryPlatform(platform: string, agents: string[]): ProjectPlatform | undefined {
  const available = routes.filter(route => agents.includes(route.agent))
  return available.find(route => route.platform === platform)?.platform ?? available[0]?.platform
}
