# Frontend UX Overhaul Design

**Date:** 2026-04-05
**Status:** Approved

## Context

The web frontend (React 19 + Tailwind v4) has accumulated several UX issues:

1. **No form validation library** — all forms use raw `useState` with manual `if (!field.trim())` checks. Errors only appear after submission as a single string at the bottom. The `error` prop on Input/Select components exists but is never used by any page.
2. **No UI component library** — all components are hand-rolled. Modal has no animations, scrollbar is browser-default (ugly on dark theme), `window.confirm()` for destructive actions.
3. **Styling inconsistencies** — `ChannelCard` and `ChannelSelector` use light-mode colors in a dark-themed app. `LoginDialog` duplicates the `Modal` component. Login/Register pages don't use the reusable `Input` component.
4. **No toast/notification system** — feedback is only via inline error strings.
5. **Directory naming** — `web/` is generic; `studio/` better reflects the content creation tool.

**Goal:** A polished, consistent dark-themed UI with proper form validation, quality components, and good user feedback.

## Decisions

| Decision | Choice | Why |
|----------|--------|-----|
| Component library | shadcn/ui | Copy-paste components on Radix UI. Fully customizable, works with Tailwind v4, de facto standard for React + Tailwind projects. |
| Form validation | zod + react-hook-form | shadcn Form integrates natively. Per-field errors, real-time validation. |
| Toasts | sonner | shadcn recommends sonner over its built-in toast. Simpler API, better DX. |
| Animations | tw-animate-css | tailwindcss-animate is deprecated for Tailwind v4. |
| Theme | Dark-only | App has no light mode. CSS variables set directly to dark values. |
| Rename | web/ → studio/ | Done first so all subsequent work uses correct paths. |

## Phase 0: Rename web/ → studio/

`git mv web studio` plus update:
- `Makefile`: `cd web` → `cd studio` in web-install, web-dev, web-build targets; `web/dist/` → `studio/dist/` in clean
- `.gitignore`: `web/dist/` → `studio/dist/`
- `CLAUDE.md`: all `web/` references → `studio/`
- Any docs referencing `web/src/` paths

## Phase 1: Foundation

### Dependencies

```bash
npm install react-hook-form @hookform/resolvers zod sonner tw-animate-css
```

### shadcn/ui Init

Run `npx shadcn@latest init` in `studio/`. For Tailwind v4, shadcn generates CSS using `@theme inline` directive (no tailwind.config.js needed). Base color: Zinc (dark theme).

### cn() Utility

Create `studio/src/lib/utils.ts`:

```typescript
import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
```

Both `clsx` and `tailwind-merge` are already installed.

### Scrollbar CSS

Add to `studio/src/index.css` after shadcn variables:

```css
@layer base {
  * {
    scrollbar-width: thin;
    scrollbar-color: var(--color-muted) transparent;
  }
  *::-webkit-scrollbar { width: 6px; height: 6px; }
  *::-webkit-scrollbar-track { background: transparent; }
  *::-webkit-scrollbar-thumb { background-color: var(--color-muted); border-radius: 3px; }
  *::-webkit-scrollbar-thumb:hover { background-color: var(--color-muted-foreground); }
}
```

Covers Firefox (`scrollbar-width`/`scrollbar-color`) and Chrome/Safari (`::-webkit-scrollbar`).

### shadcn Components (install in one batch)

```
button input textarea select dialog alert-dialog label badge card
sonner dropdown-menu popover separator tabs tooltip form
```

16 components total. These land in `studio/src/components/ui/`.

### Toaster Provider

Add `<Toaster richColors position="top-right" />` to `App.tsx`.

## Phase 2: Component Migration

Replace hand-rolled components with shadcn equivalents. Import convention changes: `@/components/ui/Button` → `@/components/ui/button` (lowercase, shadcn standard).

| Old Component | New Component | Key Changes |
|---------------|---------------|-------------|
| `Button.tsx` | `button` | `primary→default`, `danger→destructive`, `md→default` |
| `Input.tsx` | `input` + `textarea` | No `label`/`hint` props (handled by Form + Label) |
| `Select.tsx` | `select` | Compound component API (`SelectTrigger`, `SelectContent`, `SelectItem`) |
| `Modal.tsx` | `dialog` | Animated overlay, `DialogContent`/`DialogHeader`/`DialogFooter` |
| `Card.tsx` | `card` | `CardBody→CardContent` |
| `Badge.tsx` | `badge` | Add custom `success`/`warning` variants |
| `UserAccountPopover` | `dropdown-menu` | Replaces manual outside-click handler |

Delete old component files after all consumers migrated.

**Files consuming old components:** TasksPage, PlansPage, ChannelsPage, TimelinePage, TaskDetailPage, SettingsPage, ChannelCard, AppLayout, SchedulePicker.

## Phase 3: Form Validation

### Schemas (`studio/src/lib/schemas.ts`)

```typescript
// loginSchema: email (required, valid email), password (required)
// registerSchema: email (required, valid), password (min 8), nickname (optional)
// createTaskSchema: type (enum), topic (required, max 200), channel_id (optional)
// planSchema: type (enum), title (required, max 200), description (optional, max 500),
//             cron_expr (required), topic_hint (optional, max 200), channel_id (optional)
// channelSchema: platform (enum), name (required, max 100), description (optional, max 500),
//                avatar_url (optional, valid URL), wechat_app_id (optional),
//                wechat_secret (optional), keywords (optional, max 200),
//                positioning (optional, max 300), style/theme/author (optional)
```

All error messages in Chinese (e.g., "请选择渠道", "标题不能为空").

### Migration Pattern

Every form follows this transformation:

**Before:** `useState<FormData>(emptyForm)` + manual `if (!field.trim())` checks + single `formError` string.

**After:** `useForm<SchemaType>({ resolver: zodResolver(schema), defaultValues })` + `<FormField>` with `<FormMessage />` per field. API errors shown via `toast.error()` or `form.setError("root", { message })`.

### Migration Order

1. **LoginPage** (2 fields, simplest)
2. **RegisterPage** (3 fields)
3. **LoginDialog** (2 fields, refactor to share code with LoginPage)
4. **TasksPage** (3 fields)
5. **PlansPage** (6 fields, includes SchedulePicker integration)
6. **ChannelsPage** (11 fields, most complex)

**Dual-mode forms (create + edit):** PlansPage and ChannelsPage use `form.reset()` with appropriate values when switching modes. The `editingPlan`/`editingChannel` state variable still controls mode.

**Custom components in FormField:** SchedulePicker and ChannelSelector are domain-specific. They stay custom but get wrapped in `<FormField>` render functions calling `field.onChange`.

## Phase 4: UX Polish

### Replace window.confirm() with AlertDialog

Three locations:
- `ChannelsPage.tsx` — delete channel
- `PlansPage.tsx` — delete plan
- `TasksPage.tsx` — cancel task (currently in TaskDetailPage)

Pattern: `useState<string | null>(deleteTarget)` controls AlertDialog visibility.

### Fix Dark Theme Colors

- `ChannelCard.tsx`: Replace `bg-green-100 text-green-800` etc. with dark equivalents. After Phase 2 this will use shadcn Card + Badge + Button which handle dark mode automatically.
- `ChannelSelector.tsx`: Replace `bg-white text-gray-900 border-gray-300` with dark colors. After Phase 2 this uses shadcn Select.

### Add Toast Notifications

All mutation `onSuccess`/`onError` callbacks get toast calls:

```typescript
onSuccess: () => { toast.success("操作成功"); ... },
onError: () => { toast.error("操作失败，请重试"); },
```

Files: ChannelsPage, PlansPage, TasksPage, TaskDetailPage, LoginPage, RegisterPage, LoginDialog.

### Deduplicate LoginDialog

Extract a `LoginFormFields` component shared by LoginPage and LoginDialog. LoginDialog becomes a thin `<Dialog>` wrapper around the shared form.

## Files Modified/Created

### New Files
- `studio/src/lib/utils.ts` — cn() utility
- `studio/src/lib/schemas.ts` — zod schemas
- `studio/src/components/ui/*` — 16 shadcn components (via CLI)
- `studio/src/components/auth/LoginFormFields.tsx` — shared login form (Phase 4)

### Modified Files (by phase)
- **Phase 0:** Makefile, .gitignore, CLAUDE.md
- **Phase 1:** studio/src/index.css, studio/src/App.tsx, studio/package.json
- **Phase 2:** All pages, ChannelCard, ChannelSelector, SchedulePicker, AppLayout — import changes
- **Phase 3:** LoginPage, RegisterPage, LoginDialog, TasksPage, PlansPage, ChannelsPage — form rewrite
- **Phase 4:** ChannelsPage, PlansPage, TaskDetailPage — AlertDialog; ChannelCard, ChannelSelector — dark theme

### Deleted Files (after Phase 2)
- `studio/src/components/ui/Button.tsx`
- `studio/src/components/ui/Input.tsx`
- `studio/src/components/ui/Select.tsx`
- `studio/src/components/ui/Modal.tsx`
- `studio/src/components/ui/Card.tsx`
- `studio/src/components/ui/Badge.tsx`

## Challenges

1. **shadcn Select vs ChannelSelector:** shadcn Select is Radix-based (not native `<select>`). ChannelSelector uses `<optgroup>` for grouping — map to `<SelectGroup>` + `<SelectLabel>`.
2. **SchedulePicker reset:** When form is in edit mode, `form.reset()` must properly reset SchedulePicker's internal state via a `useEffect` on the field value.
3. **Tailwind v4 + shadcn:** OKLCH color space in CSS variables (different from HSL in older shadcn). Verify colors render correctly.

## Verification

After each phase:
1. `cd studio && npm run dev` — dev server starts without errors
2. Visual check: all pages render, no console errors
3. Form testing: submit empty forms → per-field errors shown inline
4. Modal testing: open/close dialogs → smooth animations
5. Scrollbar: thin, dark-themed across all scrollable areas
6. Dark theme: no light-mode color leaks
7. After Phase 4: full smoke test of all CRUD operations
