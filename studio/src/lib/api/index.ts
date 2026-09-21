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
import { wechatAnalyticsApi } from './wechat-analytics'
import { channelsAnalyticsApi } from './channels-analytics'
import { templatesApi } from './templates'
import { postersApi } from './posters'
import { viralAnalysesApi } from './viral-analyses'
import { resourcesApi } from './resources'
import { topicPoolApi } from './topic-pool'
import { imageCapabilitiesApi } from './image-capabilities'
import { hypitCapabilitiesApi } from './hypit-capabilities'
import { montageCapabilitiesApi } from './montage-capabilities'
import { ilinkApi } from './ilink'
import { aiEntryApi } from './ai-entry'
import { uploadsApi } from './uploads'
import { agentProfilesApi } from './agent-profiles'
import { agentPacksApi } from './agent-packs'
import { seednoteAdminApi } from './seednote-admin'
import { seednoteImportApi } from './seednote-import'
import { wechatAnalyticsImportApi } from './wechat-analytics-import'
import { imageAnalysesApi } from './image-analyses'

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
  wechatAnalytics: wechatAnalyticsApi,
  channelsAnalytics: channelsAnalyticsApi,
  templates: templatesApi,
  posters: postersApi,
  viralAnalyses: viralAnalysesApi,
  resources: resourcesApi,
  topicPool: topicPoolApi,
  imageCapabilities: imageCapabilitiesApi,
  hypitCapabilities: hypitCapabilitiesApi,
  montageCapabilities: montageCapabilitiesApi,
  ilink: ilinkApi,
  aiEntry: aiEntryApi,
  uploads: uploadsApi,
  agentProfiles: agentProfilesApi,
  agentPacks: agentPacksApi,
  seednoteAdmin: seednoteAdminApi,
  seednoteImport: seednoteImportApi,
  wechatAnalyticsImport: wechatAnalyticsImportApi,
  imageAnalyses: imageAnalysesApi,
}
