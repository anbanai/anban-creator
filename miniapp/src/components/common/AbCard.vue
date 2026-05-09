<template>
  <view class="ab-card">
    <slot />
  </view>
</template>

<script setup lang="ts">
</script>

<style lang="scss" scoped>
.ab-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  box-shadow: $ab-shadow-sm;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-sizing: border-box;
}
</style>
