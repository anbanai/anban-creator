import { authApi } from './auth'
import { plansApi } from './plans'
import { tasksApi } from './tasks'
import { timelineApi } from './timeline'
import { projectsApi } from './projects'
import { billingApi } from './billing'
import { apiKeysApi } from './api-keys'
import { usageApi } from './usage'
import { feedbackApi } from './feedback'
import { modelConfigApi } from './model-config'
import { seednoteAnalyticsApi } from './seednote-analytics'
import { templatesApi } from './templates'
import { postersApi } from './posters'
import { viralAnalysesApi } from './viral-analyses'
import { resourcesApi } from './resources'
import { topicPoolApi } from './topic-pool'
import { designerApi } from './designer'
import { imageModelsApi } from './image-models'
import { ilinkApi } from './ilink'
import { aiEntryApi } from './ai-entry'
import { uploadsApi } from './uploads'
import { agentProfilesApi } from './agent-profiles'
import { agentPacksApi } from './agent-packs'

export const api = {
  auth: authApi,
  plans: plansApi,
  tasks: tasksApi,
  timeline: timelineApi,
  projects: projectsApi,
  billing: billingApi,
  apiKeys: apiKeysApi,
  usage: usageApi,
  feedback: feedbackApi,
  modelConfig: modelConfigApi,
  seednoteAnalytics: seednoteAnalyticsApi,
  templates: templatesApi,
  posters: postersApi,
  viralAnalyses: viralAnalysesApi,
  resources: resourcesApi,
  topicPool: topicPoolApi,
  designer: designerApi,
  imageModels: imageModelsApi,
  ilink: ilinkApi,
  aiEntry: aiEntryApi,
  uploads: uploadsApi,
  agentProfiles: agentProfilesApi,
  agentPacks: agentPacksApi,
}
