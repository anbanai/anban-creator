# Studio Admin-Only AI Assistant Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Studio AI助手 navigation item and `/` dashboard route administrator-only while sending regular users to `/projects`.

**Architecture:** `studio/src/lib/navigation.ts` remains the authoritative navigation source: AI助手 moves from the MVP collection into the administrator collection, automatically updating Sidebar and GlobalCommandPalette visibility. `AdminRoute` changes its safe fallback to `/projects`, and the index route wraps `DashboardPage` with that guard. Login and registration keep their existing default navigation to `/`; the index guard performs the role-aware split without duplicating permission logic.

**Tech Stack:** React 19, TypeScript, React Router, lucide-react, Vitest, Testing Library, Bun, Vite

---

### Task 1: Move AI助手 into administrator navigation

**Files:**
- Modify: `studio/src/lib/navigation.test.ts`
- Modify: `studio/src/components/layout/Sidebar.test.tsx`
- Modify: `studio/src/components/GlobalCommandPalette.actions.test.tsx`
- Modify: `studio/src/lib/navigation.ts`

- [ ] **Step 1: Write failing navigation visibility tests**

Update `studio/src/lib/navigation.test.ts` so the approved collections are exact:

```ts
describe('navigation IA', () => {
  it('defines the regular-user MVP navigation in approved order', () => {
    expect(mvpNavItems.map((item) => item.label)).toEqual([
      '项目',
      '任务',
      '计划',
      '钱包',
    ])
    expect(mvpNavItems.map((item) => item.to)).toEqual([
      '/projects',
      '/tasks',
      '/plans',
      '/billing',
    ])
  })

  it('defines administrator navigation in approved order', () => {
    expect(adminNavItems.map((item) => item.label)).toEqual([
      'AI助手',
      '设计师',
      '模板库',
      'Claude Code',
      'Codex',
      '设置',
    ])
    expect(adminNavItems[0]).toMatchObject({
      to: '/',
      icon: Sparkles,
      end: true,
      adminOnly: true,
    })
    expect(adminNavItems.every((item) => item.adminOnly)).toBe(true)
  })

  it('filters the combined navigation by administrator access', () => {
    expect(visibleNavItems(allNavItems, false)).toEqual(mvpNavItems)
    expect(visibleNavItems(allNavItems, true)).toEqual([
      ...mvpNavItems,
      ...adminNavItems,
    ])
  })
})
```

Change the regular-user Sidebar expectation in `Sidebar.test.tsx` to:

```ts
expect(navigation.getAllByRole('link').map((link) => link.textContent)).toEqual([
  '项目',
  '任务',
  '计划',
  '钱包',
])
expect(navigation.queryByRole('link', { name: 'AI助手' })).not.toBeInTheDocument()
```

Extend the administrator Sidebar test with:

```ts
expect(screen.getByRole('link', { name: 'AI助手' })).toBeInTheDocument()
```

Extend `GlobalCommandPalette.actions.test.tsx` so the regular-user half asserts AI助手 is absent and the administrator half asserts it is present:

```ts
expect(screen.queryByText('AI助手')).not.toBeInTheDocument()
// after rendering as administrator
expect(await screen.findByText('AI助手')).toBeInTheDocument()
```

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test -- src/lib/navigation.test.ts src/components/layout/Sidebar.test.tsx src/components/GlobalCommandPalette.actions.test.tsx
```

Expected: FAIL because AI助手 is still in `mvpNavItems`, visible to regular users, and absent from `adminNavItems`.

- [ ] **Step 3: Move the existing navigation item into the administrator collection**

Change `studio/src/lib/navigation.ts` to:

```ts
export const mvpNavItems: NavItem[] = [
  { to: '/projects', label: '项目', icon: Rss },
  { to: '/tasks', label: '任务', icon: ListChecks },
  { to: '/plans', label: '计划', icon: CalendarRange },
  { to: '/billing', label: '钱包', icon: Coins },
]

export const adminNavItems: NavItem[] = [
  { to: '/', label: 'AI助手', icon: Sparkles, end: true, adminOnly: true },
  { to: '/designer', label: '设计师', icon: Palette, adminOnly: true },
  { to: '/templates', label: '模板库', icon: LayoutGrid, adminOnly: true },
  { to: '/connect/claude-code', label: 'Claude Code', icon: Terminal, adminOnly: true },
  { to: '/connect/codex', label: 'Codex', icon: Boxes, adminOnly: true },
  { to: '/settings', label: '设置', icon: Settings, adminOnly: true },
]
```

Keep `allNavItems`, `visibleNavItems`, Sidebar rendering, and command-palette filtering unchanged.

- [ ] **Step 4: Run tests and verify GREEN**

Run the Step 2 command again.

Expected: all three test files PASS.

- [ ] **Step 5: Commit navigation authorization**

```bash
git add studio/src/lib/navigation.ts studio/src/lib/navigation.test.ts studio/src/components/layout/Sidebar.test.tsx studio/src/components/GlobalCommandPalette.actions.test.tsx
git commit -m "fix(studio): restrict AI assistant navigation to admins"
```

### Task 2: Protect the dashboard route and redirect regular users safely

**Files:**
- Modify: `studio/src/components/auth/AdminRoute.test.tsx`
- Modify: `studio/src/components/auth/AdminRoute.tsx`
- Modify: `studio/src/App.admin-routes.test.ts`
- Modify: `studio/src/App.tsx`

- [ ] **Step 1: Write failing AdminRoute fallback tests**

Change the test auth state in `AdminRoute.test.tsx` to support an unresolved permission:

```ts
const authState = vi.hoisted(() => ({ isAdmin: false as boolean | undefined }))
```

Render `/projects` as the fallback destination:

```tsx
function renderRoute() {
  return render(
    <MemoryRouter initialEntries={['/templates']}>
      <Routes>
        <Route path="/projects" element={<h1>项目</h1>} />
        <Route
          path="/templates"
          element={<AdminRoute><h1>模板管理</h1></AdminRoute>}
        />
      </Routes>
    </MemoryRouter>,
  )
}
```

Replace and extend the behavior tests:

```tsx
it('redirects regular users to projects', async () => {
  renderRoute()
  expect(await screen.findByRole('heading', { name: '项目' })).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: '模板管理' })).not.toBeInTheDocument()
})

it('allows administrators to access protected content', () => {
  authState.isAdmin = true
  renderRoute()
  expect(screen.getByRole('heading', { name: '模板管理' })).toBeInTheDocument()
})

it('waits for a complete administrator permission field', () => {
  authState.isAdmin = undefined
  renderRoute()
  expect(screen.queryByRole('heading')).not.toBeInTheDocument()
})
```

- [ ] **Step 2: Write a failing root-route protection contract**

Add a helper and assertion to `App.admin-routes.test.ts`:

```ts
function indexRouteDefinition(): string {
  const start = source.indexOf('<Route index')
  const end = source.indexOf('<Route path="timeline"', start)
  return source.slice(start, end)
}

it('protects the AI assistant index route with AdminRoute', () => {
  const definition = indexRouteDefinition()
  expect(definition).toContain('<AdminRoute>')
  expect(definition).toContain('component={DashboardPage}')
})
```

- [ ] **Step 3: Run route tests and verify RED**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test -- src/components/auth/AdminRoute.test.tsx src/App.admin-routes.test.ts
```

Expected: FAIL because regular users still redirect to `/` and the index route is not wrapped by `AdminRoute`.

- [ ] **Step 4: Change the guard fallback and protect the index route**

Change `AdminRoute.tsx` to:

```tsx
export default function AdminRoute({ children }: { children: ReactNode }) {
  const { user } = useAuth()

  if (user && typeof user.is_admin !== 'boolean') return null
  if (!user?.is_admin) return <Navigate to="/projects" replace />
  return <>{children}</>
}
```

Change the index route in `App.tsx` to:

```tsx
<Route
  index
  element={(
    <AdminRoute>
      <LazyPage component={DashboardPage} />
    </AdminRoute>
  )}
/>
```

Do not change `LoginPage`, `RegisterPage`, `PublicRoute`, or any Server behavior.

- [ ] **Step 5: Run route tests and the combined authorization suite**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test -- src/components/auth/AdminRoute.test.tsx src/App.admin-routes.test.ts src/lib/navigation.test.ts src/components/layout/Sidebar.test.tsx src/components/GlobalCommandPalette.actions.test.tsx src/contexts/AuthContext.test.tsx
```

Expected: all six test files PASS, including cached-user permission refresh behavior.

- [ ] **Step 6: Commit route authorization**

```bash
git add studio/src/components/auth/AdminRoute.tsx studio/src/components/auth/AdminRoute.test.tsx studio/src/App.tsx studio/src/App.admin-routes.test.ts
git commit -m "fix(studio): protect AI assistant route"
```

### Task 3: Verify authorization and responsive behavior

**Files:**
- Verify: `studio/src/pages/LoginPage.tsx`
- Verify: `studio/src/pages/RegisterPage.tsx`
- Verify: `studio/src/components/GlobalCommandPalette.tsx`
- Verify: `studio/src/components/layout/Sidebar.tsx`

- [ ] **Step 1: Run the complete Studio test suite**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test
```

Expected: all Studio test files and tests PASS.

- [ ] **Step 2: Build the production Studio bundle**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run build
```

Expected: `tsc -b && vite build` exits 0.

- [ ] **Step 3: Verify regular-user browser behavior**

Use the Browser plugin with a local-only API stub and a fictional regular user. At `1440x900` and `390x844`, verify:

- login's default `/` navigation settles on `/projects`;
- Sidebar and command palette contain 项目、任务、计划、钱包 but not AI助手;
- direct navigation to `/` and `/settings` settles on `/projects`;
- mobile drawer closes after choosing a regular destination;
- no horizontal overflow, overlapping controls, framework overlay, or console errors.

- [ ] **Step 4: Verify administrator browser behavior**

Use the same local-only stub with `is_admin: true`. At desktop and mobile sizes, verify:

- login's default `/` navigation renders the AI助手 dashboard;
- Sidebar and command palette contain AI助手 plus all administrator destinations;
- AI助手 appears after the unlabeled administrator divider;
- direct navigation to `/` remains on `/`;
- no horizontal overflow, overlapping controls, framework overlay, or console errors.

- [ ] **Step 5: Check final Git hygiene**

Run:

```bash
git diff --check HEAD~2..HEAD
git status --short --branch
```

Expected: no whitespace errors; only the two pre-existing untracked plan files remain outside the committed implementation.
