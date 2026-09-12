<template>
  <view class="guide-page">
    <view class="intro-card">
      <text class="intro-card__title">Claude Code 接入</text>
      <text class="intro-card__body">按步骤安装插件、配置 API Key，并通过 /init 验证账号连接。</text>
    </view>

    <view class="step-card">
      <text class="step-card__title">1. 创建 API Key</text>
      <text class="step-text">完整 Key 只在创建时显示一次。已有密钥前缀：</text>
      <text v-if="keyPrefixes" class="step-code">{{ keyPrefixes }}</text>
      <AbButton type="ghost" size="sm" @click="goApiKeys">管理密钥</AbButton>
    </view>

    <view class="step-card">
      <text class="step-card__title">2. 安装插件市场源</text>
      <view class="copy-block" @tap="copyText('claude plugin marketplace add ./harness')">
        <text class="copy-block__code">claude plugin marketplace add ./harness</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
    </view>

    <view class="step-card">
      <text class="step-card__title">3. 安装插件</text>
      <text class="step-text">anban 是插件 ID，anbanai 是发布方；MCP server key 固定为 creator，具体工具名由 Claude Code 运行时处理。</text>
      <view class="copy-block" @tap="copyText('claude plugin install --scope user anban@anbanai')">
        <text class="copy-block__code">claude plugin install --scope user anban@anbanai</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
    </view>

    <view class="step-card">
      <text class="step-card__title">4. 配置插件 API Key</text>
      <text class="step-text">官方服务地址已内置在插件中。安装或启用插件时，在插件配置的 api_key 字段填写完整 API Key，Claude Code 会将它保存为插件的安全用户配置。</text>
    </view>

    <view class="step-card">
      <text class="step-card__title">5. 验证并重启</text>
      <view class="copy-block" @tap="copyText('/init\n/plugin')">
        <text class="copy-block__code">/init
/plugin</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { APIKey } from '@/types'
import { apiKeysApi } from '@/api/api-keys'
import AbButton from '@/components/common/AbButton.vue'

const keys = ref<APIKey[]>([])
const keyPrefixes = computed(() => keys.value.map((key) => key.key_prefix).join('、') || '暂无密钥')

function copyText(text: string) {
  uni.setClipboardData({
    data: text,
    success: () => uni.showToast({ title: '已复制', icon: 'success' }),
  })
}

function goApiKeys() {
  uni.navigateTo({ url: '/pages/settings/api-keys' })
}

async function loadKeys() {
  try {
    const res = await apiKeysApi.list()
    keys.value = res.items || []
  } catch {
    keys.value = []
  }
}

onMounted(loadKeys)
</script>

<style lang="scss" scoped>
.guide-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
}

.intro-card,
.step-card {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  margin-bottom: $ab-space-sm;
  box-shadow: $ab-shadow-sm;
}

.intro-card__title,
.step-card__title {
  display: block;
  font-size: $ab-text-md;
  font-weight: $ab-font-semibold;
  color: $ab-text;
  margin-bottom: $ab-space-xs;
}

.intro-card__body,
.step-text {
  display: block;
  font-size: $ab-text-sm;
  color: $ab-text-secondary;
  line-height: 1.6;
  margin-bottom: $ab-space-sm;
}

.step-code,
.copy-block__code {
  display: block;
  font-size: $ab-text-xs;
  color: $ab-text;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  font-family: monospace;
}

.copy-block {
  padding: $ab-space-sm;
  background-color: $ab-background;
  border-radius: $ab-radius-sm;
  border: 2rpx solid $ab-border;
}

.copy-block__hint {
  display: block;
  margin-top: $ab-space-xs;
  font-size: $ab-text-xs;
  color: $ab-primary;
}
</style>
