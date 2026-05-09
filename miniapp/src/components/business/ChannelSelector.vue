<template>
  <view class="channel-selector" @tap="showPicker">
    <view class="channel-selector__display" v-if="selectedChannel">
      <PlatformAvatar :platform="selectedChannel.platform" :size="32" />
      <text class="channel-selector__name">{{ selectedChannel.name }}</text>
    </view>
    <text class="channel-selector__placeholder" v-else>{{ placeholder }}</text>
    <text class="channel-selector__arrow">›</text>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import type { Channel } from '@/types'
import { channelsApi } from '@/api/channels'
import PlatformAvatar from './PlatformAvatar.vue'

const props = defineProps<{
  modelValue?: string
  placeholder?: string
  platformFilter?: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  change: [channel: Channel]
}>()

const channels = ref<Channel[]>([])

const selectedChannel = computed(() =>
  channels.value.find((c) => c.id === props.modelValue),
)

async function loadChannels() {
  try {
    channels.value = await channelsApi.list({ status: 'active', platform: props.platformFilter })
  } catch (err) {
    console.error('Failed to load channels:', err)
  }
}

function showPicker() {
  if (channels.value.length === 0) {
    uni.showToast({ title: '暂无可用账号', icon: 'none' })
    return
  }

  const names = channels.value.map((c) => c.name)
  uni.showActionSheet({
    itemList: names,
    success: (res) => {
      const channel = channels.value[res.tapIndex]
      if (channel) {
        emit('update:modelValue', channel.id)
        emit('change', channel)
      }
    },
  })
}

onMounted(loadChannels)

defineExpose({ loadChannels })
</script>

<style lang="scss" scoped>
.channel-selector {
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
  }

  &__name {
    font-size: $ab-text-base;
    color: $ab-text;
  }

  &__placeholder {
    font-size: $ab-text-base;
    color: $ab-text-tertiary;
  }

  &__arrow {
    font-size: $ab-text-lg;
    color: $ab-text-tertiary;
  }
}
</style>
