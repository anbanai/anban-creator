import { get, post, put, patch, del } from './request'
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
    get<Channel[]>('/channels', params as Record<string, any>),

  get: (id: string) =>
    get<ChannelDetail>(`/channels/${id}`),

  stats: (ids: string[]) =>
    get<Record<string, ChannelStats>>('/channels/stats', { ids: ids.join(',') }),

  create: (data: CreateChannelRequest) =>
    post<CreateChannelResponse>('/channels', data),

  update: (id: string, data: Partial<CreateChannelRequest>) =>
    put<Channel>(`/channels/${id}`, data),

  archive: (id: string) =>
    patch<void>(`/channels/${id}/archive`),

  restore: (id: string) =>
    patch<void>(`/channels/${id}/restore`),

  delete: (id: string) =>
    del<void>(`/channels/${id}`),

  platformConfigs: () =>
    get<PlatformConfig[]>('/channels/platform-configs'),

  /** Fetch profile info from a platform URL (e.g. WeChat MP homepage) */
  fetchProfile: (
    platform: string,
    profileUrl: string,
    wechatAppId?: string,
    wechatSecret?: string,
  ) =>
    post<PlatformProfile>(
      '/channels/fetch-profile',
      { platform, profile_url: profileUrl, wechat_app_id: wechatAppId, wechat_secret: wechatSecret },
      120000, // 2 min timeout — external fetch can be slow
    ),
}
