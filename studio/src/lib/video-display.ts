import type { VideoModelSpec } from '@/types'

const knownVideoModelNames: Record<string, string> = {
  'seedance-2.0-mini': '豆包 Seedance 2.0 Mini（轻量）',
  'seedance-2.0-fast': '豆包 Seedance 2.0 Fast（快速）',
  'seedance-2.0': '豆包 Seedance 2.0（标准）',
  'seedance-1.0-lite': '豆包 Seedance 1.0 Lite（轻量）',
  'seedance-1.0-pro': '豆包 Seedance 1.0 Pro（专业）',
  'wan2.1-t2v-plus': '通义万相 2.1 Plus',
  'wan2.1-i2v-plus': '通义万相图生视频 2.1 Plus',
  'kling-v1-6': '可灵 1.6',
  'kling-v2-1': '可灵 2.1',
}

export function videoModelDisplayName(model: Pick<VideoModelSpec, 'key' | 'display_name'> | string | null | undefined): string {
  if (!model) return ''
  if (typeof model === 'string') return knownVideoModelNames[model] ?? model
  return model.display_name?.trim() || knownVideoModelNames[model.key] || model.key
}

export const videoReferenceRoles = [
  { value: 'subject identity', label: '主体不变' },
  { value: 'product appearance', label: '产品外观' },
  { value: 'scene background', label: '场景背景' },
  { value: 'first frame', label: '首帧参考' },
  { value: 'last frame', label: '尾帧参考' },
  { value: 'action', label: '动作参考' },
  { value: 'camera movement', label: '镜头运动' },
  { value: 'rhythm', label: '节奏参考' },
  { value: 'voice tone', label: '声音/BGM' },
]

export function videoReferenceRoleLabel(value: string | null | undefined) {
  return videoReferenceRoles.find((role) => role.value === value)?.label || '主体不变'
}
