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
  home: '首',
  user: '人',
  checklist: '☑',
  calendar: '日',
  settings: '设',
  edit: '编',
  delete: '删',
  search: '搜',
  image: '图',
  link: '链',
  copy: '复',
  share: '享',
  plus: '＋',
  minus: '－',
  info: 'i',
  warning: '!',
  success: 'OK',
  error: 'ERR',
  loading: '...',
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
