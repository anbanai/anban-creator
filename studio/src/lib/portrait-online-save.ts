import type { ProjectProfile } from '@/types'
import type { projectsApi } from './api/projects'
import type { PortraitCandidate, PortraitChatMessage } from './portrait-chat-contract'
import { portraitToProjectProfile, preparePortraitProject } from './portrait-project-handoff'

export interface PortraitReceipt {
  userId: string
  portraitKey: string
  phase: 'new' | 'creating' | 'project_created' | 'saving' | 'saved' | 'task_creating' | 'task_created'
  projectId?: string
  baseRevision?: number
  profileVersion?: number
  taskId?: string
}
export function portraitKey(candidate: PortraitCandidate) {
  return JSON.stringify({ name:candidate.name, summary:candidate.summary, facets:candidate.facets })
}
export async function savePortraitProject(
  client: Pick<typeof projectsApi, 'create' | 'getAccountProfile' | 'confirmAccountProfile'>,
  candidate: PortraitCandidate, messages: PortraitChatMessage[], initial: PortraitReceipt,
  checkpoint: (receipt: PortraitReceipt) => void,
): Promise<PortraitReceipt> {
  const prepared = preparePortraitProject(candidate, messages)
  if (initial.portraitKey !== portraitKey(candidate)) throw new Error('画像已有变化，请先在已保存项目中核对修改，避免覆盖。')
  let receipt = { ...initial }
  const save = (patch: Partial<PortraitReceipt>) => { receipt = { ...receipt, ...patch }; checkpoint(receipt) }
  if (!receipt.projectId) {
    if (receipt.phase !== 'new') throw new Error('上次创建结果尚未确认，请到项目列表核对，暂不重复创建。')
    save({ phase:'creating' })
    const response = await client.create(prepared.project)
    if (!response.project?.id) throw new Error('未收到项目编号，请到项目列表核对创建结果。')
    save({ projectId:response.project.id, phase:'project_created' })
  }
  const current = await client.getAccountProfile(receipt.projectId!)
  const next = portraitToProjectProfile(candidate, current)
  const sameDimensions = (profile: ProjectProfile) => JSON.stringify(profile.dimensions) === JSON.stringify(next.dimensions)
  // Recover a lost confirmation response by reading the actual Server state.
  // The Server adds [用户编辑], so compare contents/evidence rather than source tags.
  const sameContent = (profile: ProjectProfile) => Object.keys(next.dimensions).every(key => {
    const dim = key as keyof ProjectProfile['dimensions']
    return JSON.stringify(profile.dimensions[dim].content) === JSON.stringify(next.dimensions[dim].content)
      && JSON.stringify(profile.dimensions[dim].evidence) === JSON.stringify(next.dimensions[dim].evidence)
  })
  if (current.status === 'confirmed' && (sameDimensions(current) || sameContent(current))) {
    save({phase:receipt.taskId ? 'task_created' : 'saved',profileVersion:current.version})
    return receipt
  }
  if (receipt.profileVersion !== undefined || (receipt.baseRevision !== undefined && current.version !== receipt.baseRevision)) throw new Error('线上画像已有新版本，请到项目中核对，暂不覆盖。')
  if (receipt.baseRevision === undefined && current.version !== 0) throw new Error('该项目已有画像，请到项目中核对，暂不覆盖。')
  save({phase:'saving',baseRevision:current.version})
  const confirmed = await client.confirmAccountProfile(receipt.projectId!, next)
  if (confirmed.status !== 'confirmed' || confirmed.version <= current.version) throw new Error('服务端尚未确认保存结果，请检查项目后重试。')
  save({phase:'saved',profileVersion:confirmed.version})
  return receipt
}
