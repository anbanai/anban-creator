# Studio MVP Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Studio's labeled navigation groups with a flat MVP navigation containing AI助手、项目、任务、计划、钱包 while keeping administrator tools available without category labels.

**Architecture:** `studio/src/lib/navigation.ts` will expose one ordered MVP list and one administrator list; the shared `allNavItems` collection will contain only those visible navigation entries, so the sidebar and command palette stay consistent. `Sidebar.tsx` will render the MVP list directly and append administrator links after an unlabeled divider. Existing `/timeline` and `/usage` routes and pages remain intact but are removed from global navigation.

**Tech Stack:** React 19, TypeScript, React Router, lucide-react, Vitest, Testing Library, Bun, Vite

---

### Task 1: Define the MVP navigation contract

**Files:**
- Modify: `studio/src/lib/navigation.test.ts`
- Modify: `studio/src/lib/navigation.ts`

- [ ] **Step 1: Replace the grouped-navigation test with failing MVP contract tests**

Update `studio/src/lib/navigation.test.ts` to assert the exact visible collections:

```ts
import { describe, expect, it } from 'vitest'
import { Sparkles } from 'lucide-react'

import {
  adminNavItems,
  allNavItems,
  mvpNavItems,
  visibleNavItems,
} from './navigation'

describe('navigation IA', () => {
  it('keeps only the ordered MVP destinations in the primary navigation', () => {
    expect(mvpNavItems.map((item) => item.label)).toEqual([
      'AI助手',
      '项目',
      '任务',
      '计划',
      '钱包',
    ])
    expect(mvpNavItems[0]?.icon).toBe(Sparkles)
    expect(mvpNavItems.map((item) => item.to)).not.toEqual(
      expect.arrayContaining(['/timeline', '/usage']),
    )
  })

  it('keeps administrator tools separate from the MVP destinations', () => {
    expect(adminNavItems.map((item) => item.label)).toEqual([
      '设计师',
      '模板库',
      'Claude Code',
      'Codex',
      '设置',
    ])
    expect(adminNavItems.every((item) => item.adminOnly)).toBe(true)
  })

  it('filters the shared navigation collection by administrator access', () => {
    expect(visibleNavItems(allNavItems, false)).toEqual(mvpNavItems)
    expect(visibleNavItems(allNavItems, true)).toEqual([
      ...mvpNavItems,
      ...adminNavItems,
    ])
  })
})
```

- [ ] **Step 2: Run the navigation test and verify it fails**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test -- src/lib/navigation.test.ts
```

Expected: FAIL because `mvpNavItems` and `adminNavItems` are not exported.

- [ ] **Step 3: Replace outcome groups with explicit MVP and administrator lists**

Refactor `studio/src/lib/navigation.ts` so its exported collections are:

```ts
export const mvpNavItems: NavItem[] = [
  { to: '/', label: 'AI助手', icon: Sparkles, end: true },
  { to: '/projects', label: '项目', icon: Rss },
  { to: '/tasks', label: '任务', icon: ListChecks },
  { to: '/plans', label: '计划', icon: CalendarRange },
  { to: '/billing', label: '钱包', icon: Coins },
]

export const adminNavItems: NavItem[] = [
  { to: '/designer', label: '设计师', icon: Palette, adminOnly: true },
  { to: '/templates', label: '模板库', icon: LayoutGrid, adminOnly: true },
  { to: '/connect/claude-code', label: 'Claude Code', icon: Terminal, adminOnly: true },
  { to: '/connect/codex', label: 'Codex', icon: Boxes, adminOnly: true },
  { to: '/settings', label: '设置', icon: Settings, adminOnly: true },
]

export function visibleNavItems(items: NavItem[], isAdmin: boolean): NavItem[] {
  return items.filter((item) => !item.adminOnly || isAdmin)
}

export const allNavItems: NavItem[] = [
  ...mvpNavItems,
  ...adminNavItems,
]
```

Remove the unused `Clock` and `Activity` imports and the old `todayItems`, `creationItems`, `automationItems`, `assetItems`, `businessItems`, `platformItems`, `connectSettingItems`, `workflowItems`, and `analyticsItems` exports.

- [ ] **Step 4: Run the navigation test and verify it passes**

Run the Step 2 command again.

Expected: PASS with 3 tests.

- [ ] **Step 5: Commit the navigation contract**

```bash
git add studio/src/lib/navigation.ts studio/src/lib/navigation.test.ts
git commit -m "refactor(studio): define MVP navigation"
```

### Task 2: Flatten the sidebar and preserve administrator access

**Files:**
- Modify: `studio/src/components/layout/Sidebar.test.tsx`
- Modify: `studio/src/components/layout/Sidebar.tsx`

- [ ] **Step 1: Add failing sidebar assertions for the flat MVP list**

Replace the basic navigation test in `Sidebar.test.tsx` with:

```tsx
it('renders only the flat MVP navigation for regular users', () => {
  renderSidebar()

  const navigation = screen.getByRole('navigation', { name: '主导航' })
  expect(within(navigation).getAllByRole('link').map((link) => link.textContent)).toEqual([
    'AI助手',
    '项目',
    '任务',
    '计划',
    '钱包',
  ])
  expect(within(navigation).queryByText('创作')).not.toBeInTheDocument()
  expect(within(navigation).queryByText('自动化')).not.toBeInTheDocument()
  expect(within(navigation).queryByText('经营')).not.toBeInTheDocument()
  expect(within(navigation).queryByRole('link', { name: '时间轴' })).not.toBeInTheDocument()
  expect(within(navigation).queryByRole('link', { name: '用量' })).not.toBeInTheDocument()
})
```

Add `within` to the existing Testing Library import. Extend the administrator test to assert that all five administrator links remain present and that “平台与设置” and “资产” are absent.

- [ ] **Step 2: Run the sidebar test and verify it fails**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test -- src/components/layout/Sidebar.test.tsx
```

Expected: FAIL because the sidebar still renders grouped headings plus 时间轴 and 用量.

- [ ] **Step 3: Render the MVP and administrator collections as flat lists**

In `Sidebar.tsx`, import only `mvpNavItems` and `adminNavItems` from `@/lib/navigation`. Replace all `SidebarSection` calls with:

```tsx
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
```

Delete `SidebarSection` and remove its now-unused `Workflow`, `BarChart3`, `CalendarRange`, `Boxes`, and `Settings` imports. Change the `SidebarNavLink` icon type from `typeof Workflow` to `LucideIcon`, imported as a type from `lucide-react`.

- [ ] **Step 4: Run the sidebar and navigation tests**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test -- src/components/layout/Sidebar.test.tsx src/lib/navigation.test.ts
```

Expected: PASS for both files, including collapsed, mobile, settings, and command-palette integration tests.

- [ ] **Step 5: Commit the flat sidebar**

```bash
git add studio/src/components/layout/Sidebar.tsx studio/src/components/layout/Sidebar.test.tsx
git commit -m "refactor(studio): flatten MVP sidebar"
```

### Task 3: Verify shared navigation and responsive rendering

**Files:**
- Verify: `studio/src/components/GlobalCommandPalette.tsx`
- Verify: `studio/src/App.tsx`

- [ ] **Step 1: Run command-palette and route protection tests**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test -- src/components/layout/Sidebar.test.tsx src/lib/navigation.test.ts src/App.admin-routes.test.ts
```

Expected: PASS; the command palette receives the same five MVP destinations for regular users, and administrator routes remain protected.

- [ ] **Step 2: Run the complete Studio test suite**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test
```

Expected: all Studio test files and tests PASS.

- [ ] **Step 3: Build the production Studio bundle**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run build
```

Expected: `tsc -b && vite build` exits 0.

- [ ] **Step 4: Run browser QA at desktop and mobile sizes**

Start the Studio development server, open the authenticated shell with the Browser plugin, and verify at `1440x900` and `390x844` that:

- the visible primary order is AI助手、项目、任务、计划、钱包;
- no 创作、自动化、经营、时间轴、用量 labels appear in the sidebar;
- administrator links appear after an unlabeled divider only for an administrator;
- the sidebar has no horizontal overflow, clipped labels, overlapping controls, or empty group spacing;
- the mobile drawer closes after selecting a destination;
- the browser console has no new errors.

- [ ] **Step 5: Check the final diff**

Run:

```bash
git diff --check HEAD~2..HEAD
git status --short
```

Expected: no whitespace errors; only the two pre-existing untracked plan files remain outside the committed implementation.
