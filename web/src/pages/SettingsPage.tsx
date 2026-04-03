import { useAuth } from '@/contexts/AuthContext'
import { Card, CardBody } from '@/components/ui/Card'

export default function SettingsPage() {
  const { user } = useAuth()

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-100">Settings</h1>
        <p className="mt-1 text-sm text-gray-400">Manage your account and content preferences.</p>
      </div>

      {/* Profile Section */}
      <Card>
        <div className="border-b border-gray-700 px-4 py-3">
          <h2 className="text-base font-semibold text-gray-100">Profile</h2>
        </div>
        <CardBody className="space-y-3">
          <div>
            <p className="text-xs text-gray-500">Email</p>
            <p className="text-sm text-gray-200">{user?.email || '--'}</p>
          </div>
          <div>
            <p className="text-xs text-gray-500">Nickname</p>
            <p className="text-sm text-gray-200">{user?.nickname || '--'}</p>
          </div>
          <div>
            <p className="text-xs text-gray-500">Member Since</p>
            <p className="text-sm text-gray-200">
              {user?.created_at
                ? new Date(user.created_at).toLocaleDateString('en-US', {
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
