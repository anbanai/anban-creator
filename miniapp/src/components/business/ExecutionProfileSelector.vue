<template>
  <view class="profile-selector">
    <view v-if="loading" class="profile-selector__loading">
      <text>正在加载执行配置...</text>
    </view>
    <template v-else>
      <view
        v-for="profile in profiles"
        :key="profile.id"
        class="profile-card"
        :class="{
          'profile-card--active': modelValue === profile.id,
          'profile-card--disabled': !profile.available || disabled,
        }"
        @tap="selectProfile(profile)"
      >
        <view class="profile-card__header">
          <text class="profile-card__name">{{ profile.display_name }}</text>
          <text v-if="!profile.available" class="profile-card__badge">不可用</text>
        </view>
        <text class="profile-card__provider">Provider：{{ profile.provider }}</text>
        <text
          v-for="row in modelRows(profile)"
          :key="row"
          class="profile-card__model"
        >
          {{ row }}
        </text>
        <text class="profile-card__tier">{{ tierRequirement(profile.min_tier) }}</text>
        <text class="profile-card__description">
          {{ profile.available ? profile.description : unavailableReason(profile) }}
        </text>
      </view>
    </template>
  </view>
</template>

<script setup lang="ts">
import type {
  AgentExecutionProfileCapability,
  AgentExecutionProfileID,
  AgentModelMatrix,
} from '@/types'

const modelRoles: Array<[keyof AgentModelMatrix, string]> = [
  ['default', '默认'],
  ['opus', 'Opus'],
  ['fable', 'Fable'],
  ['sonnet', 'Sonnet'],
  ['haiku', 'Haiku'],
]

const props = withDefaults(defineProps<{
  modelValue: AgentExecutionProfileID | ''
  profiles: AgentExecutionProfileCapability[]
  loading?: boolean
  disabled?: boolean
}>(), {
  loading: false,
  disabled: false,
})

const emit = defineEmits<{
  (event: 'update:modelValue', value: AgentExecutionProfileID): void
}>()

function selectProfile(profile: AgentExecutionProfileCapability) {
  if (props.disabled || !profile.available) return
  emit('update:modelValue', profile.id)
}

function modelRows(profile: AgentExecutionProfileCapability): string[] {
  const values = [
    profile.models.default,
    profile.models.opus,
    profile.models.fable,
    profile.models.sonnet,
    profile.models.haiku,
  ]
  if (values.every((model) => model === values[0])) {
    return [`全部角色：${values[0]}`]
  }
  return modelRoles.map(([role, label]) => `${label}：${profile.models[role]}`)
}

function tierRequirement(tier: AgentExecutionProfileCapability['min_tier']) {
  if (tier === 'pro') return '最低套餐：Pro 版及以上'
  if (tier === 'enterprise') return '最低套餐：企业版'
  return '所有用户可用'
}

function unavailableReason(profile: AgentExecutionProfileCapability) {
  const reason = profile.unavailable_reason
  if (reason === 'requires_pro') return '需要 Pro 套餐'
  if (reason === 'requires_enterprise') return '需要企业版套餐'
  if (reason === 'profile_configuration_missing') return '当前档位尚未配置'
  if (reason === 'agent_provider_unavailable') return '当前模型服务不可用'
  if (reason === 'agent_model_cost_unmapped') return '当前模型尚未配置成本'
  return reason || '当前不可用'
}
</script>

<style lang="scss" scoped>
.profile-selector {
  display: flex;
  flex-direction: column;
  gap: $ab-space-sm;

  &__loading {
    min-height: 160rpx;
    display: flex;
    align-items: center;
    justify-content: center;
    color: $ab-text-tertiary;
    font-size: $ab-text-sm;
  }
}

.profile-card {
  min-height: 144rpx;
  padding: $ab-space-sm $ab-space-md;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  background-color: $ab-surface;
  box-sizing: border-box;

  &--active {
    border-color: $ab-primary;
    background-color: $ab-primary-bg;
  }

  &--disabled {
    background-color: $ab-background;
    opacity: 0.68;
  }

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: $ab-space-sm;
  }

  &__name {
    color: $ab-text;
    font-size: $ab-text-base;
    font-weight: $ab-font-medium;
  }

  &__badge {
    flex-shrink: 0;
    padding: 4rpx 12rpx;
    border-radius: $ab-radius-sm;
    color: $ab-text-tertiary;
    background-color: $ab-divider;
    font-size: $ab-text-xs;
  }

  &__provider,
  &__model,
  &__tier,
  &__description {
    display: block;
    margin-top: 8rpx;
    color: $ab-text-secondary;
    font-size: $ab-text-sm;
    line-height: 1.45;
  }

  &__description {
    color: $ab-text-tertiary;
  }

  &__model {
    font-family: monospace;
    font-size: $ab-text-xs;
    overflow-wrap: anywhere;
  }

  &__tier {
    color: $ab-text-secondary;
  }
}
</style>
