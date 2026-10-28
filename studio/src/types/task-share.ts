export interface TaskShareStatus {
  status: 'none' | 'active' | 'expired' | 'revoked'
  share_path?: string
  expires_at?: string
}

export interface PublicShareImage {
  id: string
  url: string
  alt: string
  role: 'cover' | 'image' | 'tail'
}

export interface PublicShare {
  type: 'wechat-article' | 'seednote'
  title: string
  format: 'html' | 'markdown'
  body: string
  images: PublicShareImage[]
  expires_at: string
}
