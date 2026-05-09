<template>
  <text class="ab-icon" :style="iconStyle">{{ iconChar }}</text>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  name: string
  size?: string
  color?: string
}>(), {
  size: '32rpx',
  color: ''
})

const iconMap: Record<string, string> = {
  check: '✓',
  close: '✕',
  'arrow-right': '›',
  'arrow-left': '‹',
  home: '🏠',
  user: '👤',
  checklist: '☑',
  calendar: '📅',
  settings: '⚙',
  edit: '✏',
  delete: '🗑',
  search: '🔍',
  image: '\uD83D\uDBC4',
  link: '🔗',
  copy: '📋',
  share: '📤',
  plus: '＋',
  minus: '－',
  info: 'ℹ',
  warning: '⚠',
  success: '✅',
  error: '❌',
  loading: '⏳',
}

const iconChar = computed(() => iconMap[props.name] || props.name)

const iconStyle = computed(() => ({
  fontSize: props.size,
  color: props.color || undefined,
}))
</script>

<style lang="scss" scoped>
.ab-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  line-height: 1;
  vertical-align: middle;
  color: $ab-text-secondary;
}
</style>
