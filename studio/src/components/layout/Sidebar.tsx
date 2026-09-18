import { NavLink } from "react-router-dom";
import { useEffect, useState } from "react";
import type { LucideIcon } from "lucide-react";
import {
  Menu,
  X,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
} from "lucide-react";
import UserAccountPopover from "@/components/auth/UserAccountPopover";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useAuth } from "@/contexts/AuthContext";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  adminNavItems,
  mvpNavItems,
} from "@/lib/navigation";
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
  const { user } = useAuth();
  const isAdmin = user?.is_admin === true;
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
      {!mobileOpen && (
        <button
          onClick={() => setMobileOpen(true)}
          className="fixed left-4 top-4 z-50 flex h-10 w-10 items-center justify-center rounded-lg bg-sidebar text-sidebar-foreground shadow-lg md:hidden"
          aria-label="打开菜单"
        >
          <Menu className="h-5 w-5" />
        </button>
      )}

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
        {/* Top: logo and collapse toggle. */}
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
          <div className="flex flex-col gap-0.5">
            {mvpNavItems.map((item) => (
              <SidebarNavLink
                key={item.to}
                item={item}
                collapsed={collapsed}
                onClick={() => setMobileOpen(false)}
              />
            ))}
          </div>
          {isAdmin ? (
            <div className="mt-3 flex flex-col gap-0.5 border-t border-sidebar-border pt-3">
              {adminNavItems.map((item) => (
                <SidebarNavLink
                  key={item.to}
                  item={item}
                  collapsed={collapsed}
                  onClick={() => setMobileOpen(false)}
                />
              ))}
            </div>
          ) : null}
        </nav>

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

function SidebarNavLink({
  item,
  collapsed,
  onClick,
}: {
  item: { to: string; label: string; icon: LucideIcon; end?: boolean }
  collapsed: boolean
  onClick: () => void
}) {
  const link = (
    <NavLink
      to={item.to}
      end={item.end}
      onClick={onClick}
      aria-label={collapsed ? item.label : undefined}
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
