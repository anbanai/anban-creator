import { NavLink } from "react-router-dom";
import { useTheme } from "next-themes";
import { useEffect, useState } from "react";
import {
  Settings,
  Sun,
  Moon,
  Monitor,
  Menu,
  X,
  Workflow,
  PlugZap,
  BarChart3,
  ChevronLeft,
  ChevronRight,
} from "lucide-react";
import UserAccountPopover from "@/components/auth/UserAccountPopover";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { workflowItems, analyticsItems, platformItems } from "@/lib/navigation";

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

function useCollapsedState() {
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem('sidebar-collapsed') === 'true';
    } catch {
      return false;
    }
  });

  useEffect(() => {
    try {
      localStorage.setItem('sidebar-collapsed', String(collapsed));
    } catch {}
  }, [collapsed]);

  return [collapsed, setCollapsed] as const;
}

export default function Sidebar() {
  const [mobileOpen, setMobileOpen] = useState(false);
  const [collapsed, setCollapsed] = useCollapsedState();

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
        className={`fixed left-0 top-0 z-40 flex h-screen flex-col border-r border-sidebar-border bg-sidebar transition-all duration-200 md:static md:translate-x-0 ${
          collapsed ? "w-16" : "w-[220px]"
        } ${
          mobileOpen ? "translate-x-0" : "-translate-x-full"
        }`}
      >
        {/* Logo */}
        <div className={`flex h-14 items-center ${collapsed ? "justify-center px-0" : "px-5"}`}>
          {collapsed ? (
            <span className="text-sm font-bold text-sidebar-foreground">A</span>
          ) : (
            <span className="text-base font-bold tracking-tight text-sidebar-foreground">
              Anban 智能创作助手
            </span>
          )}
        </div>

        {/* Navigation */}
        <nav className="flex-1 overflow-y-auto px-3 pt-2" aria-label="主导航">
          <SidebarSection label="工作区" icon={Workflow} items={workflowItems} collapsed={collapsed} onSelect={() => setMobileOpen(false)} />
          <SidebarSection label="经营数据" icon={BarChart3} items={analyticsItems} collapsed={collapsed} onSelect={() => setMobileOpen(false)} />
        </nav>

        {/* Divider */}
        <div className="mx-3 border-t border-sidebar-border" />

        {/* Bottom: Platform + Settings */}
        <div className="px-3 py-2 space-y-0.5">
          <SidebarBottomSection collapsed={collapsed} onSelect={() => setMobileOpen(false)} />
        </div>

        {/* Bottom: Collapse toggle + User + Theme */}
        <div className={`flex items-center border-t border-sidebar-border py-3 ${
          collapsed ? "flex-col gap-2 px-0" : "flex-row gap-1 px-3"
        }`}>
          <Button
            variant="ghost"
            size="icon-xs"
            className="hidden md:flex text-muted-foreground hover:text-sidebar-foreground"
            onClick={() => setCollapsed(!collapsed)}
            aria-label={collapsed ? "展开侧边栏" : "收起侧边栏"}
          >
            {collapsed ? <ChevronRight className="h-4 w-4" /> : <ChevronLeft className="h-4 w-4" />}
          </Button>
          <UserAccountPopover collapsed={collapsed} />
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
  collapsed,
  onSelect,
}: {
  label: string
  icon: typeof Workflow
  items: Array<{ to: string; label: string; icon: typeof Workflow; end?: boolean }>
  collapsed: boolean
  onSelect: () => void
}) {
  if (collapsed) {
    return (
      <div className="mb-4">
        <Tooltip>
          <TooltipTrigger
            render={
              <div className="flex items-center justify-center py-2 cursor-pointer" />
            }
          >
            <Icon className="h-4 w-4 text-muted-foreground/70" />
          </TooltipTrigger>
          <TooltipContent side="right">{label}</TooltipContent>
        </Tooltip>
        <div className="space-y-0.5">
          {items.map((item) => (
            <SidebarNavLink key={item.to} item={item} collapsed={collapsed} onClick={onSelect} />
          ))}
        </div>
      </div>
    );
  }

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
          <SidebarNavLink key={item.to} item={item} collapsed={collapsed} onClick={onSelect} />
        ))}
      </div>
    </div>
  );
}

function SidebarBottomSection({
  collapsed,
  onSelect,
}: {
  collapsed: boolean
  onSelect: () => void
}) {
  const allItems = [
    ...platformItems,
    { to: "/settings", label: "设置", icon: Settings },
  ];

  if (collapsed) {
    return (
      <div className="space-y-0.5">
        {allItems.map((item) => (
          <SidebarNavLink key={item.to} item={item} collapsed={collapsed} onClick={onSelect} />
        ))}
      </div>
    );
  }

  return (
    <>
      <div className="px-3 py-2 text-[11px] font-medium uppercase tracking-[0.12em] text-muted-foreground/70">
        <span className="inline-flex items-center gap-2">
          <PlugZap className="h-3.5 w-3.5" />
          接入配置
        </span>
      </div>
      {allItems.map((item) => (
        <SidebarNavLink key={item.to} item={item} collapsed={collapsed} onClick={onSelect} />
      ))}
    </>
  );
}

function SidebarNavLink({
  item,
  collapsed,
  onClick,
}: {
  item: { to: string; label: string; icon: typeof Workflow; end?: boolean }
  collapsed: boolean
  onClick: () => void
}) {
  const link = (
    <NavLink
      to={item.to}
      end={item.end}
      onClick={onClick}
      className={({ isActive }) =>
        `flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors duration-150 ${
          isActive
            ? "border-l-2 border-primary bg-sidebar-accent text-sidebar-foreground -ml-[2px] pl-[calc(0.75rem+2px)]"
            : "text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-foreground"
        } ${collapsed ? "justify-center px-0" : ""}`
      }
    >
      <item.icon className="h-4 w-4 shrink-0" />
      {!collapsed && item.label}
    </NavLink>
  );

  if (collapsed) {
    return (
      <Tooltip>
        <TooltipTrigger render={<span />}>{link}</TooltipTrigger>
        <TooltipContent side="right">{item.label}</TooltipContent>
      </Tooltip>
    );
  }

  return link;
}
