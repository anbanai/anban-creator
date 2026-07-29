import { apiKeysApi } from './api-keys'
import { agentProfilesApi } from './agent-profiles'
import { authApi } from './auth'
import { projectsApi } from './projects'
import { billingApi } from './billing'
import { designerApi } from './designer'
import { imageModelsApi } from './image-models'
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
  agentProfiles: agentProfilesApi,
  auth: authApi,
  projects: projectsApi,
  billing: billingApi,
  designer: designerApi,
  imageModels: imageModelsApi,
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
