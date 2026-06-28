import { NavLink } from "react-router-dom";
import { useEffect, useState } from "react";
import {
  Settings,
  Menu,
  X,
  Workflow,
  PlugZap,
  BarChart3,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
} from "lucide-react";
import UserAccountPopover from "@/components/auth/UserAccountPopover";
import LocalExecutorStatusPill from "@/components/desktop/LocalExecutorStatusPill";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { isDesktop } from "@/lib/tauri";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { workflowItems, analyticsItems, platformItems } from "@/lib/navigation";
import {
  commandPaletteStore,
  commandPaletteAccelerator,
} from "@/lib/command-palette";

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
          className="fixed left-[196px] top-4 z-50 flex h-8 w-8 items-center justify-center rounded-md bg-sidebar text-sidebar-foreground/75 transition-colors hover:text-sidebar-foreground md:hidden"
          aria-label="关闭菜单"
        >
          <X className="h-4 w-4" />
        </button>
      )}

      {/* Sidebar */}
      <aside
        className={`fixed left-0 top-0 z-40 flex h-dvh flex-col border-r border-sidebar-border bg-sidebar transition-all duration-200 md:static md:translate-x-0 ${
          collapsed ? "w-16" : "w-[220px]"
        } ${
          mobileOpen ? "translate-x-0" : "-translate-x-full"
        }`}
      >
        {/* Top: Logo + Collapse toggle (ChatGPT-style) */}
        {collapsed ? (
          <>
            <button
              type="button"
              onClick={() => setCollapsed(false)}
              aria-expanded={false}
              aria-label="展开侧边栏"
              className="group relative hidden h-14 w-full items-center justify-center rounded-md transition-colors hover:bg-sidebar-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset md:flex"
            >
              <span className="text-sm font-bold text-sidebar-foreground transition-opacity duration-150 group-hover:opacity-0 group-focus-visible:opacity-0">
                A
              </span>
              <span className="pointer-events-none absolute inset-0 flex items-center justify-center opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-visible:opacity-100">
                <PanelLeftOpen className="h-4 w-4 text-sidebar-foreground" />
              </span>
            </button>
            <div className="flex h-14 items-center justify-center md:hidden">
              <span className="text-sm font-bold text-sidebar-foreground">A</span>
            </div>
          </>
        ) : (
          <div className="flex h-14 items-center justify-between gap-2 px-5">
            <span className="truncate text-base font-bold tracking-tight text-sidebar-foreground">
              Anban 智能创作助手
            </span>
            <button
              type="button"
              onClick={() => setCollapsed(true)}
              aria-expanded={true}
              aria-label="收起侧边栏"
              className={cn(
                buttonVariants({ variant: "ghost", size: "icon" }),
                "hidden shrink-0 md:flex"
              )}
            >
              <PanelLeftClose />
            </button>
          </div>
        )}

        {/* Command palette trigger — the ⌘K palette is otherwise invisible. */}
        <CommandPaletteTrigger collapsed={collapsed} onSelect={() => setMobileOpen(false)} />

        {/* Navigation */}
        <nav className="flex-1 overflow-y-auto px-3 pt-2" aria-label="主导航">
          <SidebarSection label="工作区" icon={Workflow} items={workflowItems} collapsed={collapsed} onSelect={() => setMobileOpen(false)} />
          <SidebarSection label="经营数据" icon={BarChart3} items={analyticsItems} collapsed={collapsed} onSelect={() => setMobileOpen(false)} />
        </nav>

        {/* Divider */}
        <div className="mx-3 border-t border-sidebar-border" />

        {/* Bottom: Platform + Settings */}
        <div className="px-3 py-2 space-y-0.5">
          {isDesktop() && <LocalExecutorStatusPill collapsed={collapsed} />}
          <SidebarBottomSection collapsed={collapsed} onSelect={() => setMobileOpen(false)} />
        </div>

        {/* Bottom: User account */}
        <div className={`border-t border-sidebar-border py-3 ${collapsed ? "flex justify-center px-0" : "px-3"}`}>
          <UserAccountPopover collapsed={collapsed} />
        </div>
      </aside>
    </>
  );
}

function CommandPaletteTrigger({
  collapsed,
  onSelect,
}: {
  collapsed: boolean
  onSelect: () => void
}) {
  const openPalette = () => {
    commandPaletteStore.open()
    onSelect()
  }

  if (collapsed) {
    return (
      <div className="px-3 pt-2 pb-1">
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                onClick={openPalette}
                aria-label={`搜索 (${commandPaletteAccelerator})`}
                className="flex w-full items-center justify-center rounded-md py-2 text-sidebar-foreground/70 transition-colors hover:bg-sidebar-accent hover:text-sidebar-foreground"
              />
            }
          >
            <Search className="h-4 w-4" />
          </TooltipTrigger>
          <TooltipContent side="right">搜索 ({commandPaletteAccelerator})</TooltipContent>
        </Tooltip>
      </div>
    )
  }

  return (
    <div className="px-3 pt-2 pb-1">
      <button
        type="button"
        onClick={openPalette}
        className="group flex w-full items-center gap-2 rounded-md border border-sidebar-border bg-sidebar-accent/40 px-3 py-2 text-sm text-sidebar-foreground/55 transition-colors hover:bg-sidebar-accent hover:text-sidebar-foreground/80"
      >
        <Search className="h-4 w-4 shrink-0" />
        <span className="flex-1 text-left">搜索…</span>
        <kbd className="pointer-events-none rounded border border-sidebar-border bg-sidebar px-1.5 py-0.5 text-[10px] font-medium text-sidebar-foreground/60">
          {commandPaletteAccelerator}
        </kbd>
      </button>
    </div>
  )
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
            <Icon className="h-4 w-4 text-sidebar-foreground/65" />
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
      <div className="px-3 py-2 text-[11px] font-medium uppercase tracking-[0.12em] text-sidebar-foreground/62">
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
      <div className="px-3 py-2 text-[11px] font-medium uppercase tracking-[0.12em] text-sidebar-foreground/62">
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
            : "text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-foreground"
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
