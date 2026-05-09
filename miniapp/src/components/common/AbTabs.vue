<template>
  <view class="ab-tabs">
    <scroll-view class="ab-tabs__scroll" scroll-x :show-scrollbar="false">
      <view class="ab-tabs__nav">
        <view
          v-for="tab in tabs"
          :key="tab.key"
          class="ab-tabs__item"
          :class="{ 'ab-tabs__item--active': modelValue === tab.key }"
          @tap="onTap(tab.key)"
        >
          <text class="ab-tabs__label">{{ tab.label }}</text>
          <view v-if="modelValue === tab.key" class="ab-tabs__indicator" />
        </view>
      </view>
    </scroll-view>
  </view>
</template>

<script setup lang="ts">
defineProps<{
  tabs: Array<{ key: string; label: string }>
  modelValue: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
}>()

function onTap(key: string) {
  emit('update:modelValue', key)
}
</script>

<style lang="scss" scoped>
.ab-tabs {
  width: 100%;
  border-bottom: 2rpx solid $ab-border;
  background-color: $ab-surface;

  &__scroll {
    white-space: nowrap;
    width: 100%;
  }

  &__nav {
    display: inline-flex;
    align-items: stretch;
    padding: 0 $ab-space-md;
  }

  &__item {
    position: relative;
    display: inline-flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: $ab-space-sm $ab-space-md;
    flex-shrink: 0;
    transition: color 0.2s ease;

    &--active {
      .ab-tabs__label {
        color: $ab-primary;
        font-weight: $ab-font-semibold;
      }
    }
  }

  &__label {
    font-size: $ab-text-base;
    color: $ab-text-secondary;
    white-space: nowrap;
    transition: color 0.2s ease;
  }

  &__indicator {
    position: absolute;
    bottom: 0;
    left: 50%;
    transform: translateX(-50%);
    width: 48rpx;
    height: 6rpx;
    background-color: $ab-primary;
    border-radius: 3rpx;
    transition: left 0.2s ease, width 0.2s ease;
  }
}
</style>
