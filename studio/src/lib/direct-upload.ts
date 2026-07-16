import http from '@/lib/http-client'
import type { InputAttachment, InputAttachmentType } from '@/types/input-attachment'

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
  signal?: AbortSignal
}

export interface UploadToOSSResult {
  uploadId: string
  key: string
  publicUrl: string
  contentType: string
  size: number
}

export function directUploadResultToInputAttachment(
  result: UploadToOSSResult,
  file: File,
  type: InputAttachmentType,
  instruction?: string,
): InputAttachment {
  return {
    type,
    upload_id: result.uploadId,
    key: result.key,
    file_name: file.name,
    content_type: result.contentType,
    size: result.size,
    instruction,
  }
}

interface PrepareUploadResponse {
  upload_id: string
  key: string
  public_url: string
  upload_url: string
  headers?: Record<string, string>
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

export async function uploadToOSS({ purpose, file, onProgress, signal }: UploadToOSSOptions): Promise<UploadToOSSResult> {
  const contentType = file.type || ''
  for (let attempt = 0; attempt < 2; attempt += 1) {
    throwIfUploadAborted(signal)
    const prepared = await prepareUpload(purpose, file, contentType, signal)
    throwIfUploadAborted(signal)
    if (file.size > prepared.max_size) {
      throw new Error(`文件大小不能超过 ${Math.round(prepared.max_size / 1024 / 1024)}MB`)
    }
    const uploadContentType = preparedContentType(prepared, contentType)

    try {
      onProgress?.(1)
      if (file.size >= MULTIPART_THRESHOLD) {
        await uploadMultipart(prepared, file, uploadContentType, onProgress, signal)
      } else {
        await uploadSignedPut(prepared, file, uploadContentType, onProgress, signal)
      }
      onProgress?.(100)
    } catch (err) {
      if (signal?.aborted || isUploadAbortError(err)) throw uploadAbortError()
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
      contentType: uploadContentType,
      size: file.size,
    }
  }

  throw new Error('上传凭证已过期，请重试上传')
}

async function uploadMultipart(
  prepared: PrepareUploadResponse,
  file: File,
  contentType: string,
  onProgress?: (percent: number) => void,
  signal?: AbortSignal,
) {
  throwIfUploadAborted(signal)
  const { default: OSS } = await import('ali-oss')
  throwIfUploadAborted(signal)
  const client = new OSS({
    region: prepared.region,
    bucket: prepared.bucket,
    endpoint: prepared.endpoint,
    accessKeyId: prepared.sts_access_key_id,
    accessKeySecret: prepared.sts_access_key_secret,
    stsToken: prepared.sts_security_token,
    secure: true,
  })
  let latestUploadId = ''
  let queueCancelled = false
  let remoteUploadCancelled = false
  const cancel = () => {
    if (latestUploadId) {
      if (remoteUploadCancelled) return
      remoteUploadCancelled = true
      client.cancel({ name: prepared.key, uploadId: latestUploadId })
      return
    }
    if (queueCancelled) return
    queueCancelled = true
    client.cancel()
  }
  signal?.addEventListener('abort', cancel, { once: true })
  try {
    await client.multipartUpload(prepared.key, file, {
      headers: { 'Content-Type': contentType },
      progress: (p: number, checkpoint) => {
        if (checkpoint?.uploadId) latestUploadId = checkpoint.uploadId
        if (signal?.aborted) {
          cancel()
          return
        }
        onProgress?.(Math.min(99, Math.max(1, Math.round(p * 100))))
      },
    })
  } finally {
    signal?.removeEventListener('abort', cancel)
  }
  throwIfUploadAborted(signal)
}

function uploadSignedPut(
  prepared: PrepareUploadResponse,
  file: File,
  contentType: string,
  onProgress?: (percent: number) => void,
  signal?: AbortSignal,
) {
  if (!prepared.upload_url) return Promise.reject(new Error('signed upload URL is unavailable'))
  throwIfUploadAborted(signal)

  return new Promise<void>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    const abort = () => xhr.abort()
    const cleanup = () => signal?.removeEventListener('abort', abort)
    xhr.open('PUT', prepared.upload_url, true)
    const headers = { ...prepared.headers }
    if (!Object.keys(headers).some((header) => header.toLowerCase() === 'content-type')) {
      headers['Content-Type'] = contentType
    }
    for (const [header, value] of Object.entries(headers)) xhr.setRequestHeader(header, value)
    xhr.upload.onprogress = (event) => {
      if (!event.lengthComputable || event.total <= 0) return
      onProgress?.(Math.min(99, Math.max(1, Math.round((event.loaded / event.total) * 100))))
    }
    xhr.onload = () => {
      cleanup()
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve()
        return
      }
      reject(new Error(xhr.responseText || `OSS PUT failed with status ${xhr.status}`))
    }
    xhr.onerror = () => {
      cleanup()
      reject(new Error('Network error while uploading to OSS'))
    }
    xhr.onabort = () => {
      cleanup()
      reject(uploadAbortError())
    }
    signal?.addEventListener('abort', abort, { once: true })
    try {
      xhr.send(file)
    } catch (error) {
      cleanup()
      reject(error)
    }
  })
}

function preparedContentType(prepared: PrepareUploadResponse, requested: string) {
  return prepared.headers?.['Content-Type'] ||
    prepared.headers?.['content-type'] ||
    requested ||
    'application/octet-stream'
}

async function prepareUpload(
  purpose: DirectUploadPurpose,
  file: File,
  contentType: string,
  signal?: AbortSignal,
): Promise<PrepareUploadResponse> {
  try {
    const payload = {
      purpose,
      filename: file.name,
      content_type: contentType,
      size: file.size,
    }
    const res = signal
      ? await http.post('/uploads/prepare', payload, { signal })
      : await http.post('/uploads/prepare', payload)
    return (res.data?.data ?? res.data) as PrepareUploadResponse
  } catch (err: any) {
    if (signal?.aborted || isUploadAbortError(err)) throw uploadAbortError()
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

function uploadAbortError() {
  return new DOMException('Upload aborted', 'AbortError')
}

function isUploadAbortError(error: unknown) {
  return (error as { name?: string } | null)?.name === 'AbortError'
}

function throwIfUploadAborted(signal?: AbortSignal): void {
  if (signal?.aborted) throw uploadAbortError()
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
