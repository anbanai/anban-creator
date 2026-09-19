export type WechatPublicationStatus =
  | 'drafting'
  | 'drafted'
  | 'awaiting_manual_publish'
  | 'ambiguous'
  | 'publishing'
  | 'published'
  | 'needs_selection'
  | 'publish_failed'
  | 'unsupported'

export interface WechatPublicationCandidate {
  article_id: string
  title?: string
  digest?: string
  content_url?: string
  update_time?: number
  published_at?: string
}

export interface WechatPublication {
  id: string
  task_id: string
  project_id: string
  draft_media_id?: string
  draft_title?: string
  draft_author?: string
  draft_digest?: string
  draft_thumb_media_id?: string
  source: 'anban_api' | 'wechat_console' | string
  status: WechatPublicationStatus | string
  publish_id?: string
  msg_data_id?: string
  msg_id?: string
  article_id?: string
  article_url?: string
  article_index?: number
  wechat_status_code?: number
  draft_created_at?: string
  published_at?: string
  next_check_at?: string
  last_checked_at?: string
  submit_attempted_at?: string
  check_attempts?: number
  last_error?: string
  manual_publish_required?: boolean
  analytics_status?: string
  candidates?: WechatPublicationCandidate[]
}
