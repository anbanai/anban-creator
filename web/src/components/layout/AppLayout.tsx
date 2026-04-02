import { NavLink, Outlet } from 'react-router-dom'
import UserAccountPopover from '@/components/auth/UserAccountPopover'

const navItems = [
  { to: '/', label: 'Dashboard', end: true },
  { to: '/timeline', label: 'Timeline' },
  { to: '/plans', label: 'Plans' },
  { to: '/tasks', label: 'Tasks' },
  { to: '/settings', label: 'Settings' },
]

export default function AppLayout() {
  return (
    <div className="flex h-screen flex-col bg-gray-900 text-gray-100">
      {/* Top navigation bar */}
      <header className="flex h-14 shrink-0 items-center justify-between border-b border-gray-800 bg-gray-900 px-4">
        <div className="flex items-center gap-6">
          <span className="text-lg font-bold text-blue-400">AnbanWriter</span>
          <nav className="hidden sm:flex items-center gap-1">
            {navItems.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  `rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
                    isActive
                      ? 'bg-gray-800 text-white'
                      : 'text-gray-400 hover:bg-gray-800 hover:text-gray-200'
                  }`
                }
              >
                {item.label}
              </NavLink>
            ))}
          </nav>
        </div>
        <UserAccountPopover />
      </header>

      {/* Mobile navigation */}
      <nav className="flex shrink-0 items-center gap-1 overflow-x-auto border-b border-gray-800 bg-gray-900 px-4 sm:hidden">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
            className={({ isActive }) =>
              `whitespace-nowrap rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                isActive
                  ? 'bg-gray-800 text-white'
                  : 'text-gray-400 hover:bg-gray-800 hover:text-gray-200'
              }`
            }
          >
            {item.label}
          </NavLink>
        ))}
      </nav>

      {/* Main content area */}
      <main className="flex-1 overflow-auto p-4 md:p-6">
        <Outlet />
      </main>
    </div>
  )
}
