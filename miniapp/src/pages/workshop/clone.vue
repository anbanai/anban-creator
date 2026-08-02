<template>
  <view class="page clone-page">
    <view class="cp-section">
      <text class="field-label">粘贴种草笔记链接</text>
      <AbInput
        v-model="sourceUrl"
        placeholder="https://xhslink.com/..."
        :error="errors.sourceUrl"
      />
    </view>

    <!-- Clone depth selection -->
    <view class="cp-section">
      <text class="field-label">复刻深度</text>
      <view class="cp-depth-list">
        <view
          v-for="option in depthOptions"
          :key="option.value"
          class="cp-depth-card"
          :class="{ 'cp-depth-card--active': cloneDepth === option.value }"
          @tap="cloneDepth = option.value"
        >
          <view class="cp-depth-card__header">
            <text class="cp-depth-card__icon">{{ option.icon }}</text>
            <text class="cp-depth-card__title">{{ option.label }}</text>
            <view v-if="option.recommended" class="cp-depth-card__badge">
              <AbBadge variant="warning" size="sm">推荐</AbBadge>
            </view>
          </view>
          <text class="cp-depth-card__desc">{{ option.description }}</text>
        </view>
      </view>
    </view>

    <!-- Target account selector -->
    <view class="cp-section">
      <text class="field-label">目标账号 <text class="field-required">*</text></text>
      <ProjectSelector
        v-model="form.project_id"
        placeholder="选择要发布的账号..."
        @change="onProjectChange"
      />
      <text v-if="errors.project" class="field-error">{{ errors.project }}</text>
    </view>

    <view class="cp-section">
      <text class="field-label">执行配置 <text class="field-required">*</text></text>
      <ExecutionProfileSelector
        v-model="selectedExecutionProfile"
        :profiles="executionProfiles"
        :loading="executionProfilesLoading"
        :disabled="submitting"
      />
      <text v-if="resolvedPrice !== undefined" class="profile-price">
        任务准入费 {{ resolvedPrice.toLocaleString() }} 积分
      </text>
      <text v-if="executionProfilesError" class="field-error">{{ executionProfilesError }}</text>
    </view>

    <!-- Additional prompt (optional) -->
    <view class="cp-section">
      <text class="field-label">额外要求 <text class="field-optional">（可选）</text></text>
      <AbTextarea
        v-model="form.extra_prompt"
        placeholder="补充你想要的风格、关键词或特殊要求..."
        :rows="3"
        :maxlength="5120"
      />
    </view>

    <!-- Fixed bottom button -->
    <view class="cp-bottom">
      <AbButton
        type="primary"
        size="lg"
        block
        :loading="submitting"
        :disabled="!canSubmit"
        @click="onClone"
      >
        开始复刻
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import type {
  AgentExecutionProfileCapability,
  AgentExecutionProfileID,
  BillingCatalog,
  Project,
} from '@/types'
import { agentProfilesApi } from '@/api/agent-profiles'
import { billingApi } from '@/api/billing'
import { tasksApi } from '@/api/tasks'
import {
  resolveExecutionProfileSelection,
  taskPriceForExecutionProfile,
} from '@/utils/execution-profiles'
import AbButton from '@/components/common/AbButton.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbTextarea from '@/components/common/AbTextarea.vue'
import AbBadge from '@/components/common/AbBadge.vue'
import ProjectSelector from '@/components/business/ProjectSelector.vue'
import ExecutionProfileSelector from '@/components/business/ExecutionProfileSelector.vue'

const depthOptions = [
  {
    value: 'style',
    icon: '图',
    label: '风格复刻',
    description: '学习写作风格和排版',
    recommended: false,
  },
  {
    value: 'medium',
    icon: '文',
    label: '中度复刻',
    description: '风格+结构+关键词',
    recommended: true,
  },
  {
    value: 'deep',
    icon: '变',
    label: '深度复刻',
    description: '全面学习并创新',
    recommended: false,
  },
]

const sourceUrl = ref('')
const cloneDepth = ref('medium')
const submitting = ref(false)
const selectedProject = ref<Project | null>(null)
const executionProfiles = ref<AgentExecutionProfileCapability[]>([])
const executionProfilesLoading = ref(false)
const executionProfilesError = ref('')
const selectedExecutionProfile = ref<AgentExecutionProfileID | ''>('')
const catalog = ref<BillingCatalog | null>(null)

const form = reactive({
  project_id: '',
  extra_prompt: '',
})

const errors = reactive<Record<string, string>>({})

const canSubmit = computed(() => {
  return !!sourceUrl.value.trim()
    && !!form.project_id
    && !!selectedExecutionProfile.value
    && resolvedPrice.value !== undefined
    && !submitting.value
})

const resolvedPrice = computed(() => {
  if (!selectedProject.value || !selectedExecutionProfile.value) return undefined
  return taskPriceForExecutionProfile(
    catalog.value,
    selectedProject.value.platform,
    selectedExecutionProfile.value,
  )
})

function onProjectChange(project: Project) {
  selectedProject.value = project
  delete errors.project
  ensureExecutionProfileSelection()
}

function ensureExecutionProfileSelection() {
  if (!selectedProject.value) return
  selectedExecutionProfile.value = resolveExecutionProfileSelection(
    selectedExecutionProfile.value,
    false,
    executionProfiles.value,
    catalog.value,
    selectedProject.value.platform,
  )
}

function validate(): boolean {
  Object.keys(errors).forEach(k => delete errors[k])

  if (!sourceUrl.value.trim()) {
    errors.sourceUrl = '请输入种草笔记链接'
    return false
  }

  if (!form.project_id) {
    errors.project = '请选择目标账号'
    return false
  }

  if (!selectedExecutionProfile.value || resolvedPrice.value === undefined) {
    uni.showToast({ title: '请选择可用的执行配置', icon: 'none' })
    return false
  }

  return true
}

async function onClone() {
  if (!validate()) return
  if (!selectedProject.value) return
  const executionProfile = selectedExecutionProfile.value
  if (!executionProfile) return

  submitting.value = true
  try {
    // Build the clone prompt from depth + source info
    const depthLabel = depthOptions.find(d => d.value === cloneDepth.value)?.label || '中度复刻'
    const depthDesc = depthOptions.find(d => d.value === cloneDepth.value)?.description || ''

    let prompt = `【爆款复刻任务】\n复刻深度: ${depthLabel}（${depthDesc}）\n`

    prompt += `来源链接: ${sourceUrl.value.trim()}\n`

    if (form.extra_prompt.trim()) {
      prompt += `额外要求: ${form.extra_prompt.trim()}\n`
    }

    prompt += `\n请参考来源内容的风格和结构，为目标账号创作新的内容。`

    const task = await tasksApi.create({
      type: selectedProject.value.platform as any,
      execution_profile: executionProfile,
      project_id: form.project_id,
      prompt: prompt.trim(),
    })

    uni.showToast({ title: '复刻任务已创建', icon: 'success' })
    setTimeout(() => {
      uni.redirectTo({ url: `/pages/tasks/detail?id=${task.id}` })
    }, 500)
  } catch (err: any) {
    const msg = err?.message || '创建失败'
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    submitting.value = false
  }
}

async function loadExecutionConfiguration() {
  executionProfilesLoading.value = true
  executionProfilesError.value = ''
  const [profilesResult, catalogResult] = await Promise.allSettled([
    agentProfilesApi.list(),
    billingApi.catalog(),
  ])
  executionProfiles.value = profilesResult.status === 'fulfilled' ? profilesResult.value : []
  catalog.value = catalogResult.status === 'fulfilled' ? catalogResult.value : null
  if (profilesResult.status === 'rejected') {
    executionProfilesError.value = '执行配置加载失败，请稍后重试'
  } else if (catalogResult.status === 'rejected') {
    executionProfilesError.value = '价格目录加载失败，请稍后重试'
  }
  executionProfilesLoading.value = false
  ensureExecutionProfileSelection()
}

onMounted(() => {
  void loadExecutionConfiguration()
})
</script>

<style lang="scss" scoped>
.clone-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  padding-bottom: 180rpx;
}

.field-label {
  font-size: $ab-text-base;
  color: $ab-text;
  font-weight: $ab-font-medium;
  display: block;
  margin-bottom: $ab-space-xs;
}

.field-required {
  color: $ab-danger;
}

.field-optional {
  color: $ab-text-tertiary;
  font-weight: $ab-font-normal;
}

.field-error {
  font-size: $ab-text-xs;
  color: $ab-danger;
  display: block;
  margin-top: $ab-space-xs;
  line-height: 1.4;
}

.profile-price {
  display: block;
  margin-top: $ab-space-sm;
  color: $ab-text-secondary;
  font-size: $ab-text-sm;
}

.cp-section {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
}

// Depth cards
.cp-depth-list {
  display: flex;
  flex-direction: column;
  gap: $ab-space-sm;
}

.cp-depth-card {
  padding: $ab-space-md;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-md;
  transition: all 0.2s ease;

  &--active {
    border-color: $ab-primary;
    background-color: $ab-primary-bg;
  }

  &__header {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
    margin-bottom: 8rpx;
  }

  &__icon {
    font-size: 40rpx;
    line-height: 1;
  }

  &__title {
    font-size: $ab-text-md;
    font-weight: $ab-font-semibold;
    color: $ab-text;
    flex: 1;
  }

  &__badge {
    flex-shrink: 0;
  }

  &__desc {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    line-height: 1.4;
    padding-left: 56rpx;
  }
}

// Bottom
.cp-bottom {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  padding: $ab-space-md $ab-space-lg;
  padding-bottom: calc(#{$ab-space-md} + env(safe-area-inset-bottom));
  background-color: $ab-surface;
  box-shadow: 0 -4rpx 12rpx rgba(0, 0, 0, 0.06);
  z-index: 20;
}
</style>
