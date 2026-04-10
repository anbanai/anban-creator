import { useAuth } from '@/contexts/AuthContext'
import { Card, CardBody } from '@/components/ui/Card'

export default function SettingsPage() {
  const { user } = useAuth()

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-foreground">设置</h1>
        <p className="mt-1 text-sm text-muted-foreground">管理你的账号和偏好设置。</p>
      </div>

      {/* Profile Section */}
      <Card>
        <div className="border-b border-border px-4 py-3">
          <h2 className="text-base font-semibold text-foreground">个人资料</h2>
        </div>
        <CardBody className="space-y-3">
          <div>
            <p className="text-xs text-muted-foreground">邮箱</p>
            <p className="text-sm text-foreground">{user?.email || '--'}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">昵称</p>
            <p className="text-sm text-foreground">{user?.nickname || '--'}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">注册时间</p>
            <p className="text-sm text-foreground">
              {user?.created_at
                ? new Date(user.created_at).toLocaleDateString('zh-CN', {
                    year: 'numeric',
                    month: 'long',
                    day: 'numeric',
                  })
                : '--'}
            </p>
          </div>
        </CardBody>
      </Card>
    </div>
  )
}
