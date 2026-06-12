import { apiKeysApi } from './api-keys'
import { authApi } from './auth'
import { channelsApi } from './channels'
import { creditsApi } from './credits'
import { designerApi } from './designer'
import { modelConfigApi } from './model-config'
import { plansApi } from './plans'
import { postersApi } from './posters'
import { resourcesApi } from './resources'
import { tasksApi } from './tasks'
import { templatesApi } from './templates'
import { timelineApi } from './timeline'
import { topicPoolApi } from './topic-pool'
import { usageApi } from './usage'
import { viralAnalysesApi } from './viral-analyses'

export { del, get, getApiErrorMessage, patch, post, put } from './request'

export const api = {
  apiKeys: apiKeysApi,
  auth: authApi,
  channels: channelsApi,
  credits: creditsApi,
  designer: designerApi,
  modelConfig: modelConfigApi,
  plans: plansApi,
  posters: postersApi,
  resources: resourcesApi,
  tasks: tasksApi,
  templates: templatesApi,
  timeline: timelineApi,
  topicPool: topicPoolApi,
  usage: usageApi,
  viralAnalyses: viralAnalysesApi,
}
