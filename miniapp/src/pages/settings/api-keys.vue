<template>
  <view class="page api-keys-page">
    <view class="section">
      <text class="section-title">创建密钥</text>
      <view class="create-row">
        <AbInput v-model="keyName" placeholder="密钥名称，例如 MacBook Pro" />
        <AbButton type="primary" size="md" :loading="creating" @click="createKey">创建</AbButton>
      </view>
      <view v-if="newKey" class="new-key">
        <text class="new-key__title">密钥只显示一次</text>
        <text class="new-key__value">{{ newKey.key }}</text>
        <view class="new-key__actions">
          <AbButton type="primary" size="sm" @click="copyText(newKey.key)">复制密钥</AbButton>
          <AbButton type="ghost" size="sm" @click="newKey = null">我已保存</AbButton>
        </view>
      </view>
    </view>

    <view class="section">
      <text class="section-title">已有密钥</text>
      <AbLoading v-if="loading" size="sm" text="加载中..." />
      <AbEmpty v-else-if="keys.length === 0" title="暂无平台密钥" />
      <view v-else>
        <view v-for="key in keys" :key="key.id" class="key-row">
          <view class="key-row__body">
            <text class="key-row__name">{{ key.name }}</text>
            <text class="key-row__meta">{{ key.key_prefix }} · {{ formatDate(key.created_at) }}</text>
          </view>
          <AbButton type="danger" size="sm" :loading="revokingId === key.id" @click="revokeKey(key.id)">吊销</AbButton>
        </view>
      </view>
    </view>

    <view class="section">
      <text class="section-title">接入指南</text>
      <view class="guide-link" @tap="goGuide('/pages/connect/claude-code')">
        <text>Claude Code</text>
        <text>›</text>
      </view>
      <view class="guide-link" @tap="goGuide('/pages/connect/openclaw')">
        <text>OpenClaw</text>
        <text>›</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { APIKey, CreateAPIKeyResponse } from '@/types'
import { apiKeysApi } from '@/api/api-keys'
import { formatDateYMD } from '@/utils/format'
import AbButton from '@/components/common/AbButton.vue'
import AbEmpty from '@/components/common/AbEmpty.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbLoading from '@/components/common/AbLoading.vue'

const loading = ref(false)
const creating = ref(false)
const revokingId = ref('')
const keyName = ref('')
const keys = ref<APIKey[]>([])
const newKey = ref<CreateAPIKeyResponse | null>(null)

function formatDate(value: string) {
  return value ? formatDateYMD(new Date(value)) : '--'
}

function copyText(text: string) {
  uni.setClipboardData({
    data: text,
    success: () => uni.showToast({ title: '已复制', icon: 'success' }),
  })
}

function goGuide(url: string) {
  uni.navigateTo({ url })
}

async function loadKeys() {
  loading.value = true
  try {
    const res = await apiKeysApi.list()
    keys.value = res.items || []
  } catch (err: any) {
    uni.showToast({ title: err?.message || '加载失败', icon: 'none' })
  } finally {
    loading.value = false
  }
}

async function createKey() {
  if (creating.value) return
  creating.value = true
  try {
    newKey.value = await apiKeysApi.create(keyName.value.trim() || 'Default')
    keyName.value = ''
    await loadKeys()
  } catch (err: any) {
    uni.showToast({ title: err?.message || '创建失败', icon: 'none' })
  } finally {
    creating.value = false
  }
}

function revokeKey(id: string) {
  uni.showModal({
    title: '吊销密钥',
    content: '吊销后使用该密钥的插件和工具将无法访问账号。',
    confirmColor: '#DC2626',
    success: async (res) => {
      if (!res.confirm) return
      revokingId.value = id
      try {
        await apiKeysApi.revoke(id)
        await loadKeys()
      } catch (err: any) {
        uni.showToast({ title: err?.message || '吊销失败', icon: 'none' })
      } finally {
        revokingId.value = ''
      }
    },
  })
}

onMounted(loadKeys)
</script>

<style lang="scss" scoped>
.api-keys-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
}

.section {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
}

.section-title {
  display: block;
  font-size: $ab-text-md;
  font-weight: $ab-font-semibold;
  color: $ab-text;
  margin-bottom: $ab-space-sm;
}

.create-row {
  display: flex;
  gap: $ab-space-sm;
  align-items: flex-start;

  :deep(.ab-input-wrapper) {
    flex: 1;
  }
}

.new-key {
  margin-top: $ab-space-sm;
  padding: $ab-space-sm;
  border-radius: $ab-radius-sm;
  background-color: $ab-success-bg;

  &__title,
  &__value {
    display: block;
  }

  &__title {
    font-size: $ab-text-sm;
    color: $ab-success;
    font-weight: $ab-font-medium;
  }

  &__value {
    margin-top: 8rpx;
    font-size: $ab-text-xs;
    color: $ab-text;
    word-break: break-all;
    font-family: monospace;
  }

  &__actions {
    display: flex;
    gap: $ab-space-sm;
    margin-top: $ab-space-sm;
  }
}

.key-row,
.guide-link {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: $ab-space-sm;
  padding: $ab-space-sm 0;
  border-bottom: 2rpx solid $ab-divider;

  &:last-child {
    border-bottom: none;
  }
}

.key-row__body {
  flex: 1;
  min-width: 0;
}

.key-row__name,
.key-row__meta {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.key-row__name,
.guide-link {
  font-size: $ab-text-base;
  color: $ab-text;
}

.key-row__meta {
  margin-top: 4rpx;
  font-size: $ab-text-xs;
  color: $ab-text-tertiary;
}
</style>
