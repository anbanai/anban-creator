import { authApi } from './auth'
import { plansApi } from './plans'
import { tasksApi } from './tasks'
import { timelineApi } from './timeline'
import { projectsApi } from './projects'
import { billingApi } from './billing'
import { apiKeysApi } from './api-keys'
import { usageApi } from './usage'
import { feedbackApi } from './feedback'
import { seednoteAnalyticsApi } from './seednote-analytics'
import { templatesApi } from './templates'
import { postersApi } from './posters'
import { viralAnalysesApi } from './viral-analyses'
import { resourcesApi } from './resources'
import { topicPoolApi } from './topic-pool'
import { designerApi } from './designer'
import { imageCapabilitiesApi } from './image-capabilities'
import { ilinkApi } from './ilink'
import { aiEntryApi } from './ai-entry'
import { uploadsApi } from './uploads'
import { agentProfilesApi } from './agent-profiles'

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
  seednoteAnalytics: seednoteAnalyticsApi,
  templates: templatesApi,
  posters: postersApi,
  viralAnalyses: viralAnalysesApi,
  resources: resourcesApi,
  topicPool: topicPoolApi,
  designer: designerApi,
  imageCapabilities: imageCapabilitiesApi,
  ilink: ilinkApi,
  aiEntry: aiEntryApi,
  uploads: uploadsApi,
  agentProfiles: agentProfilesApi,
}
