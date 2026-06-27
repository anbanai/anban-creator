<template>
  <view class="project-selector" @tap="showPicker">
    <view class="project-selector__display" v-if="selectedProject">
      <PlatformAvatar :platform="selectedProject.platform" :size="32" />
      <text class="project-selector__name">{{ selectedProject.name }}</text>
    </view>
    <text class="project-selector__placeholder" v-else>{{ placeholder }}</text>
    <text class="project-selector__arrow">›</text>
  </view>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import type { Project } from '@/types'
import { projectsApi } from '@/api/projects'
import PlatformAvatar from './PlatformAvatar.vue'

const props = defineProps<{
  modelValue?: string
  placeholder?: string
  platformFilter?: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  change: [project: Project]
}>()

const projects = ref<Project[]>([])

const selectedProject = computed(() =>
  projects.value.find((c) => c.id === props.modelValue),
)

async function loadProjects() {
  try {
    projects.value = await projectsApi.list({ status: 'active', platform: props.platformFilter })
  } catch (err) {
    console.error('Failed to load projects:', err)
  }
}

function showPicker() {
  if (projects.value.length === 0) {
    uni.showToast({ title: '暂无可用账号', icon: 'none' })
    return
  }

  const names = projects.value.map((c) => c.name)
  uni.showActionSheet({
    itemList: names,
    success: (res) => {
      const project = projects.value[res.tapIndex]
      if (project) {
        emit('update:modelValue', project.id)
        emit('change', project)
      }
    },
  })
}

onMounted(loadProjects)

defineExpose({ loadProjects })
</script>

<style lang="scss" scoped>
.project-selector {
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
