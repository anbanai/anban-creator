import { authApi } from './auth'
import { plansApi } from './plans'
import { tasksApi } from './tasks'
import { timelineApi } from './timeline'
import { channelsApi } from './channels'
import { creditsApi } from './credits'
import { apiKeysApi } from './api-keys'
import { usageApi } from './usage'
import { feedbackApi } from './feedback'
import { modelConfigApi } from './model-config'

export const api = {
  auth: authApi,
  plans: plansApi,
  tasks: tasksApi,
  timeline: timelineApi,
  channels: channelsApi,
  credits: creditsApi,
  apiKeys: apiKeysApi,
  usage: usageApi,
  feedback: feedbackApi,
  modelConfig: modelConfigApi,
}
