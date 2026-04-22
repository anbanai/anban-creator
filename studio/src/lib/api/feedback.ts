import { http, unwrap } from '@/lib/http-client'

export interface CreateFeedbackRequest {
  type: 'bug' | 'suggestion'
  content: string
}

export interface Feedback {
  id: string
  user_id: string
  type: 'bug' | 'suggestion'
  content: string
  created_at: string
  updated_at: string
}

export const feedbackApi = {
  create: async (data: CreateFeedbackRequest): Promise<Feedback> => {
    return unwrap<Feedback>(http.post('/feedback', data))
  },
}
