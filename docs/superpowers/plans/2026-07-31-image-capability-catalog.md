# Image Capability Catalog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Replace provider/model-facing image choices across Studio with one configurable, tier-gated capability catalog that supplies neutral labels, effect descriptions, and billing-catalog prices everywhere.

**Architecture:** Server configuration defines opaque capability keys, neutral public metadata, access tiers, internal provider routes, and billing SKU identities. A single server catalog contract powers task, plan, project, and Designer reads/writes. Studio uses one selectable component, one read-only display component, and one query/label resolver for every image capability surface.

**Tech Stack:** Go Fiber v3, YAML, GORM, existing billing catalog/wallet services, React 19, TypeScript, TanStack Query, shadcn/Base UI, Vitest, and table-driven Go tests.

---

### Task 1: Extend configuration and billing contracts

**Files:** Modify server/config/config.go, server/config/image_presets_test.go, server/config.yaml, server/config.example.yaml, server/billing/products.yaml, and server/billing/catalog_contract_test.go.

- [ ] Write failing table tests for Description, SortOrder, BillingSKU, blocked public brand terms, and missing billing SKU.
- [ ] Run `go test ./server/config -run 'TestValidateImagePresets|Test.*Preset' -count=1`; it must fail before implementation.
- [ ] Add ImageModelPreset fields Description, SortOrder, and BillingSKU with YAML tags; reject case-insensitive openai, chatgpt, gpt, gemini, claude, seedream, and doubao in public display text.
- [ ] Replace public keys/names with standard_image/标准图像 and professional_enhance/专业增强. Keep ProviderRoute internal.
- [ ] Add one billing SKU per capability in server/billing/products.yaml; derive price_credits from the actual approved model configuration, never from a hard-coded 500/750/1000 ladder. Preserve historical SKU identities needed by existing billing records.
- [ ] Run `go test ./server/config ./server/billing -count=1`, then commit `feat: define configurable neutral image capabilities`.

### Task 2: Implement the server capability catalog and public endpoint

**Files:** Create server/service/image_capabilities.go and its test; modify server/service/billing_catalog.go, server/handler/image_model.go, server/handler/image_model_test.go, and server/main.go.

- [ ] Write failing tests for tier filtering, sort order, neutral metadata, catalog-derived price_credits, legacy read-only labels, new-write rejection, and serialized JSON without provider/model/route/provider-key fields.
- [ ] Add BillingCatalogService.ResolvePriceBySKUID(ctx, userID, catalogID, skuID), applying existing tier discounts and frozen evidence and returning ErrBillingSKUNotFound when absent.
- [ ] Add capability-service methods ListForUser, ResolveKeyForWrite, and DisplayForKey. Resolve tier fail-closed to Free; use BillingSKU for price and ProviderRoute only after authorization. Add explicit read-only aliases for historical provider-facing keys.
- [ ] Refactor GET /api/v1/image-models to return only key, display_name, description, min_tier, sort_order, price_credits, and is_custom. Do not append a synthetic system-default option.
- [ ] Run `go test ./server/service -run 'TestImageCapability|TestBillingCatalog.*SKU' -count=1` and `go test ./server/handler -run 'TestImageModel|Test.*ImageModel' -count=1`; commit `feat: expose tiered image capability catalog`.

### Task 3: Connect selection to generation and billing

**Files:** Modify server/service/model_config.go, server/service/designer.go, server/service/designer_fixed_sku_test.go, server/service/task_image.go, server/service/task_image_test.go, server/handler/task.go, server/handler/plan.go, and related tests.

- [ ] Write failing tests proving tier authorization and distinct SKU charging for Designer and managed task-image generation, including missing-SKU failure before provider execution.
- [ ] Extend the internal resolved image descriptor with opaque capability key and BillingSKU; retain provider/model only internally.
- [ ] Treat Designer selection as a capability key. Resolve route and SKU server-side; persist selected SKU and pricing snapshot in quotes/generation records.
- [ ] Replace TaskImageService’s hard-coded image-type route pricing with the resolved capability SKU while retaining image type as a size/capability constraint.
- [ ] Allow historical keys for read-only/history (and explicit legacy execution only where required), but reject them for new task/plan writes.
- [ ] Run `go test ./server/service -run 'Test(Designer|TaskImage|ModelConfig|Billing).*' -count=1` and `go test ./server/handler -run 'Test(Task|Plan|ImageModel).*' -count=1`; commit `feat: settle image generation by capability SKU`.

### Task 4: Create shared Studio capability components

**Files:** Create studio/src/components/ImageCapabilitySelector.tsx, studio/src/components/ImageCapabilityDisplay.tsx, and component tests; modify studio/src/components/ImageModelSelector.tsx, studio/src/types/imageModel.ts, studio/src/hooks/useImageModels.ts, and studio/src/lib/api/image-models.ts.

- [ ] Write failing tests for neutral names, descriptions, tier badges, credits prices, hidden empty state, blocked/stale filtering, no provider badges, and neutral legacy fallback.
- [ ] Define public ImageCapabilityOption with key, display_name, description, min_tier, price_credits, sort_order, and is_custom only. Remove provider/model from the public type.
- [ ] Implement the shared selector and read-only display. Centralize sorting, filtering, price formatting, tier badge, selected fallback, and empty state. Remove providerLabel, provider badges, and the 平台默认 synthetic option.
- [ ] Export useImageCapabilities with nameByKey, descriptionByKey, and priceByKey. Keep useImageModels only as a migration alias; pages must not construct labels independently.
- [ ] Run `cd studio && bun run test -- src/components/image-capability.test.tsx`; commit `feat: add shared image capability components`.

### Task 5: Migrate every Studio surface

**Files:** Modify studio/src/components/tasks/TaskFormDialog.tsx, studio/src/pages/PlansPage.tsx, studio/src/pages/ProjectsPage.tsx, studio/src/pages/DesignerPage.tsx, studio/src/lib/api/designer.ts, studio/src/types/designer.ts, studio/src/components/tasks/TaskConfigurationDetails.tsx, studio/src/pages/TaskDetailPage.tsx, and their tests.

- [ ] Replace selector/hook usage in task, plan, and project forms with the shared capability selector/query; retain API field names image_model_key and ecommerce_image_model_key.
- [ ] Replace or adapt the public /designer/providers contract so Designer receives neutral capability IDs/names, descriptions, prices, and only operational constraints needed for reference/mask/size controls. Never expose provider, provider_key, route, or model.
- [ ] Use ImageCapabilityDisplay and the shared resolver in task details and project dialogs; show neutral legacy label or 已停用能力, never a raw key.
- [ ] Add cross-surface fixtures proving task, plan, project, Designer, and detail views render the same label/description/price and ignore stale provider fields.
- [ ] Run `cd studio && bun run test -- src/pages/DesignerPage.provider-contract.test.tsx src/lib/task-form.test.ts src/pages/TaskDetailPage.test.tsx src/components/image-capability.test.tsx`; commit `feat: use image capability catalog across Studio`.

### Task 6: Verify contracts and full builds

**Files:** Modify only affected Go/Studio fixtures that assert legacy provider names.

- [ ] Search public surfaces with `rg -n -i 'chatgpt|openai|gpt-image|gemini|claude|seedream|doubao|providerLabel|provider_key' studio/src server/handler server/service`; distinguish internal logs/cost records from public fields.
- [ ] Run `go test ./server/config ./server/billing ./server/handler ./server/service -count=1`.
- [ ] Run `go test ./...`, `go build -o /tmp/anban-creator-server ./server`, and `go build -o /tmp/anban ./agent`.
- [ ] Run `cd studio && bun run test` and `cd studio && bun run build`.
- [ ] Run `git diff --check`, inspect `git status --short` for unrelated changes, and commit only verification fixture changes with `test: verify unified image capability catalog`.
