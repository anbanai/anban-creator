# 图像模型展示策略设计

## 目标

在保留企业版高级图像能力的同时，移除整个 Studio 图像能力选择链路中的供应商和模型品牌暴露。产品界面只描述用户可理解的能力，不展示底层供应商或模型身份。该策略适用于 Designer、任务创建/编辑、计划配置、项目电商默认值、任务详情和任何复用 `image_model_key` 的入口，而不是只适用于 Designer。

本设计覆盖前端暴露和等级授权，但不认为“改名”本身可以让被禁止的供应商变得合规。如果政策要求完全停用某个供应商，必须另行移除服务端路由和凭据。

## 展示原则

取消“平台默认”作为前端可选择项。未指定模型时，服务端仍使用系统默认配置回退；这个内部行为不再作为一个低价值选项展示给用户。

## 用户可见行为

- Free and Pro users receive the configured `标准图像` capability; higher-tier capabilities are returned only when their `min_tier` is satisfied.
- 当前先提供 `标准图像` 和 `专业增强` 两个能力项；后续可通过服务端配置自由添加更多能力项，不受三档数量限制。
- 每个能力项可以独立配置可见等级、排序、效果说明和计费 SKU。展示名可配置，但必须通过品牌敏感词校验。
- An empty `image_model_key` remains the implicit server default for new and existing tasks; it is not rendered as `平台默认`.
- Public labels and descriptions must not contain provider or model brands, including OpenAI、ChatGPT、GPT、Gemini、Claude、Seedream 和 Doubao，除非合规策略明确允许。
- The selector must not derive a provider badge from response data. It renders only the public capability fields returned by the API.

## Configuration and identity boundary

`image_presets` remains the server-side source of selectable capabilities, but each public preset uses an opaque, capability-oriented key such as `standard_image` or `professional_enhance`. The actual provider route remains server-internal and is never returned as `provider` or `model` to normal Studio clients.

Example:

```yaml
image_presets:
  - key: standard_image
    display_name: "标准图像"
    description: "适合日常内容配图和常规视觉创作"
    provider_route: "image_generation.designer.seedream"
    min_tier: "free"
    billing_sku: "image.standard.designer"
    sort_order: 10

  - key: professional_enhance
    display_name: "专业增强"
    description: "更适合复杂构图、细节表现和高要求视觉任务"
    provider_route: "image_generation.designer.gpt_image_2"
    min_tier: "enterprise"
    billing_sku: "image.professional-enhance.designer"
    sort_order: 20
```

`billing_sku`、`sort_order` 和公开 `description` 是能力目录属性。价格不写死在展示配置中，而是通过计费目录按 SKU 读取实际 credits 价格；新增能力只需新增 preset、对应 billing SKU 和路由映射。

The server validates public display names and descriptions at startup (case-insensitive sensitive-brand blocklist). Configuration fails closed when a blocked term is present. The route name may retain an internal legacy identifier during migration, but it must not be serialized into the public API contract.

## API 与授权

`GET /api/v1/image-models` is the single public image-capability catalog for all Studio surfaces. It returns `key`, `display_name`, `description`, `min_tier`, `sort_order`, `price_credits`, and `is_custom` as applicable. It omits provider/model fields from the public DTO and does not synthesize a system-default option. The Designer page must stop using a provider-identity catalog as a separate public contract; its capability data is either served by this endpoint or by a server-side capability-specific adapter that returns the same neutral identity and pricing fields.

Task and plan create/update handlers continue to validate the submitted key server-side:

- `""` remains valid for all tiers and resolves to the server default. `system_default` remains readable for compatibility but is not returned as a selectable option.
- `professional_enhance`（以及后续新增的高等级能力项）仅在其 `min_tier` 允许的等级使用。
- Unknown keys and keys belonging to a higher tier are rejected.
- Existing persisted legacy keys remain readable for history/detail views but are rejected for new writes.

The server must not use a client-supplied display name to resolve a provider route. Resolution is always by the opaque key against the server configuration.

## 前端改动

Create a shared Studio image-capability component layer. A single selectable component (the successor to `ImageModelSelector`) and a single read-only display component must consume only the public capability contract. Remove `providerLabel` and all provider-derived badges. Render nothing when there are no selectable options, and add the optional capability description and credits price where the current option list presents secondary text.

All current Studio image-model surfaces must use the same catalog and label resolver:

- `TaskFormDialog` for task creation and editing.
- `PlansPage` for plan creation and editing.
- `ProjectsPage` for project e-commerce image defaults.
- `DesignerPage`, replacing the current `/designer/providers` provider-identity display with the shared capability catalog/adapter.
- `TaskDetailPage` and `TaskConfigurationDetails` for read-only task/project configuration display.
- Any future form or detail view that reads or writes `image_model_key`.

Read-only views must use the shared display component and resolve a stored key through the same catalog/legacy-label mapping; they must never render the raw key as a user-facing model name. The shared hook may expose `nameByKey`, `descriptionByKey`, and `priceByKey` so pages cannot independently reconstruct model labels.

Add a defensive allowlist/filter in the hook or selector so stale cached data cannot render a blocked public label. This is defense in depth; authorization remains server-owned.

## 兼容性与迁移

- Do not rewrite historical task/plan rows solely for presentation.
- Existing empty or `system_default` task/plan values continue resolving to the server default, but their new UI representation is implicit rather than the label `平台默认`.
- Add a server-side legacy-key mapping only for read-time labels, with no new-task eligibility.
- Update tests and example configuration to use `standard_image` / `professional_enhance` and neutral public text.
- The public API may include the resolved `price_credits` and `description` for each accessible capability. These values come from the billing catalog and must not expose provider/model identities.
- Keep billing and provider-cost records keyed by internal provider/model identities; these are operational records, not public UI data.

## 测试与验收

Server tests must cover:

1. Listing returns all configured capabilities allowed by the caller's tier, with no system-default option.
2. Listing includes the configured neutral label, effect description, and catalog-derived credits price.
3. Blocked words in display name/description fail configuration validation.
4. Free/Pro create and update requests using the advanced key are rejected.
5. Unknown and legacy keys cannot be used for new writes.
6. API JSON does not contain provider/model fields or blocked brand strings.
7. Read-only task/project responses resolve legacy and current keys to neutral labels without exposing raw provider/model identities.
8. Designer capability responses do not expose provider, model, route, or provider-key fields.

Studio tests must cover:

1. Every image-model selection surface renders configurable labels, effect descriptions, and credits prices from the shared catalog.
2. The selector is not rendered when the selectable list is empty.
3. Provider badges are never rendered, even if stale response data contains provider fields.
4. A blocked/stale option is filtered from the visible list.
5. Task details, plan pages, project defaults, and task forms all use the same neutral label resolver.
6. Designer uses the same selectable/read-only capability components and never renders provider identity.

Acceptance requires targeted server handler/config tests and the full Go and Studio test/build commands required by `AGENTS.md`.

## Non-goal

This change does not by itself remove or disable an underlying OpenAI-compatible provider. If the compliance decision is a complete provider ban, that is a separate configuration and deployment change with its own review.
