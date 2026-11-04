import type { ContentAsset } from '@/types'
import type { QueryClient } from '@tanstack/react-query'
export const topicPageSize = 25
export function topicFilters(params: URLSearchParams) {
  const requested = Number(params.get('page') ?? 1)
  const page = Number.isSafeInteger(requested) && requested > 0 ? requested : 1
  const usage = params.get('usage')
  return { q: params.get('q') || undefined, project_id: params.get('project_id') || undefined, usage: usage === 'used' || usage === 'unused' ? usage : undefined, offset: (page - 1) * topicPageSize, limit: topicPageSize, page } as const
}
export function topicUsage(asset: ContentAsset, projectId?: string) {
  const uses = asset.project_uses ?? []
  return projectId ? uses.find(use => use.project_id === projectId)?.status ?? 'unused' : uses.some(use => use.status === 'used') ? 'used' : 'unused'
}
export function topicCreationHref(asset: ContentAsset, projectId: string) {
  return `/tasks/new?${new URLSearchParams({ project_id: projectId, asset_id: asset.asset_id, prompt: [asset.title, asset.core_question].filter(Boolean).join('\n') })}`
}
export async function invalidateTopics(client: QueryClient) {
  await Promise.all(['content-assets', 'topic-radar', 'content-reuse-runs', 'tasks', 'projects', 'project-stats'].map(key => client.invalidateQueries({ queryKey: [key] })))
}

export function isTopicScript(output: NonNullable<ContentAsset['outputs']>[number]) {
  return output.type === 'topic-script' || output.type === 'content-script'
}
export function topicOutputHref(assetId: string, output: NonNullable<ContentAsset['outputs']>[number]) {
  if (output.job_id) return `/topics?${new URLSearchParams({ asset_id: assetId, detail: 'script', script_id: output.job_id })}`
  return output.task_id ? `/tasks/${output.task_id}` : undefined
}

export function topicOutputSummaries(asset: ContentAsset) {
  const outputs = asset.outputs?.filter(output => topicOutputHref(asset.asset_id, output)) ?? []
  return [...outputs.filter(isTopicScript).slice(0, 1), ...outputs.filter(output => !isTopicScript(output)).slice(0, 2)]
}
