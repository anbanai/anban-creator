<template>
  <view class="tag-input">
    <view class="tag-input__tags">
      <view
        v-for="(tag, i) in tags"
        :key="i"
        class="tag-input__tag"
      >
        <text class="tag-input__tag-text">{{ tag }}</text>
        <text class="tag-input__tag-remove" @tap="remove(i)">×</text>
      </view>
      <input
        v-if="!maxTags || tags.length < maxTags"
        class="tag-input__input"
        :placeholder="tags.length === 0 ? placeholder : ''"
        :value="inputValue"
        @input="onInput"
        @confirm="addTag"
        confirm-type="done"
      />
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref } from 'vue'

const props = withDefaults(defineProps<{
  modelValue?: string[]
  placeholder?: string
  maxTags?: number
}>(), {
  modelValue: () => [],
  placeholder: '添加标签',
})

const emit = defineEmits<{
  'update:modelValue': [value: string[]]
}>()

const tags = ref<string[]>([...props.modelValue])
const inputValue = ref('')

function onInput(e: any) {
  inputValue.value = e.detail.value
}

function addTag() {
  const val = inputValue.value.trim()
  if (val && !tags.value.includes(val)) {
    if (props.maxTags && tags.value.length >= props.maxTags) return
    tags.value.push(val)
    emit('update:modelValue', [...tags.value])
  }
  inputValue.value = ''
}

function remove(index: number) {
  tags.value.splice(index, 1)
  emit('update:modelValue', [...tags.value])
}
</script>

<style lang="scss" scoped>
.tag-input {
  background-color: $ab-surface;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  padding: $ab-space-xs $ab-space-sm;
  min-height: 80rpx;

  &__tags {
    display: flex;
    flex-wrap: wrap;
    gap: $ab-space-xs;
  }

  &__tag {
    display: flex;
    align-items: center;
    gap: 4rpx;
    padding: 4rpx 12rpx;
    background-color: $ab-primary-bg;
    border-radius: $ab-radius-full;
  }

  &__tag-text {
    font-size: $ab-text-xs;
    color: $ab-primary;
  }

  &__tag-remove {
    font-size: $ab-text-sm;
    color: $ab-text-tertiary;
    padding: 0 4rpx;
  }

  &__input {
    flex: 1;
    min-width: 120rpx;
    font-size: $ab-text-sm;
    border: none;
    outline: none;
    padding: 4rpx 0;
  }
}
</style>
