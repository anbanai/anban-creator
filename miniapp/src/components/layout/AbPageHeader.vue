<template>
  <view class="page-header">
    <text class="page-header__title">{{ title }}</text>
    <text class="page-header__subtitle" v-if="subtitle">{{ subtitle }}</text>
    <slot />
  </view>
</template>

<script setup lang="ts">
defineProps<{
  title: string
  subtitle?: string
}>()
</script>

<style lang="scss" scoped>
.page-header {
  padding: $ab-space-lg 0 $ab-space-md;

  &__title {
    font-size: $ab-text-xl;
    font-weight: $ab-font-bold;
    color: $ab-text;
    display: block;
  }

  &__subtitle {
    font-size: $ab-text-sm;
    color: $ab-text-secondary;
    margin-top: $ab-space-xs;
    display: block;
  }
}
</style>
