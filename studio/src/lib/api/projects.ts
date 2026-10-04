import { http, unwrap } from '@/lib/http-client'
import type {
  Project,
  ProjectDetail,
  ProjectStats,
  CreateProjectRequest,
  CreateProjectResponse,
  PlatformConfig,
  PlatformProfile,
  ProjectMemory,
  ProjectProfile,
  ProfileDimension,
  ProfileAnalysisResponse,
  Task,
  FeedbackDashboard,
  ProjectChannelConfig,
} from '@/types'

export const projectsApi = {
  list: (params?: { status?: string; platform?: string }) =>
    unwrap<Project[]>(http.get('/projects', { params })),

  get: (id: string, signal?: AbortSignal) =>
    unwrap<ProjectDetail>(http.get(`/projects/${id}`, { signal })),

  feedback: (id: string, signal?: AbortSignal) =>
    unwrap<FeedbackDashboard>(http.get(`/projects/${id}/feedback`, { signal })),

  rerunFeedback: (id: string, cadence: 'daily' | 'weekly' | 'monthly', period?: { start: string; end: string }) =>
    unwrap<{ created: number; enqueued: number; skipped: number }>(http.post(`/projects/${id}/feedback/rerun`, undefined, {
      params: {
        cadence,
        ...(period ? { period_start: period.start, period_end: period.end } : {}),
      },
    })),

  setFeedbackPaused: (id: string, paused: boolean) =>
    unwrap<{ project_id: string; feedback_paused: boolean }>(http.post(`/projects/${id}/feedback/${paused ? 'pause' : 'resume'}`)),

  memory: (id: string) =>
    unwrap<ProjectMemory>(http.get(`/projects/${id}/memory`)),

  stats: (ids: string[]) =>
    unwrap<Record<string, ProjectStats>>(http.get('/projects/stats', {
      params: {
        ids: ids.join(','),
      },
    })),

  create: (data: CreateProjectRequest) =>
    unwrap<CreateProjectResponse>(http.post('/projects', data)),

  update: (id: string, data: Partial<CreateProjectRequest>) =>
    unwrap<Project>(http.put(`/projects/${id}`, data)),

  archive: (id: string) =>
    unwrap<void>(http.patch(`/projects/${id}/archive`)),

  restore: (id: string) =>
    unwrap<void>(http.patch(`/projects/${id}/restore`)),

  delete: (id: string) =>
    unwrap<void>(http.delete(`/projects/${id}`)),

  listChannelConfigs: (id: string) =>
    unwrap<ProjectChannelConfig[]>(http.get(`/projects/${id}/channel-configs`)),

  getChannelConfig: (id: string, channel: string) =>
    unwrap<ProjectChannelConfig>(http.get(`/projects/${id}/channel-configs/${encodeURIComponent(channel)}`)),

  upsertChannelConfig: (id: string, channel: string, config: Record<string, unknown>) =>
    unwrap<ProjectChannelConfig>(http.put(`/projects/${id}/channel-configs/${encodeURIComponent(channel)}`, { config })),

  deleteChannelConfig: (id: string, channel: string) =>
    unwrap<void>(http.delete(`/projects/${id}/channel-configs/${encodeURIComponent(channel)}`)),

  platformConfigs: () =>
    unwrap<PlatformConfig[]>(http.get('/projects/platform-configs')),

  fetchProfile: (platform: string, profileUrl: string, wechatAppId?: string, wechatSecret?: string) =>
    unwrap<PlatformProfile>(http.post('/projects/fetch-profile', {
      platform,
      profile_url: profileUrl,
      wechat_app_id: wechatAppId,
      wechat_secret: wechatSecret,
  }, { timeout: 120000 })),

  getAccountProfile: (id: string) => unwrap<ProjectProfile>(http.get(`/projects/${id}/profile`)),
  refreshProfile: (id: string, expectedRevision: number) => unwrap<{ task: Task; profile: ProjectProfile }>(http.post(`/projects/${id}/profile/refresh`, { expected_revision: expectedRevision })),
  retryProfile: (id: string, expectedRevision: number) => unwrap<{ task: Task; profile: ProjectProfile }>(http.post(`/projects/${id}/profile/retry`, { expected_revision: expectedRevision })),
  profileAnalysis: (id: string) => unwrap<ProfileAnalysisResponse>(http.get(`/projects/${id}/profile/analysis`)),
  confirmAccountProfile: (id: string, profile: ProjectProfile) => unwrap<ProjectProfile>(http.put(`/projects/${id}/profile`, { version: profile.version, profile })),
  updateProfileDimension: (id: string, dimension: string, version: number, value: ProfileDimension) => unwrap<ProjectProfile>(http.patch(`/projects/${id}/profile/dimensions/${dimension}`, { expected_revision: version, content: value.content })),
}
