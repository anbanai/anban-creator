import { readFileSync, existsSync } from 'node:fs'
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

function assertContains(path, terms) {
  const body = read(path)
  for (const term of terms) {
    assert.equal(body.includes(term), true, `${path} should contain ${term}`)
  }
}

for (const path of [
  'src/api/request.ts',
  'src/api/api-keys.ts',
  'src/api/designer.ts',
  'src/api/model-config.ts',
  'src/api/resources.ts',
  'src/api/topic-pool.ts',
  'src/types/designer.ts',
  'src/types/resource.ts',
  'src/types/topic-pool.ts',
  'src/pages/designer/index.vue',
  'src/pages/connect/claude-code.vue',
  'src/pages/connect/openclaw.vue',
  'src/pages/settings/api-keys.vue',
  'src/pages/settings/model-config.vue',
  'src/pages/settings/password.vue',
]) {
  assertFile(path)
}

assertContains('src/pages.json', [
  '"path": "pages/designer/index"',
  '"path": "pages/connect/claude-code"',
  '"path": "pages/connect/openclaw"',
  '"path": "pages/settings/api-keys"',
  '"path": "pages/settings/model-config"',
  '"path": "pages/settings/password"',
])

assertContains('src/api/index.ts', [
  "export const api",
  'apiKeys',
  'designer',
  'modelConfig',
  'resources',
  'topicPool',
])

assertContains('src/types/auth.ts', [
  'has_password',
])

assertContains('src/types/task.ts', [
  'topic?: string',
  'skip_reference_image?: boolean',
  'reference_image_url?: string',
  'watermark?: boolean',
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

assertContains('src/types/designer.ts', [
  'provider_id?: string',
])

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

assertContains('src/pages/designer/index.vue', [
  'canInpaint',
  'startEdit',
  'mask_file_id',
  'provider_id: selectedProvider.value.id',
  'chooseMask',
  'generateEdit',
])

assertContains('src/pages/tasks/create.vue', [
  'form.skip_reference_image',
  'form.reference_image_url',
  'form.watermark',
  'skip_reference_image: form.skip_reference_image',
  'reference_image_url: form.reference_image_url',
  'watermark: form.watermark',
])

assertContains('src/pages/plans/create.vue', [
  'skipReferenceImage',
  'referenceImageUrl',
  'watermark',
  'skip_reference_image: form.skipReferenceImage',
  'reference_image_url: form.referenceImageUrl',
])

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
assertContains('src/api/request.ts', [
  'apiUrl(',
])
assertContains('src/stores/auth.ts', [
  'apiUrl(',
])
assertContains('src/api/projects.ts', [
  'uploadUrl(',
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
