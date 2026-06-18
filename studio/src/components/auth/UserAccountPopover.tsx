import { useTheme } from 'next-themes'
import { Sun, Moon, Monitor, LogOut } from 'lucide-react'
import { useAuth } from '@/contexts/AuthContext'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuGroup,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
} from '@/components/ui/dropdown-menu'
import { Badge } from '@/components/ui/badge'
import { tierLabels } from '@/lib/labels'

const themeOptions = [
  { value: 'light', label: '亮色模式', icon: Sun },
  { value: 'dark', label: '暗色模式', icon: Moon },
  { value: 'system', label: '跟随系统', icon: Monitor },
] as const

const tierBadgeVariant: Record<string, 'secondary' | 'outline'> = {
  free: 'secondary',
  pro: 'outline',
  enterprise: 'secondary',
}

export default function UserAccountPopover({ collapsed }: { collapsed?: boolean }) {
  const { user, logout } = useAuth()
  const { theme, setTheme } = useTheme()

  if (!user) return null

  const initials = user.nickname
    ? user.nickname.slice(0, 1).toUpperCase()
    : user.email.slice(0, 1).toUpperCase()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className={`flex items-center rounded-lg text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground outline-none ${
          collapsed ? "justify-center p-2" : "gap-2 px-3 py-1.5"
        }`}
      >
        {user.avatar ? (
          <img
            src={user.avatar}
            alt={user.nickname}
            className="h-7 w-7 rounded-full object-cover"
          />
        ) : (
          <span className="flex h-7 w-7 items-center justify-center rounded-full bg-blue-600 text-xs font-medium text-white">
            {initials}
          </span>
        )}
        {!collapsed && <span className="hidden sm:inline max-w-[120px] truncate">{user.nickname || user.email}</span>}
      </DropdownMenuTrigger>

      <DropdownMenuContent
        side={collapsed ? "right" : "top"}
        align={collapsed ? "end" : "start"}
        className="w-56"
      >
        <DropdownMenuGroup>
          <DropdownMenuLabel>
            <div className="flex items-center justify-between gap-2">
              <p className="min-w-0 truncate text-sm font-medium text-popover-foreground">{user.nickname || '用户'}</p>
              <Badge variant={tierBadgeVariant[user.tier] ?? 'secondary'} className="shrink-0">
                {tierLabels[user.tier] ?? '免费版'}
              </Badge>
            </div>
            <p className="truncate text-xs font-normal text-muted-foreground">{user.email}</p>
          </DropdownMenuLabel>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuRadioGroup
          value={theme || 'system'}
          onValueChange={setTheme}
        >
          <DropdownMenuLabel>主题模式</DropdownMenuLabel>
          {themeOptions.map((option) => (
            <DropdownMenuRadioItem
              key={option.value}
              value={option.value}
            >
              <option.icon className="h-4 w-4" />
              {option.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          variant="destructive"
          onClick={() => void logout()}
        >
          <LogOut className="h-4 w-4" />
          退出登录
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
