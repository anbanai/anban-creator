<template>
  <view class="page password-page">
    <view class="section">
      <text class="section-title">{{ hasPassword ? '修改密码' : '设置密码' }}</text>
      <view v-if="hasPassword" class="field-block">
        <text class="field-label">当前密码</text>
        <AbInput v-model="oldPassword" type="password" placeholder="输入当前密码" />
      </view>
      <view class="field-block">
        <text class="field-label">新密码</text>
        <AbInput v-model="newPassword" type="password" placeholder="至少 8 个字符" />
      </view>
      <view class="field-block">
        <text class="field-label">确认新密码</text>
        <AbInput v-model="confirmPassword" type="password" placeholder="再次输入新密码" />
      </view>
      <AbButton type="primary" size="lg" block :loading="saving" :disabled="!canSubmit" @click="savePassword">
        保存密码
      </AbButton>
    </view>
  </view>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { authApi } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import AbButton from '@/components/common/AbButton.vue'
import AbInput from '@/components/common/AbInput.vue'

const authStore = useAuthStore()
const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const saving = ref(false)

const hasPassword = computed(() => !!authStore.user?.has_password)
const canSubmit = computed(() => {
  return newPassword.value.length >= 8 && newPassword.value === confirmPassword.value && (!hasPassword.value || !!oldPassword.value)
})

async function savePassword() {
  if (!canSubmit.value || saving.value) return
  saving.value = true
  try {
    if (hasPassword.value) {
      await authApi.changePassword(oldPassword.value, newPassword.value)
    } else {
      await authApi.setPassword(newPassword.value)
    }
    uni.showToast({ title: '密码已保存', icon: 'success' })
    oldPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    await authStore.fetchUser()
  } catch (err: any) {
    uni.showToast({ title: err?.message || '保存失败', icon: 'none' })
  } finally {
    saving.value = false
  }
}
</script>

<style lang="scss" scoped>
.password-page {
  min-height: 100vh;
  background-color: $ab-background;
  padding: $ab-space-md;
}

.section {
  background-color: $ab-surface;
  border-radius: $ab-radius-md;
  padding: $ab-space-md;
  box-shadow: $ab-shadow-sm;
}

.section-title,
.field-label {
  display: block;
  font-size: $ab-text-base;
  color: $ab-text;
  font-weight: $ab-font-medium;
}

.section-title {
  margin-bottom: $ab-space-sm;
}

.field-block {
  margin-bottom: $ab-space-sm;
}

.field-label {
  margin-bottom: $ab-space-xs;
}
</style>
