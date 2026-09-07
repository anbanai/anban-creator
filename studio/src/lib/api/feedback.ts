import { http, unwrap } from '@/lib/http-client'
import type { TaskFeedback } from '@/types'

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
  getTask: async (taskId: string): Promise<TaskFeedback | null> => {
    return unwrap<TaskFeedback | null>(http.get(`/tasks/${taskId}/feedback`))
  },
  saveTask: async (taskId: string, data: { rating: number; content?: string }): Promise<TaskFeedback> => {
    return unwrap<TaskFeedback>(http.put(`/tasks/${taskId}/feedback`, data))
  },
}
