<template>
  <view class="guide-page">
    <view class="intro-card">
      <text class="intro-card__title">Codex 接入</text>
      <text class="intro-card__body">
        通过 Codex 原生插件，你可以在 OpenAI Codex CLI 中用自然语言驱动 AI 创作流程，并调用专门的 subagent 完成端到端流水线。建议按「注册账号 → 创建 Key → 安装插件 → 配置 Key → $setup → 重启 → 开始使用」的顺序接入。
      </text>
    </view>

    <view class="step-card">
      <text class="step-card__title">1. 注册或登录 Anban 账号</text>
      <text class="step-text">先打开 Anban Studio / Web 管理端，完成注册或登录。没有平台账号的话，Codex 插件无法连接平台服务。</text>
      <view class="copy-block" @tap="copyText('https://creator.anbanai.com')">
        <text class="copy-block__code">https://creator.anbanai.com</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
    </view>

    <view class="step-card">
      <text class="step-card__title">2. 创建 API Key</text>
      <text class="step-text">
        登录后进入设置页创建 API Key。建议按设备命名，方便后面管理。注意：完整 Key 只在创建成功时展示一次。
      </text>
      <text v-if="keyPrefixes" class="step-code">已有密钥前缀：{{ keyPrefixes }}</text>
      <view class="copy-block" @tap="copyText('https://creator.anbanai.com/settings')">
        <text class="copy-block__code">https://creator.anbanai.com/settings</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
      <AbButton type="ghost" size="sm" @click="goApiKeys">管理密钥</AbButton>
    </view>

    <view class="step-card">
      <text class="step-card__title">3. 安装插件</text>
      <text class="step-text">
        方式 A（推荐）：在 Codex CLI 里直接告诉它「帮我安装 Anban Creator Codex 插件」并贴上仓库地址，AI 会自动完成 marketplace 注册、插件安装、以及 5 个 subagent 的注册。
      </text>
      <view class="copy-block" @tap="copyText('帮我安装 Anban Creator Codex 插件 https://github.com/anbanai/anban-creator-codex')">
        <text class="copy-block__code">帮我安装 Anban Creator Codex 插件 https://github.com/anbanai/anban-creator-codex</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
      <text class="step-text step-text--muted">
        方式 B（手动）：克隆仓库后依次执行 marketplace 注册、插件安装、subagent 注册脚本。最后一行脚本会把 5 个 subagent 注册到 ~/.codex/config.toml（幂等，可重复执行）。
      </text>
      <view class="copy-block" @tap="copyText(manualInstallSnippet)">
        <text class="copy-block__code">{{ manualInstallSnippet }}</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
    </view>

    <view class="step-card">
      <text class="step-card__title">4. 配置 API Key</text>
      <text class="step-text">
        Codex 插件通过环境变量读取平台连接信息。最少只需要配置 API Key：
      </text>
      <view class="copy-block" @tap="copyText(envKeySnippet)">
        <text class="copy-block__code">{{ envKeySnippet }}</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
      <text class="step-text">
        把这行写入 ~/.zshrc（或 ~/.bashrc），然后执行 source ~/.zshrc，或重新打开终端。如果使用官方在线服务，可以再加一行 ANBAN_API_URL；接自建或本地服务则填你自己的服务地址。
      </text>
      <view class="copy-block" @tap="copyText(envUrlSnippet)">
        <text class="copy-block__code">{{ envUrlSnippet }}</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
    </view>

    <view class="step-card">
      <text class="step-card__title">5. 运行 $setup</text>
      <text class="step-text">
        安装并配置好 Key 后，运行初始化命令，让插件检查 API Key、MCP 服务和账号连接是否正常：
      </text>
      <view class="copy-block" @tap="copyText('$setup')">
        <text class="copy-block__code">$setup</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
    </view>

    <view class="step-card">
      <text class="step-card__title">6. 重启并再次验证</text>
      <text class="step-text">
        $setup 跑完以后，请完全退出并重新启动 Codex。重启后再执行一次 $setup，确认连接已经正式生效。
      </text>
      <view class="copy-block" @tap="copyText('$setup')">
        <text class="copy-block__code">$setup</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
    </view>

    <view class="step-card">
      <text class="step-card__title">7. 开始使用</text>
      <text class="step-text">你可以直接说需求，也可以指定 subagent 启动端到端流水线。</text>
      <text class="step-text step-text--muted">自然语言示例：</text>
      <view class="copy-block" @tap="copyText(naturalExample)">
        <text class="copy-block__code">{{ naturalExample }}</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
      <text class="step-text step-text--muted">指定 subagent 示例：</text>
      <view class="copy-block" @tap="copyText(subagentExample)">
        <text class="copy-block__code">{{ subagentExample }}</text>
        <text class="copy-block__hint">点击复制</text>
      </view>
      <text class="step-text">
        第一次验证时，优先跑 use the wechatarticle subagent 或直接说一条自然语言需求，最容易确认整条链路是否通了。
      </text>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { APIKey } from '@/types'
import { apiKeysApi } from '@/api/api-keys'
import AbButton from '@/components/common/AbButton.vue'

const keys = ref<APIKey[]>([])
const keyPrefixes = computed(
  () => keys.value.map((key) => key.key_prefix).join('、') || '暂无密钥',
)

const manualInstallSnippet = `git clone https://github.com/anbanai/anban-creator-codex.git
cd anban-creator-codex
codex plugin marketplace add .
codex plugin install anban
bash install/install-subagents.sh`

const envKeySnippet = `export ANBAN_API_KEY="你的完整 API Key"`
const envUrlSnippet = `export ANBAN_API_URL="https://api.creator.anbanai.com"`

const naturalExample = `写一篇关于 AI Agent 的公众号文章
种草笔记，主题是降噪耳机`

const subagentExample = `use the wechatarticle subagent to write a 3000-word article about Rust ownership
use the seednote subagent for a 种草笔记 about 降噪耳机
delegate to designer: colorize the line art at /path/to/lineart/ using a warm summer palette`

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

  &--muted {
    color: $ab-text-tertiary;
    margin-bottom: $ab-space-xs;
  }
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
  margin-bottom: $ab-space-sm;
}

.copy-block__hint {
  display: block;
  margin-top: $ab-space-xs;
  font-size: $ab-text-xs;
  color: $ab-primary;
}
</style>
