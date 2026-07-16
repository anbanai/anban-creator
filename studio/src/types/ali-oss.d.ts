declare module 'ali-oss' {
  export interface OSSClientOptions {
    region?: string
    bucket?: string
    endpoint?: string
    accessKeyId: string
    accessKeySecret: string
    stsToken?: string
    secure?: boolean
  }

  export interface OSSUploadOptions {
    headers?: Record<string, string>
    progress?: (percent: number, checkpoint?: OSSMultipartCheckpoint) => void
  }

  export interface OSSMultipartCheckpoint {
    name?: string
    uploadId?: string
  }

  export interface OSSMultipartCancelOptions {
    name: string
    uploadId: string
  }

  export default class OSS {
    constructor(options: OSSClientOptions)
    put(name: string, file: Blob | File, options?: OSSUploadOptions): Promise<unknown>
    multipartUpload(name: string, file: Blob | File, options?: OSSUploadOptions): Promise<unknown>
    cancel(options?: OSSMultipartCancelOptions): void
  }
}
