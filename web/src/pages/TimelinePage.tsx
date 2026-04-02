export default function TimelinePage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-100">Timeline</h1>
        <p className="mt-1 text-sm text-gray-400">Your scheduled content calendar.</p>
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
            d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"
          />
        </svg>
        <p className="text-sm text-gray-400">No scheduled content yet</p>
        <p className="mt-1 text-xs text-gray-500">Create a plan and schedule tasks to see them here.</p>
      </div>
    </div>
  )
}
