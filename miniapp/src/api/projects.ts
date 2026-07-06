import { get, post, put, patch, del } from './request'
import { uploadUrl } from './api-base'
import { TOKEN_KEY } from '@/utils/constants'
import type {
  Project,
  ProjectDetail,
  ProjectStats,
  CreateProjectRequest,
  CreateProjectResponse,
  PlatformConfig,
  PlatformProfile,
  AnalyzeImageResponse,
  FileUploadResponse,
} from '@/types'

export const projectsApi = {
  list: (params?: { status?: string; platform?: string }) =>
    get<Project[]>('/projects', params as Record<string, any>),

  get: (id: string) =>
    get<ProjectDetail>(`/projects/${id}`),

  stats: (ids: string[]) =>
    get<Record<string, ProjectStats>>('/projects/stats', { ids: ids.join(',') }),

  create: (data: CreateProjectRequest) =>
    post<CreateProjectResponse>('/projects', data),

  update: (id: string, data: Partial<CreateProjectRequest>) =>
    put<Project>(`/projects/${id}`, data),

  archive: (id: string) =>
    patch<void>(`/projects/${id}/archive`),

  restore: (id: string) =>
    patch<void>(`/projects/${id}/restore`),

  delete: (id: string) =>
    del<void>(`/projects/${id}`),

  platformConfigs: () =>
    get<PlatformConfig[]>('/projects/platform-configs'),

  /** Fetch profile info from a platform URL (e.g. WeChat MP homepage) */
  fetchProfile: (
    platform: string,
    profileUrl: string,
    wechatAppId?: string,
    wechatSecret?: string,
  ) =>
    post<PlatformProfile>(
      '/projects/fetch-profile',
      { platform, profile_url: profileUrl, wechat_app_id: wechatAppId, wechat_secret: wechatSecret },
      120000, // 2 min timeout — external fetch can be slow
    ),

  /** Analyze a reference image URL via vision LLM and return a visual style description. */
  analyzeImage: (imageUrl: string) =>
    post<AnalyzeImageResponse>(
      '/projects/analyze-image',
      { image_url: imageUrl },
      120000, // 2 min — vision LLM call
    ),

  /**
   * Upload an image (avatar / reference image) via the generic /files/upload
   * endpoint. `purpose` is required by the server and must be either
   * "project" (avatars) or "reference" (reference images).
   */
  uploadImage: (
    filePath: string,
    purpose: 'project' | 'reference' = 'project',
  ) =>
    new Promise<FileUploadResponse>((resolve, reject) => {
      const token = uni.getStorageSync(TOKEN_KEY)
      uni.uploadFile({
        url: uploadUrl('/files/upload'),
        filePath,
        name: 'file',
        header: token ? { Authorization: `Bearer ${token}` } : undefined,
        formData: { purpose },
        success(res) {
          try {
            const body = JSON.parse(res.data) as { code: number; msg?: string; data?: FileUploadResponse }
            if (body.code === 0 && body.data) {
              resolve(body.data)
            } else {
              reject(new Error(body.msg || '上传失败'))
            }
          } catch {
            reject(new Error('上传响应解析失败'))
          }
        },
        fail(err) {
          reject(new Error(err.errMsg || '上传失败'))
        },
      })
    }),
}
