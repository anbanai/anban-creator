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
  profileQuote: (id: string, data: { execution_profile?: string; answers?: Record<string, unknown>; samples?: string[] }) => unwrap<{ id: string; price_credits: number; list_price_credits: number; execution_profile: string; currency: string; expires_at: string; request_fingerprint: string }>(http.post(`/projects/${id}/profile/analysis/quote`, data)),
  startProfileAnalysis: (id: string, data: { quote_id: string; quote_confirmed: boolean; execution_profile?: string; answers?: Record<string, unknown>; samples?: string[] }) => unwrap<{ task: Task; profile: ProjectProfile }>(http.post(`/projects/${id}/profile/analysis`, data)),
  profileAnalysis: (id: string) => unwrap<ProfileAnalysisResponse>(http.get(`/projects/${id}/profile/analysis`)),
  confirmAccountProfile: (id: string, profile: ProjectProfile) => unwrap<ProjectProfile>(http.put(`/projects/${id}/profile`, { version: profile.version, profile })),
  updateProfileDimension: (id: string, dimension: string, version: number, value: ProfileDimension) => unwrap<ProjectProfile>(http.patch(`/projects/${id}/profile/${dimension}`, { version, value })),
}
