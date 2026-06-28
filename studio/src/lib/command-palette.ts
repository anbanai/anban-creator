// Global command-palette open state.
//
// The command palette (⌘K) is mounted once at the app root and owns its own
// keydown listener, but other surfaces — the sidebar "搜索" trigger, empty-state
// CTAs, help overlays — need to OPEN it without prop-drilling or a state library.
// This is a tiny zero-dependency external store they can call directly, read via
// React 19's useSyncExternalStore. Booleans are primitives, so getSnapshot needs
// no caching. No Zustand/Redux: the project uses TanStack Query for server state
// and local React state for the rest, so a hand-rolled store matches the codebase.

type Listener = () => void

let open = false
const listeners = new Set<Listener>()

function emit() {
  for (const l of listeners) l()
}

export const commandPaletteStore = {
  subscribe(listener: Listener): () => void {
    listeners.add(listener)
    return () => {
      listeners.delete(listener)
    }
  },
  getSnapshot(): boolean {
    return open
  },
  open(): void {
    if (open) return
    open = true
    emit()
  },
  close(): void {
    if (!open) return
    open = false
    emit()
  },
  toggle(): void {
    open = !open
    emit()
  },
}

// Display the platform-correct accelerator: ⌘ on Apple platforms, Ctrl elsewhere.
// Guarded for non-browser (test) environments where navigator is absent.
export const commandPaletteAccelerator =
  typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.userAgent)
    ? '⌘K'
    : 'Ctrl K'
