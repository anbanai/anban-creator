<template>
  <view class="image-model-selector" @tap="showPicker">
    <view class="image-model-selector__display" v-if="selectedModel">
      <text class="image-model-selector__name">{{ selectedModel.display_name }}</text>
      <text class="image-model-selector__provider" v-if="selectedModel.provider">{{ selectedModel.provider }}</text>
    </view>
    <text class="image-model-selector__placeholder" v-else>{{ placeholder }}</text>
    <text class="image-model-selector__arrow">›</text>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import type { ImageModelOption } from '@/types'
import { imageModelsApi } from '@/api/image-models'

const props = withDefaults(
  defineProps<{
    modelValue?: string
    placeholder?: string
  }>(),
  {
    modelValue: '',
    placeholder: '选择图片模型',
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  change: [model: ImageModelOption]
}>()

const models = ref<ImageModelOption[]>([])
const userTier = ref('')
const loaded = ref(false)

const selectedModel = computed(() => models.value.find((m) => m.key === props.modelValue))

async function loadModels() {
  try {
    const res = await imageModelsApi.list()
    models.value = res.items || []
    userTier.value = res.tier || ''
    loaded.value = true
  } catch (err) {
    console.error('Failed to load image models:', err)
  }
}

function showPicker() {
  if (!loaded.value) {
    loadModels().then(() => {
      if (models.value.length === 0) {
        uni.showToast({ title: '暂无可用图片模型', icon: 'none' })
      } else {
        openSheet()
      }
    })
    return
  }
  if (models.value.length === 0) {
    uni.showToast({ title: '暂无可用图片模型', icon: 'none' })
    return
  }
  openSheet()
}

function openSheet() {
  const labels = models.value.map((m) => m.display_name)
  uni.showActionSheet({
    itemList: labels,
    success: (res) => {
      const model = models.value[res.tapIndex]
      if (model) {
        emit('update:modelValue', model.key)
        emit('change', model)
      }
    },
  })
}

onMounted(loadModels)

defineExpose({ loadModels, userTier })
</script>

<style lang="scss" scoped>
.image-model-selector {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background-color: $ab-surface;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  padding: $ab-space-sm $ab-space-md;
  min-height: 80rpx;

  &__display {
    display: flex;
    align-items: center;
    gap: $ab-space-sm;
    flex: 1;
    min-width: 0;
  }

  &__name {
    font-size: $ab-text-base;
    color: $ab-text;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__provider {
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    flex-shrink: 0;
  }

  &__placeholder {
    font-size: $ab-text-base;
    color: $ab-text-tertiary;
  }

  &__arrow {
    font-size: $ab-text-lg;
    color: $ab-text-tertiary;
    margin-left: $ab-space-sm;
  }
}
</style>
