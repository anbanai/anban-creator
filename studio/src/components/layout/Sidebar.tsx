import { NavLink } from "react-router-dom";
import { useTheme } from "next-themes";
import { useEffect, useState } from "react";
import {
  LayoutDashboard,
  Rss,
  CalendarRange,
  ListChecks,
  Clock,
  Settings,
  Sun,
  Moon,
  Monitor,
  Coins,
  Menu,
  X,
  Activity,
  Terminal,
  Puzzle,
  Workflow,
  PlugZap,
  BarChart3,
} from "lucide-react";
import UserAccountPopover from "@/components/auth/UserAccountPopover";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

const workflowItems = [
  { to: "/", label: "仪表盘", icon: LayoutDashboard, end: true },
  { to: "/channels", label: "账号", icon: Rss },
  { to: "/plans", label: "计划", icon: CalendarRange },
  { to: "/tasks", label: "任务", icon: ListChecks },
  { to: "/timeline", label: "时间轴", icon: Clock },
];

const analyticsItems = [
  { to: "/credits", label: "积分", icon: Coins },
  { to: "/usage", label: "用量", icon: Activity },
];

const platformItems = [
  { to: "/connect/claude-code", label: "Claude Code", icon: Terminal },
  { to: "/connect/openclaw", label: "OpenClaw", icon: Puzzle },
];

const themeOptions = [
  { value: "light", label: "亮色", icon: Sun },
  { value: "dark", label: "暗色", icon: Moon },
  { value: "system", label: "系统", icon: Monitor },
] as const;

type ThemePreference = (typeof themeOptions)[number]["value"];

function isThemePreference(value: string): value is ThemePreference {
  return themeOptions.some((option) => option.value === value);
}

function ThemeToggle() {
  const { theme, setTheme } = useTheme();
  const [mounted, setMounted] = useState(false);

  useEffect(() => setMounted(true), []);

  if (!mounted) {
    return <div className="h-8 w-8" />;
  }

  const currentTheme = isThemePreference(theme ?? "") ? theme : "system";
  const currentOption = themeOptions.find((option) => option.value === currentTheme) ?? themeOptions[2];
  const CurrentIcon = currentOption.icon;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            className="flex h-8 w-8 items-center justify-center rounded-md text-muted-foreground transition-colors duration-150 hover:bg-sidebar-accent hover:text-sidebar-foreground"
            title={`主题模式：${currentOption.label}`}
            aria-label={`主题模式：${currentOption.label}`}
          />
        }
      >
        <CurrentIcon className="h-4 w-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" side="top" className="w-32">
        <DropdownMenuRadioGroup
          value={currentTheme}
          onValueChange={(value) => {
            if (isThemePreference(value)) {
              setTheme(value);
            }
          }}
        >
          {themeOptions.map((option) => (
            <DropdownMenuRadioItem key={option.value} value={option.value}>
              <option.icon className="h-4 w-4" />
              {option.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export default function Sidebar() {
  const [mobileOpen, setMobileOpen] = useState(false);

  useEffect(() => {
    if (!mobileOpen) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setMobileOpen(false);
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [mobileOpen]);

  return (
    <>
      {/* Mobile hamburger */}
      <button
        onClick={() => setMobileOpen(true)}
        className="fixed left-4 top-4 z-50 flex h-10 w-10 items-center justify-center rounded-lg bg-sidebar text-sidebar-foreground shadow-lg md:hidden"
        aria-label="打开菜单"
      >
        <Menu className="h-5 w-5" />
      </button>

      {/* Mobile overlay */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm md:hidden"
          onClick={() => setMobileOpen(false)}
        />
      )}

      {/* Mobile close button */}
      {mobileOpen && (
        <button
          onClick={() => setMobileOpen(false)}
          className="fixed left-[196px] top-4 z-50 flex h-8 w-8 items-center justify-center rounded-md bg-sidebar text-muted-foreground transition-colors hover:text-sidebar-foreground md:hidden"
          aria-label="关闭菜单"
        >
          <X className="h-4 w-4" />
        </button>
      )}

      {/* Sidebar */}
      <aside
        className={`fixed left-0 top-0 z-40 flex h-screen w-[220px] flex-col border-r border-sidebar-border bg-sidebar transition-transform duration-200 md:static md:translate-x-0 ${
          mobileOpen ? "translate-x-0" : "-translate-x-full"
        }`}
      >
        {/* Logo */}
        <div className="flex h-14 items-center px-5">
          <span className="text-base font-bold tracking-tight text-sidebar-foreground">
            Anban 智能创作助手
          </span>
        </div>

        {/* Navigation */}
        <nav className="flex-1 overflow-y-auto px-3 pt-2">
          <SidebarSection label="工作区" icon={Workflow} items={workflowItems} onSelect={() => setMobileOpen(false)} />
          <SidebarSection label="经营数据" icon={BarChart3} items={analyticsItems} onSelect={() => setMobileOpen(false)} />
        </nav>

        {/* Divider */}
        <div className="mx-3 border-t border-sidebar-border" />

        {/* Bottom: Platform + Settings */}
        <div className="px-3 py-2 space-y-0.5">
          <div className="px-3 py-2 text-[11px] font-medium uppercase tracking-[0.12em] text-muted-foreground/70">
            <span className="inline-flex items-center gap-2">
              <PlugZap className="h-3.5 w-3.5" />
              接入配置
            </span>
          </div>
          {platformItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              onClick={() => setMobileOpen(false)}
              className={({ isActive }) =>
                `flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors duration-150 ${
                  isActive
                    ? "border-l-2 border-primary bg-sidebar-accent text-sidebar-foreground -ml-[2px] pl-[calc(0.75rem+2px)]"
                    : "text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-foreground"
                }`
              }
            >
              <item.icon className="h-4 w-4 shrink-0" />
              {item.label}
            </NavLink>
          ))}
          <NavLink
            to="/settings"
            className={({ isActive }) =>
              `flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors duration-150 ${
                isActive
                  ? "border-l-2 border-primary bg-sidebar-accent text-sidebar-foreground -ml-[2px] pl-[calc(0.75rem+2px)]"
                  : "text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-foreground"
              }`
            }
          >
            <Settings className="h-4 w-4 shrink-0" />
            设置
          </NavLink>
        </div>

        {/* Bottom: Notifications + User + Theme */}
        <div className="flex items-center gap-1 border-t border-sidebar-border px-3 py-3">
          <UserAccountPopover />
          <ThemeToggle />
        </div>
      </aside>
    </>
  );
}

function SidebarSection({
  label,
  icon: Icon,
  items,
  onSelect,
}: {
  label: string
  icon: typeof Workflow
  items: Array<{ to: string; label: string; icon: typeof Workflow; end?: boolean }>
  onSelect: () => void
}) {
  return (
    <div className="mb-4">
      <div className="px-3 py-2 text-[11px] font-medium uppercase tracking-[0.12em] text-muted-foreground/70">
        <span className="inline-flex items-center gap-2">
          <Icon className="h-3.5 w-3.5" />
          {label}
        </span>
      </div>
      <div className="space-y-0.5">
        {items.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
            onClick={onSelect}
            className={({ isActive }) =>
              `flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors duration-150 ${
                isActive
                  ? "border-l-2 border-primary bg-sidebar-accent text-sidebar-foreground -ml-[2px] pl-[calc(0.75rem+2px)]"
                  : "text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-foreground"
              }`
            }
          >
            <item.icon className="h-4 w-4 shrink-0" />
            {item.label}
          </NavLink>
        ))}
      </div>
    </div>
  )
}
