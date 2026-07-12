# Studio AI Assistant Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the redundant Studio homepage section with one direct `AI助手` root navigation entry using the Lucide `Sparkles` icon.

**Architecture:** Keep `/` and the existing `NavItem` data flow unchanged. Update the root item metadata in `navigation.ts`, then render that item directly in `Sidebar.tsx` instead of passing it through a one-item `SidebarSection`.

**Tech Stack:** React 19, TypeScript, React Router, Lucide React, Vitest, Testing Library

---

### Task 1: Replace the redundant homepage sidebar group

**Files:**
- Modify: `studio/src/lib/navigation.test.ts`
- Modify: `studio/src/components/layout/Sidebar.test.tsx`
- Modify: `studio/src/lib/navigation.ts`
- Modify: `studio/src/components/layout/Sidebar.tsx`

- [x] **Step 1: Write the failing navigation metadata test**

Import `Sparkles` from `lucide-react`, change the root-label expectations to `AI助手`, and assert `todayItems[0].icon` is `Sparkles`:

```ts
expect(todayItems.map((item) => item.label)).toEqual(['AI助手'])
expect(todayItems[0]?.icon).toBe(Sparkles)
expect(allNavItems.find((item) => item.to === '/')?.label).toBe('AI助手')
```

- [x] **Step 2: Write the failing sidebar presentation test**

Replace the loose homepage text assertion with an explicit accessible-link assertion and verify the redundant section label is gone:

```tsx
expect(screen.getByRole('link', { name: 'AI助手' })).toBeInTheDocument()
expect(screen.queryByText('首页')).not.toBeInTheDocument()
```

- [x] **Step 3: Run the targeted tests and verify the expected failures**

Run:

```bash
cd studio && bun run test -- src/lib/navigation.test.ts src/components/layout/Sidebar.test.tsx
```

Expected: failures report that the root label remains `首页`, the icon is not `Sparkles`, and no `AI助手` link exists.

- [x] **Step 4: Update the root navigation metadata**

In `studio/src/lib/navigation.ts`, replace the `LayoutDashboard` import and root item with:

```ts
import { Sparkles } from 'lucide-react'

export const todayItems: NavItem[] = [
  { to: '/', label: 'AI助手', icon: Sparkles, end: true },
]
```

- [x] **Step 5: Render the root item directly**

Remove `LayoutDashboard` from the `Sidebar.tsx` icon imports and replace the homepage `SidebarSection` with:

```tsx
<div className="mb-2">
  {todayItems.map((item) => (
    <SidebarNavLink
      key={item.to}
      item={item}
      collapsed={collapsed}
      onClick={() => setMobileOpen(false)}
    />
  ))}
</div>
```

- [x] **Step 6: Run targeted tests and verify they pass**

Run:

```bash
cd studio && bun run test -- src/lib/navigation.test.ts src/components/layout/Sidebar.test.tsx
```

Expected: both test files pass with no warnings or errors.

- [x] **Step 7: Run full Studio verification**

Run:

```bash
cd studio && bun run test
cd studio && bun run build
```

Expected: the full test suite and production build pass.

- [x] **Step 8: Verify the rendered sidebar**

Start Vite, open `/`, and verify the expanded and collapsed sidebar states. Confirm the visible first entry is `AI助手`, its icon is the star-shaped Sparkles glyph, the collapsed tooltip says `AI助手`, and navigation still resolves to `/` without console errors.

- [x] **Step 9: Commit the implementation**

```bash
git add docs/superpowers/plans/2026-07-12-studio-ai-assistant-nav.md \
  studio/src/lib/navigation.test.ts \
  studio/src/components/layout/Sidebar.test.tsx \
  studio/src/lib/navigation.ts \
  studio/src/components/layout/Sidebar.tsx
git commit -m "fix(studio): simplify AI assistant navigation"
```
