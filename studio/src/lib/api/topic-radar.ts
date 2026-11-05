import { http, unwrap } from '@/lib/http-client'
import type { TopicDigest, TopicItems, TopicScriptJob, TopicSubscription, TopicSuggestion, TopicSubscriptionSettings, TopicDigestDetail, TopicScriptArtifact, TopicNotificationChannel, TopicChannelSettings } from '@/types/topic-radar'

export const topicRadarApi = {
  updateSubscription: (id: string, data: Partial<TopicSubscriptionSettings>) => unwrap<TopicSubscription>(http.patch(`/topic-radar/subscriptions/${id}`, data)),
  deleteSubscription: (id: string) => unwrap<{ deleted: boolean }>(http.delete(`/topic-radar/subscriptions/${id}`)),
  digest: (id: string) => unwrap<TopicDigestDetail>(http.get(`/topic-radar/digests/${id}`)),
  retryDigest: (id: string) => unwrap<TopicDigest>(http.post(`/topic-radar/digests/${id}/retry`)),
  script: (id: string) => unwrap<TopicScriptJob>(http.get(`/topic-radar/scripts/${id}`)),
  retryScript: (id: string) => unwrap<TopicScriptJob>(http.post(`/topic-radar/scripts/${id}/retry`)),
  scriptArtifact: (id: string) => unwrap<TopicScriptArtifact>(http.get(`/topic-radar/scripts/${id}/artifact`)),
  channels: (projectId: string) => unwrap<TopicItems<TopicNotificationChannel>>(http.get(`/projects/${projectId}/topic-radar/notification-channels`)),
  createChannel: (projectId: string, data: TopicChannelSettings) => unwrap<TopicNotificationChannel>(http.post(`/projects/${projectId}/topic-radar/notification-channels`, data)),
  updateChannel: (id: string, data: Partial<TopicChannelSettings>) => unwrap<TopicNotificationChannel>(http.patch(`/topic-radar/notification-channels/${id}`, data)),
  deleteChannel: (id: string) => unwrap<{deleted: boolean}>(http.delete(`/topic-radar/notification-channels/${id}`)),
  testChannel: (id: string) => unwrap<{sent: boolean}>(http.post(`/topic-radar/notification-channels/${id}/test`)),
  createSubscription: (projectId: string, data: TopicSubscriptionSettings) =>
    unwrap<TopicSubscription>(http.post(`/projects/${projectId}/topic-radar/subscriptions`, data)),
  subscriptions: (projectId: string) =>
    unwrap<TopicItems<TopicSubscription>>(http.get(`/projects/${projectId}/topic-radar/subscriptions`)),
  digests: (subscriptionId: string) =>
    unwrap<TopicItems<TopicDigest>>(http.get(`/topic-radar/subscriptions/${subscriptionId}/digests`)),
  suggestions: (digestId: string) =>
    unwrap<TopicItems<TopicSuggestion>>(http.get(`/topic-radar/digests/${digestId}/suggestions`)),
  run: (subscriptionId: string, data: { request_key: string }) =>
    unwrap<TopicDigest>(http.post(`/topic-radar/subscriptions/${subscriptionId}/run`, data)),

}
