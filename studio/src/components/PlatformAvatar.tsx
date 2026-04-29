import { renderPlatformIcon, platformBgColor } from '@/lib/PlatformIcon'

const sizeMap = {
  sm: 'h-8 w-8',
  md: 'h-10 w-10',
  lg: 'h-11 w-11',
} as const

interface PlatformAvatarProps {
  avatarUrl?: string
  name?: string
  platform: string
  size?: 'sm' | 'md' | 'lg'
}

export function PlatformAvatar({ avatarUrl, name, platform, size = 'md' }: PlatformAvatarProps) {
  if (avatarUrl) {
    return (
      <img
        src={avatarUrl}
        alt={name || ''}
        className={`${sizeMap[size]} shrink-0 rounded-full object-cover ring-1 ring-border`}
      />
    )
  }
  return (
    <div className={`flex shrink-0 items-center justify-center rounded-full ${sizeMap[size]} ${platformBgColor[platform] || 'bg-primary/10'}`}>
      {renderPlatformIcon(platform)}
    </div>
  )
}
