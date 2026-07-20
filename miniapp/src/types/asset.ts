export type ReferenceImageSelection =
  | { asset_id: string; upload_session_id?: never }
  | { upload_session_id: string; asset_id?: never }

export interface ReferenceAssetView {
  asset_id: string
  file_name: string
  content_type: string
  size: number
  download_url: string
  download_expires_at: string
}

export type ReferenceImageValue = ReferenceImageSelection | ReferenceAssetView
