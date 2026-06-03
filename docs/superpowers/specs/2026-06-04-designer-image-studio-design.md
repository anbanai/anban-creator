# Designer Image Studio - Design Spec

## Context

The platform needs a dedicated image generation tool for designers, leveraging GPT Image 2's advanced capabilities (image editing, batch generation, streaming, inpainting). Currently, image generation is only accessible through the MCP protocol for AI agent workflows. There's no direct UI for human designers to generate images interactively.

This spec adds a top-level `/designer` page with a professional tool-style interface supporting three providers: GPT Image 2 (OpenAI), Gemini (Google), and Seedream (Volcengine).

## Architecture

**Approach: New REST API + Extend Provider Interface**

Reuse the existing `ImageService` billing/channel/storage logic, add dedicated REST endpoints, and extend the `Provider` interface for GPT Image 2 features. Streaming via SSE.

```
Browser ──REST/SSE──> Server Handler ──> ImageService ──> Provider (OpenAI/Gemini/Volcengine)
                                                │
                                                ├── billing (credits deduction)
                                                ├── channel association
                                                └── storage (local/OSS)
```

## 1. Backend: Provider Interface Extension

### Files to modify

- `app/image/provider.go` — Extend `GenerateOptions` and `GenerateResult`

**GenerateOptions additions:**

```go
type GenerateOptions struct {
    RefImagePath  string   // single reference (backward compat)
    RefImagePaths []string // multi-reference (up to 16)
    MaskPath      string   // inpainting mask (PNG alpha)
    Quality       string   // "low", "medium", "high", "auto"
    OutputFormat  string   // "png", "jpeg", "webp"
    N             int      // batch count (1-10)
    Size          string   // custom size or ratio
    StreamCB      StreamCallback // streaming callback (nil = no streaming)
}
```

**New types:**

```go
type StreamCallback func(partial *PartialImage)

type PartialImage struct {
    Index    int
    B64Data  string
    Progress int // 0-100
    Final    bool
}
```

**GenerateResult additions:**

```go
type GenerateResult struct {
    URL            string
    RevisedPrompt  string
    Model          string
    Size           string
    ResponseType   string
    ResponsePreview string
    Images         []GeneratedImage // multiple images for batch
}

type GeneratedImage struct {
    URL   string
    B64   string
    Index int
}
```

### Provider-specific changes

**`app/image/openai.go`:**
- Pass `Quality`, `OutputFormat`, `N` to `ImageGenerateParams`
- Use `ImageEditParams` with multiple `OfImageFile` when `RefImagePaths` has entries
- Support `Mask` parameter for inpainting
- Implement streaming via `client.Images.GenerateStreaming()` with `StreamCB`
- GPT Image 2 flexible sizes: pass custom pixel sizes directly instead of mapping to fixed presets

**`app/image/gemini.go`:**
- Support `RefImagePaths` (multiple inline images)
- Ignore unsupported params (Quality, N>1, Streaming, Mask) gracefully

**`app/image/volcengine.go`:**
- Support `N` parameter for batch generation
- Ignore unsupported params (Streaming, Mask, Quality) gracefully

### Capability reporting

Add a `Capabilities()` method to the `Provider` interface or a standalone capability map:

```go
type ProviderCapabilities struct {
    MaxRefImages   int
    Batch          bool
    Streaming      bool
    Inpainting     bool
    QualityLevels  []string
    OutputFormats  []string
    FlexibleSize   bool
}
```

## 2. Backend: REST API Layer

### Files to create

- `server/handler/designer.go` — HTTP handlers
- `server/service/designer.go` — Business logic (wraps ImageService)

### Files to modify

- `server/router/router.go` — Register new routes
- `server/handlers.go` — Wire designer handler

### Endpoints

```
POST /api/v1/designer/generate          — Generate images (SSE streaming)
POST /api/v1/designer/upload-reference  — Upload reference/mask image
GET  /api/v1/designer/history           — Generation history (paginated)
GET  /api/v1/designer/generations/:id   — Single generation detail
```

### Generate request/response

**Request:**
```json
{
  "channel_id": 1,
  "prompt": "...",
  "provider": "openai",
  "model": "gpt-image-2",
  "quality": "high",
  "size": "1024x1024",
  "n": 2,
  "output_format": "png",
  "reference_file_ids": ["ref_1", "ref_2"],
  "mask_file_id": "mask_1",
  "stream": true
}
```

**SSE Response (stream=true):**
```
event: partial
data: {"index": 0, "b64_preview": "...", "progress": 33}

event: partial
data: {"index": 0, "b64_preview": "...", "progress": 66}

event: completed
data: {"generation_id": "gen_xxx", "images": [{"url": "...", "index": 0}, ...], "usage": {...}}
```

**JSON Response (stream=false):**
```json
{
  "data": {
    "generation_id": "gen_xxx",
    "images": [{"url": "...", "width": 1024, "height": 1024, "index": 0}],
    "revised_prompt": "...",
    "usage": {"input_tokens": 50, "output_tokens": 50}
  }
}
```

### Upload reference

**Request:** multipart/form-data with file upload.
**Response:** `{"data": {"file_id": "...", "url": "...", "width": 800, "height": 600}}`

### Service layer

`DesignerService` wraps `ImageService`:
- `Generate(ctx, userID, req)` — resolves channel, builds processor with extended options, handles streaming callbacks, saves results to DB, deducts credits
- `UploadReference(ctx, userID, file)` — stores reference/mask image, returns file ID
- `GetHistory(ctx, userID, channelID, page, pageSize)` — paginated generation history
- `GetGeneration(ctx, userID, generationID)` — single generation with all images

### Database model

```go
type ImageGeneration struct {
    ID           uint   `gorm:"primaryKey"`
    UserID       uint   `gorm:"index"`
    ChannelID    uint   `gorm:"index"`
    Prompt       string `gorm:"type:text"`
    RevisedPrompt string `gorm:"type:text"`
    Provider     string
    Model        string
    Quality      string
    Size         string
    N            int
    OutputFormat string
    Status       string // "generating", "completed", "failed"
    Error        string
    InputTokens  int
    OutputTokens int
    CreatedAt    time.Time
}

type ImageGenerationResult struct {
    ID           uint   `gorm:"primaryKey"`
    GenerationID uint   `gorm:"index"`
    ImageURL     string
    ImagePath    string
    Width        int
    Height       int
    Index        int
    FileID       string // storage file identifier
}
```

## 3. Frontend: Designer Page

### Files to create

- `studio/src/pages/DesignerPage.tsx` — Main page component
- `studio/src/components/designer/ModelSelector.tsx` — Provider/model switcher
- `studio/src/components/designer/SettingsPanel.tsx` — Left panel with settings
- `studio/src/components/designer/GenerationGrid.tsx` — Center area with generated images
- `studio/src/components/designer/PromptInput.tsx` — Bottom prompt input
- `studio/src/components/designer/HistorySidebar.tsx` — Right panel with history
- `studio/src/components/designer/ReferenceUpload.tsx` — Reference image upload area
- `studio/src/components/designer/ImagePreview.tsx` — Full-size image preview modal
- `studio/src/components/designer/SizeSelector.tsx` — Size/aspect ratio picker
- `studio/src/lib/api/designer.ts` — API client module
- `studio/src/types/designer.ts` — TypeScript types

### Files to modify

- `studio/src/App.tsx` — Add lazy-loaded route for `/designer`
- `studio/src/lib/navigation.ts` — Add nav item to `workflowItems`
- `studio/src/lib/api/index.ts` — Register designer API module
- `studio/src/lib/query-keys.ts` — Add designer query keys

### Page layout

Three-column layout (collapsible side panels):

```
┌──────────────────────────────────────────────────┐
│  Designer                   [ModelSelector: GPT] │
├──────────┬──────────────────────┬────────────────┤
│ Settings │   Generation Area    │    History     │
│ Panel    │                      │    Sidebar     │
│ (240px)  │   ┌──────┐┌──────┐  │   (240px)      │
│          │   │ img1 ││ img2 │  │                │
│ ▸ 尺寸   │   └──────┘└──────┘  │  gen_001 09:30 │
│ ▸ 质量   │                      │  gen_002 09:25 │
│ ▸ 数量   │  [Reference Upload]  │  gen_003 09:20 │
│ ▸ 格式   │  [Mask Upload]      │                │
│          │                      │                │
│          │ ┌──────────────────┐ │                │
│          │ │ Prompt...   [⚡] │ │                │
│          │ └──────────────────┘ │                │
└──────────┴──────────────────────┴────────────────┘
```

### Component details

**ModelSelector**: Three clickable pills/chips for GPT Image 2 / Gemini / Seedream. Switching model updates available settings via capability matrix.

**SettingsPanel**: Collapsible sections:
- Size: Grid of common aspect ratios (1:1, 16:9, 9:16, 4:3, 3:4) + custom pixel input
- Quality: low/medium/high/auto (only for GPT Image 2)
- Count: 1-4 slider (hidden for Gemini)
- Format: png/jpeg/webp (filtered by model)
- Reference images: Drag-and-drop upload area, max 16 images (for GPT Image 2)
- Mask: Single image upload with alpha channel (only for GPT Image 2)

**GenerationGrid**: Shows generated images in responsive grid. During streaming, shows progressively-rendered images with a shimmer/skeleton loading state. Each image card has hover actions: download, upload to channel, view full size.

**PromptInput**: Auto-growing textarea at bottom of center column. Submit button with loading state. Keyboard shortcut: Ctrl+Enter to generate.

**HistorySidebar**: Chronological list of past generations. Each item shows thumbnail + prompt preview + timestamp. Click to load that generation's results into the grid. Paginated with infinite scroll.

**ImagePreview**: Full-screen modal overlay for viewing generated images at full resolution. Includes download button and "upload to channel" action.

### Model capability matrix (drives UI show/hide)

```typescript
const CAPABILITIES: Record<string, ModelCapabilities> = {
  'gpt-image-2': {
    maxRefImages: 16,
    batch: true,
    maxBatch: 10,
    streaming: true,
    inpainting: true,
    qualityLevels: ['auto', 'low', 'medium', 'high'],
    outputFormats: ['png', 'jpeg', 'webp'],
    flexibleSize: true,
  },
  'gemini': {
    maxRefImages: 10,
    batch: false,
    streaming: false,
    inpainting: false,
    qualityLevels: [],
    outputFormats: ['png'],
    flexibleSize: false,
  },
  'seedream': {
    maxRefImages: 1,
    batch: true,
    maxBatch: 4,
    streaming: false,
    inpainting: false,
    qualityLevels: [],
    outputFormats: ['png', 'jpeg'],
    flexibleSize: false,
  },
}
```

## 4. Data Flow

### Generation flow (streaming)

```
1. User selects model, configures settings, types prompt
2. Frontend POST /api/v1/designer/generate (stream: true)
3. Handler creates ImageGeneration record (status: "generating")
4. DesignerService resolves channel → builds Processor with extended GenerateOptions
5. Processor calls Provider.Generate() with StreamCB
6. Provider (e.g. OpenAI) makes streaming API call
7. Each partial image → StreamCB → SSE event → Frontend updates grid progressively
8. Final image → Provider returns result → DesignerService saves to storage + DB
9. SSE "completed" event with final image URLs
10. Frontend displays final images, adds to history
```

### Generation flow (non-streaming)

Same as above but without steps 6-7. Frontend shows loading spinner until completed response.

### Upload reference flow

```
1. User drags image into ReferenceUpload
2. Frontend POST /api/v1/designer/upload-reference (multipart)
3. Server stores file, returns file_id
4. file_id included in generate request
5. DesignerService resolves file_id → local path → passes as RefImagePaths
```

## 5. Error Handling

- **Provider not configured**: Show "Model not available" in ModelSelector, disable that option
- **Credit insufficient**: Return 402, frontend shows credit purchase prompt
- **Streaming failure**: Fall back to non-streaming, show error toast
- **Image too large**: Validate on upload, show size limit error
- **Provider API error**: Map provider errors to user-friendly messages, show in toast
- **Mask validation**: Verify PNG with alpha channel on upload

## 6. Verification Plan

### Backend
1. Run `go test -v ./app/image/...` to verify provider changes don't break existing tests
2. Test each provider with extended options via unit tests (httptest mocks)
3. Test SSE streaming with a test handler
4. Test billing integration with credit deduction
5. Run `make ci` for full CI checks

### Frontend
1. `cd studio && bun run build` — Verify no TypeScript errors
2. `cd studio && bun run test` — Run existing tests (no regression)
3. Manual testing:
   - Switch between models, verify settings panel updates
   - Generate with GPT Image 2, verify streaming partial images appear
   - Upload reference images and mask
   - Generate batch (n=2-4)
   - Check history sidebar loads past generations
   - Verify channel gallery association after generation
   - Test responsive layout on smaller screens

### Integration
1. Start dev server (`make server-dev`)
2. Start frontend (`make web-dev`)
3. Navigate to `/designer`, select model, generate image
4. Verify image appears in channel's file list
5. Test with each provider (OpenAI, Gemini, Volcengine)
