# Frontend UX Overhaul Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace hand-rolled UI components with shadcn/ui, add zod form validation to all forms, fix scrollbar styling, fix dark-theme inconsistencies, and rename `web/` to `studio/`.

**Architecture:** 5-phase approach — rename directory first, then install shadcn/ui foundation, then migrate components, then migrate forms, then polish UX. Each phase produces a working build.

**Tech Stack:** React 19, Tailwind CSS v4, shadcn/ui (Radix-based), zod, react-hook-form, sonner, tw-animate-css

**Spec:** `docs/superpowers/specs/2026-04-05-frontend-ux-overhaul-design.md`

---

## Phase 0: Rename `web/` → `studio/`

### Task 0.1: Rename directory and update references

**Files:**
- Rename: `web/` → `studio/`
- Modify: `Makefile:43,115,119,123` — `web/` → `studio/`
- Modify: `.gitignore:62` — `web/dist/` → `studio/dist/`
- Modify: `CLAUDE.md` — all `web/` references → `studio/`

- [ ] **Step 1: Rename directory**

```bash
git mv web studio
```

- [ ] **Step 2: Update Makefile**

Change line 43: `@rm -rf bin/ web/dist/ web/node_modules/` → `@rm -rf bin/ studio/dist/ studio/node_modules/`

Change lines 115, 119, 123: `cd web` → `cd studio`

- [ ] **Step 3: Update .gitignore**

Change line 62: `web/dist/` → `studio/dist/`

- [ ] **Step 4: Update CLAUDE.md**

Replace all occurrences of `web/` → `studio/` and `web ` → `studio ` where referencing the directory. Key sections: Web Frontend header, Build & Test Commands, Architecture section, Skills Integration, all path references.

- [ ] **Step 5: Verify dev server starts**

```bash
cd studio && npm run dev
```

Expected: Vite dev server starts on port 5173

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "refactor: rename web/ to studio/"
```

---

## Phase 1: Foundation

### Task 1.1: Install dependencies

**Files:** Modify: `studio/package.json`

- [ ] **Step 1: Install form/validation/toast/animation deps**

```bash
cd studio && npm install react-hook-form @hookform/resolvers zod sonner tw-animate-css
```

- [ ] **Step 2: Verify install succeeds**

```bash
npm ls react-hook-form @hookform/resolvers zod sonner tw-animate-css
```

Expected: All packages listed with versions

### Task 1.2: Initialize shadcn/ui

**Files:** Modify: `studio/src/index.css`, `studio/components.json` (new), `studio/src/lib/utils.ts` (new)

- [ ] **Step 1: Run shadcn init**

```bash
cd studio && npx shadcn@latest init
```

Configuration choices:
- Style: **New York** (more polished)
- Base color: **Zinc**
- CSS variables: **Yes**

This will:
- Create `studio/components.json`
- Modify `studio/src/index.css` with `@theme inline` CSS variables
- Create `studio/src/lib/utils.ts` with `cn()` function

- [ ] **Step 2: Verify cn() utility was created**

Read `studio/src/lib/utils.ts` — should contain:
```typescript
import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
```

- [ ] **Step 3: Add scrollbar CSS to index.css**

After the `@theme inline { ... }` block in `studio/src/index.css`, append:

```css
/* Custom scrollbar - thin and dark-themed */
@layer base {
  * {
    scrollbar-width: thin;
    scrollbar-color: var(--color-muted) transparent;
  }
  *::-webkit-scrollbar {
    width: 6px;
    height: 6px;
  }
  *::-webkit-scrollbar-track {
    background: transparent;
  }
  *::-webkit-scrollbar-thumb {
    background-color: var(--color-muted);
    border-radius: 3px;
  }
  *::-webkit-scrollbar-thumb:hover {
    background-color: var(--color-muted-foreground);
  }
}
```

- [ ] **Step 4: Add Toaster to App.tsx**

In `studio/src/App.tsx`, add import and component:

```typescript
import { Toaster } from '@/components/ui/sonner'
```

Add inside `<AuthProvider>`, before `<AppRoutes />`:

```tsx
<Toaster richColors position="top-right" />
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(studio): initialize shadcn/ui, add cn() utility, scrollbar CSS, sonner toaster"
```

### Task 1.3: Install shadcn components

- [ ] **Step 1: Install all needed shadcn components in one batch**

```bash
cd studio && npx shadcn@latest add button input textarea select dialog alert-dialog label badge card sonner dropdown-menu popover separator tabs tooltip form
```

This creates 16 component directories under `studio/src/components/ui/`. Note: `sonner` may already exist from init; that's fine.

- [ ] **Step 2: Verify components were created**

```bash
ls studio/src/components/ui/
```

Expected: `alert-dialog.tsx`, `badge.tsx`, `button.tsx`, `card.tsx`, `dialog.tsx`, `dropdown-menu.tsx`, `form.tsx`, `input.tsx`, `label.tsx`, `popover.tsx`, `select.tsx`, `separator.tsx`, `sonner.tsx`, `tabs.tsx`, `textarea.tsx`, `tooltip.tsx`

- [ ] **Step 3: Verify build succeeds**

```bash
cd studio && npm run build
```

Expected: Build completes with no errors (old component imports still exist but are not yet migrated)

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat(studio): install 16 shadcn/ui components"
```

---

## Phase 2: Component Migration

### Task 2.1: Migrate Badge

**Files:** Modify: `DashboardPage.tsx`, `TasksPage.tsx`, `PlansPage.tsx`, `TimelinePage.tsx`, `TaskDetailPage.tsx`
Delete: `studio/src/components/ui/Badge.tsx`

The old Badge has custom variants (`success`, `danger`, `warning`, `neutral`, `info`, `outline`). The shadcn Badge has `default`, `secondary`, `destructive`, `outline`. We need to add our custom variants to the shadcn badge file.

- [ ] **Step 1: Add custom variants to shadcn Badge**

Edit `studio/src/components/ui/badge.tsx`. In the `badgeVariants` cva, add these variants:

```typescript
success: "border-transparent bg-green-500/15 text-green-400",
warning: "border-transparent bg-amber-500/15 text-amber-400",
info: "border-transparent bg-blue-500/15 text-blue-400",
neutral: "border-transparent bg-gray-500/15 text-gray-400",
```

Keep existing `default`, `secondary`, `destructive`, `outline` variants.

- [ ] **Step 2: Update all Badge imports**

In all 5 files above, change:
- `import Badge from '@/components/ui/Badge'` → `import { Badge } from '@/components/ui/badge'`

No prop changes needed — the variants `success`, `danger`, `warning`, `neutral`, `info`, `outline` will work since we added them to the cva.

Note: The old `danger` variant maps to shadcn's `destructive`. Add a `danger` alias in the badge variants:
```typescript
danger: "border-transparent bg-red-500/15 text-red-400",
```

- [ ] **Step 3: Delete old Badge.tsx**

```bash
rm studio/src/components/ui/Badge.tsx
```

- [ ] **Step 4: Verify build**

```bash
cd studio && npm run build
```

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "refactor(studio): migrate Badge to shadcn/ui with custom variants"
```

### Task 2.2: Migrate Card

**Files:** Modify: `DashboardPage.tsx`, `TasksPage.tsx`, `PlansPage.tsx`, `TimelinePage.tsx`, `TaskDetailPage.tsx`, `SettingsPage.tsx`
Delete: `studio/src/components/ui/Card.tsx`

The old Card has `Card`, `CardHeader`, `CardBody`, `CardFooter`. shadcn has `Card`, `CardHeader`, `CardContent`, `CardDescription`, `CardFooter`, `CardTitle`. Map `CardBody` → `CardContent`.

- [ ] **Step 1: Update all Card imports and usages**

In each file, change:
- `import { Card } from '@/components/ui/Card'` → `import { Card, CardContent } from '@/components/ui/card'`
- `import { Card, CardBody } from '@/components/ui/Card'` → `import { Card, CardContent } from '@/components/ui/card'`
- All `<CardBody` → `<CardContent`
- All `</CardBody>` → `</CardContent>`
- `<Card className="transition-colors hover:border-gray-600">` stays as-is (shadcn Card accepts className)

- [ ] **Step 2: Delete old Card.tsx**

```bash
rm studio/src/components/ui/Card.tsx
```

- [ ] **Step 3: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "refactor(studio): migrate Card to shadcn/ui"
```

### Task 2.3: Migrate Button

**Files:** Modify: `TasksPage.tsx`, `PlansPage.tsx`, `ChannelsPage.tsx`, `TimelinePage.tsx`, `TaskDetailPage.tsx`, `AppLayout.tsx`
Delete: `studio/src/components/ui/Button.tsx`

Variant mapping: `primary` → `default`, `danger` → `destructive`, `secondary` → `secondary`, `ghost` → `ghost`
Size mapping: `sm` → `sm`, `md` → `default`, `lg` → `lg`

The old Button has a `loading` prop. shadcn Button doesn't. We need to add it.

- [ ] **Step 1: Add loading prop to shadcn Button**

Edit `studio/src/components/ui/button.tsx`. Extend the component to accept a `loading` prop:

```typescript
import { forwardRef } from "react"
// ... existing imports

interface ButtonProps extends React.ComponentProps<"button"> {
  variant?: "default" | "destructive" | "outline" | "secondary" | "ghost" | "link"
  size?: "default" | "sm" | "lg" | "icon"
  loading?: boolean
}

const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, loading, disabled, children, ...props }, ref) => {
    return (
      <button
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        disabled={disabled || loading}
        {...props}
      >
        {loading && (
          <svg className="mr-2 h-4 w-4 animate-spin" viewBox="0 0 24 24" fill="none">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
        )}
        {children}
      </button>
    )
  }
)
Button.displayName = "Button"
```

- [ ] **Step 2: Update all Button imports and props**

In each consuming file:
- `import Button from '@/components/ui/Button'` → `import { Button } from '@/components/ui/button'`
- `variant="primary"` → remove (default variant) or `variant="default"`
- `variant="danger"` → `variant="destructive"`
- `size="md"` → remove (default size) or `size="default"`

Files and specific changes:

**TasksPage.tsx:** Default import change + variant="primary" → variant="default" or just remove (lines 124, 238-239)

**PlansPage.tsx:** Default import change + variant prop changes (lines 179, 238-261)

**ChannelsPage.tsx:** Default import change (line 5)

**TimelinePage.tsx:** Default import change + variant="primary"/"secondary"/"ghost" mapping (lines 7, 144-155, 179-191)

**TaskDetailPage.tsx:** `variant="danger"` → `variant="destructive"` (line 195)

**AppLayout.tsx:** No Button usage, skip.

- [ ] **Step 3: Delete old Button.tsx**

```bash
rm studio/src/components/ui/Button.tsx
```

- [ ] **Step 4: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "refactor(studio): migrate Button to shadcn/ui with loading prop"
```

### Task 2.4: Migrate Input and Textarea

**Files:** Modify: `ChannelsPage.tsx`, `TasksPage.tsx`, `PlansPage.tsx`, `TimelinePage.tsx`, `SchedulePicker.tsx`
Delete: `studio/src/components/ui/Input.tsx`

The old Input has `label`, `hint`, `error` props. shadcn Input has none of these — they're handled by Label and FormField. For now (before Phase 3 form migration), we need a simple wrapper.

- [ ] **Step 1: Create a temporary FormInput wrapper**

Create `studio/src/components/FormInput.tsx`:

```typescript
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

interface FormInputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string
  hint?: string
  error?: string
}

export function FormInput({ label, hint, error, className, id, ...props }: FormInputProps) {
  const inputId = id || label?.toLowerCase().replace(/\s+/g, '-')
  return (
    <div>
      {label && <Label htmlFor={inputId}>{label}</Label>}
      <Input id={inputId} className={className} {...props} />
      {(hint || error) && (
        <p className={`mt-1 text-xs ${error ? 'text-destructive' : 'text-muted-foreground'}`}>
          {error || hint}
        </p>
      )}
    </div>
  )
}

interface FormTextareaProps extends React.TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: string
  hint?: string
  error?: string
}

export function FormTextarea({ label, hint, error, className, id, ...props }: FormTextareaProps) {
  const inputId = id || label?.toLowerCase().replace(/\s+/g, '-')
  return (
    <div>
      {label && <Label htmlFor={inputId}>{label}</Label>}
      <Textarea id={inputId} className={className} rows={3} {...props} />
      {(hint || error) && (
        <p className={`mt-1 text-xs ${error ? 'text-destructive' : 'text-muted-foreground'}`}>
          {error || hint}
        </p>
      )}
    </div>
  )
}
```

This is a temporary bridge until Phase 3 replaces all forms with shadcn Form + zod.

- [ ] **Step 2: Update all Input/Textarea imports**

In each file:
- `import { Input } from '@/components/ui/Input'` → `import { FormInput as Input } from '@/components/FormInput'`
- `import { Textarea } from '@/components/ui/Input'` → `import { FormTextarea as Textarea } from '@/components/FormInput'`

Files: ChannelsPage, TasksPage, PlansPage, TimelinePage, SchedulePicker

**SchedulePicker.tsx:** Has an inline `<input type="time">` on line 114-118. This should change to use shadcn Input:
```typescript
import { Input } from '@/components/ui/input'
```
And the time input becomes:
```tsx
<Input type="time" value={time} onChange={e => setTime(e.target.value)} className="w-auto" />
```

- [ ] **Step 3: Delete old Input.tsx**

```bash
rm studio/src/components/ui/Input.tsx
```

- [ ] **Step 4: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "refactor(studio): migrate Input/Textarea to shadcn/ui with FormInput bridge"
```

### Task 2.5: Migrate Select

**Files:** Modify: `ChannelsPage.tsx`, `TasksPage.tsx`, `PlansPage.tsx`
Delete: `studio/src/components/ui/Select.tsx`

The old Select is a simple `<select>` wrapper. shadcn Select is a Radix compound component. For now (before Phase 3), create a simple wrapper that matches the old API.

- [ ] **Step 1: Create a temporary FormSelect wrapper**

Create `studio/src/components/FormSelect.tsx`:

```typescript
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

interface FormSelectProps {
  label?: string
  hint?: string
  error?: string
  options: { value: string; label: string }[]
  value: string
  onChange: (value: string) => void
  disabled?: boolean
  className?: string
  placeholder?: string
}

export default function FormSelect({ label, hint, error, options, value, onChange, disabled, placeholder }: FormSelectProps) {
  return (
    <div>
      {label && <Label>{label}</Label>}
      <Select value={value} onValueChange={onChange} disabled={disabled}>
        <SelectTrigger className="mt-1.5">
          <SelectValue placeholder={placeholder || '请选择...'} />
        </SelectTrigger>
        <SelectContent>
          {options.map((opt) => (
            <SelectItem key={opt.value} value={opt.value}>
              {opt.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {(hint || error) && (
        <p className={`mt-1 text-xs ${error ? 'text-destructive' : 'text-muted-foreground'}`}>
          {error || hint}
        </p>
      )}
    </div>
  )
}
```

Note: The old Select used `onChange={(e) => ...}` (React change event). The new one uses `onChange={(value: string) => void}`. This means callers change from `onChange={(e) => setForm({ ...form, field: e.target.value })}` to `onChange={(v) => setForm({ ...form, field: v })}`.

- [ ] **Step 2: Update all Select imports and usages**

In each file:
- `import Select from '@/components/ui/Select'` → `import FormSelect from '@/components/FormSelect'`
- Change `<Select` → `<FormSelect`
- Change `onChange={(e) => setForm({ ...form, field: e.target.value as Type })}` → `onChange={(v) => setForm({ ...form, field: v as Type })}`

Specific changes:

**ChannelsPage.tsx line 288-294:**
```tsx
<FormSelect
  label="平台"
  options={platformOptions}
  value={form.platform}
  onChange={(v) => setForm({ ...form, platform: v })}
  disabled={!!editingChannel}
/>
```

**TasksPage.tsx line 263-268:**
```tsx
<FormSelect
  label="内容类型"
  options={contentTypeOptions}
  value={form.type}
  onChange={(v) => setForm({ ...form, type: v as TaskType })}
/>
```

**PlansPage.tsx line 300-305:**
```tsx
<FormSelect
  label="内容类型"
  options={contentTypeOptions}
  value={form.type}
  onChange={(v) => setForm({ ...form, type: v as PlanType })}
/>
```

- [ ] **Step 3: Delete old Select.tsx**

```bash
rm studio/src/components/ui/Select.tsx
```

- [ ] **Step 4: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "refactor(studio): migrate Select to shadcn/ui with FormSelect bridge"
```

### Task 2.6: Migrate Modal to Dialog

**Files:** Modify: `ChannelsPage.tsx`, `TasksPage.tsx`, `PlansPage.tsx`
Delete: `studio/src/components/ui/Modal.tsx`

The old Modal has: `open`, `onClose`, `title`, `children`, `footer`, `maxWidth`.
shadcn Dialog: `open`, `onOpenChange`, `DialogContent`, `DialogHeader`, `DialogTitle`, `DialogFooter`.

- [ ] **Step 1: Update all Modal imports and usages**

In each file:
- `import Modal from '@/components/ui/Modal'` → `import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogClose } from '@/components/ui/dialog'`

**Pattern transformation (apply to all 3 files):**

Old:
```tsx
<Modal open={modalOpen} onClose={closeModal} title="..." footer={<><Button variant="secondary" onClick={closeModal}>取消</Button><Button ...>创建</Button></>}>
  <form ...>...</form>
</Modal>
```

New:
```tsx
<Dialog open={modalOpen} onOpenChange={(open) => { if (!open) closeModal() }}>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>...</DialogTitle>
    </DialogHeader>
    <div className="max-h-[60vh] overflow-y-auto">
      <form ...>...</form>
    </div>
    <DialogFooter>
      <Button variant="secondary" onClick={closeModal}>取消</Button>
      <Button ...>创建</Button>
    </DialogFooter>
  </DialogContent>
</Dialog>
```

Note: The `handleSubmit` was previously attached to the `<form onSubmit>` AND the footer button's `onClick`. With Dialog, keep the `<form onSubmit>` handler but the footer buttons should be type="button" for cancel and type="submit" wrapped in a form, or just keep onClick handlers. Simplest: keep the existing pattern where footer buttons call handleSubmit/closeModal directly.

For ChannelsPage specifically: the form has `className="max-h-[60vh] space-y-4 overflow-y-auto pr-1"` — the `pr-1` hack is no longer needed with thin scrollbars.

- [ ] **Step 2: Delete old Modal.tsx**

```bash
rm studio/src/components/ui/Modal.tsx
```

- [ ] **Step 3: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "refactor(studio): migrate Modal to shadcn/ui Dialog"
```

### Task 2.7: Migrate UserAccountPopover to DropdownMenu

**Files:** Modify: `studio/src/components/auth/UserAccountPopover.tsx`

- [ ] **Step 1: Rewrite UserAccountPopover using shadcn DropdownMenu**

```typescript
import { useAuth } from '@/contexts/AuthContext'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'

export default function UserAccountPopover() {
  const { user, logout } = useAuth()

  if (!user) return null

  const initials = user.nickname
    ? user.nickname.slice(0, 1).toUpperCase()
    : user.email.slice(0, 1).toUpperCase()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button className="flex items-center gap-2 rounded-lg px-3 py-1.5 text-sm text-gray-300 transition-colors hover:bg-gray-800 hover:text-gray-100">
          {user.avatar ? (
            <img src={user.avatar} alt={user.nickname} className="h-7 w-7 rounded-full object-cover" />
          ) : (
            <span className="flex h-7 w-7 items-center justify-center rounded-full bg-blue-600 text-xs font-medium text-white">
              {initials}
            </span>
          )}
          <span className="hidden sm:inline max-w-[120px] truncate">{user.nickname || user.email}</span>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        <DropdownMenuLabel>
          <p className="truncate text-sm font-medium">{user.nickname || '用户'}</p>
          <p className="truncate text-xs text-muted-foreground">{user.email}</p>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => void logout()}>
          退出登录
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
```

- [ ] **Step 2: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "refactor(studio): migrate UserAccountPopover to shadcn DropdownMenu"
```

### Task 2.8: Fix ChannelCard dark theme

**Files:** Modify: `studio/src/components/ChannelCard.tsx`

- [ ] **Step 1: Replace all light-mode colors with dark-theme equivalents**

Full rewrite of `ChannelCard.tsx`:

```typescript
import { type Channel, type ChannelStats } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

interface ChannelCardProps {
  channel: Channel
  stats?: ChannelStats
  onEdit?: (channel: Channel) => void
  onArchive?: (id: string) => void
  onRestore?: (id: string) => void
  onDelete?: (id: string) => void
}

const platformLabels: Record<string, string> = {
  article: '公众号',
  xls: '小绿书',
  seednote: '种草笔记',
}

export function ChannelCard({ channel, stats, onEdit, onArchive, onRestore, onDelete }: ChannelCardProps) {
  return (
    <div className="rounded-xl border border-gray-700 bg-gray-800 p-4 transition-shadow hover:border-gray-600">
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-3">
          {channel.avatar_url ? (
            <img src={channel.avatar_url} alt={channel.name} className="h-12 w-12 rounded-full object-cover" />
          ) : (
            <div className="flex h-12 w-12 items-center justify-center rounded-full bg-gray-700 text-lg font-medium text-gray-300">
              {channel.name.charAt(0)}
            </div>
          )}
          <div>
            <h3 className="font-medium text-gray-100">{channel.name}</h3>
            <Badge variant="outline" className="mt-1 text-[10px]">
              {platformLabels[channel.platform] || channel.platform}
            </Badge>
          </div>
        </div>
        {channel.status === 'archived' && (
          <Badge variant="warning">已归档</Badge>
        )}
      </div>
      {channel.description && (
        <p className="mt-2 line-clamp-2 text-sm text-gray-400">{channel.description}</p>
      )}
      {stats && (
        <div className="mt-3 flex gap-4 border-t border-gray-700 pt-3 text-xs text-gray-500">
          <span>任务 {stats.total_tasks}</span>
          <span>完成 {stats.completed_tasks}</span>
          {stats.total_tasks > 0 && <span>成功率 {(stats.success_rate * 100).toFixed(0)}%</span>}
        </div>
      )}
      <div className="mt-3 flex gap-2">
        {onEdit && (
          <Button variant="ghost" size="sm" onClick={() => onEdit(channel)}>编辑</Button>
        )}
        {channel.status === 'active' && onArchive && (
          <Button variant="ghost" size="sm" className="text-amber-400 hover:text-amber-300" onClick={() => onArchive(channel.id)}>归档</Button>
        )}
        {channel.status === 'archived' && onRestore && (
          <Button variant="ghost" size="sm" className="text-green-400 hover:text-green-300" onClick={() => onRestore(channel.id)}>恢复</Button>
        )}
        {onDelete && (
          <Button variant="ghost" size="sm" className="text-red-400 hover:text-red-300" onClick={() => onDelete(channel.id)}>删除</Button>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Fix ChannelSelector dark theme**

In `studio/src/components/ChannelSelector.tsx`, change line 28 and lines 40-56:

The loading skeleton: `bg-gray-200` → `bg-gray-700`
The select element: `bg-white text-gray-900 border-gray-300` → `bg-gray-700 text-gray-100 border-gray-600 focus:border-blue-500 focus:ring-1 focus:ring-blue-500`

- [ ] **Step 3: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "fix(studio): dark theme colors for ChannelCard and ChannelSelector"
```

---

## Phase 3: Form Validation

### Task 3.1: Create zod schemas

**Files:** Create: `studio/src/lib/schemas.ts`

- [ ] **Step 1: Create the schemas file**

```typescript
import { z } from "zod"

// Login
export const loginSchema = z.object({
  email: z.string().min(1, "邮箱不能为空").email("请输入有效的邮箱地址"),
  password: z.string().min(1, "密码不能为空"),
})
export type LoginFormValues = z.infer<typeof loginSchema>

// Register
export const registerSchema = z.object({
  email: z.string().min(1, "邮箱不能为空").email("请输入有效的邮箱地址"),
  password: z.string().min(8, "密码至少 8 个字符"),
  nickname: z.string().optional(),
})
export type RegisterFormValues = z.infer<typeof registerSchema>

// Create Task
export const createTaskSchema = z.object({
  channel_id: z.string().optional(),
  type: z.enum(["seednote", "article", "xls"]),
  topic: z.string().min(1, "主题不能为空").max(200, "主题不能超过 200 个字符"),
})
export type CreateTaskFormValues = z.infer<typeof createTaskSchema>

// Create/Edit Plan
export const planSchema = z.object({
  channel_id: z.string().optional(),
  type: z.enum(["seednote", "article", "xls"]),
  title: z.string().min(1, "标题不能为空").max(200, "标题不能超过 200 个字符"),
  description: z.string().max(500, "描述不能超过 500 个字符").optional(),
  cron_expr: z.string().min(1, "请设置排期"),
  topic_hint: z.string().max(200, "主题方向不能超过 200 个字符").optional(),
})
export type PlanFormValues = z.infer<typeof planSchema>

// Create/Edit Channel
export const channelSchema = z.object({
  platform: z.enum(["article", "xls", "seednote"]),
  name: z.string().min(1, "频道名称不能为空").max(100, "名称不能超过 100 个字符"),
  description: z.string().max(500, "简介不能超过 500 个字符").optional(),
  avatar_url: z.string().url("请输入有效的 URL").or(z.literal("")).optional(),
  wechat_app_id: z.string().optional(),
  wechat_secret: z.string().optional(),
  keywords: z.string().max(200, "关键词不能超过 200 个字符").optional(),
  positioning: z.string().max(300, "定位不能超过 300 个字符").optional(),
  style: z.string().max(100, "写作风格不能超过 100 个字符").optional(),
  theme: z.string().max(100, "主题不能超过 100 个字符").optional(),
  author: z.string().max(50, "作者名不能超过 50 个字符").optional(),
})
export type ChannelFormValues = z.infer<typeof channelSchema>
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd studio && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "feat(studio): add zod schemas for all forms"
```

### Task 3.2: Migrate LoginPage

**Files:** Modify: `studio/src/pages/LoginPage.tsx`

- [ ] **Step 1: Rewrite LoginPage with zod + react-hook-form**

```typescript
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Link, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { useAuth } from '@/contexts/AuthContext'
import { loginSchema, type LoginFormValues } from '@/lib/schemas'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'

export default function LoginPage() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const form = useForm<LoginFormValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: '', password: '' },
  })

  async function onSubmit(values: LoginFormValues) {
    try {
      const response = await api.auth.login(values.email, values.password)
      login(response.token, response.refresh_token, response.user)
      navigate('/', { replace: true })
    } catch (err) {
      const message = err instanceof Error ? err.message : '登录失败，请重试。'
      toast.error(message)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-900 px-4">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <h1 className="text-2xl font-bold text-blue-400">Anban 智能创作助手</h1>
          <p className="mt-2 text-sm text-gray-400">登录你的账号</p>
        </div>

        <Card>
          <CardContent className="pt-6">
            <Form {...form}>
              <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
                <FormField
                  control={form.control}
                  name="email"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>邮箱</FormLabel>
                      <FormControl>
                        <Input type="email" placeholder="请输入邮箱地址" {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name="password"
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>密码</FormLabel>
                      <FormControl>
                        <Input type="password" placeholder="请输入密码" {...field} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <Button type="submit" className="w-full" loading={form.formState.isSubmitting}>
                  登录
                </Button>
              </form>
            </Form>

            <p className="mt-4 text-center text-sm text-gray-400">
              还没有账号？{' '}
              <Link to="/register" className="text-blue-400 hover:text-blue-300">注册</Link>
            </p>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "feat(studio): migrate LoginPage to zod + react-hook-form"
```

### Task 3.3: Migrate RegisterPage

**Files:** Modify: `studio/src/pages/RegisterPage.tsx`

- [ ] **Step 1: Rewrite RegisterPage with zod + react-hook-form**

Same pattern as LoginPage but with `registerSchema` and 3 fields (email, nickname, password). Use `registerSchema` and `RegisterFormValues` from schemas. The nickname field is optional. Add `toast.error()` for error handling.

Follow the exact same form pattern: `useForm<RegisterFormValues>({ resolver: zodResolver(registerSchema) })`, three `<FormField>` components, `<FormMessage />` per field.

- [ ] **Step 2: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "feat(studio): migrate RegisterPage to zod + react-hook-form"
```

### Task 3.4: Migrate LoginDialog

**Files:** Modify: `studio/src/components/auth/LoginDialog.tsx`

- [ ] **Step 1: Rewrite LoginDialog using shadcn Dialog + zod form**

```typescript
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { useAuth } from '@/contexts/AuthContext'
import { loginSchema, type LoginFormValues } from '@/lib/schemas'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'

interface LoginDialogProps {
  open: boolean
  onClose: () => void
}

export default function LoginDialog({ open, onClose }: LoginDialogProps) {
  const { login } = useAuth()
  const form = useForm<LoginFormValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: '', password: '' },
  })

  async function onSubmit(values: LoginFormValues) {
    try {
      const response = await api.auth.login(values.email, values.password)
      login(response.token, response.refresh_token, response.user)
      form.reset()
      onClose()
    } catch (err) {
      const message = err instanceof Error ? err.message : '登录失败，请重试。'
      toast.error(message)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) onClose() }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>登录</DialogTitle>
        </DialogHeader>
        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
            <FormField
              control={form.control}
              name="email"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>邮箱</FormLabel>
                  <FormControl>
                    <Input type="email" placeholder="请输入邮箱地址" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="password"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>密码</FormLabel>
                  <FormControl>
                    <Input type="password" placeholder="请输入密码" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <Button type="submit" className="w-full" loading={form.formState.isSubmitting}>
              登录
            </Button>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}
```

- [ ] **Step 2: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "feat(studio): migrate LoginDialog to shadcn Dialog + zod form"
```

### Task 3.5: Migrate TasksPage form

**Files:** Modify: `studio/src/pages/TasksPage.tsx`

- [ ] **Step 1: Replace useState form with useForm + zod**

Key changes:
- Remove `useState` for form and formError
- Add `useForm<CreateTaskFormValues>({ resolver: zodResolver(createTaskSchema) })`
- Replace `ChannelSelector` usage: wrap in `<FormField>` with custom render
- Replace `FormSelect` for type: wrap in `<FormField>`
- Replace `FormInput` for topic: wrap in `<FormField>` with shadcn Input directly
- Replace `formError` div with form-level error display
- Change mutation error handler to use `toast.error()`

The ChannelSelector integration pattern:
```tsx
<FormField
  control={form.control}
  name="channel_id"
  render={({ field }) => (
    <FormItem>
      <FormLabel>频道</FormLabel>
      <FormControl>
        <ChannelSelector
          value={field.value || ''}
          onChange={(id, platform) => {
            field.onChange(id)
            if (id) form.setValue('type', platform as TaskType)
          }}
        />
      </FormControl>
      <FormMessage />
    </FormItem>
  )}
/>
```

- [ ] **Step 2: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "feat(studio): migrate TasksPage form to zod + react-hook-form"
```

### Task 3.6: Migrate PlansPage form

**Files:** Modify: `studio/src/pages/PlansPage.tsx`

- [ ] **Step 1: Replace useState form with useForm + zod**

Same pattern as TasksPage but with 6 fields. Key differences:
- Dual mode (create/edit): when `editingPlan` is set, use `form.reset(planToFormValues(editingPlan))` in the `openEdit` function
- SchedulePicker integration: wrap in `<FormField>` and call `field.onChange` from SchedulePicker's `onChange`
- The `channel_platform` state is no longer needed in the form — it's only used to auto-set type, handled via `form.setValue`

SchedulePicker integration:
```tsx
<FormField
  control={form.control}
  name="cron_expr"
  render={({ field }) => (
    <FormItem>
      <FormLabel>排期设置</FormLabel>
      <FormControl>
        <SchedulePicker value={field.value} onChange={field.onChange} />
      </FormControl>
      <FormMessage />
    </FormItem>
  )}
/>
```

- [ ] **Step 2: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "feat(studio): migrate PlansPage form to zod + react-hook-form"
```

### Task 3.7: Migrate ChannelsPage form

**Files:** Modify: `studio/src/pages/ChannelsPage.tsx`

- [ ] **Step 1: Replace 11-field useState form with useForm + zod**

Most complex form. Key changes:
- Use `channelSchema` with `useForm<ChannelFormValues>`
- Dual mode: `openEdit` calls `form.reset(channelToForm(channel))`
- 11 `<FormField>` components for each field
- `wechat_secret` shown only when NOT editing (use conditional render)
- Platform select disabled during edit
- `channelToForm` helper converts Channel to form default values
- Use `toast.success()` / `toast.error()` in mutation callbacks

- [ ] **Step 2: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "feat(studio): migrate ChannelsPage form to zod + react-hook-form"
```

---

## Phase 4: UX Polish

### Task 4.1: Replace window.confirm() with AlertDialog

**Files:** Modify: `ChannelsPage.tsx`, `PlansPage.tsx`, `TaskDetailPage.tsx`

- [ ] **Step 1: Add AlertDialog to PlansPage**

Add state: `const [deleteTarget, setDeleteTarget] = useState<string | null>(null)`

Replace the delete button onClick:
```tsx
// Old: onClick={() => { if (window.confirm('确定删除此计划？')) { deleteMutation.mutate(plan.id) } }}
// New:
onClick={() => setDeleteTarget(plan.id)}
```

Add AlertDialog component:
```tsx
<AlertDialog open={!!deleteTarget} onOpenChange={(open) => { if (!open) setDeleteTarget(null) }}>
  <AlertDialogContent>
    <AlertDialogHeader>
      <AlertDialogTitle>确定删除此计划？</AlertDialogTitle>
      <AlertDialogDescription>此操作不可撤销。删除后计划及其所有数据将被永久移除。</AlertDialogDescription>
    </AlertDialogHeader>
    <AlertDialogFooter>
      <AlertDialogCancel>取消</AlertDialogCancel>
      <AlertDialogAction onClick={() => { deleteMutation.mutate(deleteTarget!); setDeleteTarget(null) }}>
        删除
      </AlertDialogAction>
    </AlertDialogFooter>
  </AlertDialogContent>
</AlertDialog>
```

Import: `import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, AlertDialogTrigger } from '@/components/ui/alert-dialog'`

Wait — AlertDialogAction needs destructive styling. Add `className="bg-destructive text-destructive-foreground hover:bg-destructive/90"` to the delete button.

- [ ] **Step 2: Add AlertDialog to ChannelsPage**

Same pattern as PlansPage but for channel deletion. Currently line 200-203 uses `window.confirm`.

- [ ] **Step 3: Add AlertDialog to TaskDetailPage**

For cancel task. Currently line 199-202. Same pattern but with "取消任务" title and "确定要取消此任务吗？" description.

- [ ] **Step 4: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "fix(studio): replace window.confirm with AlertDialog for destructive actions"
```

### Task 4.2: Add toast notifications to all mutations

**Files:** Modify: `ChannelsPage.tsx`, `PlansPage.tsx`, `TasksPage.tsx`, `TaskDetailPage.tsx`

- [ ] **Step 1: Add toast imports and calls**

In each file, add:
```typescript
import { toast } from 'sonner'
```

Then add `toast.success()` / `toast.error()` to all mutations:

**PlansPage mutations (6):**
- `createMutation.onSuccess`: `toast.success('计划创建成功')`
- `createMutation.onError`: `toast.error('创建计划失败，请检查输入')`
- `updateMutation.onSuccess`: `toast.success('计划更新成功')`
- `updateMutation.onError`: `toast.error('更新计划失败')`
- `deleteMutation.onSuccess`: `toast.success('计划已删除')`
- `pauseMutation.onSuccess`: `toast.success('计划已暂停')`
- `resumeMutation.onSuccess`: `toast.success('计划已恢复')`

**ChannelsPage mutations (5):**
- `createMutation.onSuccess`: `toast.success('频道创建成功')`
- `createMutation.onError`: `toast.error('创建频道失败')`
- `updateMutation.onSuccess`: `toast.success('频道更新成功')`
- `updateMutation.onError`: `toast.error('更新频道失败')`
- `archiveMutation.onSuccess`: `toast.success('频道已归档')`
- `restoreMutation.onSuccess`: `toast.success('频道已恢复')`
- `deleteMutation.onSuccess`: `toast.success('频道已删除')`

**TasksPage (1):**
- `createMutation.onError`: `toast.error('创建任务失败，请重试')` (success navigates away, no toast needed)

**TaskDetailPage (1):**
- `cancelMutation.onSuccess`: `toast.success('任务已取消')`

- [ ] **Step 2: Remove old formError state and divs**

Since errors are now handled by zod (per-field) and toast (API errors), remove:
- `const [formError, setFormError] = useState('')`
- All `setFormError(...)` calls
- All `{formError && <div ...>{formError}</div>}` blocks

This applies to: TasksPage, PlansPage, ChannelsPage (LoginPage, RegisterPage, LoginDialog already migrated in Phase 3).

- [ ] **Step 3: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "feat(studio): add toast notifications to all mutations, remove formError state"
```

### Task 4.3: Remove temporary bridge components

**Files:** Delete: `studio/src/components/FormInput.tsx`, `studio/src/components/FormSelect.tsx`

After Phase 3, all forms use shadcn Form + zod directly. The temporary FormInput and FormSelect bridges are no longer needed.

- [ ] **Step 1: Check no files still import FormInput or FormSelect**

```bash
grep -r "FormInput\|FormSelect" studio/src/ --include="*.tsx" --include="*.ts"
```

Expected: No matches (all forms migrated to use FormField directly)

- [ ] **Step 2: Delete bridge components**

```bash
rm studio/src/components/FormInput.tsx studio/src/components/FormSelect.tsx
```

- [ ] **Step 3: Verify build and commit**

```bash
cd studio && npm run build && git add -A && git commit -m "refactor(studio): remove temporary FormInput/FormSelect bridge components"
```

### Task 4.4: Final verification

- [ ] **Step 1: Build succeeds**

```bash
cd studio && npm run build
```

Expected: Build completes with no errors

- [ ] **Step 2: Dev server smoke test**

```bash
cd studio && npm run dev
```

Then in browser:
1. Login page renders with styled form
2. Submit empty form → per-field validation errors
3. Navigate to Channels — dark-themed cards
4. Open create dialog → animated modal with thin scrollbar
5. Create plan without channel → validation error on submit
6. All scrollable areas have thin, dark scrollbars

- [ ] **Step 3: Final commit**

If any fixes needed:
```bash
git add -A && git commit -m "fix(studio): final polish after UX overhaul"
```

---

## Summary

| Phase | Tasks | Commits |
|-------|-------|---------|
| 0: Rename | 1 | 1 |
| 1: Foundation | 3 | 3 |
| 2: Component Migration | 8 | 8 |
| 3: Form Validation | 7 | 7 |
| 4: UX Polish | 4 | 4 |
| **Total** | **23** | **~23** |
