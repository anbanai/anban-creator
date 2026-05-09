export type PosterTaskStatus = 'drafting' | 'generating' | 'completed' | 'failed'

export interface PosterImage {
  url: string
  width: number
  height: number
}

export interface PosterTask {
  id: string
  user_id: string
  template_id: string | null
  input_content: {
    title: string
    selling_points: string[]
    brand: string
    price?: string
  }
  style_preference: string
  images: PosterImage[]
  conversation: Array<{
    role: 'user' | 'assistant'
    content: string
  }>
  status: PosterTaskStatus
  error_message?: string
  created_at: string
  updated_at: string
}

export interface CreatePosterRequest {
  template_id?: string
  input_content: {
    title: string
    selling_points: string[]
    brand: string
    price?: string
  }
  style_preference: string
}
