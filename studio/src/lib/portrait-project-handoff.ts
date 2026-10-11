import type { CreateProjectRequest, ProfileDimension, ProjectProfile } from '@/types'
import { validatePortraitCandidate, type PortraitCandidate, type PortraitChatMessage } from './portrait-chat-contract'

/** Only call after the user confirms the current conversation revision. */
export function preparePortraitProject(candidate: PortraitCandidate, messages: PortraitChatMessage[]) {
  const checked = validatePortraitCandidate(candidate, messages)
  const project: CreateProjectRequest = {
    name: checked.name || '我的内容项目',
    instructions: checked.summary || checked.facets.identity?.text || '画像信息尚待补充，请勿编造用户身份与经历。',
  }
  return { project, candidate: checked }
}

/** Preserve the Server's revision/lifecycle metadata; it decides the saved state. */
export function portraitToProjectProfile(candidate: PortraitCandidate, current: ProjectProfile): ProjectProfile {
  const dimension = (fact: PortraitCandidate['creationIdea']): ProfileDimension => fact ? {
    content: { description: fact.text, original_certainty: fact.certainty },
    sources: ['[用户确认]'],
    evidence: fact.evidence.map(item => `${item.messageId}: ${item.quote}`),
    missing_fields: [],
  } : { content: {}, sources: ['[待补充]'], evidence: [], missing_fields: ['尚未提供'] }
  return {
    ...current,
    dimensions: {
      identity: dimension(candidate.facets.identity),
      audience: dimension(candidate.facets.audience),
      style: dimension(candidate.facets.style),
      platforms: dimension(candidate.facets.platforms),
      preferences: dimension(candidate.facets.preferences),
      memory: dimension(candidate.facets.experience),
    },
    analysis_limits: ['来自用户确认的对话整理；未核实外部经营数据。'],
    follow_up_questions: [],
  }
}

/** A single-work brief must not leak into long-term project instructions. */
export function portraitCreationBrief(candidate: PortraitCandidate, idea = candidate.creationIdea?.text): string {
  if (!idea?.trim()) throw new Error('请写下这次想创作什么。')
  return [
    `本次创作：${idea.trim()}`,
    '依据已确认的项目画像创作；没有提供的案例、截图、数字和效果不得编造。',
    '交付可审阅的作品，保留需要补充的素材位置；不要自行公开发布。',
  ].join('\n\n')
}
