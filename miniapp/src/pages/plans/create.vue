<template>
  <view class="plan-create">
    <!-- Loading -->
    <AbLoading v-if="pageLoading" text="加载中..." />

    <view v-if="!pageLoading" class="form">
      <!-- Channel selector -->
      <view class="form-section">
        <text class="form-section__label">选择账号 <text class="form-section__required">*</text></text>
        <ChannelSelector
          v-model="form.channelId"
          placeholder="选择账号..."
          @change="onChannelChange"
        />
      </view>

      <!-- Plan title -->
      <view class="form-section">
        <text class="form-section__label">计划标题</text>
        <AbInput
          v-model="form.title"
          placeholder="如: 每日种草笔记"
        />
      </view>

      <!-- Schedule frequency -->
      <view class="form-section">
        <text class="form-section__label">执行频率 <text class="form-section__required">*</text></text>
        <SchedulePicker v-model="form.cronExpr" />
      </view>

      <!-- Custom time picker -->
      <view class="form-section">
        <text class="form-section__label">执行时间 <text class="form-section__required">*</text></text>
        <picker
          mode="time"
          :value="selectedTime"
          @change="onTimeChange"
        >
          <view class="time-picker">
            <text class="time-picker__value">{{ selectedTime || '选择时间' }}</text>
            <text class="time-picker__arrow">›</text>
          </view>
        </picker>
      </view>

      <!-- Weekday multi-select (visible for weekly presets) -->
      <view v-if="showWeekdaySelect" class="form-section">
        <text class="form-section__label">星期选择</text>
        <view class="weekday-grid">
          <view
            v-for="(day, index) in weekdays"
            :key="index"
            :class="['weekday-item', { active: form.weekdays.includes(index) }]"
            @tap="toggleWeekday(index)"
          >
            <text class="weekday-item__label">{{ day }}</text>
          </view>
        </view>
      </view>

      <!-- Optional prompt -->
      <view class="form-section">
        <text class="form-section__label">创作提示（可选）</text>
        <AbTextarea
          v-model="form.prompt"
          placeholder="每次执行时的额外要求，如: 结合当日热点..."
          :maxlength="5120"
          :rows="3"
        />
      </view>

      <!-- Image generation options -->
      <view class="form-section">
        <view class="switch-row">
          <view class="switch-row__text">
            <text class="form-section__label switch-row__label">跳过参考图</text>
            <text class="form-section__hint">执行计划时不使用账号默认视觉参考图</text>
          </view>
          <AbSwitch v-model="form.skipReferenceImage" />
        </view>

        <view class="field-spacer">
          <text class="form-section__label">参考图片</text>
          <AbInput
            v-model="form.referenceImageUrl"
            placeholder="可选，输入图片 URL 覆盖账号默认参考图"
          />
        </view>

        <view class="switch-row field-spacer">
          <view class="switch-row__text">
            <text class="form-section__label switch-row__label">图片水印</text>
            <text class="form-section__hint">生成支持水印的图片时添加平台水印</text>
          </view>
          <AbSwitch v-model="form.watermark" />
        </view>
      </view>
    </view>

    <!-- Fixed bottom button -->
    <view class="bottom-bar">
      <AbButton
        type="primary"
        block
        :loading="submitting"
        @click="handleSubmit"
      >
        {{ isEditing ? '保存修改' : '创建计划' }}
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { ref, reactive, computed } from 'vue'
import { onLoad } from '@dcloudio/uni-app'
import { plansApi } from '@/api/plans'
import ChannelSelector from '@/components/business/ChannelSelector.vue'
import SchedulePicker from '@/components/business/SchedulePicker.vue'
import AbButton from '@/components/common/AbButton.vue'
import AbInput from '@/components/common/AbInput.vue'
import AbSwitch from '@/components/common/AbSwitch.vue'
import AbTextarea from '@/components/common/AbTextarea.vue'
import AbLoading from '@/components/common/AbLoading.vue'

// --- Form state ---
const form = reactive({
  channelId: '',
  title: '',
  cronExpr: '0 9 * * *',
  prompt: '',
  weekdays: [] as number[],
  skipReferenceImage: false,
  referenceImageUrl: '',
  watermark: false,
})

const selectedTime = ref('09:00')
const errors = reactive({
  channelId: '',
  cronExpr: '',
})

const submitting = ref(false)
const pageLoading = ref(false)
const editingId = ref<string | null>(null)
const isEditing = computed(() => !!editingId.value)

// Weekday labels (starting from Sunday = 0)
const weekdays = ['日', '一', '二', '三', '四', '五', '六']

// Show weekday selector when cron uses day-of-week field
const showWeekdaySelect = computed(() => {
  const parts = form.cronExpr.trim().split(/\s+/)
  if (parts.length < 5) return false
  // The dayOfWeek field (index 4) has specific values
  const dow = parts[4]
  return dow !== '*' && dow !== '?'
})

// --- Time picker ---
function onTimeChange(e: any) {
  const time = e.detail.value as string
  selectedTime.value = time
  updateCronWithTime(time)
}

function updateCronWithTime(time: string) {
  const [hour, minute] = time.split(':')
  const parts = form.cronExpr.trim().split(/\s+/)
  if (parts.length >= 5) {
    parts[0] = minute
    parts[1] = hour
    form.cronExpr = parts.join(' ')
  }
}

// --- Weekday toggle ---
function toggleWeekday(index: number) {
  const idx = form.weekdays.indexOf(index)
  if (idx >= 0) {
    form.weekdays.splice(idx, 1)
  } else {
    form.weekdays.push(index)
    // Sort ascending
    form.weekdays.sort((a, b) => a - b)
  }
  updateCronWithWeekdays()
}

function updateCronWithWeekdays() {
  const parts = form.cronExpr.trim().split(/\s+/)
  if (parts.length < 5) return

  if (form.weekdays.length === 0) {
    parts[4] = '*'
  } else {
    // Convert Sunday=0 to cron Sunday=0
    parts[4] = form.weekdays.join(',')
  }
  form.cronExpr = parts.join(' ')
}

// --- Channel change ---
function onChannelChange(channel: any) {
  form.channelId = channel.id
  // Type is inferred from channel platform
  errors.channelId = ''
}

// --- Validation ---
function validate(): boolean {
  errors.channelId = ''
  errors.cronExpr = ''

  if (!form.channelId) {
    errors.channelId = '请选择账号'
    uni.showToast({ title: '请选择账号', icon: 'none' })
    return false
  }

  if (!form.cronExpr.trim()) {
    errors.cronExpr = '请设置执行频率'
    uni.showToast({ title: '请设置执行频率', icon: 'none' })
    return false
  }

  return true
}

// --- Submit ---
async function handleSubmit() {
  if (submitting.value) return
  if (!validate()) return

  submitting.value = true

  try {
    // Determine type from channel (fetch if needed)
    const cronExpr = form.cronExpr.trim()

    if (isEditing.value && editingId.value) {
      await plansApi.update(editingId.value, {
        cron_expr: cronExpr,
        prompt: form.prompt || undefined,
        skip_reference_image: form.skipReferenceImage || undefined,
        reference_image_url: form.referenceImageUrl.trim() || undefined,
        watermark: form.watermark || undefined,
      })
      uni.showToast({ title: '修改成功', icon: 'none' })
    } else {
      await plansApi.create({
        channel_id: form.channelId,
        type: 'seednote',
        cron_expr: cronExpr,
        prompt: form.prompt || undefined,
        skip_reference_image: form.skipReferenceImage || undefined,
        reference_image_url: form.referenceImageUrl.trim() || undefined,
        watermark: form.watermark || undefined,
      })
      uni.showToast({ title: '创建成功', icon: 'none' })
    }

    // Navigate back
    setTimeout(() => {
      uni.navigateBack()
    }, 500)
  } catch (err: any) {
    const msg = err?.message || (isEditing.value ? '修改失败' : '创建失败')
    uni.showToast({ title: msg, icon: 'none' })
  } finally {
    submitting.value = false
  }
}

// --- Edit mode: load existing plan ---
async function loadPlan(planId: string) {
  pageLoading.value = true
  try {
    const plan = await plansApi.get(planId)
    form.channelId = plan.channel_id || ''
    form.title = plan.title || ''
    form.cronExpr = plan.cron_expr || '0 9 * * *'
    form.prompt = plan.prompt || ''
    form.skipReferenceImage = plan.skip_reference_image || false
    form.referenceImageUrl = plan.reference_image_url || ''
    form.watermark = plan.watermark || false

    // Extract time from cron
    const parts = plan.cron_expr.trim().split(/\s+/)
    if (parts.length >= 2) {
      selectedTime.value = `${parts[1].padStart(2, '0')}:${parts[0].padStart(2, '0')}`
    }

    // Extract weekdays from cron day-of-week field
    if (parts.length >= 5 && parts[4] !== '*' && parts[4] !== '?') {
      const dowParts = parts[4].split(/[,-]/)
      form.weekdays = dowParts
        .map((d) => parseInt(d.trim(), 10))
        .filter((d) => !isNaN(d) && d >= 0 && d <= 6)
    }

    // Set navigation bar title
    uni.setNavigationBarTitle({ title: '编辑计划' })
  } catch (err) {
    console.error('Failed to load plan:', err)
    uni.showToast({ title: '加载计划失败', icon: 'none' })
  } finally {
    pageLoading.value = false
  }
}

// --- Page lifecycle ---
onLoad((query) => {
  if (query?.id) {
    editingId.value = query.id
    loadPlan(query.id)
  }
})
</script>

<style lang="scss" scoped>
.plan-create {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
  padding-bottom: 160rpx;
  box-sizing: border-box;
}

// Form
.form {
  display: flex;
  flex-direction: column;
  gap: $ab-space-md;
}

.form-section {
  display: flex;
  flex-direction: column;
  gap: $ab-space-xs;

  &__label {
    font-size: $ab-text-base;
    font-weight: $ab-font-medium;
    color: $ab-text;
  }

  &__required {
    color: $ab-danger;
  }

  &__hint {
    display: block;
    font-size: $ab-text-xs;
    color: $ab-text-tertiary;
    line-height: 1.4;
  }
}

.switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: $ab-space-md;

  &__text {
    flex: 1;
    min-width: 0;
  }

  &__label {
    margin-bottom: 4rpx;
  }
}

.field-spacer {
  margin-top: $ab-space-md;
}

// Time picker
.time-picker {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background-color: $ab-surface;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  padding: $ab-space-sm $ab-space-md;
  min-height: 80rpx;

  &__value {
    font-size: $ab-text-base;
    color: $ab-text;
  }

  &__arrow {
    font-size: $ab-text-lg;
    color: $ab-text-tertiary;
  }
}

// Weekday grid
.weekday-grid {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  gap: $ab-space-xs;
}

.weekday-item {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 72rpx;
  border: 2rpx solid $ab-border;
  border-radius: $ab-radius-sm;
  background-color: $ab-surface;

  &.active {
    border-color: $ab-primary;
    background-color: $ab-primary-bg;
  }

  &__label {
    font-size: $ab-text-sm;
    color: $ab-text;

    .active & {
      color: $ab-primary;
      font-weight: $ab-font-medium;
    }
  }
}

// Bottom bar
.bottom-bar {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  padding: $ab-space-sm $ab-space-md;
  padding-bottom: calc(#{$ab-space-sm} + env(safe-area-inset-bottom, 0px));
  background-color: $ab-surface;
  box-shadow: $ab-shadow-md;
  z-index: 10;
}
</style>
