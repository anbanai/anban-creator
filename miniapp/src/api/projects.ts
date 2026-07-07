import { get, post, put, patch, del } from './request'
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

export type DirectUploadPurpose =
  | 'project_reference'
  | 'task_reference'
  | 'ecommerce_product_photo'
  | 'video_reference'
  | 'designer_reference'
  | 'ai_entry_attachment'

interface PrepareUploadResponse {
  upload_id: string
  key: string
  public_url: string
  upload_url: string
  method: string
  headers?: Record<string, string>
  max_size: number
}

function filenameFromPath(filePath: string, fallbackExt = 'jpg'): string {
  const clean = filePath.split('?')[0]?.split('#')[0] || ''
  const raw = clean.split('/').filter(Boolean).pop() || ''
  let decoded = raw
  try {
    decoded = raw ? decodeURIComponent(raw) : ''
  } catch {
    decoded = raw
  }
  const filename = decoded || `upload-${Date.now()}`
  const ext = filename.split('.').pop()?.toLowerCase() || ''
  if (['jpg', 'jpeg', 'png', 'webp', 'gif', 'bmp'].includes(ext)) return filename
  return `${filename}.${fallbackExt}`
}

function contentTypeForFilename(filename: string): string {
  const ext = filename.split('.').pop()?.toLowerCase() || ''
  switch (ext) {
    case 'jpg':
    case 'jpeg':
      return 'image/jpeg'
    case 'png':
      return 'image/png'
    case 'webp':
      return 'image/webp'
    case 'gif':
      return 'image/gif'
    case 'bmp':
      return 'image/bmp'
    default:
      return ''
  }
}

function sniffImageType(data: ArrayBuffer): { contentType: string; ext: string } | undefined {
  const bytes = new Uint8Array(data.slice(0, 16))
  if (bytes[0] === 0xff && bytes[1] === 0xd8 && bytes[2] === 0xff) {
    return { contentType: 'image/jpeg', ext: 'jpg' }
  }
  if (
    bytes[0] === 0x89 &&
    bytes[1] === 0x50 &&
    bytes[2] === 0x4e &&
    bytes[3] === 0x47
  ) {
    return { contentType: 'image/png', ext: 'png' }
  }
  if (bytes[0] === 0x47 && bytes[1] === 0x49 && bytes[2] === 0x46 && bytes[3] === 0x38) {
    return { contentType: 'image/gif', ext: 'gif' }
  }
  if (
    bytes[0] === 0x52 &&
    bytes[1] === 0x49 &&
    bytes[2] === 0x46 &&
    bytes[3] === 0x46 &&
    bytes[8] === 0x57 &&
    bytes[9] === 0x45 &&
    bytes[10] === 0x42 &&
    bytes[11] === 0x50
  ) {
    return { contentType: 'image/webp', ext: 'webp' }
  }
  if (bytes[0] === 0x42 && bytes[1] === 0x4d) {
    return { contentType: 'image/bmp', ext: 'bmp' }
  }
  return undefined
}

function readFileAsArrayBuffer(filePath: string): Promise<ArrayBuffer> {
  const fs = typeof (uni as any).getFileSystemManager === 'function'
    ? (uni as any).getFileSystemManager()
    : null
  if (fs?.readFile) {
    return new Promise((resolve, reject) => {
      fs.readFile({
        filePath,
        success(res: { data: ArrayBuffer }) {
          resolve(res.data)
        },
        fail(err: { errMsg?: string }) {
          reject(new Error(err.errMsg || '读取文件失败'))
        },
      })
    })
  }
  if (typeof fetch === 'function') {
    return fetch(filePath).then((res) => {
      if (!res.ok) throw new Error('读取文件失败')
      return res.arrayBuffer()
    })
  }
  return Promise.reject(new Error('当前环境不支持读取本地文件'))
}

function putObjectToOSS(prepared: PrepareUploadResponse, data: ArrayBuffer, contentType: string): Promise<void> {
  const uploadURL = prepared.upload_url
  if (!uploadURL) return Promise.reject(new Error('上传地址为空'))
  const header = prepared.headers && Object.keys(prepared.headers).length > 0
    ? prepared.headers
    : { 'Content-Type': contentType || 'application/octet-stream' }

  return new Promise((resolve, reject) => {
    uni.request({
      url: uploadURL,
      method: (prepared.method || 'PUT') as any,
      data,
      header,
      success(res) {
        if (res.statusCode >= 200 && res.statusCode < 300) {
          resolve()
          return
        }
        reject(new Error(`OSS 上传失败 (${res.statusCode})`))
      },
      fail(err) {
        reject(new Error(err.errMsg || 'OSS 上传失败'))
      },
    })
  })
}

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

  /** Upload a local image through /uploads/prepare and a signed OSS PUT. */
  uploadImage: async (
    filePath: string,
    purpose: DirectUploadPurpose = 'project_reference',
  ): Promise<FileUploadResponse> => {
    const data = await readFileAsArrayBuffer(filePath)
    const sniffed = sniffImageType(data)
    const filename = filenameFromPath(filePath, sniffed?.ext || 'jpg')
    const contentType = sniffed?.contentType || contentTypeForFilename(filename)
    const prepared = await post<PrepareUploadResponse>('/uploads/prepare', {
      purpose,
      filename,
      content_type: contentType,
      size: data.byteLength,
    })
    if (data.byteLength > prepared.max_size) {
      throw new Error(`文件大小不能超过 ${Math.round(prepared.max_size / 1024 / 1024)}MB`)
    }
    const uploadContentType = prepared.headers?.['Content-Type'] ||
      prepared.headers?.['content-type'] ||
      contentType ||
      'application/octet-stream'
    await putObjectToOSS(prepared, data, uploadContentType)
    return {
      url: prepared.public_url,
      key: prepared.key,
      size: data.byteLength,
      type: uploadContentType,
    }
  },
}
