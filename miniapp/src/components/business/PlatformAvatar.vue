<template>
  <view class="platform-avatar" :style="{ borderColor: color }">
    <text class="platform-avatar__label">{{ label }}</text>
  </view>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { taskTypeLabelCN } from '@/utils/labels'

const props = defineProps<{
  platform: string
  size?: number
}>()

const labelMap: Record<string, string> = {
  seednote: '种',
  article: '公',
  moments: '圈',
}

const colorMap: Record<string, string> = {
  seednote: '#FF2442',
  article: '#07C160',
  moments: '#22C55E',
}

const label = computed(() => labelMap[props.platform] || props.platform.charAt(0))
const color = computed(() => colorMap[props.platform] || '#6B7280')
const size = computed(() => (props.size || 48) + 'rpx')
</script>

<style lang="scss" scoped>
.platform-avatar {
  width: v-bind(size);
  height: v-bind(size);
  border-radius: 50%;
  border: 3rpx solid v-bind(color);
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: rgba(0, 0, 0, 0.02);
  flex-shrink: 0;

  &__label {
    font-size: 22rpx;
    font-weight: 600;
    color: v-bind(color);
  }
}
</style>
