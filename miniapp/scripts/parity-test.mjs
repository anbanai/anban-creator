import { readFileSync, existsSync } from 'node:fs'
import { resolve } from 'node:path'
import assert from 'node:assert/strict'

const root = resolve(import.meta.dirname, '..')

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

assertContains('src/types/channel.ts', [
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

assertContains('src/pages/channels/detail.vue', [
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

assertContains('src/pages/tasks/detail.vue', [
  'topic: task.value.topic',
  'skip_reference_image: task.value.skip_reference_image',
  'reference_image_url: task.value.reference_image_url',
  'watermark: task.value.watermark',
  'tasksApi.getFiles',
  'tasksApi.getSeednoteAnalytics',
  'tasksApi.fileDownloadUrl',
  'tasksApi.zipDownloadUrl',
])

assertContains('src/pages/designer/index.vue', [
  'canInpaint',
  'startEdit',
  'mask_file_id',
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

console.log('miniapp parity contracts passed')
