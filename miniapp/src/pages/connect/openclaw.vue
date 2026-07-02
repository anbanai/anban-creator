<template>
  <view class="guide-page">
    <view class="intro-card">
      <text class="intro-card__title">OpenClaw 接入</text>
      <text class="intro-card__body">安装 OpenClaw 原生插件后，通过环境变量连接 Anban 平台。</text>
    </view>

    <view class="step-card">
      <text class="step-card__title">1. 创建 API Key</text>
      <text class="step-text">已有密钥前缀：</text>
      <text class="step-code">{{ keyPrefixes }}</text>
      <AbButton type="ghost" size="sm" @click="goApiKeys">管理密钥</AbButton>
    </view>

    <view class="step-card">
      <text class="step-card__title">2. 克隆插件</text>
      <view class="copy-block" @tap="copyText('git clone https://github.com/anbanai/anban-creator-openclaw.git\ncd anban-creator-openclaw')">
        <text class="copy-block__code">git clone https://github.com/anbanai/anban-creator-openclaw.git
cd anban-creator-openclaw</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
    </view>

    <view class="step-card">
      <text class="step-card__title">3. 安装插件</text>
      <view class="copy-block" @tap="copyText('openclaw plugins install ./')">
        <text class="copy-block__code">openclaw plugins install ./</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
    </view>

    <view class="step-card">
      <text class="step-card__title">4. 配置环境变量</text>
      <view class="copy-block" @tap="copyText(envSnippet)">
        <text class="copy-block__code">{{ envSnippet }}</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
    </view>

    <view class="step-card">
      <text class="step-card__title">5. 验证与使用</text>
      <view class="copy-block" @tap="copyText('/init\n/article AI Agent 入门指南\n/seednote 降噪耳机种草笔记')">
        <text class="copy-block__code">/init
/article AI Agent 入门指南
/seednote 降噪耳机种草笔记</text>
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
const envSnippet = `export ANBAN_API_KEY="你的完整 API Key"
export ANBAN_API_URL="https://api.creator.anbanai.com"`

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
