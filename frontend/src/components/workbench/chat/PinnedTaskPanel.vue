<template>
  <details v-if="tasks.length" class="bs-chat-task-panel">
    <summary><Icon :name="running ? 'refresh' : run.status === 'completed' ? 'checkCircle' : run.status === 'failed' || run.status === 'needs_confirmation' ? 'exclamationCircle' : 'clock'" :class="{ 'bs-chat-spin': running }" size="sm" /><strong>{{ title }}</strong><span v-if="current" class="bs-chat-current-task">{{ current.title }}</span><span class="bs-chat-muted">{{ done }}/{{ tasks.length }}</span><Icon name="chevronDown" size="sm" /></summary>
    <ul><li v-for="task in tasks" :key="task.id"><Icon :name="task.status === 'running' ? 'refresh' : task.status === 'completed' ? 'checkCircle' : task.status === 'failed' ? 'exclamationCircle' : 'clock'" :class="{ 'bs-chat-spin': task.status === 'running' && running }" size="sm" /><span>{{ task.title }}</span></li></ul>
  </details>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { taskItems } from './chatPresentation'
import type { ChatEvent, ChatRun } from '@/types/workbenchChat'
const props = defineProps<{ run: ChatRun; events: ChatEvent[] }>()
const tasks = computed(() => taskItems(props.events))
const done = computed(() => tasks.value.filter(t => t.status === 'completed').length)
const current = computed(() => tasks.value.find(t => t.status === 'running'))
const running = computed(() => ['queued', 'running', 'recovering'].includes(props.run.status))
const title = computed(() => props.run.status === 'completed' ? '任务已完成' : props.run.status === 'cancelled' ? '任务已停止' : running.value && done.value === tasks.value.length ? '正在整理成果' : '任务')
</script>
