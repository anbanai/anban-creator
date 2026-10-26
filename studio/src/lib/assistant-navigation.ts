import type { LinkProps } from 'react-router-dom'
import type { AssistantModeID } from '@/lib/api/assistant'

export type AssistantRouteState = {
  modeId?: AssistantModeID
  projectId?: string
  contentId?: string
  checkpointId?: string
  initialMessage?: string
}

export function profileInterviewLinkProps(projectId: string): Pick<LinkProps, 'to' | 'state'> {
  return { to: '/', state: { modeId: 'profile', projectId } satisfies AssistantRouteState }
}

export function hotTrendCreationState(projectId: string | undefined, title: string, url?: string): AssistantRouteState {
  const source = url ? `\n来源：${url}` : ''
  return {
    modeId: 'creation',
    projectId,
    initialMessage: `围绕当前热点「${title}」：先判断它是否适合我的账号赛道；若合适，给出 2-3 个差异化的二创角度，并把最推荐的一条写成可直接发布的文案初稿。${source}`,
  }
}
