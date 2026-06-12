<template>
  <view class="page model-config-page">
    <view class="section">
      <view class="section-header">
        <text class="section-title">文本模型</text>
        <AbBadge v-if="textHasConfig" variant="info" size="sm">已配置</AbBadge>
      </view>
      <view class="field-block">
        <text class="field-label">模型</text>
        <AbInput v-model="text.model" placeholder="gpt-5.1 或 claude-sonnet..." />
      </view>
      <view class="field-block">
        <text class="field-label">Endpoint</text>
        <AbInput v-model="text.endpoint" placeholder="https://api.openai.com/v1" />
      </view>
      <view class="field-block">
        <text class="field-label">API Key</text>
        <AbInput v-model="text.api_key" type="password" placeholder="留空不修改；已有值显示 ****" />
      </view>
      <view class="field-block">
        <text class="field-label">代理</text>
        <AbInput v-model="text.proxy" placeholder="可选，例如 http://127.0.0.1:7890" />
      </view>
      <AbButton type="primary" size="lg" block :loading="savingText" @click="saveText">保存文本模型</AbButton>
    </view>

    <view class="section">
      <view class="section-header">
        <text class="section-title">图片模型</text>
        <AbBadge v-if="imageHasConfig" variant="info" size="sm">已配置</AbBadge>
      </view>
      <text class="field-label">服务商</text>
      <AbSelect v-model="image.provider" :options="providerOptions" placeholder="选择图片服务商" />
      <view class="field-block">
        <text class="field-label">模型</text>
        <AbInput v-model="image.model" placeholder="gpt-image-2 / gemini..." />
      </view>
      <view class="field-block">
        <text class="field-label">Endpoint</text>
        <AbInput v-model="image.endpoint" placeholder="图片接口地址" />
      </view>
      <view class="field-block">
        <text class="field-label">API Key</text>
        <AbInput v-model="image.api_key" type="password" placeholder="留空不修改；已有值显示 ****" />
      </view>
      <view class="field-block">
        <text class="field-label">代理</text>
        <AbInput v-model="image.proxy" placeholder="可选" />
      </view>
      <AbButton type="primary" size="lg" block :loading="savingImage" @click="saveImage">保存图片模型</AbButton>
    </view>

    <view class="section">
      <AbButton type="danger" size="lg" block :loading="clearing" :disabled="!textHasConfig && !imageHasConfig" @click="clearConfig">
        恢复系统默认
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { modelConfigApi, type ImageConfigDTO, type TextConfigDTO } from '@/api/model-config'
import AbBadge from '@/components/common/AbBadge.vue'
import AbButton from '@/components/common/AbButton.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbSelect from '@/components/common/AbSelect.vue'

const providerOptions = [
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Google Gemini' },
  { value: 'volcengine', label: 'Volcengine/Seedream' },
]

type TextConfigForm = Required<TextConfigDTO>
type ImageConfigForm = Required<ImageConfigDTO>

const text = reactive<TextConfigForm>({ model: '', endpoint: '', api_key: '', proxy: '' })
const image = reactive<ImageConfigForm>({ provider: '', model: '', endpoint: '', api_key: '', proxy: '' })
const savingText = ref(false)
const savingImage = ref(false)
const clearing = ref(false)

const textHasConfig = computed(() => !!(text.model || text.endpoint || text.api_key || text.proxy))
const imageHasConfig = computed(() => !!(image.provider || image.model || image.endpoint || image.api_key || image.proxy))

function assignText(data?: TextConfigDTO | null) {
  text.model = data?.model || ''
  text.endpoint = data?.endpoint || ''
  text.api_key = data?.api_key ? '****' : ''
  text.proxy = data?.proxy || ''
}

function assignImage(data?: ImageConfigDTO | null) {
  image.provider = data?.provider || ''
  image.model = data?.model || ''
  image.endpoint = data?.endpoint || ''
  image.api_key = data?.api_key ? '****' : ''
  image.proxy = data?.proxy || ''
}

async function loadConfig() {
  try {
    const config = await modelConfigApi.get()
    assignText(config.text)
    assignImage(config.image)
  } catch (err: any) {
    uni.showToast({ title: err?.message || '加载失败', icon: 'none' })
  }
}

async function saveText() {
  if (text.endpoint && !text.model) {
    uni.showToast({ title: '请填写文本模型名称', icon: 'none' })
    return
  }
  savingText.value = true
  try {
    await modelConfigApi.update({ text: { ...text } })
    uni.showToast({ title: '已保存', icon: 'success' })
    await loadConfig()
  } catch (err: any) {
    uni.showToast({ title: err?.message || '保存失败', icon: 'none' })
  } finally {
    savingText.value = false
  }
}

async function saveImage() {
  const hasAny = !!(image.provider || image.endpoint || image.api_key || image.model || image.proxy)
  if (hasAny && !(image.provider && image.endpoint && image.api_key && image.model)) {
    uni.showToast({ title: '图片模型需要服务商、Endpoint、API Key 和模型名称', icon: 'none' })
    return
  }
  savingImage.value = true
  try {
    await modelConfigApi.update({ image: { ...image } })
    uni.showToast({ title: '已保存', icon: 'success' })
    await loadConfig()
  } catch (err: any) {
    uni.showToast({ title: err?.message || '保存失败', icon: 'none' })
  } finally {
    savingImage.value = false
  }
}

function clearConfig() {
  uni.showModal({
    title: '恢复默认',
    content: '将清除当前账号的 MCP 模型覆盖配置。',
    confirmColor: '#DC2626',
    success: async (res) => {
      if (!res.confirm) return
      clearing.value = true
      try {
        await modelConfigApi.clear()
        assignText(null)
        assignImage(null)
      } catch (err: any) {
        uni.showToast({ title: err?.message || '操作失败', icon: 'none' })
      } finally {
        clearing.value = false
      }
    },
  })
}

onMounted(loadConfig)
</script>

<style lang="scss" scoped>
.model-config-page {
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

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: $ab-space-sm;
}

.section-title,
.field-label {
  display: block;
  font-size: $ab-text-base;
  color: $ab-text;
  font-weight: $ab-font-medium;
}

.field-block {
  margin-bottom: $ab-space-sm;
}

.field-label {
  margin-bottom: $ab-space-xs;
}
</style>
