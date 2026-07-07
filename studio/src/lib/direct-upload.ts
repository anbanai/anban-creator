import http from '@/lib/http-client'

export type DirectUploadPurpose =
  | 'project_reference'
  | 'task_reference'
  | 'ecommerce_product_photo'
  | 'video_reference'
  | 'designer_reference'
  | 'ai_entry_attachment'

export interface UploadToOSSOptions {
  purpose: DirectUploadPurpose
  file: File
  onProgress?: (percent: number) => void
  fallbackToLegacy?: boolean
}

export interface UploadToOSSResult {
  uploadId: string
  key: string
  publicUrl: string
  contentType: string
  size: number
  inputDurationSeconds?: number
  warning?: string
}

interface PrepareUploadResponse {
  upload_id: string
  key: string
  public_url: string
  region: string
  bucket: string
  endpoint: string
  sts_access_key_id: string
  sts_access_key_secret: string
  sts_security_token: string
  expires_at: string
  max_size: number
}

const MULTIPART_THRESHOLD = 8 * 1024 * 1024

export async function uploadToOSS({ purpose, file, onProgress, fallbackToLegacy = true }: UploadToOSSOptions): Promise<UploadToOSSResult> {
  const contentType = file.type || 'application/octet-stream'
  for (let attempt = 0; attempt < 2; attempt += 1) {
    let prepared: PrepareUploadResponse
    try {
      prepared = await prepareUpload(purpose, file, contentType)
    } catch (err) {
      if (fallbackToLegacy && isDirectUploadUnavailable(err)) {
        return uploadWithLegacyEndpoint(purpose, file, onProgress)
      }
      throw err
    }
    if (file.size > prepared.max_size) {
      throw new Error(`文件大小不能超过 ${Math.round(prepared.max_size / 1024 / 1024)}MB`)
    }

    const { default: OSS } = await import('ali-oss')
    const client = new OSS({
      region: prepared.region,
      bucket: prepared.bucket,
      endpoint: prepared.endpoint,
      accessKeyId: prepared.sts_access_key_id,
      accessKeySecret: prepared.sts_access_key_secret,
      stsToken: prepared.sts_security_token,
      secure: true,
    })

    try {
      onProgress?.(1)
      if (file.size >= MULTIPART_THRESHOLD) {
        await client.multipartUpload(prepared.key, file, {
          headers: { 'Content-Type': contentType },
          progress: (p: number) => {
            onProgress?.(Math.min(99, Math.max(1, Math.round(p * 100))))
          },
        })
      } else {
        await client.put(prepared.key, file, {
          headers: { 'Content-Type': contentType },
        })
      }
      onProgress?.(100)
    } catch (err) {
      const friendly = friendlyDirectUploadError(err)
      if (attempt === 0 && friendly === '上传凭证已过期，请重试上传') {
        onProgress?.(0)
        continue
      }
      throw new Error(friendly)
    }

    return {
      uploadId: prepared.upload_id,
      key: prepared.key,
      publicUrl: prepared.public_url,
      contentType,
      size: file.size,
    }
  }

  throw new Error('上传凭证已过期，请重试上传')
}

async function prepareUpload(purpose: DirectUploadPurpose, file: File, contentType: string): Promise<PrepareUploadResponse> {
  try {
    const res = await http.post('/uploads/prepare', {
      purpose,
      filename: file.name,
      content_type: contentType,
      size: file.size,
    })
    return (res.data?.data ?? res.data) as PrepareUploadResponse
  } catch (err: any) {
    const msg = err?.response?.data?.msg || err?.message || ''
    if (err?.response?.status === 503 && isDirectUploadUnavailableMessage(msg)) {
      if (isDirectUploadCredentialUnavailableMessage(msg)) {
        throw new Error('当前环境 OSS 直传凭证不可用，请联系管理员检查 RAM/STS 权限。')
      }
      throw new Error('当前环境未配置 OSS 直传，请联系管理员配置对象存储。')
    }
    throw new Error(msg || '获取上传凭证失败，请重试')
  }
}

async function uploadWithLegacyEndpoint(
  purpose: DirectUploadPurpose,
  file: File,
  onProgress?: (percent: number) => void,
): Promise<UploadToOSSResult> {
  const legacyPurpose = legacyPurposeForDirectUpload(purpose)
  if (!legacyPurpose) throw new Error('当前环境未配置 OSS 直传，请联系管理员配置对象存储。')
  const form = new FormData()
  form.append('file', file)
  form.append('purpose', legacyPurpose)
  const res = await http.post('/files/upload', form, {
    headers: { 'Content-Type': 'multipart/form-data' },
    onUploadProgress: (event) => {
      const total = event.total || file.size
      onProgress?.(total ? Math.min(99, Math.round((event.loaded / total) * 100)) : 50)
    },
  })
  const data = res.data?.data ?? res.data
  onProgress?.(100)
  return {
    uploadId: data.key || data.url || file.name,
    key: data.key || '',
    publicUrl: data.url,
    contentType: data.type || file.type || 'application/octet-stream',
    size: data.size || file.size,
    inputDurationSeconds: data.input_duration_seconds,
    warning: data.duration_probe_warning,
  }
}

function legacyPurposeForDirectUpload(purpose: DirectUploadPurpose) {
  switch (purpose) {
    case 'project_reference':
      return 'project'
    case 'task_reference':
    case 'ecommerce_product_photo':
    case 'designer_reference':
      return 'reference'
    case 'video_reference':
      return 'video_reference'
    case 'ai_entry_attachment':
      return ''
    default:
      return ''
  }
}

function isDirectUploadUnavailable(err: unknown) {
  const message = err instanceof Error ? err.message : String(err || '')
  return isDirectUploadUnavailableMessage(message)
}

function isDirectUploadUnavailableMessage(message: string) {
  return /未配置 OSS 直传|OSS 直传凭证不可用|direct upload|storage provider|requires OSS|file storage is not available|browser direct uploads|storage\.sts_role_arn|issue upload credential|refresh session token|AssumeRole|NoPermission|authorized by RAM/i.test(message)
}

function isDirectUploadCredentialUnavailableMessage(message: string) {
  return /OSS 直传凭证不可用|browser direct uploads|storage\.sts_role_arn|issue upload credential|refresh session token|AssumeRole|NoPermission|authorized by RAM/i.test(message)
}

function friendlyDirectUploadError(err: unknown) {
  const anyErr = err as { code?: string; message?: string }
  const message = anyErr?.message || String(err || '')
  if (/expired|expire|RequestTimeTooSkewed/i.test(message) || anyErr?.code === 'AccessDenied') {
    return '上传凭证已过期，请重试上传'
  }
  if (/Network|Failed to fetch|timeout/i.test(message)) {
    return '连接 OSS 失败，请检查网络后重试'
  }
  return 'OSS 上传失败，请重试'
}
