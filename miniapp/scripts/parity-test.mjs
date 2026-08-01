import { readFileSync, existsSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import assert from 'node:assert/strict'
import { fileURLToPath } from 'node:url'

const root = resolve(fileURLToPath(new URL('.', import.meta.url)), '..')

function read(path) {
  return readFileSync(resolve(root, path), 'utf8')
}

function assertFile(path) {
  assert.equal(existsSync(resolve(root, path)), true, `${path} should exist`)
}

function assertNoFile(path) {
  assert.equal(existsSync(resolve(root, path)), false, `${path} should not exist`)
}

function assertContains(path, terms) {
  const body = read(path)
  for (const term of terms) {
    assert.equal(body.includes(term), true, `${path} should contain ${term}`)
  }
}

function assertNotContains(path, terms) {
  const body = read(path)
  for (const term of terms) {
    assert.equal(body.includes(term), false, `${path} should not contain ${term}`)
  }
}

function assertOccurrenceCount(path, term, expectedCount) {
  const body = read(path)
  const count = body.split(term).length - 1
  assert.equal(count, expectedCount, `${path} should contain ${term} ${expectedCount} times`)
}

function filesUnder(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = resolve(dir, entry.name)
    return entry.isDirectory() ? filesUnder(path) : [path]
  })
}

function assertSubmitButtonsDisabled(path, clickHandler, disabledBinding, expectedCount) {
  const tags = [...read(path).matchAll(/<AbButton\b[\s\S]*?>/g)]
    .map((match) => match[0])
    .filter((tag) => tag.includes(`@click="${clickHandler}"`))
  assert.equal(tags.length, expectedCount, `${path} should have ${expectedCount} ${clickHandler} submit buttons`)
  for (const tag of tags) {
    assert.equal(tag.includes(`:disabled="${disabledBinding}"`), true, `${path} ${clickHandler} button must be disabled by ${disabledBinding}`)
  }
}

function assertSubmitGuard(path, functionName, guardPattern) {
  const body = read(path)
  const functionStart = new RegExp(`async function ${functionName}\\(\\) \\{\\s*${guardPattern.source}`)
  assert.match(body, functionStart, `${path} ${functionName} must reject submission before validation or API calls`)
}

for (const path of [
  'src/api/agent-profiles.ts',
  'src/api/request.ts',
  'src/api/api-keys.ts',
  'src/api/designer.ts',
  'src/api/image-capabilities.ts',
  'src/api/resources.ts',
  'src/api/topic-pool.ts',
  'src/types/designer.ts',
  'src/types/imageCapability.ts',
  'src/types/agent-profile.ts',
  'src/types/resource.ts',
  'src/types/topic-pool.ts',
  'src/pages/designer/index.vue',
  'src/pages/connect/claude-code.vue',
  'src/pages/settings/api-keys.vue',
  'src/pages/settings/password.vue',
  'src/components/business/ImageCapabilitySelector.vue',
  'src/components/business/ImageAspectRatioField.vue',
]) {
  assertFile(path)
}

assertContains('src/pages.json', [
  '"path": "pages/designer/index"',
  '"path": "pages/connect/claude-code"',
  '"path": "pages/settings/api-keys"',
  '"path": "pages/settings/password"',
])
assertNotContains('src/pages.json', ['"path": "pages/settings/model-config"'])
assertNoFile('src/api/model-config.ts')
assertNoFile('src/pages/settings/model-config.vue')
assertNoFile('src/api/image-models.ts')
assertNoFile('src/components/business/ImageModelSelector.vue')
assertNoFile('src/types/imageModel.ts')

assertContains('src/api/index.ts', [
  "export const api",
  'agentProfiles',
  'apiKeys',
  'designer',
  'imageCapabilities',
  'resources',
  'topicPool',
])

// === Agent execution profiles (2026-07-28): server-owned capability catalog ===
assertContains('src/api/agent-profiles.ts', [
  'export const agentProfilesApi',
  "'/agent/execution-profiles'",
])

assertContains('src/types/agent-profile.ts', [
  "export type AgentExecutionProfileID = 'effective' | 'balanced' | 'quality'",
  'display_name: string',
  'provider: string',
  'model_name: string',
  'schema_version: 3',
  'envs: Record<string, string>',
  "min_tier: 'free' | 'pro' | 'enterprise'",
  'available: boolean',
  'unavailable_reason?: string',
])
assertNotContains('src/types/agent-profile.ts', [
  'cost_effective',
  'maximum_quality',
  'AgentModelMatrix',
  'AgentClaudeControls',
  'model_id: string',
  'thinking_required',
  'reasoning_effort',
  'context_window',
])

assertContains('src/types/index.ts', [
  "from './agent-profile'",
])

assertFile('src/utils/execution-profiles.ts')
const {
  cheapestAvailableExecutionProfile,
  resolveExecutionProfileSelection,
  taskPriceForExecutionProfile,
} = await import('../src/utils/execution-profiles.ts')

const profileCatalog = {
  catalog_id: 'retail-test',
  currency: 'credits',
  skus: [
    { id: 'seednote-cost', operation: 'task.seednote', charge_policy: 'task_admission', execution_profile: 'effective', price_credits: 4000, delivery: 'task' },
    { id: 'seednote-balanced', operation: 'task.seednote', charge_policy: 'task_admission', execution_profile: 'balanced', price_credits: 5000, delivery: 'task' },
    { id: 'seednote-max', operation: 'task.seednote', charge_policy: 'task_admission', execution_profile: 'quality', price_credits: 15000, delivery: 'task' },
    { id: 'article-balanced', operation: 'task.article', charge_policy: 'task_admission', execution_profile: 'balanced', price_credits: 6000, delivery: 'task' },
  ],
}
const profileCapabilities = [
  { id: 'effective', available: true },
  { id: 'balanced', available: true },
  { id: 'quality', available: false, unavailable_reason: '需要企业版' },
]

assert.equal(taskPriceForExecutionProfile(profileCatalog, 'seednote', 'balanced'), 5000)
assert.equal(taskPriceForExecutionProfile(profileCatalog, 'article', 'balanced'), 6000)
assert.equal(taskPriceForExecutionProfile(profileCatalog, 'article', 'effective'), undefined)
assert.equal(cheapestAvailableExecutionProfile(profileCapabilities, profileCatalog, 'seednote'), 'effective')
assert.equal(
  resolveExecutionProfileSelection(
    'quality',
    true,
    profileCapabilities,
    profileCatalog,
    'seednote',
  ),
  'quality',
)
assert.equal(
  resolveExecutionProfileSelection('', false, profileCapabilities, profileCatalog, 'seednote'),
  'effective',
)

assertContains('src/types/billing.ts', [
  'execution_profile?: AgentExecutionProfileID',
])

assertFile('src/components/business/ExecutionProfileSelector.vue')
assertContains('src/components/business/ExecutionProfileSelector.vue', [
  'v-for="profile in profiles"',
  'profile.display_name',
  'profile.min_tier',
  'profile.available',
  'profile.unavailable_reason',
  'unavailableReason(profile)',
  '@tap="selectProfile(profile)"',
  '最低套餐：Pro 版及以上',
])
assertNotContains('src/components/business/ExecutionProfileSelector.vue', [
  'profile.provider',
  'profile.model_name',
  'profile.models',
  'modelRows(profile)',
  'min_tier ===',
])

assertContains('src/types/auth.ts', [
  'has_password',
])

assertContains('src/types/task.ts', [
  'topic?: string',
  'execution_profile: AgentExecutionProfileID',
  'agent_profile_snapshot: AgentProfileSnapshot',
  'export interface TaskBillingChargeDetail',
  "charge_kind: 'task' | 'operation' | 'reversal'",
  'billing_total_credits?: number',
  'billing_charge_details?: TaskBillingChargeDetail[]',
  'skip_reference_image?: boolean',
  'reference_image?: ReferenceAssetView | null',
  'reference_image?: ReferenceImageSelection | null',
  'watermark?: boolean',
])

// Task cards show the cumulative amount, while detail pages enumerate every
// server-owned charge and reversal instead of exposing only task admission.
assertFile('src/utils/task-billing.ts')
const {
  taskBillingAmountLabel,
  taskBillingChargeLabel,
  taskBillingDetails,
  taskBillingIdentity,
  taskBillingPricingEvidence,
  taskBillingTotal,
} = await import('../src/utils/task-billing.ts')

const billedTask = {
  billing_price_credits: 5000,
  billing_total_credits: 5750,
  billing_sku_id: 'task.seednote.standard.v1',
  billing_charge_details: [
    { id: 'task-charge', charge_kind: 'task', sku_id: 'task.seednote.standard.v1', credits: 5000 },
    { id: 'analysis-charge', charge_kind: 'operation', sku_id: 'analysis.content.v1', resource_type: 'analysis', tool_call_id: 'analysis:1', credits: 300 },
    { id: 'image-charge', charge_kind: 'operation', sku_id: 'image.standard', resource_type: 'image', credits: 500, pricing_tier: 'pro', list_price_credits: 600, discount_credits: 100 },
    { id: 'reversal', charge_kind: 'reversal', sku_id: 'image.standard', resource_type: 'image', reversal_of_id: 'image-charge', credits: -50 },
  ],
}
assert.equal(taskBillingTotal(billedTask), 5750)
assert.equal(taskBillingDetails(billedTask).length, 4)
assert.equal(taskBillingChargeLabel(billedTask.billing_charge_details[0]), '任务固定费')
assert.equal(taskBillingChargeLabel(billedTask.billing_charge_details[1]), '内容分析费')
assert.equal(taskBillingChargeLabel(billedTask.billing_charge_details[2]), '标准图像生成费')
assert.equal(taskBillingChargeLabel(billedTask.billing_charge_details[3]), '标准图像生成费退回')
assert.equal(taskBillingIdentity(billedTask.billing_charge_details[1]), 'analysis.content.v1 · analysis:1')
assert.equal(taskBillingPricingEvidence(billedTask.billing_charge_details[2]), '专业版 · 标准价 600，优惠 100')
assert.equal(taskBillingAmountLabel(billedTask.billing_charge_details[3]), '退回 50 积分')
assert.equal(taskBillingDetails({ billing_price_credits: 5000, billing_sku_id: 'task.seednote.standard.v1' }).length, 1)

assertContains('src/components/business/TaskCard.vue', [
  'taskBillingTotal',
  '累计扣费：{{ billingCredits.toLocaleString() }} 积分',
])
assertContains('src/pages/tasks/detail.vue', [
  'taskBillingDetails',
  'taskBillingChargeLabel',
  'taskBillingIdentity',
  'taskBillingPricingEvidence',
  'taskBillingAmountLabel',
  '积分明细',
  '累计扣费',
  'v-for="(detail, index) in billingDetails"',
  'task.agent_profile_snapshot',
  'agentProfileRows',
])
assertContains('src/pages/tasks/detail.vue', [
  "envs.CLAUDE_CODE_EFFORT_LEVEL",
  "envs.CLAUDE_CODE_MAX_CONTEXT_TOKENS",
  "envs.CLAUDE_CODE_DISABLE_THINKING",
])
assertNotContains('src/pages/tasks/detail.vue', [
  'task.agent_profile_snapshot.provider',
  'envs.ANTHROPIC_MODEL',
  'ANTHROPIC_AUTH_TOKEN',
  'snapshot.models',
  'snapshot.claude',
])

assertContains('src/types/project.ts', [
  'description?: string',
  'layout: string',
  'image_preset: string',
  'layout?: string',
  'image_preset?: string',
])

assertContains('src/types/viral-analysis.ts', [
  "export type ViralAnalysisSourceType = 'note'",
  'evidence_table',
  'clone_suggestions',
  'viral_template',
  'template_meta',
])

assertContains('src/types/index.ts', [
  "from './resource'",
  "from './topic-pool'",
  "from './designer'",
])

assertContains('src/api/image-capabilities.ts', ["'/image-capabilities'", 'ImageCapabilityListResponse'])
assertContains('src/types/imageCapability.ts', [
  'export interface ImageCapabilityOption',
  'price_credits?: number',
  'features?: ImageCapabilityFeatures',
  'size_presets: string[]',
  'max_reference_images: number',
])
assertContains('src/components/business/ImageCapabilitySelector.vue', [
  'option.display_name',
  'option.description',
  '每张 {{ option.price_credits.toLocaleString() }} 积分',
  'const retiredValue = computed(() =>',
  'props.modelValue && !props.options.some((option) => option.key === props.modelValue)',
  'v-if="retiredValue"',
  '已停用图像能力（请重新选择）',
])
assertNotContains('src/components/business/ImageCapabilitySelector.vue', ['provider', 'model_name'])
assertContains('src/components/business/ImageAspectRatioField.vue', [
  '智能适配',
  'supportedSizes',
  "value: ''",
  "value: '21:9'",
])

assertContains('src/types/designer.ts', ['capability_key: string', 'capability_name?: string'])
assertNotContains('src/types/designer.ts', ['provider_id', 'provider: string', 'model: string', 'getModelCapabilities'])

for (const path of [
  'src/api/designer.ts',
  'src/types/designer.ts',
  'src/pages/designer/index.vue',
]) {
  assertNotContains(path, [
    'execution_profile',
    'agentProfilesApi',
    'ExecutionProfileSelector',
  ])
}

assertContains('src/pages/projects/detail.vue', [
  "resourcesApi.list('themes'",
  "resourcesApi.list('layouts'",
  "resourcesApi.list('image_presets'",
  'topicPoolApi.list',
  'topicPoolApi.create',
  'topicPoolApi.reset',
  'topicPoolApi.delete',
  'layout',
  'image_preset',
])

// Task detail clone is now server-side (tasksApi.retry preserves ALL fields —
// 3D style/persona/ecommerce/model/watermark/goal — replacing the old partial
// client-side create() that forwarded only a subset). File handling stays intact.
assertContains('src/pages/tasks/detail.vue', [
  'tasksApi.retry',
  'tasksApi.getFiles',
  'tasksApi.getSeednoteAnalytics',
  'tasksApi.fileDownloadUrl',
  'tasksApi.zipDownloadUrl',
])

assertContains('src/pages/tasks/detail.vue', [
  '`execution_profile=${encodeURIComponent(t.execution_profile)}`',
])

assertContains('src/pages/designer/index.vue', [
  '<ImageCapabilitySelector',
  ':model-value="selectedCapabilityKey"',
  '@update:model-value="selectCapability"',
  'canInpaint',
  'startEdit',
  'mask_file_id',
  'capability_key: selectedCapability.value.id',
  'chooseMask',
  'generateEdit',
  'const usableCapabilities = computed(() => capabilities.value.filter((capability) => capability.enabled && capability.priceAvailable === true))',
  'selectedCapability.value?.priceAvailable === true',
])
assertOccurrenceCount('src/pages/designer/index.vue', 'selectedCapability.value?.priceAvailable === true', 2)
assertContains('src/components/business/ImageCapabilitySelector.vue', [
  "option.enabled !== true || option.price_available !== true",
  'capability-option--disabled',
])
assertContains('src/api/designer.ts', ["'/designer/quote'", "'/designer/generate'", 'request_fingerprint'])
assertNotContains('src/api/designer.ts', ["'/designer/providers'", 'getProviders'])
assertNotContains('src/pages/designer/index.vue', [
  'class="capability-card"',
  'selectedProvider',
  'provider_id',
  'item.provider',
  'item.model',
  '模型：',
])

assertContains('src/pages/tasks/create.vue', [
  'form.reference_image',
  "uploadImage(filePath, 'task_reference')",
  'upload_session_id: uploaded.upload_session_id',
  'form.watermark',
  'skip_reference_image: form.skip_reference_image',
  'reference_image: form.reference_image',
  'watermark: form.watermark',
  '<ExecutionProfileSelector',
  'v-model="form.execution_profile"',
  'agentProfilesApi.list()',
  'resolveExecutionProfileSelection',
  'taskPriceForExecutionProfile',
  'execution_profile: executionProfile',
  'image_capability_key: form.image_capability_key || undefined',
  '<ImageCapabilitySelector',
  '<ImageAspectRatioField',
  'form.execution_profile = String(query.execution_profile) as AgentExecutionProfileID',
  'resolveExecutionProfileSelection',
  'imageRatioUnsupported',
  '当前图像能力不支持所选比例，请重新选择比例或智能适配',
  'const imageCapabilityUnavailable = computed(() =>',
  'selectedCapability.value.enabled !== true',
  '该图像能力已停用，请重新选择',
])
assertOccurrenceCount('src/pages/tasks/create.vue', 'if (imageCapabilityUnavailable.value)', 2)
assertNotContains('src/pages/tasks/create.vue', [
  "if (form.image_ratio && supported && !supported.includes(form.image_ratio)) form.image_ratio = ''",
])
assertNotContains('src/pages/tasks/create.vue', ['provider:', 'models:', 'claude:'])

assertContains('src/types/task.ts', [
  'execution_profile: AgentExecutionProfileID',
])

assertContains('src/pages/plans/create.vue', [
  'skipReferenceImage',
  'referenceImage',
  "uploadImage(filePath, 'task_reference')",
  'upload_session_id: uploaded.upload_session_id',
  'watermark',
  'skip_reference_image: form.skipReferenceImage',
  'reference_image: form.referenceImage',
  '<ExecutionProfileSelector',
  'v-model="form.executionProfile"',
  'agentProfilesApi.list()',
  'resolveExecutionProfileSelection',
  'taskPriceForExecutionProfile',
  'execution_profile: executionProfile',
  'image_capability_key: form.imageCapabilityKey || undefined',
  '<ImageCapabilitySelector',
  '<ImageAspectRatioField',
  'form.executionProfile = plan.execution_profile',
  'resolveExecutionProfileSelection',
  'imageRatioUnsupported',
  '当前图像能力不支持所选比例，请重新选择比例或智能适配',
  'const imageCapabilityUnavailable = computed(() =>',
  'selectedCapability.value.enabled !== true',
  '该图像能力已停用，请重新选择',
])
assertOccurrenceCount('src/pages/plans/create.vue', 'if (imageCapabilityUnavailable.value)', 2)
assertNotContains('src/pages/plans/create.vue', [
  "if (form.imageRatio && supported && !supported.includes(form.imageRatio)) form.imageRatio = ''",
])
assertNotContains('src/pages/plans/create.vue', ['provider:', 'models:', 'claude:'])

assertContains('src/types/plan.ts', [
  'execution_profile: AgentExecutionProfileID',
])

assertContains('src/pages/projects/detail.vue', [
  'form.reference_image',
  'referencePreviewUrl',
  'upload_session_id: result.upload_session_id',
  'reference_image: form.reference_image',
  '<ImageCapabilitySelector',
  '<ImageAspectRatioField',
  'image_capability_key: form.image_capability_key || undefined',
  'imageRatioUnsupported',
  '当前图像能力不支持所选比例，请重新选择比例或智能适配',
  'const imageCapabilityUnavailable = computed(() =>',
  'selectedCapability.value.enabled !== true',
  '该图像能力已停用，请重新选择',
])
assertOccurrenceCount('src/pages/projects/detail.vue', 'if (imageCapabilityUnavailable.value)', 1)
assertNotContains('src/pages/projects/detail.vue', [
  "if (form.image_ratio && supported && !supported.includes(form.image_ratio)) form.image_ratio = ''",
])

assertContains('src/types/asset.ts', [
  'export type ReferenceImageSelection',
  'upload_session_id: string',
  'export interface ReferenceAssetView',
  'download_url: string',
])

assertSubmitButtonsDisabled('src/pages/projects/detail.vue', 'onSave', 'referenceUploading', 2)
assertSubmitGuard('src/pages/projects/detail.vue', 'onSave', /if \(saving\.value \|\| referenceUploading\.value\) return/)
assertSubmitButtonsDisabled('src/pages/plans/create.vue', 'handleSubmit', '!canSubmit', 1)
assertSubmitGuard('src/pages/plans/create.vue', 'handleSubmit', /if \(!canSubmit\.value \|\| referenceUploading\.value\) return/)
assertSubmitButtonsDisabled('src/pages/tasks/create.vue', 'onSubmit', '!canSubmit', 1)
assertSubmitGuard('src/pages/tasks/create.vue', 'onSubmit', /if \(!canSubmit\.value \|\| referenceUploading\.value\) return/)
assert.match(
  read('src/pages/tasks/create.vue'),
  /const canSubmit = computed\(\(\) => \{[\s\S]*?return !submitting\.value && !referenceUploading\.value[\s\S]*?\}\)/,
  'task canSubmit must remain false for the complete reference upload lifecycle',
)

for (const path of filesUnder(resolve(root, 'src'))) {
  if (!/\.(ts|vue)$/.test(path)) continue
  const body = readFileSync(path, 'utf8')
  assert.equal(body.includes('reference_image_url'), false, `${path} still uses the removed reference URL field`)
  assert.equal(body.includes('image_model_key'), false, `${path} still uses the removed image model field`)
  assert.equal(body.includes('/image-models'), false, `${path} still uses the removed image models endpoint`)
  assert.equal(body.includes('/designer/providers'), false, `${path} still uses the removed designer providers endpoint`)
  assert.equal(body.includes('/model-config'), false, `${path} still uses the removed model config endpoint`)
}

// === channel→project migration (2026-06-27): miniapp must call /projects, never /channels ===
// The Go server renamed channels→projects (router/router.go registers only /projects/*,
// task/plan handlers require project_id). The miniapp must follow or every project/task/plan
// call 404s. These assertions lock the migration in and catch regressions.
assertFile('src/api/projects.ts')
assertFile('src/types/project.ts')
assertFile('src/pages/projects/index.vue')
assertFile('src/pages/projects/detail.vue')
assertFile('src/components/business/ProjectSelector.vue')
assertFile('src/components/business/ProjectCard.vue')

assertContains('src/api/projects.ts', [
  'export const projectsApi',
  "'/projects'",
  "'/projects/stats'",
  "'/projects/fetch-profile'",
  'get<ProjectDetail>(`/projects/${id}`)',
])
assertContains('src/api/topic-pool.ts', [
  '`/projects/${projectId}/topics`',
])
assertContains('src/api/tasks.ts', [
  'project_id?: string',
  'plan_id?: string',
])
assertContains('src/api/plans.ts', [
  'project_id?: string',
])
assertContains('src/types/task.ts', [
  'project_id: string',
])
assertContains('src/types/plan.ts', [
  'project_id: string',
])
assertContains('src/types/topic-pool.ts', [
  'project_id: string',
])
assertContains('src/types/project.ts', [
  'export interface ProjectDetail',
  'project: Project',
])
assertContains('src/types/index.ts', [
  "from './project'",
])
assertContains('src/api/index.ts', [
  "from './projects'",
  'projects: projectsApi',
])
assertContains('src/pages.json', [
  '"path": "pages/projects/index"',
  '"path": "pages/projects/detail"',
])
assertContains('src/types/timeline.ts', [
  'project_id?: string',
  'project_name?: string',
])

// === miniapp UX workbench contracts (2026-07-06) ===
assertFile('src/api/api-base.ts')
assertContains('src/api/api-base.ts', [
  'https://api.creator.anbanai.com/api/v1',
  'VITE_API_BASE_URL',
])
assertContains('src/pages/connect/claude-code.vue', ['api_key'])
assertNotContains('src/pages/connect/claude-code.vue', ['ANBAN_API_KEY', 'settings.json'])
assertContains('src/pages/connect/codex.vue', ['ANBAN_API_KEY'])
assertNotContains('src/pages/connect/codex.vue', ['api_key', 'userConfig'])
assertContains('src/api/request.ts', [
  'apiUrl(',
])
assertContains('src/stores/auth.ts', [
  'apiUrl(',
])
assertContains('src/api/projects.ts', [
  "'/uploads/prepare'",
  'upload_url',
  'uni.request({',
])
assertContains('src/api/designer.ts', [
  'uploadUrl(',
])

assertContains('src/pages/projects/index.vue', [
  'onMounted',
  'fetchProjects(true)',
])

assertContains('src/pages/tasks/index.vue', [
  'planId',
  'onLoad',
  'plan_id: planId.value',
])

assertContains('src/pages/tasks/create.vue', [
  'onLoad',
  'template_id',
  'prefillProjectId',
  'applyTemplate',
  'advancedOpen',
])
assertContains('src/pages/tasks/create.vue', [
  'const billableGoalMode = computed(() => !isEcommerce.value && form.goal_mode)',
  '<!-- Goal mode -->\n    <view class="task-create__section" v-if="advancedOpen && !isEcommerce">',
  'if (balance.value < creationCost.value)',
  'goal: billableGoalMode.value && form.goal.trim() ? form.goal.trim() : undefined',
  'goal_mode: billableGoalMode.value || undefined',
])

assertContains('src/pages/index/index.vue', [
  'nextSuggestion',
  'runningTasks',
  'recentOutputs',
  'workshopEntries',
  'planReminders',
])

assertContains('src/pages/tasks/detail.vue', [
  'nextActions',
  'createFollowUpTask',
  'copyResultText',
  'downloadZip',
])

assertContains('src/components/business/ProjectSelector.vue', [
  'emptyActionText',
  'createProject',
  'refresh',
])

assertContains('src/pages/workshop/viral-analysis.vue', [
  'buildClonePrompt',
  'copyClonePrompt',
  'createCloneTask',
])

assertContains('src/pages/workshop/clone.vue', [
  '<ExecutionProfileSelector',
  'v-model="selectedExecutionProfile"',
  'agentProfilesApi.list()',
  'resolveExecutionProfileSelection',
  'taskPriceForExecutionProfile',
  'execution_profile: executionProfile',
])

assertContains('src/pages/workshop/poster.vue', [
  'copyPosterPrompt',
  'regenerateVariant',
])

assertContains('src/pages/designer/index.vue', [
  'copyImagePrompt',
  'regenerateVariant',
])

// OLD names must be GONE — no /channels API, no channel_id, no Channel type
assert.equal(existsSync(resolve(root, 'src/api/channels.ts')), false, 'src/api/channels.ts must be removed (renamed to projects.ts)')
assert.equal(existsSync(resolve(root, 'src/types/channel.ts')), false, 'src/types/channel.ts must be removed (renamed to project.ts)')
assert.equal(existsSync(resolve(root, 'src/pages/channels')), false, 'src/pages/channels/ must be removed (renamed to projects/)')
assert.equal(existsSync(resolve(root, 'src/components/business/ChannelSelector.vue')), false, 'ChannelSelector.vue must be removed (renamed to ProjectSelector.vue)')
assert.equal(existsSync(resolve(root, 'src/components/business/ChannelCard.vue')), false, 'ChannelCard.vue must be removed (renamed to ProjectCard.vue)')

console.log('miniapp parity contracts passed')
