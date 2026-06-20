import { http, unwrap } from '@/lib/http-client'
import type {
  Channel,
  ChannelDetail,
  ChannelStats,
  CreateChannelRequest,
  CreateChannelResponse,
  PlatformConfig,
  PlatformProfile,
} from '@/types'

export const channelsApi = {
  list: (params?: { status?: string; platform?: string }) =>
    unwrap<Channel[]>(http.get('/channels', { params })),

  get: (id: string) =>
    unwrap<ChannelDetail>(http.get(`/channels/${id}`)),

  stats: (ids: string[]) =>
    unwrap<Record<string, ChannelStats>>(http.get('/channels/stats', {
      params: {
        ids: ids.join(','),
      },
    })),

  create: (data: CreateChannelRequest) =>
    unwrap<CreateChannelResponse>(http.post('/channels', data)),

  update: (id: string, data: Partial<CreateChannelRequest>) =>
    unwrap<Channel>(http.put(`/channels/${id}`, data)),

  archive: (id: string) =>
    unwrap<void>(http.patch(`/channels/${id}/archive`)),

  restore: (id: string) =>
    unwrap<void>(http.patch(`/channels/${id}/restore`)),

  delete: (id: string) =>
    unwrap<void>(http.delete(`/channels/${id}`)),

  platformConfigs: () =>
    unwrap<PlatformConfig[]>(http.get('/channels/platform-configs')),

  fetchProfile: (platform: string, profileUrl: string, wechatAppId?: string, wechatSecret?: string) =>
    unwrap<PlatformProfile>(http.post('/channels/fetch-profile', {
      platform,
      profile_url: profileUrl,
      wechat_app_id: wechatAppId,
      wechat_secret: wechatSecret,
    }, { timeout: 120000 })),

  analyzeImage: (imageUrl: string) =>
    unwrap<{ style: string }>(http.post('/channels/analyze-image', {
      image_url: imageUrl,
    }, { timeout: 120000 })),
}
