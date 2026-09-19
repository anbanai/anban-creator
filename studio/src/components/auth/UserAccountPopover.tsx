import { useTheme } from 'next-themes'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { CreditCard, LogOut, Monitor, Moon, ReceiptText, RefreshCw, Sun, TriangleAlert } from 'lucide-react'
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
import { Skeleton } from '@/components/ui/skeleton'
import { api } from '@/lib/api'
import { tierLabels } from '@/lib/labels'
import { queryKeys } from '@/lib/query-keys'
import { cn } from '@/lib/utils'

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
  const walletQuery = useQuery({
    queryKey: queryKeys.billing.wallet,
    queryFn: () => api.billing.wallet(),
    enabled: Boolean(user),
    staleTime: 30_000,
  })

  if (!user) return null

  const initials = user.nickname
    ? user.nickname.slice(0, 1).toUpperCase()
    : user.email.slice(0, 1).toUpperCase()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={`账户菜单：${user.nickname || user.email}`}
        className={cn(
          'flex items-center rounded-lg text-sm text-muted-foreground outline-none transition-colors hover:bg-accent hover:text-foreground',
          collapsed ? 'justify-center p-2' : 'gap-2 px-3 py-1.5',
        )}
      >
        {user.avatar ? (
          <img
            src={user.avatar}
            alt={user.nickname}
            className="size-7 rounded-full object-cover"
          />
        ) : (
          <span className="flex size-7 items-center justify-center rounded-full bg-primary text-xs font-medium text-primary-foreground">
            {initials}
          </span>
        )}
        {!collapsed && <span className="max-w-[150px] truncate">{user.nickname || user.email}</span>}
      </DropdownMenuTrigger>

      <DropdownMenuContent
        side={collapsed ? "right" : "top"}
        align={collapsed ? "end" : "start"}
        className="w-64"
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
        <DropdownMenuGroup>
          <DropdownMenuLabel>积分账户</DropdownMenuLabel>
          {walletQuery.isLoading ? (
            <div className="grid grid-cols-2 gap-2 px-1.5 pb-1" aria-label="正在加载钱包">
              {[0, 1, 2, 3].map((index) => <Skeleton key={index} className="h-10 w-full" />)}
            </div>
          ) : walletQuery.isError ? (
            <>
              <div className="px-1.5 py-1 text-xs text-muted-foreground">钱包加载失败</div>
              <DropdownMenuItem closeOnClick={false} onClick={() => void walletQuery.refetch()}>
                <RefreshCw />
                重试钱包
              </DropdownMenuItem>
            </>
          ) : walletQuery.data ? (
            <>
              <div className="grid grid-cols-2 gap-x-3 gap-y-2 px-1.5 pb-1.5">
                <WalletValue label="可用余额" value={walletQuery.data.balance} emphasized />
                <WalletValue label="现金积分" value={walletQuery.data.paid} />
                <WalletValue label="奖励积分" value={walletQuery.data.promotional} />
                <WalletValue label="待补欠费" value={walletQuery.data.debt} debt={walletQuery.data.debt > 0} />
              </div>
              {walletQuery.data.debt > 0 ? (
                <div className="mx-1.5 mb-1.5 flex items-start gap-1.5 rounded-md bg-destructive/10 px-2 py-1.5 text-xs text-destructive" role="alert">
                  <TriangleAlert className="mt-0.5 shrink-0" />
                  <span>当前欠费 {walletQuery.data.debt.toLocaleString()} 积分，充值后优先补齐。</span>
                </div>
              ) : null}
              <DropdownMenuItem render={<Link to="/billing" />}>
                <ReceiptText />
                账单明细
              </DropdownMenuItem>
              <DropdownMenuItem render={<Link to="/billing" />}>
                <CreditCard />
                充值
              </DropdownMenuItem>
            </>
          ) : null}
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
              <option.icon />
              {option.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          variant="destructive"
          onClick={() => void logout()}
        >
          <LogOut />
          退出登录
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function WalletValue({
  label,
  value,
  emphasized = false,
  debt = false,
}: {
  label: string
  value: number
  emphasized?: boolean
  debt?: boolean
}) {
  return (
    <div className="min-w-0">
      <p className="text-[11px] font-normal text-muted-foreground">{label}</p>
      <p className={cn(
        'truncate text-sm text-foreground',
        emphasized ? 'font-semibold' : 'font-medium',
        debt && 'text-destructive',
      )}>
        {value.toLocaleString()}
      </p>
    </div>
  )
}
