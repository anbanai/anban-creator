import { useState } from 'react'
import { Bell, Check, CheckCheck, X, Play, Pause } from 'lucide-react'
import { useNotificationStore, type Notification } from '@/stores/notification-store'
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Button } from '@/components/ui/Button'

function notificationIcon(type: Notification['type']) {
  switch (type) {
    case 'task_completed': return <Check className="h-4 w-4 text-emerald-400" />
    case 'task_failed': return <X className="h-4 w-4 text-red-400" />
    case 'plan_executed': return <Play className="h-4 w-4 text-primary" />
    case 'plan_paused': return <Pause className="h-4 w-4 text-amber-400" />
    default: return <Bell className="h-4 w-4 text-muted-foreground" />
  }
}

function formatTimeAgo(timestamp: string): string {
  const diff = Date.now() - new Date(timestamp).getTime()
  const minutes = Math.floor(diff / 60000)
  if (minutes < 1) return '刚刚'
  if (minutes < 60) return `${minutes}分钟前`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}小时前`
  const days = Math.floor(hours / 24)
  return `${days}天前`
}

export default function NotificationCenter() {
  const { notifications, markAsRead, markAllAsRead, unreadCount } = useNotificationStore()
  const [open, setOpen] = useState(false)
  const count = unreadCount()

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        render={
          <button className="relative flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors duration-150 hover:bg-sidebar-accent hover:text-sidebar-foreground" />
        }
      >
        <Bell className="h-4 w-4" />
        {count > 0 && (
          <span className="absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-medium text-primary-foreground">
            {count > 9 ? '9+' : count}
          </span>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-80 p-0">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <span className="text-sm font-medium text-foreground">通知</span>
          {count > 0 && (
            <Button
              variant="ghost"
              size="xs"
              onClick={markAllAsRead}
              className="text-muted-foreground"
            >
              <CheckCheck className="h-3.5 w-3.5" />
              全部已读
            </Button>
          )}
        </div>
        <div className="max-h-80 overflow-y-auto">
          {notifications.length === 0 ? (
            <div className="px-4 py-8 text-center text-sm text-muted-foreground">
              暂无通知
            </div>
          ) : (
            notifications.slice(0, 20).map((notification) => (
              <div
                key={notification.id}
                className={`flex gap-3 border-b border-border px-4 py-3 transition-colors duration-150 hover:bg-accent ${
                  !notification.read ? 'bg-primary/5' : ''
                }`}
                onClick={() => markAsRead(notification.id)}
              >
                <span className="mt-0.5 shrink-0">
                  {notificationIcon(notification.type)}
                </span>
                <div className="min-w-0 flex-1">
                  <p className={`text-sm ${!notification.read ? 'font-medium text-foreground' : 'text-foreground'}`}>
                    {notification.title}
                  </p>
                  <p className="mt-0.5 truncate text-xs text-muted-foreground">
                    {notification.message}
                  </p>
                  <p className="mt-1 text-[10px] text-muted-foreground">
                    {formatTimeAgo(notification.timestamp)}
                  </p>
                </div>
                {!notification.read && (
                  <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-primary" />
                )}
              </div>
            ))
          )}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
