import { useAuth } from '@/contexts/AuthContext'
import { Card, CardBody } from '@/components/ui/Card'
import PageHeader from '@/components/layout/PageHeader'

export default function SettingsPage() {
  const { user } = useAuth()

  return (
    <div className="space-y-6">
      <PageHeader title="设置" description="管理你的账号和偏好设置。" />

      <Card>
        <div className="border-b border-border px-4 py-3">
          <h2 className="text-sm font-semibold text-foreground">个人资料</h2>
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
