export const montageSubtitleOptions = [
  { value: '', label: '智能决定' },
  { value: 'burned_in', label: '烧录字幕' },
  { value: 'none', label: '无字幕' },
]

export const montageVoiceoverOptions = [
  { value: '', label: '智能决定' },
  { value: 'original', label: '保留原声' },
  { value: 'narrated', label: '生成旁白' },
  { value: 'none', label: '无配音' },
]

export function includesMontageOption(options: Array<{ value: string }>, value: string | undefined): boolean {
  return options.some((option) => option.value === (value ?? ''))
}

export function montageDurationError(duration: number | undefined, maxDurationSeconds: number): string {
  if (duration === undefined) return ''
  if (!Number.isInteger(duration) || duration < 1) return '目标时长必须是大于 0 的整数秒'
  if (maxDurationSeconds > 0 && duration > maxDurationSeconds) return `目标时长不能超过 ${maxDurationSeconds} 秒`
  return ''
}
