export default function PlansPage() {
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-100">Plans</h1>
          <p className="mt-1 text-sm text-gray-400">Manage your content plans.</p>
        </div>
        <button className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-blue-700">
          New Plan
        </button>
      </div>

      <div className="flex flex-col items-center justify-center rounded-xl border border-gray-700 bg-gray-800 py-16">
        <svg
          className="mb-4 h-12 w-12 text-gray-600"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={1.5}
            d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
          />
        </svg>
        <p className="text-sm text-gray-400">No plans yet</p>
        <p className="mt-1 text-xs text-gray-500">Create your first content plan to get started.</p>
      </div>
    </div>
  )
}
