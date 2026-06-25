import { http, unwrap } from '@/lib/http-client'
import type {
  Project,
  ProjectDetail,
  ProjectStats,
  CreateProjectRequest,
  CreateProjectResponse,
  PlatformConfig,
  PlatformProfile,
} from '@/types'

export const projectsApi = {
  list: (params?: { status?: string; platform?: string }) =>
    unwrap<Project[]>(http.get('/projects', { params })),

  get: (id: string) =>
    unwrap<ProjectDetail>(http.get(`/projects/${id}`)),

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

  analyzeImage: (imageUrl: string) =>
    unwrap<{ style: string }>(http.post('/projects/analyze-image', {
      image_url: imageUrl,
    }, { timeout: 120000 })),
}
