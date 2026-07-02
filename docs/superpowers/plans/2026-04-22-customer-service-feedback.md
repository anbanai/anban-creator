# 客服与反馈功能 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Studio 前端增加右下角悬浮 FAB 按钮，提供企微客服二维码和用户反馈两个功能，降低用户与开发者之间的沟通门槛。

**Architecture:** 前端单组件 FeedbackFab（FAB + 展开面板，Tabs 切换联系客服/意见反馈）。后端新增 feedback 表 + Repository + Service + Handler，遵循现有分层模式。一个 API endpoint `POST /api/v1/feedback`。

**Tech Stack:** Go (Fiber v3, GORM) / TypeScript (React 19, Base UI Tabs, Sonner toast)

---

## Task 1: Backend — Feedback Model + Auto-Migration

**Files:**
- Create: `server/model/feedback.go`
- Modify: `server/model/model.go`

- [ ] **Step 1: Create Feedback model**

Create `server/model/feedback.go`:

```go
package model

import "time"

// Feedback represents a user feedback submission.
type Feedback struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID    string    `gorm:"type:char(36);index;not null" json:"user_id"`
	Type      string    `gorm:"type:varchar(20);not null" json:"type"` // bug, suggestion
	Content   string    `gorm:"type:varchar(1000);not null" json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName returns the database table name for Feedback.
func (Feedback) TableName() string { return "feedbacks" }

// Feedback type constants.
const (
	FeedbackTypeBug        = "bug"
	FeedbackTypeSuggestion = "suggestion"
)
```

- [ ] **Step 2: Add Feedback to AutoMigrate**

In `server/model/model.go`, add `&Feedback{}` to the `AutoMigrate` call:

```go
func AutoMigrate(db *gorm.DB) error {
	err := db.AutoMigrate(
		&User{},
		&LoginSession{},
		&Channel{},
		&Plan{},
		&Task{},
		&TaskFile{},
		&CreditTransaction{},
		&APIKey{},
		&Feedback{},
	)
	if err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 3: Verify model compiles**

Run: `cd /Users/medivh/WORKSPACE/anban-creator && go build ./server/model/...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add server/model/feedback.go server/model/model.go
git commit -m "feat(server): add Feedback model and auto-migration"
```

---

## Task 2: Backend — Feedback Repository

**Files:**
- Create: `server/repository/feedback.go`
- Modify: `server/repository/repository.go`

- [ ] **Step 1: Add FeedbackRepository interface**

In `server/repository/repository.go`, add the interface after `APIKeyRepository` and add `Feedbacks()` to the `Repository` interface:

Add to the `Repository` interface (line 21, after `APIKeys() APIKeyRepository`):

```go
Feedbacks() FeedbackRepository
```

Add the `FeedbackRepository` interface after the existing repository interfaces:

```go
// FeedbackRepository provides access to the feedbacks table.
type FeedbackRepository interface {
	Create(ctx context.Context, feedback *model.Feedback) error
}
```

- [ ] **Step 2: Add feedbacks field to repository struct and initialize**

In the `repository` struct (around line 116), add `feedbacks FeedbackRepository`.

In the `New` function (around line 129), add `feedbacks: newFeedbackRepository(db)` to the return struct.

Add the accessor method:

```go
func (r *repository) Feedbacks() FeedbackRepository { return r.feedbacks }
```

Do the same for `txRepository` struct and `newTxRepository` function — add `feedbacks` field, initialize in constructor, add accessor.

- [ ] **Step 3: Create feedback repository implementation**

Create `server/repository/feedback.go`:

```go
package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

type feedbackRepository struct {
	db *gorm.DB
}

func newFeedbackRepository(db *gorm.DB) FeedbackRepository {
	return &feedbackRepository{db: db}
}

func (r *feedbackRepository) Create(ctx context.Context, feedback *model.Feedback) error {
	return r.db.WithContext(ctx).Create(feedback).Error
}
```

- [ ] **Step 4: Verify compilation**

Run: `cd /Users/medivh/WORKSPACE/anban-creator && go build ./server/repository/...`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add server/repository/feedback.go server/repository/repository.go
git commit -m "feat(server): add FeedbackRepository interface and implementation"
```

---

## Task 3: Backend — Feedback Service

**Files:**
- Create: `server/service/feedback.go`

- [ ] **Step 1: Create FeedbackService**

Create `server/service/feedback.go`:

```go
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// FeedbackService handles feedback business logic.
type FeedbackService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

// NewFeedbackService creates a new FeedbackService.
func NewFeedbackService(repo repository.Repository, logger *zerolog.Logger) *FeedbackService {
	return &FeedbackService{repo: repo, logger: logger}
}

// Create validates and persists a new feedback entry.
func (s *FeedbackService) Create(ctx context.Context, userID, feedbackType, content string) (*model.Feedback, error) {
	if feedbackType != model.FeedbackTypeBug && feedbackType != model.FeedbackTypeSuggestion {
		return nil, fmt.Errorf("invalid feedback type: %s", feedbackType)
	}
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}
	if len(content) > 1000 {
		return nil, fmt.Errorf("content must be 1000 characters or less")
	}

	feedback := &model.Feedback{
		ID:      uuid.New().String(),
		UserID:  userID,
		Type:    feedbackType,
		Content: content,
	}

	if err := s.repo.Feedbacks().Create(ctx, feedback); err != nil {
		return nil, fmt.Errorf("create feedback: %w", err)
	}

	s.logger.Info().
		Str("feedback_id", feedback.ID).
		Str("user_id", userID).
		Str("type", feedbackType).
		Msg("feedback created")

	return feedback, nil
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd /Users/medivh/WORKSPACE/anban-creator && go build ./server/service/...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add server/service/feedback.go
git commit -m "feat(server): add FeedbackService with Create method"
```

---

## Task 4: Backend — Feedback Handler + Test

**Files:**
- Create: `server/handler/feedback.go`
- Create: `server/handler/feedback_test.go`

- [ ] **Step 1: Create FeedbackHandler**

Create `server/handler/feedback.go`:

```go
package handler

import (
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
)

// FeedbackHandler handles feedback-related HTTP endpoints.
type FeedbackHandler struct {
	service *service.FeedbackService
	logger  *zerolog.Logger
}

// NewFeedbackHandler creates a new FeedbackHandler.
func NewFeedbackHandler(svc *service.FeedbackService, logger *zerolog.Logger) *FeedbackHandler {
	return &FeedbackHandler{service: svc, logger: logger}
}

type createFeedbackRequest struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// Create handles POST /api/v1/feedback.
func (h *FeedbackHandler) Create(c fiber.Ctx) error {
	var req createFeedbackRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.Type == "" {
		return Error(c, fiber.StatusBadRequest, "type is required")
	}
	if req.Content == "" {
		return Error(c, fiber.StatusBadRequest, "content is required")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	feedback, err := h.service.Create(c.Context(), userID, req.Type, req.Content)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to create feedback")
		return Error(c, fiber.StatusInternalServerError, "failed to create feedback")
	}

	return c.Status(fiber.StatusCreated).JSON(Response{Code: 0, Msg: "success", Data: feedback})
}
```

- [ ] **Step 2: Write handler test**

Create `server/handler/feedback_test.go`:

```go
package handler

import (
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

func setupFeedbackHandler(t *testing.T) (*FeedbackHandler, repository.Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.Feedback{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	repo := repository.New(db)
	logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
	svc := service.NewFeedbackService(repo, &logger)
	return NewFeedbackHandler(svc, &logger), repo
}

func TestFeedbackService_Create(t *testing.T) {
	_, repo := setupFeedbackHandler(t)
	ctx := t.Context()

	t.Run("valid bug feedback", func(t *testing.T) {
		logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
		svc := service.NewFeedbackService(repo, &logger)

		fb, err := svc.Create(ctx, "user-1", model.FeedbackTypeBug, "Something is broken")
		if err != nil {
			t.Fatalf("create feedback: %v", err)
		}
		if fb.ID == "" {
			t.Error("expected non-empty ID")
		}
		if fb.UserID != "user-1" {
			t.Errorf("expected user_id 'user-1', got %q", fb.UserID)
		}
		if fb.Type != model.FeedbackTypeBug {
			t.Errorf("expected type 'bug', got %q", fb.Type)
		}
	})

	t.Run("valid suggestion feedback", func(t *testing.T) {
		logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
		svc := service.NewFeedbackService(repo, &logger)

		fb, err := svc.Create(ctx, "user-1", model.FeedbackTypeSuggestion, "Add dark mode")
		if err != nil {
			t.Fatalf("create feedback: %v", err)
		}
		if fb.Type != model.FeedbackTypeSuggestion {
			t.Errorf("expected type 'suggestion', got %q", fb.Type)
		}
	})

	t.Run("empty content rejected", func(t *testing.T) {
		logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
		svc := service.NewFeedbackService(repo, &logger)

		_, err := svc.Create(ctx, "user-1", model.FeedbackTypeBug, "")
		if err == nil {
			t.Error("expected error for empty content")
		}
	})

	t.Run("invalid type rejected", func(t *testing.T) {
		logger := zerolog.New(zerolog.NewTestWriter(nil)).With().Timestamp().Logger()
		svc := service.NewFeedbackService(repo, &logger)

		_, err := svc.Create(ctx, "user-1", "invalid", "test content")
		if err == nil {
			t.Error("expected error for invalid type")
		}
	})
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `cd /Users/medivh/WORKSPACE/anban-creator && go test -v ./server/handler/ -run TestFeedbackService`
Expected: all PASS

- [ ] **Step 4: Commit**

```bash
git add server/handler/feedback.go server/handler/feedback_test.go
git commit -m "feat(server): add FeedbackHandler with tests"
```

---

## Task 5: Backend — Wiring (main.go + router)

**Files:**
- Modify: `server/main.go` (lines ~183-234, ~316-341)
- Modify: `server/router/router.go` (lines ~29-53, ~289-293)

- [ ] **Step 1: Add FeedbackService and FeedbackHandler to main.go**

In `server/main.go`, in the "13. Create services" section (after line 193), add:

```go
var feedbackSvc *service.FeedbackService
```

Inside the `if repo != nil` block (after line 193), add:

```go
feedbackSvc = service.NewFeedbackService(repo, log)
```

In the "14. Create handlers" section (after line 216), add to the var block:

```go
var feedbackHandler *handler.FeedbackHandler
```

Inside the `if repo != nil` block (after line 226), add:

```go
feedbackHandler = handler.NewFeedbackHandler(feedbackSvc, log)
```

In the Services struct initialization (line ~317-341), add:

```go
FeedbackHandler: feedbackHandler,
```

- [ ] **Step 2: Add route registration in router.go**

In `server/router/router.go`, add `FeedbackHandler *handler.FeedbackHandler` to the `Services` struct.

After the API Key endpoints block (around line 286), add:

```go
// ---------------------------------------------------------------------------
// Feedback endpoint
// ---------------------------------------------------------------------------

if svc.FeedbackHandler != nil {
	apiV1.Post("/feedback", svc.FeedbackHandler.Create)
}
```

- [ ] **Step 3: Verify full server compilation**

Run: `cd /Users/medivh/WORKSPACE/anban-creator && go build ./server/...`
Expected: no errors

- [ ] **Step 4: Run all server tests**

Run: `cd /Users/medivh/WORKSPACE/anban-creator && go test -v ./server/...`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add server/main.go server/router/router.go
git commit -m "feat(server): wire FeedbackService and FeedbackHandler into app"
```

---

## Task 6: Frontend — Feedback API Module

**Files:**
- Create: `studio/src/lib/api/feedback.ts`
- Modify: `studio/src/lib/api/index.ts`

- [ ] **Step 1: Create feedback API module**

Create `studio/src/lib/api/feedback.ts`:

```ts
import { http, unwrap } from '@/lib/http-client'

export interface CreateFeedbackRequest {
  type: 'bug' | 'suggestion'
  content: string
}

export interface Feedback {
  id: string
  user_id: string
  type: string
  content: string
  created_at: string
  updated_at: string
}

export const feedbackApi = {
  create: async (data: CreateFeedbackRequest): Promise<Feedback> => {
    return unwrap<Feedback>(http.post('/feedback', data))
  },
}
```

- [ ] **Step 2: Add to barrel export**

In `studio/src/lib/api/index.ts`, add the import and export:

```ts
import { feedbackApi } from './feedback'

export const api = {
  auth: authApi,
  plans: plansApi,
  tasks: tasksApi,
  timeline: timelineApi,
  channels: channelsApi,
  credits: creditsApi,
  apiKeys: apiKeysApi,
  usage: usageApi,
  feedback: feedbackApi,
}
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/medivh/WORKSPACE/anban-creator/studio && bun run tsc --noEmit 2>&1 | head -20`
Expected: no errors related to feedback files

- [ ] **Step 4: Commit**

```bash
git add studio/src/lib/api/feedback.ts studio/src/lib/api/index.ts
git commit -m "feat(studio): add feedback API module"
```

---

## Task 7: Frontend — FeedbackFab Component

**Files:**
- Create: `studio/src/components/layout/FeedbackFab.tsx`

Note: The user needs to provide the actual WeChat QR code image at `studio/public/wechat-qr.png`. For development, any placeholder PNG will work.

- [ ] **Step 1: Create FeedbackFab component**

Create `studio/src/components/layout/FeedbackFab.tsx`:

```tsx
import { useEffect, useRef, useState } from 'react'
import { MessageCircle } from 'lucide-react'
import { toast } from 'sonner'

import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Button } from '@/components/ui/Button'
import { Textarea } from '@/components/ui/textarea'
import { feedbackApi } from '@/lib/api/feedback'

type FeedbackType = 'bug' | 'suggestion'

export default function FeedbackFab() {
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  // Feedback form state
  const [type, setType] = useState<FeedbackType>('bug')
  const [content, setContent] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // Click outside to close
  useEffect(() => {
    if (!open) return
    function handleClick(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [open])

  async function handleSubmit() {
    const trimmed = content.trim()
    if (!trimmed) return
    setSubmitting(true)
    try {
      await feedbackApi.create({ type, content: trimmed })
      toast.success('反馈提交成功，感谢您的建议！')
      setContent('')
      setType('bug')
    } catch {
      toast.error('提交失败，请稍后重试')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div ref={containerRef} className="fixed bottom-6 right-6 z-50">
      {/* Expandable panel */}
      {open && (
        <div className="absolute bottom-full right-0 mb-2 w-80 rounded-lg border border-border bg-card shadow-lg">
          <Tabs defaultValue="contact">
            <div className="border-b border-border px-1 pt-1">
              <TabsList className="w-full">
                <TabsTrigger value="contact" className="flex-1">
                  联系客服
                </TabsTrigger>
                <TabsTrigger value="feedback" className="flex-1">
                  意见反馈
                </TabsTrigger>
              </TabsList>
            </div>

            {/* Contact tab */}
            <TabsContent value="contact" className="flex flex-col items-center gap-3 p-4">
              <img
                src="/wechat-qr.png"
                alt="企业微信客服二维码"
                className="h-[200px] w-[200px] rounded-md object-contain"
              />
              <p className="text-sm text-muted-foreground">扫码添加企业微信客服</p>
            </TabsContent>

            {/* Feedback tab */}
            <TabsContent value="feedback" className="flex flex-col gap-3 p-4">
              {/* Type selector */}
              <div className="flex gap-2">
                <Button
                  variant={type === 'bug' ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => setType('bug')}
                >
                  问题反馈
                </Button>
                <Button
                  variant={type === 'suggestion' ? 'default' : 'outline'}
                  size="sm"
                  onClick={() => setType('suggestion')}
                >
                  功能建议
                </Button>
              </div>

              {/* Content */}
              <Textarea
                placeholder="请描述您遇到的问题或想要的功能..."
                rows={4}
                value={content}
                onChange={(e) => setContent(e.target.value)}
                maxLength={1000}
              />

              {/* Submit */}
              <Button
                className="w-full"
                loading={submitting}
                disabled={!content.trim()}
                onClick={handleSubmit}
              >
                提交反馈
              </Button>
            </TabsContent>
          </Tabs>
        </div>
      )}

      {/* FAB button */}
      <Button
        variant="default"
        className="h-12 w-12 rounded-full shadow-lg"
        onClick={() => setOpen(!open)}
      >
        <MessageCircle className="size-5" />
      </Button>
    </div>
  )
}
```

- [ ] **Step 2: Add placeholder QR code image**

The user needs to provide the actual WeChat QR code image. Place it at `studio/public/wechat-qr.png`. For development testing, use any 200x200 PNG placeholder.

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/medivh/WORKSPACE/anban-creator/studio && bun run tsc --noEmit 2>&1 | head -20`
Expected: no errors related to FeedbackFab

- [ ] **Step 4: Commit**

```bash
git add studio/src/components/layout/FeedbackFab.tsx
git commit -m "feat(studio): add FeedbackFab component with contact and feedback tabs"
```

---

## Task 8: Frontend — Integration into AppLayout

**Files:**
- Modify: `studio/src/components/layout/AppLayout.tsx`

- [ ] **Step 1: Import and add FeedbackFab to AppLayout**

In `studio/src/components/layout/AppLayout.tsx`:

```tsx
import { Outlet } from 'react-router-dom'
import { PageTransition } from '@/components/PageTransition'
import Sidebar from './Sidebar'
import FeedbackFab from './FeedbackFab'

export default function AppLayout() {
  return (
    <div className="flex h-screen bg-background text-foreground">
      <Sidebar />
      <main className="flex-1 overflow-auto px-4 py-6 md:px-8 md:py-8">
        <PageTransition>
          <Outlet />
        </PageTransition>
      </main>
      <FeedbackFab />
    </div>
  )
}
```

- [ ] **Step 2: Verify full build**

Run: `cd /Users/medivh/WORKSPACE/anban-creator/studio && bun run tsc --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add studio/src/components/layout/AppLayout.tsx
git commit -m "feat(studio): integrate FeedbackFab into AppLayout"
```

---

## Verification

1. Start backend: `make server-dev`
2. Start frontend: `make web-dev`
3. Log in to the Studio
4. Confirm FAB button appears at bottom-right corner
5. Click FAB → panel expands with two tabs
6. 「联系客服」tab shows QR code image
7. 「意见反馈」tab → select type, enter text, submit → success toast
8. Check MySQL `feedbacks` table for the new record
9. Click outside panel → panel closes
10. Run all tests: `go test -v ./server/...` and `cd studio && bun run test`
