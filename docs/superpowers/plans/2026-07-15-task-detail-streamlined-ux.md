# Task Detail Streamlined UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the task detail page's stacked metadata/configuration/material/log panels with a result-first workbench and one responsive details sheet.

**Architecture:** `TaskDetailPage` keeps queries, mutations, state-specific actions, and specialized result surfaces. A new `TaskDetailsSheet` owns the Overview, Configuration, Materials, and Logs secondary views, while `TaskConfigurationDetails` owns task/project snapshot rendering and `ReferenceUsageSummary` gains a compact sheet variant without changing its parsing contract.

**Tech Stack:** React 19, TypeScript, Base UI-backed shadcn Sheet/Tabs, TanStack Query, Vitest, Testing Library, Tailwind CSS v4, Bun, Vite 8.

---

## File Map

- Create `studio/src/components/tasks/TaskDetailsSheet.tsx`: responsive sheet shell, tabs, overview, log controls, and composition of configuration/material views.
- Create `studio/src/components/tasks/TaskDetailsSheet.test.tsx`: focused interaction tests for opening, tab navigation, logs, credits, and task changes.
- Create `studio/src/components/tasks/TaskConfigurationDetails.tsx`: immutable project/task snapshot plus video input/resolved configuration.
- Modify `studio/src/components/tasks/ReferenceUsageSummary.tsx`: add `variant="compact"` and compact loading/fallback/valid-summary presentation.
- Modify `studio/src/components/tasks/ReferenceUsageSummary.test.tsx`: prove the compact missing/malformed summary states stay honest and low-noise.
- Modify `studio/src/pages/TaskDetailPage.tsx`: remove auxiliary panels from the main scroll flow, add a compact result destination, add the More details trigger, and wire the sheet.
- Modify `studio/src/pages/TaskDetailPage.test.tsx`: update ordering assertions and protect existing primary actions/result workflows.

## Task 1: Add Compact Reference Material Presentation

**Files:**
- Modify: `studio/src/components/tasks/ReferenceUsageSummary.tsx`
- Test: `studio/src/components/tasks/ReferenceUsageSummary.test.tsx`

- [ ] **Step 1: Write failing compact-fallback tests**

Add tests that request the compact variant and assert the approved copy and removed visual noise:

```tsx
it('renders a compact first-input fallback without warning-card copy', () => {
  render(
    <ReferenceUsageSummary
      variant="compact"
      task={{
        ...seednoteTask,
        input_attachments: [{
          type: 'image',
          file_name: 'product-front.png',
          url: 'https://cdn.test/product-front.png',
          instruction: '保留瓶身标签',
        }],
      }}
      files={[]}
    />,
  )

  expect(screen.getByText('未生成素材使用结论，仅展示任务输入。')).toBeInTheDocument()
  expect(screen.getByText('product-front.png')).toBeInTheDocument()
  expect(screen.queryByText('仅显示输入快照')).not.toBeInTheDocument()
  expect(screen.queryByText('输入快照不代表 AI 实际使用结论')).not.toBeInTheDocument()
})

it('renders an honest compact parse failure with the intact input snapshot', async () => {
  vi.mocked(api.tasks.downloadFileBlob).mockResolvedValue(
    new Blob([JSON.stringify({ version: '1.0', inputs: 'invalid', outputs: [] })]),
  )

  render(
    <ReferenceUsageSummary
      variant="compact"
      task={{
        ...seednoteTask,
        input_attachments: [{ type: 'image', file_name: 'plan-product.png' }],
      }}
      files={[summaryTaskFile]}
    />,
  )

  expect(await screen.findByText('素材使用结论无法解析，仅展示任务输入。')).toBeInTheDocument()
  expect(screen.getByText('plan-product.png')).toBeInTheDocument()
})
```

- [ ] **Step 2: Run the tests and verify RED**

Run:

```bash
cd studio && bun run test -- src/components/tasks/ReferenceUsageSummary.test.tsx
```

Expected: FAIL because `variant` is not a valid prop and compact copy is absent.

- [ ] **Step 3: Implement the compact variant without duplicating parsing**

Extend the public prop and thread `compact` through the existing render branches:

```tsx
interface ReferenceUsageSummaryProps {
  task: Task
  files: TaskFile[]
  variant?: 'card' | 'compact'
}

export function ReferenceUsageSummary({
  task,
  files,
  variant = 'card',
}: ReferenceUsageSummaryProps) {
  // Keep the existing summary-file lookup, download, schema validation,
  // cancellation guard, and task/file identity checks unchanged.
  const compact = variant === 'compact'

  if (!summaryFile) {
    if (!(task.input_attachments?.length)) {
      return compact
        ? <p className="py-8 text-center text-sm text-muted-foreground">没有参考素材。</p>
        : null
    }
    return <SnapshotFallback task={task} parseFailed={false} compact={compact} />
  }

  const isCurrent = loaded?.taskId === task.id && loaded.fileId === summaryFile.id
  if (!isCurrent) return <SummaryLoading compact={compact} />
  if (loaded.summary) return <ValidSummary summary={loaded.summary} compact={compact} />
  return <SnapshotFallback task={task} parseFailed={loaded.parseFailed} compact={compact} />
}
```

For compact fallback content, render an open list with no outer `Card`, warning `Alert`, or disclaimer row:

```tsx
function SnapshotFallback({ task, parseFailed, compact }: {
  task: Task
  parseFailed: boolean
  compact: boolean
}) {
  const attachments = task.input_attachments ?? []
  const compactMessage = parseFailed
    ? '素材使用结论无法解析，仅展示任务输入。'
    : '未生成素材使用结论，仅展示任务输入。'

  if (compact) {
    return (
      <div className="space-y-3">
        <p className="text-xs text-muted-foreground">{compactMessage}</p>
        {attachments.length > 0 ? (
          <div className="divide-y divide-border border-y border-border">
            {attachments.map((attachment, index) => (
              <SnapshotAttachment
                compact
                key={`${attachment.upload_id || attachment.url || attachment.file_name || index}-${index}`}
                attachment={attachment}
                index={index}
              />
            ))}
          </div>
        ) : (
          <p className="py-8 text-center text-sm text-muted-foreground">没有参考素材。</p>
        )}
      </div>
    )
  }

  return <SnapshotFallbackCard task={task} parseFailed={parseFailed} />
}
```

Extract the current non-compact JSX from `SnapshotFallback` into `SnapshotFallbackCard` without changing its copy or structure. Use `compact ? 'py-3' : 'rounded-lg border border-border/70 bg-background/70 p-3'` for each snapshot attachment. For a valid compact summary, keep all decisions, warnings, verification states, and provider/model data, but remove the outer `Card` header and use one vertical `space-y-5` container suitable for the `576px` sheet.

- [ ] **Step 4: Run the focused tests and verify GREEN**

Run:

```bash
cd studio && bun run test -- src/components/tasks/ReferenceUsageSummary.test.tsx
```

Expected: all `ReferenceUsageSummary` tests PASS, including the existing card variant tests.

- [ ] **Step 5: Commit the compact material variant**

```bash
git add studio/src/components/tasks/ReferenceUsageSummary.tsx studio/src/components/tasks/ReferenceUsageSummary.test.tsx
git commit -m "refactor(studio): compact task reference details"
```

## Task 2: Build the Task Details Sheet

**Files:**
- Create: `studio/src/components/tasks/TaskConfigurationDetails.tsx`
- Create: `studio/src/components/tasks/TaskDetailsSheet.tsx`
- Create: `studio/src/components/tasks/TaskDetailsSheet.test.tsx`

- [ ] **Step 1: Write failing sheet interaction tests**

Create a focused fixture with an article project snapshot, one input attachment, and Markdown logs. Test the public contract:

```tsx
render(
  <TaskDetailsSheet
    open
    onOpenChange={onOpenChange}
    task={task}
    project={project}
    files={[]}
    netConsumedCredits={188}
    showCreditDetails
    onOpenCreditDetails={onOpenCreditDetails}
    logs={['## 阶段日志', '- 已完成选题']}
    sseError={null}
    autoScrollLogs
    onToggleAutoScroll={onToggleAutoScroll}
    onCopyLogs={onCopyLogs}
    onReconnectLogs={onReconnectLogs}
    logContainerRef={{ current: null }}
  />,
)

expect(screen.getByRole('heading', { name: '任务详情' })).toBeInTheDocument()
expect(screen.getByText('手动创建')).toBeInTheDocument()
expect(screen.getByText('188')).toBeInTheDocument()

fireEvent.click(screen.getByRole('tab', { name: '配置' }))
expect(screen.getByText('柔光生活摄影')).toBeInTheDocument()

fireEvent.click(screen.getByRole('tab', { name: '素材' }))
expect(screen.getByText('未生成素材使用结论，仅展示任务输入。')).toBeInTheDocument()

fireEvent.click(screen.getByRole('tab', { name: '日志' }))
expect(screen.getByRole('heading', { name: '阶段日志' })).toBeInTheDocument()
fireEvent.click(screen.getByRole('button', { name: '复制日志' }))
expect(onCopyLogs).toHaveBeenCalledTimes(1)
```

Add a rerender test that selects Logs, changes `task.id`, and asserts Overview is selected again.

- [ ] **Step 2: Run the new test and verify RED**

Run:

```bash
cd studio && bun run test -- src/components/tasks/TaskDetailsSheet.test.tsx
```

Expected: FAIL because the sheet modules do not exist.

- [ ] **Step 3: Implement focused configuration rendering**

Create `TaskConfigurationDetails` with this interface:

```tsx
interface TaskConfigurationDetailsProps {
  task: Task
  project?: Project
}

export function TaskConfigurationDetails({ task, project }: TaskConfigurationDetailsProps) {
  const snapshot = task.project_snapshot
  const visualStyle = snapshot?.visual_style || project?.visual_style || '—'
  const imageRatio = snapshot?.image_ratio || project?.image_ratio || task.image_ratio || '—'
  const imageModel = task.image_model_key
    || snapshot?.ecommerce_defaults?.image_model_key
    || project?.ecommerce_defaults?.image_model_key
    || '—'
  const videoInput = isVideoCreator(task.type)
    ? task.video_creator_input
    : isVideoEditor(task.type)
      ? task.video_editor_input
      : undefined
  const videoConfig = isVideoCreator(task.type)
    ? task.video_creator_config
    : isVideoEditor(task.type)
      ? task.video_editor_config
      : undefined

  return (
    <div className="space-y-6">
      <section aria-labelledby="task-project-snapshot" className="space-y-3">
        <h3 id="task-project-snapshot" className="text-sm font-semibold">项目快照</h3>
        <dl className="grid gap-x-4 gap-y-3 sm:grid-cols-2">
          <Detail label="项目" value={snapshot?.project_name || project?.name || '—'} />
          <Detail label="内容类型" value={contentTypeLabel[snapshot?.platform || project?.platform || task.type] || task.type} />
          <Detail label="视觉风格" value={visualStyle} wide />
          <Detail label="图片比例" value={imageRatio} />
          <Detail label="图片模型" value={imageModel} />
        </dl>
      </section>
      {task.type === 'article' ? <ArticleSnapshot task={task} project={project} /> : null}
      {task.type === 'ecommerce' ? <EcommerceSnapshot task={task} project={project} /> : null}
      {isVideoPlatform(task.type) ? (
        <VideoSnapshot input={videoInput} config={videoConfig} task={task} />
      ) : null}
    </div>
  )
}
```

Define the helpers used above in the same module:

```tsx
function Detail({ label, value, wide = false }: {
  label: string
  value: string
  wide?: boolean
}) {
  return (
    <div className={wide ? 'sm:col-span-2' : undefined}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 break-words text-sm text-foreground">{value}</dd>
    </div>
  )
}

function ArticleSnapshot({ task, project }: TaskConfigurationDetailsProps) {
  const snapshot = task.project_snapshot
  return (
    <section aria-labelledby="article-task-snapshot" className="space-y-3 border-t border-border pt-5">
      <h3 id="article-task-snapshot" className="text-sm font-semibold">文章配置</h3>
      <dl className="grid gap-3 sm:grid-cols-2">
        <Detail label="作者" value={snapshot?.author || project?.author || '—'} />
        <Detail label="写作风格" value={snapshot?.writer || project?.writer || '默认'} />
        <Detail label="排版主题" value={snapshot?.theme || project?.theme || '默认'} />
      </dl>
    </section>
  )
}

function EcommerceSnapshot({ task, project }: TaskConfigurationDetailsProps) {
  const defaults = task.project_snapshot?.ecommerce_defaults || project?.ecommerce_defaults
  if (!defaults) return null
  const modules = Object.entries(defaults.default_selected_modules || {})
    .map(([key, quantity]) => `${key} x${quantity}`)
    .join('、') || '—'
  return (
    <section aria-labelledby="ecommerce-task-snapshot" className="space-y-3 border-t border-border pt-5">
      <h3 id="ecommerce-task-snapshot" className="text-sm font-semibold">电商配置</h3>
      <dl className="grid gap-3 sm:grid-cols-2">
        <Detail label="目标平台" value={defaults.target_platform || '—'} />
        <Detail label="默认模块" value={modules} />
        <Detail label="品牌简述" value={defaults.brand_brief || '—'} wide />
      </dl>
    </section>
  )
}

function VideoSnapshot({
  task,
  input,
  config,
}: {
  task: Task
  input?: Task['video_creator_input']
  config?: Task['video_creator_config']
}) {
  const references = input?.references ?? []
  const constraints = input?.hard_constraints
  const resolvedReferences = config?.references ?? []
  const resolvedDuration = config?.target_duration_seconds
    || config?.pricing_breakdown?.output_seconds
    || config?.duration

  return (
    <div className="space-y-5 border-t border-border pt-5">
      <section aria-labelledby="video-user-input" className="space-y-3">
        <h3 id="video-user-input" className="text-sm font-semibold">用户输入</h3>
        <p className="whitespace-pre-wrap text-sm leading-6 text-foreground">{input?.brief?.trim() || task.prompt || '—'}</p>
        <dl className="grid gap-3 sm:grid-cols-3">
          <Detail label="比例硬约束" value={constraints?.ratio || '由 Agent 判断'} />
          <Detail label="时长硬约束" value={constraints?.duration ? `${constraints.duration}s` : '由 Agent 判断'} />
          <Detail label="水印硬约束" value={typeof constraints?.watermark === 'boolean' ? (constraints.watermark ? '加水印' : '不加水印') : '由 Agent 判断'} />
        </dl>
        <div>
          <p className="text-xs text-muted-foreground">参考素材</p>
          {references.length > 0 ? (
            <div className="mt-2 divide-y divide-border border-y border-border">
              {references.map((reference, index) => (
                <p key={`${reference.type}-${reference.url || reference.text}-${index}`} className="break-words py-2 text-xs">
                  {reference.reference_role || '由 Agent 判断'} · {reference.file_name || reference.text || reference.url || '—'}
                </p>
              ))}
            </div>
          ) : <p className="mt-1 text-xs text-muted-foreground">未提供参考素材</p>}
        </div>
      </section>
      {config ? (
        <section aria-labelledby="video-resolved-config" className="space-y-3 border-t border-border pt-5">
          <h3 id="video-resolved-config" className="text-sm font-semibold">Agent 解析结果</h3>
          <dl className="grid gap-3 sm:grid-cols-2">
            <Detail label="视频模型" value={videoModelDisplayName(config.model_key || config.model) || '—'} />
            <Detail label="规格" value={[config.resolution || '—', config.ratio || '—', resolvedDuration ? `${resolvedDuration}s` : '—'].join(' · ')} />
            <Detail label="创意类型" value={videoCreativeTypeLabel(config.creative_type)} />
            <Detail label="商业目标" value={videoPurposeLabel(config.purpose)} />
            <Detail label="人物 / 主体" value={config.subject_profile?.trim() || '—'} wide />
            <Detail label="目标受众" value={config.audience?.trim() || '—'} wide />
            <Detail label="核心信息" value={config.single_message?.trim() || '—'} wide />
            <Detail label="估算积分" value={(task.video_estimated_credits || config.estimated_credits || 0).toLocaleString()} />
            <Detail label="积分消耗" value={(task.video_credits_charged || 0).toLocaleString()} />
          </dl>
          {resolvedReferences.length > 0 ? (
            <div>
              <p className="text-xs text-muted-foreground">解析后的参考素材</p>
              <div className="mt-2 divide-y divide-border border-y border-border">
                {resolvedReferences.map((reference, index) => (
                  <p key={`${reference.type}-${reference.url || reference.text}-${index}`} className="break-words py-2 text-xs">
                    {reference.reference_role || reference.type} · {reference.file_name || reference.text || reference.url || '—'}
                  </p>
                ))}
              </div>
            </div>
          ) : null}
        </section>
      ) : null}
    </div>
  )
}
```

- [ ] **Step 4: Implement the responsive sheet shell**

Create the typed sheet contract and tab structure:

```tsx
export interface TaskDetailsSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  task: Task
  project?: Project
  files: TaskFile[]
  netConsumedCredits: number
  showCreditDetails: boolean
  onOpenCreditDetails: () => void
  logs: string[]
  sseError: string | null
  autoScrollLogs: boolean
  onToggleAutoScroll: () => void
  onCopyLogs: () => void
  onReconnectLogs: () => void
  logContainerRef: RefObject<HTMLDivElement | null>
}

export function TaskDetailsSheet(props: TaskDetailsSheetProps) {
  const [tab, setTab] = useState('overview')

  useEffect(() => {
    setTab('overview')
  }, [props.task.id])

  const logMarkdown = props.logs.join('  \n')

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent side="right" className="w-full gap-0 p-0 sm:max-w-xl">
        <SheetHeader className="shrink-0 border-b px-4 py-3 pr-12">
          <SheetTitle>任务详情</SheetTitle>
          <SheetDescription className="sr-only">任务概览、配置、参考素材和执行日志</SheetDescription>
        </SheetHeader>
        <Tabs value={tab} onValueChange={setTab} className="min-h-0 flex-1 gap-0">
          <TabsList variant="line" className="w-full shrink-0 justify-start overflow-x-auto border-b px-4 py-2">
            <TabsTrigger value="overview">概览</TabsTrigger>
            <TabsTrigger value="configuration">配置</TabsTrigger>
            <TabsTrigger value="materials">素材</TabsTrigger>
            <TabsTrigger value="logs">日志</TabsTrigger>
          </TabsList>
          <TabsContent value="overview" className="overflow-y-auto p-4">
            <TaskOverviewDetails
              task={props.task}
              project={props.project}
              netConsumedCredits={props.netConsumedCredits}
              showCreditDetails={props.showCreditDetails}
              onOpenCreditDetails={props.onOpenCreditDetails}
            />
          </TabsContent>
          <TabsContent value="configuration" className="overflow-y-auto p-4">
            <TaskConfigurationDetails task={props.task} project={props.project} />
          </TabsContent>
          <TabsContent value="materials" className="overflow-y-auto p-4">
            <ErrorBoundary fallback={<CompactMaterialError />}>
              <ReferenceUsageSummary variant="compact" task={props.task} files={props.files} />
            </ErrorBoundary>
          </TabsContent>
          <TabsContent value="logs" className="min-h-0 overflow-y-auto p-4">
            <TaskLogDetails
              logs={props.logs}
              logMarkdown={logMarkdown}
              sseError={props.sseError}
              autoScrollLogs={props.autoScrollLogs}
              onToggleAutoScroll={props.onToggleAutoScroll}
              onCopyLogs={props.onCopyLogs}
              onReconnectLogs={props.onReconnectLogs}
              logContainerRef={props.logContainerRef}
            />
          </TabsContent>
        </Tabs>
      </SheetContent>
    </Sheet>
  )
}
```

Implement the three focused internal views with these concrete contracts:

```tsx
function TaskOverviewDetails({
  task,
  project,
  netConsumedCredits,
  showCreditDetails,
  onOpenCreditDetails,
}: {
  task: Task
  project?: Project
  netConsumedCredits: number
  showCreditDetails: boolean
  onOpenCreditDetails: () => void
}) {
  const rows = [
    ['创建时间', formatFullDateTimeCN(task.created_at)],
    ['开始时间', formatFullDateTimeCN(task.started_at)],
    ['完成时间', formatFullDateTimeCN(task.completed_at)],
    ['来源', task.plan_id ? '计划任务' : '手动创建'],
    ['项目', task.project_snapshot?.project_name || project?.name || '—'],
  ]

  return (
    <div className="space-y-4">
      <dl className="divide-y divide-border border-y border-border">
        {rows.map(([label, value]) => (
          <div key={label} className="grid grid-cols-[6rem_1fr] gap-3 py-3">
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="min-w-0 break-words text-sm text-foreground">{value}</dd>
          </div>
        ))}
      </dl>
      {showCreditDetails ? (
        <div className="flex items-center justify-between gap-3 border-b border-border pb-3">
          <div>
            <p className="text-xs text-muted-foreground">积分消耗</p>
            <p className="mt-1 text-sm font-medium tabular-nums">{netConsumedCredits.toLocaleString()}</p>
          </div>
          <Button size="sm" variant="ghost" onClick={onOpenCreditDetails}>查看明细</Button>
        </div>
      ) : null}
    </div>
  )
}

function CompactMaterialError() {
  return (
    <div className="flex items-start gap-3 py-3">
      <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
      <div>
        <p className="text-sm font-medium text-foreground">参考素材摘要暂时无法显示</p>
        <p className="mt-1 text-xs text-muted-foreground">任务状态与生成文件不受影响，可稍后重试。</p>
      </div>
    </div>
  )
}

function TaskLogDetails({
  logs,
  logMarkdown,
  sseError,
  autoScrollLogs,
  onToggleAutoScroll,
  onCopyLogs,
  onReconnectLogs,
  logContainerRef,
}: {
  logs: string[]
  logMarkdown: string
  sseError: string | null
  autoScrollLogs: boolean
  onToggleAutoScroll: () => void
  onCopyLogs: () => void
  onReconnectLogs: () => void
  logContainerRef: RefObject<HTMLDivElement | null>
}) {
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-2">
        <span className="text-xs text-muted-foreground">{logs.length} 条</span>
        <div className="flex items-center gap-1">
          <Button size="xs" variant="ghost" onClick={onToggleAutoScroll}>
            {autoScrollLogs ? '跟随输出' : '暂停跟随'}
          </Button>
          <Button
            size="xs"
            variant="ghost"
            aria-label="复制日志"
            disabled={logs.length === 0}
            onClick={onCopyLogs}
          >
            <Copy className="size-3.5" />复制
          </Button>
        </div>
      </div>
      <div ref={logContainerRef} className="min-h-40 overflow-y-auto border-y border-border py-3">
        {sseError ? (
          <div className="mb-3 flex items-center justify-between gap-2 text-xs text-amber-600">
            <span>{sseError}</span>
            <Button size="xs" variant="ghost" onClick={onReconnectLogs}>重新连接</Button>
          </div>
        ) : null}
        {logs.length === 0 ? (
          <p className="py-8 text-center text-xs text-muted-foreground">等待输出中...</p>
        ) : (
          <Streamdown mode="streaming" className="prose prose-sm max-w-none dark:prose-invert">
            {logMarkdown}
          </Streamdown>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 5: Run the sheet tests and verify GREEN**

Run:

```bash
cd studio && bun run test -- src/components/tasks/TaskDetailsSheet.test.tsx src/components/tasks/ReferenceUsageSummary.test.tsx
```

Expected: all sheet and reference tests PASS.

- [ ] **Step 6: Commit the sheet components**

```bash
git add studio/src/components/tasks/TaskConfigurationDetails.tsx studio/src/components/tasks/TaskDetailsSheet.tsx studio/src/components/tasks/TaskDetailsSheet.test.tsx
git commit -m "feat(studio): add task details sheet"
```

## Task 3: Integrate the Result-First Task Page

**Files:**
- Modify: `studio/src/pages/TaskDetailPage.tsx`
- Modify: `studio/src/pages/TaskDetailPage.test.tsx`

- [ ] **Step 1: Update page tests to describe the approved hierarchy**

Replace assertions that expect Task information, Task configuration, reference usage, or logs directly on the page. Test the new trigger and sheet integration:

```tsx
it('keeps progress and result destination on the page and moves auxiliary details into the sheet', async () => {
  mockTask(taskWith({
    status: 'running',
    progress: 42,
    progress_log: '## 阶段日志\n- 已完成选题',
    latest_progress: { stage: 'writing', title: '正在写作正文', percent: 42 },
    input_attachments: [{ type: 'image', file_name: 'front.png' }],
    result: { files: null, output: '' },
    completed_at: '',
  }))

  render(<TaskDetailPage />)

  expect(await screen.findByText('正在写作正文')).toBeInTheDocument()
  expect(screen.getByText('任务结果')).toBeInTheDocument()
  expect(screen.getByText('结果生成后将在这里显示')).toBeInTheDocument()
  expect(screen.queryByText('阶段日志')).not.toBeInTheDocument()
  expect(screen.queryByText('front.png')).not.toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: '更多详情' }))
  expect(await screen.findByRole('heading', { name: '任务详情' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('tab', { name: '素材' }))
  expect(screen.getByText('front.png')).toBeInTheDocument()
})
```

Update the existing project-parameter test to open More details, select Configuration, and then assert visual style/ratio/model/author/writer/theme. Update the Markdown-log test to open More details, select Logs, and click `复制日志`. Keep all task-action and specialized-result assertions unchanged.

- [ ] **Step 2: Run the page test and verify RED**

Run:

```bash
cd studio && bun run test -- src/pages/TaskDetailPage.test.tsx
```

Expected: FAIL because auxiliary content remains in the page and More details does not exist.

- [ ] **Step 3: Wire the sheet and remove auxiliary sections**

Add one local state and import:

```tsx
import { FileOutput, Info } from 'lucide-react'
import { TaskDetailsSheet } from '@/components/tasks/TaskDetailsSheet'

const [showTaskDetails, setShowTaskDetails] = useState(false)
```

Delete the main-flow `details` blocks for Task information and Task configuration, the standalone `ReferenceUsageSummary` error boundary, the Execution logs `details`, and the page-local video input/resolved configuration cards now owned by `TaskConfigurationDetails`.

Move generated files, collected files, execution output, workflow review, Seednote analytics, and `VideoProductionPanel` into the uninterrupted result flow. When a non-completed task has no published/collected files and no result output, render:

```tsx
<section aria-labelledby="task-results-title" className="border-y border-border py-4">
  <div className="flex items-center justify-between gap-3">
    <h2 id="task-results-title" className="text-sm font-semibold text-foreground">任务结果</h2>
    <span className="text-xs text-muted-foreground">0 个文件</span>
  </div>
  <div className="flex min-h-28 flex-col items-center justify-center text-center">
    <FileOutput className="size-5 text-muted-foreground/60" />
    <p className="mt-2 text-sm text-muted-foreground">结果生成后将在这里显示</p>
    <p className="mt-1 text-xs text-muted-foreground/70">无需停留等待，可稍后返回。</p>
  </div>
</section>
```

Add the quiet command after the result flow:

```tsx
<Button
  variant="ghost"
  className="w-full justify-between border-y border-border px-1 py-3 text-muted-foreground hover:text-foreground"
  onClick={() => setShowTaskDetails(true)}
>
  <span className="flex items-center gap-2"><Info className="size-4" />更多详情</span>
  <span className="hidden text-xs font-normal sm:inline">概览 · 配置 · 素材 · 日志</span>
</Button>
```

Render the sheet near the existing dialogs and pass the current task/project/files/credits/log state and stable callbacks. Keep copy behavior in the page callback so toast behavior remains unchanged:

```tsx
<TaskDetailsSheet
  open={showTaskDetails}
  onOpenChange={setShowTaskDetails}
  task={task}
  project={project}
  files={publishedFiles}
  netConsumedCredits={netConsumedCredits}
  showCreditDetails={showCreditDetails}
  onOpenCreditDetails={() => setShowCreditDialog(true)}
  logs={displayLogs}
  sseError={sseError}
  autoScrollLogs={autoScrollLogs}
  onToggleAutoScroll={() => setAutoScrollLogs((value) => !value)}
  onCopyLogs={() => {
    void navigator.clipboard.writeText(displayLogs.join('\n'))
    toast.success('已复制执行日志')
  }}
  onReconnectLogs={() => {
    setSseError(null)
    connectSSE(0)
  }}
  logContainerRef={logContainerRef}
/>
```

- [ ] **Step 4: Run page and component tests and verify GREEN**

Run:

```bash
cd studio && bun run test -- src/pages/TaskDetailPage.test.tsx src/components/tasks/TaskDetailsSheet.test.tsx src/components/tasks/ReferenceUsageSummary.test.tsx
```

Expected: all targeted tests PASS.

- [ ] **Step 5: Commit the page integration**

```bash
git add studio/src/pages/TaskDetailPage.tsx studio/src/pages/TaskDetailPage.test.tsx
git commit -m "refactor(studio): streamline task detail UX"
```

## Task 4: Full Verification And Visual QA

**Files:**
- Modify only if verification exposes a defect in the files listed above.

- [ ] **Step 1: Run the complete Studio test suite**

Run:

```bash
cd studio && bun run test
```

Expected: all Vitest suites PASS.

- [ ] **Step 2: Run the production build**

Run:

```bash
cd studio && bun run build
```

Expected: `tsc -b && vite build` exits `0` with no TypeScript or bundling errors.

- [ ] **Step 3: Start Studio and verify real task states in Browser/IAB**

Run the existing Studio development command on an available local port:

```bash
cd studio && bun run dev --host 127.0.0.1
```

Use the in-app Browser first. Verify running, completed, failed, and publish-approval tasks at desktop and mobile viewports. Check the reference screenshot scale when practical.

Required checks:

- Progress and result destination are in the first viewport for running tasks.
- Completed tasks lead with deliverables and do not show a redundant progress panel.
- More details opens a `w-full sm:max-w-xl` sheet without changing the page scroll position.
- Overview, Configuration, Materials, and Logs are keyboard/click reachable.
- Materials fallback is compact and retains input filenames/instructions.
- Logs render Markdown and copy raw text.
- Mobile sheet has no horizontal overflow, clipped tabs, or overlapped close control.
- Cancel, continuation, publish approval, preview, and download actions remain reachable.

- [ ] **Step 4: Capture desktop and mobile screenshots and inspect them**

Capture the running-task main page, open sheet, and mobile sheet. Inspect them alongside the accepted visual companion concept saved under `.superpowers/brainstorm/93290-1784098139/content/`.

Record a five-point fidelity ledger covering hierarchy, first-viewport density, typography/control sizing, sheet width/tab behavior, and material fallback density. Fix all material drift before continuing.

- [ ] **Step 5: Run final verification after any QA fixes**

Run:

```bash
cd studio && bun run test && bun run build
git diff --check
git status --short
```

Expected: tests and build PASS, `git diff --check` is silent, and status contains only the intended Studio changes plus the pre-existing `claudecode` submodule state.

- [ ] **Step 6: Commit QA fixes when present**

If visual QA required code changes:

```bash
git add studio/src/components/tasks studio/src/pages/TaskDetailPage.tsx studio/src/pages/TaskDetailPage.test.tsx
git commit -m "fix(studio): polish task detail responsive UX"
```

If no QA fixes were required, do not create an empty commit.
