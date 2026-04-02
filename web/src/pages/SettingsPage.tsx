export default function SettingsPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-100">Settings</h1>
        <p className="mt-1 text-sm text-gray-400">Manage your account and preferences.</p>
      </div>

      <div className="space-y-4">
        {/* Profile section */}
        <div className="rounded-xl border border-gray-700 bg-gray-800 p-4">
          <h2 className="mb-3 text-lg font-semibold text-gray-100">Profile</h2>
          <div className="space-y-3">
            <div>
              <label className="mb-1 block text-sm font-medium text-gray-300">Email</label>
              <p className="text-sm text-gray-400">Configure your email address</p>
            </div>
            <div>
              <label className="mb-1 block text-sm font-medium text-gray-300">Nickname</label>
              <p className="text-sm text-gray-400">Set your display name</p>
            </div>
          </div>
        </div>

        {/* API Keys section */}
        <div className="rounded-xl border border-gray-700 bg-gray-800 p-4">
          <h2 className="mb-3 text-lg font-semibold text-gray-100">API Keys</h2>
          <div className="space-y-3">
            <div>
              <label className="mb-1 block text-sm font-medium text-gray-300">WeChat AppID</label>
              <p className="text-sm text-gray-400">Configure your WeChat official account</p>
            </div>
            <div>
              <label className="mb-1 block text-sm font-medium text-gray-300">AI Provider</label>
              <p className="text-sm text-gray-400">Select and configure AI service provider</p>
            </div>
          </div>
        </div>

        {/* Danger zone */}
        <div className="rounded-xl border border-red-900/50 bg-gray-800 p-4">
          <h2 className="mb-3 text-lg font-semibold text-red-400">Danger Zone</h2>
          <p className="text-sm text-gray-400">
            Account deletion and other irreversible actions will be available here.
          </p>
        </div>
      </div>
    </div>
  )
}
