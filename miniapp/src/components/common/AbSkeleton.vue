<template>
  <view v-if="rows > 1" class="ab-skeleton-text">
    <view
      v-for="i in rows"
      :key="i"
      class="ab-skeleton"
      :style="{
        width: rowWidth(i),
        height: height || '26rpx',
        borderRadius: radius || '8rpx',
      }"
    />
  </view>
  <view
    v-else
    class="ab-skeleton"
    :style="{
      width: width,
      height: height,
      borderRadius: circle ? '50%' : radius || '8rpx',
    }"
  />
</template>

<script setup lang="ts">
const props = withDefaults(
  defineProps<{
    /** Width (any CSS length, e.g. '100%', '200rpx'). */
    width?: string
    /** Height (any CSS length). */
    height?: string
    /** Border radius (any CSS length). Ignored when `circle` is set. */
    radius?: string
    /** Render a circular skeleton (e.g. avatar). */
    circle?: boolean
    /** When > 1, render N stacked text lines (last line shorter). */
    rows?: number
  }>(),
  {
    width: '100%',
    height: '28rpx',
    radius: '',
    circle: false,
    rows: 1,
  },
)

function rowWidth(i: number): string {
  // Last line ~70% width for a natural text look.
  return i === props.rows ? '70%' : '100%'
}
</script>

<style lang="scss" scoped>
.ab-skeleton {
  display: block;
  background: linear-gradient(
    90deg,
    $ab-divider 0%,
    rgba(255, 255, 255, 0.14) 50%,
    $ab-divider 100%
  );
  background-size: 200% 100%;
  animation: ab-shimmer 1.4s ease-in-out infinite;
}

.ab-skeleton-text {
  display: flex;
  flex-direction: column;
  gap: $ab-space-xs;
  width: 100%;
}

@keyframes ab-shimmer {
  0% {
    background-position: 200% 0;
  }
  100% {
    background-position: -200% 0;
  }
}
</style>
