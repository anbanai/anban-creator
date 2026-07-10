# Designer Smart Reference Drop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Designer reference upload button with a full-workspace external-image drop mode and a responsive reference material dock that uses deterministic filtering, deduplication, capacity, and feedback rules.

**Architecture:** `DesignerPage` remains the single owner of `DesignerSettings.referenceFiles` and becomes the only drag/drop event boundary. Pure helpers admit files, `DesignerReferenceDock` renders picker/previews for desktop and touch layouts, and `DesignerDropOverlay` renders global drag feedback; `DesignerToolbar` only composes the controlled dock.

**Tech Stack:** React 19, TypeScript 6, Vite 8, Tailwind CSS v4, Lucide React, Sonner, Vitest 4, Testing Library, Bun.

---

## File Responsibility Map

| File | Change | Responsibility |
|---|---|---|
| `studio/src/components/designer/reference-files.ts` | Create | Image detection, stable identity, deduplication, provider capacity, structured admission result |
| `studio/src/components/designer/reference-files.test.ts` | Create | Pure admission rule coverage |
| `studio/src/components/designer/DesignerReferenceDock.tsx` | Create | Controlled picker, previews, removal, empty/populated/full and compact layouts |
| `studio/src/components/designer/DesignerReferenceDock.test.tsx` | Create | Dock rendering, picker, removal, capacity and object URL lifecycle |
| `studio/src/components/designer/DesignerDropOverlay.tsx` | Create | Full-workspace visual drop state |
| `studio/src/components/designer/DesignerDropOverlay.test.tsx` | Create | Overlay copy, capacity and inactive state |
| `studio/src/components/designer/DesignerToolbar.tsx` | Modify | Remove inline upload implementation and compose the dock on desktop/mobile |
| `studio/src/pages/DesignerPage.reference-drop.test.tsx` | Create | Global drag state machine, drop-once integration, capacity and unsupported-provider behavior |
| `studio/src/pages/DesignerPage.tsx` | Modify | Own admission callback, drag-depth state, provider normalization, overlay and controlled dock callbacks |
| `docs/superpowers/specs/2026-07-10-designer-smart-reference-drop-design.md` | Modify | Clarify that the page root, not the dock, owns all drop events to avoid double admission |

Do not modify `studio/src/components/ui/*`, the Designer API contract, backend code, or generation upload timing.

---

### Task 1: Implement deterministic reference-file admission

**Files:**
- Create: `studio/src/components/designer/reference-files.test.ts`
- Create: `studio/src/components/designer/reference-files.ts`

- [ ] **Step 1: Write the failing pure unit tests**

Create `studio/src/components/designer/reference-files.test.ts`:

```ts
import { describe, expect, it } from 'vitest'

import {
  admitReferenceFiles,
  isReferenceImageFile,
  referenceFileKey,
} from './reference-files'

function image(name: string, options: { type?: string; size?: number; lastModified?: number } = {}) {
  return new File(
    [new Uint8Array(options.size ?? 4)],
    name,
    {
      type: options.type ?? 'image/png',
      lastModified: options.lastModified ?? 100,
    },
  )
}

describe('reference file admission', () => {
  it('accepts image files in stable order', () => {
    const first = image('first.png')
    const second = image('second.webp', { type: 'image/webp', lastModified: 200 })

    const result = admitReferenceFiles([], [first, second], 16)

    expect(result.files).toEqual([first, second])
    expect(result).toMatchObject({
      accepted: 2,
      rejectedNonImages: 0,
      rejectedDuplicates: 0,
      rejectedOverflow: 0,
    })
  })

  it('rejects non-image files while accepting empty-MIME image extensions', () => {
    const text = new File(['notes'], 'notes.txt', { type: 'text/plain' })
    const cameraJpeg = image('camera.JPEG', { type: '' })
    const unknown = image('raw-file', { type: '' })

    expect(isReferenceImageFile(cameraJpeg)).toBe(true)
    expect(isReferenceImageFile(unknown)).toBe(false)

    const result = admitReferenceFiles([], [text, cameraJpeg, unknown], 16)

    expect(result.files).toEqual([cameraJpeg])
    expect(result.rejectedNonImages).toBe(2)
  })

  it('deduplicates against current files and within the incoming batch', () => {
    const existing = image('same.png', { size: 7, lastModified: 123 })
    const duplicateOfExisting = image('same.png', { size: 7, lastModified: 123 })
    const next = image('next.png', { size: 9, lastModified: 456 })
    const duplicateOfNext = image('next.png', { size: 9, lastModified: 456 })

    expect(referenceFileKey(existing)).toBe(referenceFileKey(duplicateOfExisting))

    const result = admitReferenceFiles(
      [existing],
      [duplicateOfExisting, next, duplicateOfNext],
      16,
    )

    expect(result.files).toEqual([existing, next])
    expect(result.accepted).toBe(1)
    expect(result.rejectedDuplicates).toBe(2)
  })

  it('fills only remaining provider capacity and reports overflow', () => {
    const existing = image('existing.png')
    const first = image('first.png', { lastModified: 200 })
    const second = image('second.png', { lastModified: 300 })

    const result = admitReferenceFiles([existing], [first, second], 2)

    expect(result.files).toEqual([existing, first])
    expect(result.accepted).toBe(1)
    expect(result.rejectedOverflow).toBe(1)
  })

  it('returns unchanged files and full overflow when capacity is already full', () => {
    const existing = image('existing.png')
    const incoming = image('incoming.png', { lastModified: 200 })

    const result = admitReferenceFiles([existing], [incoming], 1)

    expect(result.files).toEqual([existing])
    expect(result.accepted).toBe(0)
    expect(result.rejectedOverflow).toBe(1)
  })

  it('counts non-images, duplicates, and overflow in the same batch', () => {
    const existing = image('existing.png')
    const duplicate = image('existing.png')
    const accepted = image('accepted.png', { lastModified: 200 })
    const overflow = image('overflow.png', { lastModified: 300 })
    const text = new File(['x'], 'x.txt', { type: 'text/plain' })

    const result = admitReferenceFiles(
      [existing],
      [duplicate, accepted, overflow, text],
      2,
    )

    expect(result.files).toEqual([existing, accepted])
    expect(result).toMatchObject({
      accepted: 1,
      rejectedNonImages: 1,
      rejectedDuplicates: 1,
      rejectedOverflow: 1,
    })
  })
})
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
cd studio && bun run test -- src/components/designer/reference-files.test.ts
```

Expected: FAIL because `./reference-files` does not exist.

- [ ] **Step 3: Add the minimal admission implementation**

Create `studio/src/components/designer/reference-files.ts`:

```ts
const IMAGE_EXTENSIONS = new Set([
  'avif',
  'bmp',
  'gif',
  'heic',
  'heif',
  'jpeg',
  'jpg',
  'png',
  'tif',
  'tiff',
  'webp',
])

export interface ReferenceAdmissionResult {
  files: File[]
  accepted: number
  rejectedNonImages: number
  rejectedDuplicates: number
  rejectedOverflow: number
}

export function referenceFileKey(file: File): string {
  return [file.name, file.size, file.lastModified, file.type].join('\u0000')
}

export function isReferenceImageFile(file: File): boolean {
  if (file.type.startsWith('image/')) return true
  if (file.type !== '') return false

  const extension = file.name.split('.').pop()?.toLowerCase()
  return extension ? IMAGE_EXTENSIONS.has(extension) : false
}

export function admitReferenceFiles(
  currentFiles: File[],
  incomingFiles: Iterable<File>,
  maxFiles: number,
): ReferenceAdmissionResult {
  const safeMax = Math.max(0, maxFiles)
  const files = currentFiles.slice(0, safeMax)
  const seen = new Set(files.map(referenceFileKey))
  let accepted = 0
  let rejectedNonImages = 0
  let rejectedDuplicates = 0
  let rejectedOverflow = Math.max(0, currentFiles.length - safeMax)

  for (const file of incomingFiles) {
    if (!isReferenceImageFile(file)) {
      rejectedNonImages += 1
      continue
    }

    const key = referenceFileKey(file)
    if (seen.has(key)) {
      rejectedDuplicates += 1
      continue
    }

    if (files.length >= safeMax) {
      rejectedOverflow += 1
      continue
    }

    seen.add(key)
    files.push(file)
    accepted += 1
  }

  return {
    files,
    accepted,
    rejectedNonImages,
    rejectedDuplicates,
    rejectedOverflow,
  }
}
```

- [ ] **Step 4: Run the focused test and verify GREEN**

Run:

```bash
cd studio && bun run test -- src/components/designer/reference-files.test.ts
```

Expected: PASS with 6 tests and 0 failures.

- [ ] **Step 5: Commit the pure admission slice**

```bash
git add studio/src/components/designer/reference-files.ts studio/src/components/designer/reference-files.test.ts
git commit -m "feat(studio): add designer reference admission rules"
```

---

### Task 2: Build the controlled reference material dock

**Files:**
- Create: `studio/src/components/designer/DesignerReferenceDock.test.tsx`
- Create: `studio/src/components/designer/DesignerReferenceDock.tsx`

- [ ] **Step 1: Write failing dock tests**

Create `studio/src/components/designer/DesignerReferenceDock.test.tsx`:

```tsx
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import DesignerReferenceDock from './DesignerReferenceDock'

function image(name: string, lastModified = 100) {
  return new File(['image'], name, { type: 'image/png', lastModified })
}

describe('DesignerReferenceDock', () => {
  const createObjectURL = vi.fn((file: File) => `blob:${file.name}`)
  const revokeObjectURL = vi.fn()

  beforeEach(() => {
    const NativeURL = URL
    vi.stubGlobal('URL', class extends NativeURL {
      static createObjectURL = createObjectURL
      static revokeObjectURL = revokeObjectURL
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('renders a full-surface empty picker with provider capacity', () => {
    render(
      <DesignerReferenceDock
        files={[]}
        maxFiles={16}
        onFilesAdded={vi.fn()}
        onFileRemove={vi.fn()}
      />,
    )

    expect(screen.getByRole('button', { name: '添加参考图' })).toBeInTheDocument()
    expect(screen.getByText('拖入参考图')).toBeInTheDocument()
    expect(screen.getByText('或点按浏览 · 最多 16 张')).toBeInTheDocument()
  })

  it('forwards every picker selection and resets the input', () => {
    const onFilesAdded = vi.fn()
    const first = image('first.png')
    const second = image('second.png', 200)

    render(
      <DesignerReferenceDock
        files={[]}
        maxFiles={16}
        onFilesAdded={onFilesAdded}
        onFileRemove={vi.fn()}
      />,
    )

    const input = screen.getByTestId('designer-reference-input') as HTMLInputElement
    fireEvent.change(input, { target: { files: [first, second] } })

    expect(onFilesAdded).toHaveBeenCalledWith([first, second])
    expect(input.value).toBe('')
  })

  it('renders thumbnails, accessible removal, and the add-more tile', async () => {
    const onFileRemove = vi.fn()
    const first = image('first.png')
    const second = image('second.png', 200)

    render(
      <DesignerReferenceDock
        files={[first, second]}
        maxFiles={3}
        onFilesAdded={vi.fn()}
        onFileRemove={onFileRemove}
      />,
    )

    expect(await screen.findByRole('img', { name: 'first.png' })).toHaveAttribute('src', 'blob:first.png')
    expect(screen.getByRole('img', { name: 'second.png' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '添加更多参考图' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '移除参考图：second.png' }))
    expect(onFileRemove).toHaveBeenCalledWith(1)
  })

  it('renders a clear full-capacity state without an add control', async () => {
    const first = image('first.png')

    render(
      <DesignerReferenceDock
        files={[first]}
        maxFiles={1}
        onFilesAdded={vi.fn()}
        onFileRemove={vi.fn()}
      />,
    )

    expect(await screen.findByRole('img', { name: 'first.png' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '添加更多参考图' })).not.toBeInTheDocument()
    expect(screen.getByText('已达上限')).toBeInTheDocument()
  })

  it('uses a horizontal material rail in compact mode', () => {
    render(
      <DesignerReferenceDock
        files={[]}
        maxFiles={4}
        onFilesAdded={vi.fn()}
        onFileRemove={vi.fn()}
        compact
      />,
    )

    expect(screen.getByTestId('designer-reference-dock')).toHaveAttribute('data-compact', 'true')
  })

  it('revokes preview URLs when files change and on unmount', async () => {
    const first = image('first.png')
    const second = image('second.png', 200)
    const props = {
      maxFiles: 3,
      onFilesAdded: vi.fn(),
      onFileRemove: vi.fn(),
    }
    const { rerender, unmount } = render(
      <DesignerReferenceDock files={[first]} {...props} />,
    )

    await screen.findByRole('img', { name: 'first.png' })
    rerender(<DesignerReferenceDock files={[second]} {...props} />)

    await waitFor(() => expect(revokeObjectURL).toHaveBeenCalledWith('blob:first.png'))
    unmount()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:second.png')
  })
})
```

- [ ] **Step 2: Run the dock test and verify RED**

Run:

```bash
cd studio && bun run test -- src/components/designer/DesignerReferenceDock.test.tsx
```

Expected: FAIL because `DesignerReferenceDock.tsx` does not exist.

- [ ] **Step 3: Implement the dock component**

Create `studio/src/components/designer/DesignerReferenceDock.tsx`:

```tsx
import { useEffect, useRef, useState } from 'react'
import { ImagePlus, Plus, X } from 'lucide-react'

import { cn } from '@/lib/utils'

interface DesignerReferenceDockProps {
  files: File[]
  maxFiles: number
  onFilesAdded: (files: File[]) => void
  onFileRemove: (index: number) => void
  compact?: boolean
  dropActive?: boolean
}

export default function DesignerReferenceDock({
  files,
  maxFiles,
  onFilesAdded,
  onFileRemove,
  compact = false,
  dropActive = false,
}: DesignerReferenceDockProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [previewUrls, setPreviewUrls] = useState<string[]>([])
  const hasCapacity = files.length < maxFiles

  useEffect(() => {
    const urls = files.map((file) => URL.createObjectURL(file))
    setPreviewUrls(urls)
    return () => urls.forEach((url) => URL.revokeObjectURL(url))
  }, [files])

  function openPicker() {
    if (hasCapacity) inputRef.current?.click()
  }

  function handleFilesSelected(event: React.ChangeEvent<HTMLInputElement>) {
    const selectedFiles = Array.from(event.target.files ?? [])
    if (selectedFiles.length > 0) onFilesAdded(selectedFiles)
    event.target.value = ''
  }

  return (
    <div
      data-testid="designer-reference-dock"
      data-compact={compact ? 'true' : 'false'}
      data-drop-active={dropActive ? 'true' : 'false'}
      className={cn(
        'rounded-xl transition-[background-color,box-shadow] motion-reduce:transition-none',
        dropActive && 'bg-primary/10 ring-1 ring-primary/40 shadow-[0_0_24px_-10px_var(--color-primary)]',
      )}
    >
      <input
        ref={inputRef}
        data-testid="designer-reference-input"
        type="file"
        accept="image/*"
        multiple
        className="hidden"
        onChange={handleFilesSelected}
      />

      {files.length === 0 ? (
        <button
          type="button"
          aria-label="添加参考图"
          onClick={openPicker}
          className="group flex w-full flex-col items-center justify-center gap-1.5 rounded-xl border border-dashed border-border/70 bg-muted/20 px-3 py-5 text-center transition-all hover:border-primary/50 hover:bg-primary/5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
        >
          <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-primary/10 text-primary transition-transform group-hover:scale-105 motion-reduce:transition-none">
            <ImagePlus className="h-4 w-4" />
          </span>
          <span className="text-xs font-semibold text-foreground">拖入参考图</span>
          <span className="text-[10px] text-muted-foreground">或点按浏览 · 最多 {maxFiles} 张</span>
        </button>
      ) : (
        <div
          className={cn(
            compact
              ? 'flex gap-2 overflow-x-auto pb-1'
              : 'grid grid-cols-3 gap-1.5',
          )}
        >
          {previewUrls.map((url, index) => {
            const file = files[index]
            return (
              <div
                key={`${file.name}-${file.size}-${file.lastModified}-${file.type}`}
                className="group relative aspect-square min-w-14 overflow-hidden rounded-lg bg-muted/30 ring-1 ring-border/70"
              >
                <img src={url} alt={file.name} className="h-full w-full object-cover" />
                <button
                  type="button"
                  aria-label={`移除参考图：${file.name}`}
                  onClick={() => onFileRemove(index)}
                  className="absolute right-1 top-1 flex h-5 w-5 items-center justify-center rounded-full bg-background/85 text-foreground opacity-0 shadow-sm ring-1 ring-border/70 backdrop-blur transition-opacity group-hover:opacity-100 group-focus-within:opacity-100 focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50"
                >
                  <X className="h-3 w-3" />
                </button>
              </div>
            )
          })}

          {hasCapacity ? (
            <button
              type="button"
              aria-label="添加更多参考图"
              onClick={openPicker}
              className="flex aspect-square min-w-14 flex-col items-center justify-center gap-1 rounded-lg border border-dashed border-border/70 bg-muted/20 text-muted-foreground transition-all hover:border-primary/50 hover:bg-primary/5 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
            >
              <Plus className="h-4 w-4" />
              <span className="text-[9px]">添加</span>
            </button>
          ) : (
            <div className="flex aspect-square min-w-14 items-center justify-center rounded-lg border border-border/70 bg-muted/30 px-1 text-center text-[9px] font-medium text-muted-foreground">
              已达上限
            </div>
          )}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 4: Run the dock tests and verify GREEN**

Run:

```bash
cd studio && bun run test -- src/components/designer/DesignerReferenceDock.test.tsx
```

Expected: PASS with 6 tests and 0 failures.

- [ ] **Step 5: Commit the dock slice**

```bash
git add studio/src/components/designer/DesignerReferenceDock.tsx studio/src/components/designer/DesignerReferenceDock.test.tsx
git commit -m "feat(studio): add designer reference material dock"
```

---

### Task 3: Add the full-workspace drop overlay

**Files:**
- Create: `studio/src/components/designer/DesignerDropOverlay.test.tsx`
- Create: `studio/src/components/designer/DesignerDropOverlay.tsx`

- [ ] **Step 1: Write the failing overlay tests**

Create `studio/src/components/designer/DesignerDropOverlay.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import DesignerDropOverlay from './DesignerDropOverlay'

describe('DesignerDropOverlay', () => {
  it('does not render when inactive', () => {
    render(<DesignerDropOverlay active={false} remainingCapacity={8} />)
    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()
  })

  it('shows incoming image count and remaining capacity', () => {
    render(
      <DesignerDropOverlay
        active
        incomingCount={3}
        remainingCapacity={13}
      />,
    )

    expect(screen.getByText('释放以添加 3 张参考图')).toBeInTheDocument()
    expect(screen.getByText('当前模型还可添加 13 张')).toBeInTheDocument()
  })

  it('uses generic copy when the browser hides incoming files until drop', () => {
    render(<DesignerDropOverlay active remainingCapacity={4} />)
    expect(screen.getByText('释放以添加参考图')).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the overlay test and verify RED**

Run:

```bash
cd studio && bun run test -- src/components/designer/DesignerDropOverlay.test.tsx
```

Expected: FAIL because `DesignerDropOverlay.tsx` does not exist.

- [ ] **Step 3: Implement the overlay**

Create `studio/src/components/designer/DesignerDropOverlay.tsx`:

```tsx
import { Images } from 'lucide-react'

interface DesignerDropOverlayProps {
  active: boolean
  incomingCount?: number
  remainingCapacity: number
}

export default function DesignerDropOverlay({
  active,
  incomingCount,
  remainingCapacity,
}: DesignerDropOverlayProps) {
  if (!active) return null

  const action = incomingCount && incomingCount > 0
    ? `释放以添加 ${incomingCount} 张参考图`
    : '释放以添加参考图'

  return (
    <div
      data-testid="designer-drop-overlay"
      aria-hidden="true"
      className="pointer-events-none absolute inset-0 z-50 p-3 md:p-5"
    >
      <div className="flex h-full items-center justify-center rounded-[28px] border-2 border-dashed border-primary/70 bg-[linear-gradient(135deg,color-mix(in_oklch,var(--color-background)_88%,var(--color-primary)),color-mix(in_oklch,var(--color-card)_90%,var(--color-chart-2)))] shadow-[inset_0_0_80px_color-mix(in_oklch,var(--color-primary)_12%,transparent),0_20px_70px_-30px_color-mix(in_oklch,var(--color-primary)_55%,transparent)] backdrop-blur-xl animate-in fade-in-0 zoom-in-95 duration-150 motion-reduce:animate-none"
      >
        <div className="flex max-w-sm flex-col items-center gap-3 px-6 text-center">
          <span className="flex h-16 w-16 items-center justify-center rounded-2xl bg-primary/15 text-primary ring-1 ring-primary/30 shadow-lg">
            <Images className="h-7 w-7" />
          </span>
          <div className="space-y-1">
            <p className="text-lg font-bold tracking-tight text-foreground">{action}</p>
            <p className="text-sm text-muted-foreground">当前模型还可添加 {remainingCapacity} 张</p>
          </div>
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Run the overlay tests and verify GREEN**

Run:

```bash
cd studio && bun run test -- src/components/designer/DesignerDropOverlay.test.tsx
```

Expected: PASS with 3 tests and 0 failures.

- [ ] **Step 5: Run the Designer dark clarity test**

Run:

```bash
cd studio && bun run test -- src/pages/DesignerPage.dark-clarity.test.ts
```

Expected: PASS; the new overlay must not contain prohibited low-contrast class fragments.

- [ ] **Step 6: Commit the overlay slice**

```bash
git add studio/src/components/designer/DesignerDropOverlay.tsx studio/src/components/designer/DesignerDropOverlay.test.tsx
git commit -m "feat(studio): add designer workspace drop overlay"
```

---

### Task 4: Replace the toolbar upload button with the material dock

**Files:**
- Modify: `studio/src/components/designer/DesignerToolbar.tsx:1-24,169-212,223-229,426-470,489-545`
- Test: `studio/src/components/designer/DesignerReferenceDock.test.tsx`

- [ ] **Step 1: Add a failing source-contract assertion for removal of the old button**

Append this test to `studio/src/components/designer/DesignerReferenceDock.test.tsx` and add the Node imports shown below:

```tsx
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
```

```tsx
it('replaces the legacy toolbar upload button with the controlled dock', () => {
  const toolbar = readFileSync(
    resolve(import.meta.dirname, 'DesignerToolbar.tsx'),
    'utf8',
  )

  expect(toolbar).toContain('<DesignerReferenceDock')
  expect(toolbar).not.toContain('上传参考图')
  expect(toolbar).not.toContain('handleRefFiles')
  expect(toolbar).not.toContain('refInputRef')
})
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
cd studio && bun run test -- src/components/designer/DesignerReferenceDock.test.tsx
```

Expected: FAIL because the toolbar still contains `上传参考图`, `handleRefFiles`, and `refInputRef`.

- [ ] **Step 3: Replace toolbar imports and props**

In `studio/src/components/designer/DesignerToolbar.tsx`:

1. Delete the React hook import entirely:

```ts
import { useRef, useState, useEffect } from 'react'
```

Keep the existing `import { useQuery } from '@tanstack/react-query'` line. The toolbar no longer uses React hooks after the inline preview lifecycle is removed.

2. Remove `ImagePlus`, `X`, and `Upload` from the Lucide import, keeping all unrelated icons.

3. Remove the unused `Button` import.

4. Add:

```ts
import DesignerReferenceDock from '@/components/designer/DesignerReferenceDock'
```

5. Replace `DesignerToolbarProps` with:

```ts
interface DesignerToolbarProps {
  providers: DesignerProvider[]
  selectedProviderId: string
  onModelChange: (id: string) => void
  capabilities?: ModelCapabilities
  settings: DesignerSettings
  onSettingsChange: (settings: DesignerSettings) => void
  onHistoryToggle: () => void
  onReferenceFilesAdded: (files: File[]) => void
  onReferenceFileRemove: (index: number) => void
  referenceDropActive: boolean
}
```

6. Replace the component destructuring with:

```ts
export default function DesignerToolbar({
  providers,
  selectedProviderId,
  onModelChange,
  capabilities,
  settings,
  onSettingsChange,
  onHistoryToggle,
  onReferenceFilesAdded,
  onReferenceFileRemove,
  referenceDropActive,
}: DesignerToolbarProps) {
  const caps = capabilities
```

7. Delete `handleRefFiles`, `removeRefFile`, `refPreviewUrls`, and the object-URL effect. Keep `update()` because the rest of the toolbar still uses it.

- [ ] **Step 4: Replace the desktop reference markup**

Replace the current `{/* References */}` block with:

```tsx
{/* Reference material dock */}
{showRefs && (
  <div className="space-y-2">
    <div className="flex items-center justify-between gap-2">
      <h4 className={sectionHeader}>参考素材</h4>
      <span className="text-[10px] font-medium tabular-nums text-muted-foreground">
        {settings.referenceFiles.length}/{caps!.maxReferenceImages}
      </span>
    </div>
    <DesignerReferenceDock
      files={settings.referenceFiles}
      maxFiles={caps!.maxReferenceImages}
      onFilesAdded={onReferenceFilesAdded}
      onFileRemove={onReferenceFileRemove}
      dropActive={referenceDropActive}
    />
  </div>
)}
```

- [ ] **Step 5: Add the compact dock to the mobile panel**

Insert this block after the mobile `ModelSelector` and before the mobile settings row:

```tsx
{showRefs && (
  <DesignerReferenceDock
    files={settings.referenceFiles}
    maxFiles={caps!.maxReferenceImages}
    onFilesAdded={onReferenceFilesAdded}
    onFileRemove={onReferenceFileRemove}
    dropActive={referenceDropActive}
    compact
  />
)}
```

- [ ] **Step 6: Run the focused dock test and TypeScript build**

Run:

```bash
cd studio && bun run test -- src/components/designer/DesignerReferenceDock.test.tsx
cd studio && bun run build
```

Expected test result: PASS.

Expected build result at this intermediate checkpoint: FAIL in `DesignerPage.tsx` because the new required toolbar props have not yet been supplied. This expected compile failure proves the next integration task is required; do not commit the temporarily uncompilable state.

- [ ] **Step 7: Leave the toolbar changes unstaged and continue directly to Task 5**

Do not commit Task 4 independently because the required parent integration is intentionally completed in Task 5. The next commit will include both toolbar composition and page ownership, keeping every commit buildable.

---

### Task 5: Integrate page-owned admission and the global drag state machine

**Files:**
- Create: `studio/src/pages/DesignerPage.reference-drop.test.tsx`
- Modify: `studio/src/pages/DesignerPage.tsx:1-17,46-93,240-260,302-350`
- Modify: `studio/src/components/designer/DesignerToolbar.tsx`
- Modify: `docs/superpowers/specs/2026-07-10-designer-smart-reference-drop-design.md`

- [ ] **Step 1: Write failing page integration tests**

Create `studio/src/pages/DesignerPage.reference-drop.test.tsx`:

```tsx
import { fireEvent, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import DesignerPage from './DesignerPage'
import { designerApi } from '@/lib/api/designer'
import { render } from '@/test/test-utils'
import type { DesignerProvider } from '@/types/designer'

const toast = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}))

vi.mock('sonner', () => ({ toast }))

vi.mock('@/lib/api/designer', () => ({
  designerApi: {
    getProviders: vi.fn(),
    generate: vi.fn(),
    uploadReference: vi.fn(),
    uploadReferenceFromUrl: vi.fn(),
    getHistory: vi.fn(),
    getGeneration: vi.fn(),
  },
}))

function provider(overrides: Partial<DesignerProvider['capabilities']> = {}): DesignerProvider {
  return {
    id: 'gpt_image_2',
    name: 'GPT Image 2',
    provider: 'openai',
    providerKey: 'wangcai_openai',
    route: 'image_generation.designer.gpt_image_2',
    model: 'gpt-image-2',
    credits: 0,
    enabled: true,
    idx: 0,
    capabilities: {
      qualityLevels: ['auto'],
      sizePresets: ['auto'],
      defaultSize: 'auto',
      maxBatch: 1,
      maxReferenceImages: 2,
      supportsReference: true,
      supportsMask: false,
      outputFormats: ['png'],
      hasBackground: false,
      hasCompression: false,
      watermark: false,
      ...overrides,
    },
    pricing: {},
  }
}

function dragData(files: File[] = [], types: string[] = ['Files']) {
  return {
    types,
    files,
    items: files.map((file) => ({ kind: 'file', type: file.type })),
    dropEffect: 'none',
  }
}

function image(name: string, lastModified = 100) {
  return new File(['image'], name, { type: 'image/png', lastModified })
}

describe('Designer workspace reference drop', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.localStorage.clear()
    window.sessionStorage.clear()
    const NativeURL = URL
    vi.stubGlobal('URL', class extends NativeURL {
      static createObjectURL = vi.fn((file: File) => `blob:${file.name}`)
      static revokeObjectURL = vi.fn()
    })
    vi.mocked(designerApi.getProviders).mockResolvedValue([provider()])
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows a stable overlay across nested drag enter/leave events', async () => {
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    const canvas = screen.getByTestId('designer-canvas-frame')
    const dataTransfer = dragData()

    fireEvent.dragEnter(workspace, { dataTransfer })
    expect(screen.getByText('释放以添加参考图')).toBeInTheDocument()

    fireEvent.dragEnter(canvas, { dataTransfer })
    fireEvent.dragLeave(canvas, { dataTransfer })
    expect(screen.getByText('释放以添加参考图')).toBeInTheDocument()

    fireEvent.dragLeave(workspace, { dataTransfer })
    expect(screen.queryByText('释放以添加参考图')).not.toBeInTheDocument()
  })

  it('drops multiple images anywhere once and updates the material dock', async () => {
    render(<DesignerPage />)
    await screen.findByTestId('designer-workspace')
    const dock = screen.getAllByTestId('designer-reference-dock')[0]
    const first = image('first.png')
    const second = image('second.png', 200)

    fireEvent.dragEnter(dock, { dataTransfer: dragData([first, second]) })
    fireEvent.drop(dock, { dataTransfer: dragData([first, second]) })

    expect(await screen.findAllByRole('img', { name: 'first.png' })).toHaveLength(2)
    expect(screen.getAllByRole('img', { name: 'second.png' })).toHaveLength(2)
    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()
  })

  it('enforces provider capacity and reports partial admission', async () => {
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([
      provider({ maxReferenceImages: 1 }),
    ])
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    const first = image('first.png')
    const second = image('second.png', 200)

    fireEvent.drop(workspace, { dataTransfer: dragData([first, second]) })

    expect(await screen.findAllByRole('img', { name: 'first.png' })).toHaveLength(2)
    expect(screen.queryByRole('img', { name: 'second.png' })).not.toBeInTheDocument()
    expect(toast.warning).toHaveBeenCalledWith(
      '已添加 1 张，另外 1 张超过当前模型的 1 张上限',
    )
  })


  it('retains only the supported prefix when switching to a lower-capacity provider', async () => {
    const singleReferenceProvider = {
      ...provider({ maxReferenceImages: 1 }),
      id: 'single_reference',
      name: 'Single Reference',
      idx: 1,
    }
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([
      provider({ maxReferenceImages: 2 }),
      singleReferenceProvider,
    ])
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    const first = image('first.png')
    const second = image('second.png', 200)

    fireEvent.drop(workspace, { dataTransfer: dragData([first, second]) })
    expect(await screen.findAllByRole('img', { name: 'second.png' })).toHaveLength(2)

    fireEvent.click(screen.getAllByRole('combobox')[0])
    fireEvent.click(await screen.findByText('Single Reference'))

    expect(screen.getAllByRole('img', { name: 'first.png' })).toHaveLength(2)
    expect(screen.queryByRole('img', { name: 'second.png' })).not.toBeInTheDocument()
    expect(toast.warning).toHaveBeenCalledWith(
      '当前模型最多支持 1 张参考图，已移除 1 张',
    )
  })

  it('reports duplicate and non-image files instead of silently discarding them', async () => {
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    const existing = image('existing.png')
    const duplicate = image('existing.png')
    const text = new File(['notes'], 'notes.txt', { type: 'text/plain' })

    fireEvent.drop(workspace, { dataTransfer: dragData([existing]) })
    expect(await screen.findAllByRole('img', { name: 'existing.png' })).toHaveLength(2)
    toast.warning.mockClear()

    fireEvent.drop(workspace, { dataTransfer: dragData([duplicate, text]) })

    expect(toast.warning).toHaveBeenCalledWith(
      '忽略 1 个非图片文件，忽略 1 张重复图片',
    )
  })

  it('does not activate for text drags or providers without reference support', async () => {
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([
      provider({ supportsReference: false, maxReferenceImages: 0 }),
    ])
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')

    fireEvent.dragEnter(workspace, { dataTransfer: dragData([], ['text/plain']) })
    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()

    fireEvent.dragEnter(workspace, { dataTransfer: dragData([image('ignored.png')]) })
    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()
  })

  it('clears the overlay when Escape is pressed', async () => {
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')

    fireEvent.dragEnter(workspace, { dataTransfer: dragData() })
    expect(screen.getByTestId('designer-drop-overlay')).toBeInTheDocument()

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()
  })
})
```

The expected thumbnail count is two because `DesignerToolbar` renders both its desktop and mobile variants in the DOM while CSS controls visibility.

- [ ] **Step 2: Run the page integration test and verify RED**

Run:

```bash
cd studio && bun run test -- src/pages/DesignerPage.reference-drop.test.tsx
```

Expected: FAIL because the page has no workspace test IDs, global drag state, admission callback, or overlay.

- [ ] **Step 3: Add page imports and reference drag state**

In `studio/src/pages/DesignerPage.tsx`, add these imports:

```ts
import DesignerDropOverlay from '@/components/designer/DesignerDropOverlay'
import {
  admitReferenceFiles,
  isReferenceImageFile,
  type ReferenceAdmissionResult,
} from '@/components/designer/reference-files'
```

Inside `DesignerPage`, immediately after `abortedRef`, add:

```ts
const referenceFilesRef = useRef<File[]>([])
const referenceDragDepthRef = useRef(0)
const [referenceDropActive, setReferenceDropActive] = useState(false)
const [incomingReferenceCount, setIncomingReferenceCount] = useState<number>()
```

After `canInpaint`, add:

```ts
const maxReferenceImages = effectiveCaps?.supportsReference
  ? Math.max(0, effectiveCaps.maxReferenceImages)
  : 0
const remainingReferenceCapacity = Math.max(
  0,
  maxReferenceImages - settings.referenceFiles.length,
)
const canAcceptReferenceDrop = maxReferenceImages > 0 && remainingReferenceCapacity > 0
```

- [ ] **Step 4: Add admission feedback and stable state mutation**

Add these functions before `handleModelChange`:

```ts
function describeReferenceAdmission(
  result: ReferenceAdmissionResult,
  maxFiles: number,
): string | undefined {
  const details: string[] = []
  if (result.rejectedNonImages > 0) details.push(`忽略 ${result.rejectedNonImages} 个非图片文件`)
  if (result.rejectedDuplicates > 0) details.push(`忽略 ${result.rejectedDuplicates} 张重复图片`)
  if (result.rejectedOverflow > 0) {
    details.push(`另外 ${result.rejectedOverflow} 张超过当前模型的 ${maxFiles} 张上限`)
  }

  if (details.length === 0) return undefined
  if (result.accepted > 0) return `已添加 ${result.accepted} 张，${details.join('，')}`
  if (result.rejectedOverflow > 0 && result.rejectedNonImages === 0 && result.rejectedDuplicates === 0) {
    return `参考图已达到当前模型的 ${maxFiles} 张上限`
  }
  if (result.rejectedDuplicates > 0 && result.rejectedNonImages === 0 && result.rejectedOverflow === 0) {
    return '这些图片已经在参考素材中'
  }
  return details.join('，')
}

const resetReferenceDrag = useCallback(() => {
  referenceDragDepthRef.current = 0
  setReferenceDropActive(false)
  setIncomingReferenceCount(undefined)
}, [])

const addReferenceFiles = useCallback((incomingFiles: File[]) => {
  const result = admitReferenceFiles(
    referenceFilesRef.current,
    incomingFiles,
    maxReferenceImages,
  )

  referenceFilesRef.current = result.files
  setSettings((current) => ({
    ...current,
    referenceFiles: result.files,
  }))

  const message = describeReferenceAdmission(result, maxReferenceImages)
  if (message) toast.warning(message)
}, [maxReferenceImages])

const removeReferenceFile = useCallback((index: number) => {
  const files = referenceFilesRef.current.filter((_, fileIndex) => fileIndex !== index)
  referenceFilesRef.current = files
  setSettings((current) => ({ ...current, referenceFiles: files }))
}, [])
```

- [ ] **Step 5: Add drag eligibility, counting, and event handlers**

Add these helpers at module scope below the poll constants:

```ts
function hasExternalFiles(dataTransfer: DataTransfer): boolean {
  return Array.from(dataTransfer.types).includes('Files')
}

function countIncomingReferenceImages(dataTransfer: DataTransfer): number | undefined {
  const files = Array.from(dataTransfer.files)
  if (files.length > 0) return files.filter(isReferenceImageFile).length

  const imageItems = Array.from(dataTransfer.items).filter(
    (item) => item.kind === 'file' && item.type.startsWith('image/'),
  )
  return imageItems.length > 0 ? imageItems.length : undefined
}
```

Add these handlers inside `DesignerPage` after `removeReferenceFile`:

```ts
function handleReferenceDragEnter(event: React.DragEvent<HTMLDivElement>) {
  if (!canAcceptReferenceDrop || !hasExternalFiles(event.dataTransfer)) return
  event.preventDefault()
  referenceDragDepthRef.current += 1
  setReferenceDropActive(true)
  setIncomingReferenceCount(countIncomingReferenceImages(event.dataTransfer))
}

function handleReferenceDragOver(event: React.DragEvent<HTMLDivElement>) {
  if (!canAcceptReferenceDrop || !hasExternalFiles(event.dataTransfer)) return
  event.preventDefault()
  event.dataTransfer.dropEffect = 'copy'
}

function handleReferenceDragLeave(event: React.DragEvent<HTMLDivElement>) {
  if (!hasExternalFiles(event.dataTransfer)) return
  event.preventDefault()
  referenceDragDepthRef.current = Math.max(0, referenceDragDepthRef.current - 1)
  if (referenceDragDepthRef.current === 0) resetReferenceDrag()
}

function handleReferenceDrop(event: React.DragEvent<HTMLDivElement>) {
  if (!hasExternalFiles(event.dataTransfer)) return
  event.preventDefault()
  const files = Array.from(event.dataTransfer.files)
  resetReferenceDrag()
  if (maxReferenceImages === 0) return
  if (files.length > 0) addReferenceFiles(files)
}
```

- [ ] **Step 6: Add Escape and provider-capacity normalization**

Add this effect immediately after the `resetReferenceDrag`, `addReferenceFiles`, and `removeReferenceFile` callbacks defined in Steps 3-4; keeping it after `resetReferenceDrag` avoids reading that `const` before initialization:

```ts
useEffect(() => {
  if (!referenceDropActive) return

  function handleKeyDown(event: KeyboardEvent) {
    if (event.key === 'Escape') resetReferenceDrag()
  }

  window.addEventListener('keydown', handleKeyDown)
  return () => window.removeEventListener('keydown', handleKeyDown)
}, [referenceDropActive, resetReferenceDrag])
```

Replace `handleModelChange` with:

```ts
function handleModelChange(providerId: string) {
  setSelectedProviderId(providerId)
  resetReferenceDrag()
  const newProvider = providerList.find((provider) => provider.id === providerId)
  const caps = newProvider?.capabilities
  const maxFiles = caps?.supportsReference ? Math.max(0, caps.maxReferenceImages) : 0
  const retainedReferenceFiles = referenceFilesRef.current.slice(0, maxFiles)
  const removedCount = referenceFilesRef.current.length - retainedReferenceFiles.length
  referenceFilesRef.current = retainedReferenceFiles

  setSettings({
    ...DEFAULT_SETTINGS,
    size: caps?.defaultSize || DEFAULT_SETTINGS.size,
    quality: caps?.qualityLevels?.[0] ?? DEFAULT_SETTINGS.quality,
    n: Math.min(DEFAULT_SETTINGS.n, Math.max(1, caps?.maxBatch ?? 1)),
    referenceFiles: retainedReferenceFiles,
  })

  if (removedCount > 0) {
    toast.warning(`当前模型最多支持 ${maxFiles} 张参考图，已移除 ${removedCount} 张`)
  }
}
```

- [ ] **Step 7: Wire the root drop boundary, toolbar, canvas test ID, and overlay**

Replace the opening root element with:

```tsx
<div
  data-testid="designer-workspace"
  className="relative -mx-4 -my-6 flex overflow-hidden bg-background md:-mx-8 md:-my-8"
  style={{ height: '100dvh' }}
  onDragEnter={handleReferenceDragEnter}
  onDragOver={handleReferenceDragOver}
  onDragLeave={handleReferenceDragLeave}
  onDrop={handleReferenceDrop}
>
```

Add the new toolbar props:

```tsx
onReferenceFilesAdded={addReferenceFiles}
onReferenceFileRemove={removeReferenceFile}
referenceDropActive={referenceDropActive}
```

Add `data-testid="designer-canvas-frame"` to the canvas frame div that currently starts with `className="relative flex-1 overflow-hidden..."`.

Render the overlay immediately before the History drawer:

```tsx
<DesignerDropOverlay
  active={referenceDropActive}
  incomingCount={incomingReferenceCount}
  remainingCapacity={remainingReferenceCapacity}
/>
```

- [ ] **Step 8: Run focused tests and fix only contract mismatches**

Run:

```bash
cd studio && bun run test -- \
  src/components/designer/reference-files.test.ts \
  src/components/designer/DesignerReferenceDock.test.tsx \
  src/components/designer/DesignerDropOverlay.test.tsx \
  src/pages/DesignerPage.reference-drop.test.tsx \
  src/pages/DesignerPage.provider-contract.test.ts \
  src/pages/DesignerPage.dark-clarity.test.ts
```

Expected: all listed files PASS with 0 failures.

If TypeScript reports that mocked `DataTransfer` objects are incomplete, keep production handler types unchanged and cast only the test helper return value:

```ts
return {
  types,
  files,
  items: files.map((file) => ({ kind: 'file', type: file.type })),
  dropEffect: 'none',
} as unknown as DataTransfer
```

- [ ] **Step 9: Run the Studio build**

Run:

```bash
cd studio && bun run build
```

Expected: `tsc -b && vite build` exits 0.

- [ ] **Step 10: Commit the integrated Designer interaction**

```bash
git add \
  studio/src/components/designer/DesignerToolbar.tsx \
  studio/src/pages/DesignerPage.tsx \
  studio/src/pages/DesignerPage.reference-drop.test.tsx \
  docs/superpowers/specs/2026-07-10-designer-smart-reference-drop-design.md
git commit -m "feat(studio): add smart designer reference dropping"
```

---

### Task 6: Verify rendered behavior with the in-app Browser

**Files:**
- No committed files expected
- Temporary screenshots or scripts, if needed, must stay outside the repository

- [ ] **Step 1: Load the Browser control skill and inspect Browser documentation**

Use `browser:control-in-app-browser`. Initialize the Browser client through `mcp__node_repl` and read its documentation exactly as required by that skill before issuing Browser calls.

- [ ] **Step 2: Start the Studio dev server**

Run:

```bash
cd studio && bun run dev -- --host 127.0.0.1
```

Keep the process running and record the exact Vite URL, normally `http://127.0.0.1:5173`.

- [ ] **Step 3: Verify the Designer route and baseline health**

Open the Designer route in the in-app Browser. Use the route configured by the application router, verify page identity through visible `设计师` chrome, and check:

- the page is not blank;
- there is no Vite/React error overlay;
- browser console has no new error caused by this change;
- the old `上传参考图` button is absent;
- the `拖入参考图` material dock is visible for a reference-capable provider.

- [ ] **Step 4: Exercise file-picker admission**

Use the Browser file-input API on the hidden `designer-reference-input` control with two temporary image fixtures outside the repository. Verify:

- both thumbnails appear;
- the count updates;
- the add-more tile remains while capacity remains;
- deleting one thumbnail restores one capacity slot.

- [ ] **Step 5: Exercise drag state and drop behavior**

Use Browser-supported drag/file-drop automation when available. If Browser cannot synthesize an OS-backed file drop, use `tab.playwright` evaluation only for the interaction boundary and dispatch a `DataTransfer` containing real `File` objects to `designer-workspace`.

Verify:

- drag enter shows `释放以添加参考图`;
- moving over nested canvas content does not dismiss the overlay;
- dropping adds the image once, not twice;
- Escape removes the overlay;
- text-only drag data does not activate the overlay.

- [ ] **Step 6: Verify responsive and theme presentation**

Check at least:

- desktop viewport around `1440×900`;
- narrow viewport around `390×844`;
- light theme;
- dark theme.

Look for clipped thumbnails, unreadable count text, overlap with the prompt bar, overlay z-index problems, broken mobile scrolling, or low-contrast full-capacity copy.

- [ ] **Step 7: Capture evidence outside the repository**

Capture at least:

- empty material dock;
- active full-workspace drop overlay;
- populated dock;
- narrow compact rail.

Save screenshots outside the repository, such as `/tmp/anban-designer-reference-drop-*.png`.

---

### Task 7: Run final verification and review the complete diff

**Files:**
- All files changed in Tasks 1-5

- [ ] **Step 1: Run the complete Studio test suite**

Run:

```bash
cd studio && bun run test
```

Expected: all Vitest files pass with 0 failures.

- [ ] **Step 2: Run a fresh production build**

Run:

```bash
cd studio && bun run build
```

Expected: `tsc -b && vite build` exits 0 and produces the normal Vite bundle summary.

- [ ] **Step 3: Inspect repository state and diff hygiene**

Run:

```bash
git diff --check
git status --short
git diff --stat 15d630b..HEAD
git diff 15d630b..HEAD -- \
  studio/src/components/designer \
  studio/src/pages/DesignerPage.tsx \
  studio/src/pages/DesignerPage.reference-drop.test.tsx \
  docs/superpowers/specs/2026-07-10-designer-smart-reference-drop-design.md
```

Expected:

- `git diff --check` prints nothing;
- the unrelated untracked `docs/superpowers/plans/2026-07-10-multi-reference-materials.md` remains untouched;
- no shared `studio/src/components/ui/*` file is changed;
- no backend or API contract file is changed.

- [ ] **Step 4: Re-read acceptance criteria against evidence**

Confirm every item explicitly:

- old upload button removed;
- drop anywhere works;
- drag overlay is stable across nested elements;
- dock shows empty, populated, add-more, removal, compact, and full states;
- non-image, duplicate, and overflow inputs are never silently discarded;
- provider capacity remains authoritative;
- generation still uploads the ordered `settings.referenceFiles` array at submit time;
- focused tests, full tests, build, and rendered Browser checks have fresh passing evidence.

- [ ] **Step 5: Commit any verification-only corrections**

Only if Task 6 or Task 7 required code corrections:

```bash
git add studio/src/components/designer studio/src/pages/DesignerPage.tsx studio/src/pages/DesignerPage.reference-drop.test.tsx
git commit -m "fix(studio): polish designer reference drop behavior"
```

Do not create an empty commit when no correction was needed.
