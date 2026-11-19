<template>
  <section v-if="tasks.length" class="bso-pinned-task-panel" data-component="PinnedTaskPanel">
    <button type="button" class="bso-plan-trigger" :aria-expanded="open" @click="open = !open">
      <Icon :name="running && (!open || wrappingUp) ? 'refresh' : complete ? 'check' : 'clipboard'" :class="{ 'bs-chat-spin': running && (!open || wrappingUp) }" size="sm" />
      <strong>{{ title }}</strong><span v-if="!open && current && running" class="bso-current-task">{{ current.title }}</span>
      <span class="bso-plan-count">{{ done }}/{{ tasks.length }}</span><Icon name="chevronDown" class="bso-plan-chevron" :class="{ open }" size="sm" />
    </button>
    <div class="bso-plan-collapse" :class="{ open }" :inert="!open"><div><ul>
      <li v-for="task in tasks" :key="task.id" :data-state="task.status"><span class="bso-plan-marker"><Icon v-if="task.status !== 'pending'" :name="task.status === 'running' && running ? 'refresh' : task.status === 'completed' ? 'checkCircle' : task.status === 'failed' ? 'exclamationCircle' : 'clock'" :class="{ 'bs-chat-spin': task.status === 'running' && running }" size="sm" /><span v-else class="bso-pending-ring" /></span><span :title="task.title">{{ task.title }}</span><span class="bso-sr-only">{{ task.status === 'completed' ? '已完成' : task.status === 'failed' ? '未完成' : task.status === 'running' && running ? '进行中' : '待完成' }}</span></li>
    </ul></div></div>
  </section>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { taskItems } from './chatPresentation'
import { isExecuting } from './taskPresentation'
import type { ChatEvent, ChatRun } from '@/types/workbenchChat'
import './workbenchArtifacts.css'
const props = defineProps<{ run: ChatRun; events: ChatEvent[] }>()
const open = ref(false)
watch(() => props.run.id, () => { open.value = false })
const tasks = computed(() => taskItems(props.events))
const done = computed(() => tasks.value.filter(task => task.status === 'completed').length)
const current = computed(() => tasks.value.find(task => task.status === 'running'))
const running = computed(() => isExecuting(props.run))
const complete = computed(() => props.run.status === 'completed')
const wrappingUp = computed(() => running.value && done.value === tasks.value.length)
const title = computed(() => complete.value ? '任务已完成' : props.run.status === 'cancelled' ? '任务已停止' : props.run.status === 'failed' ? '任务未完成' : props.run.status === 'partially_completed' ? '任务部分完成' : props.run.status === 'needs_confirmation' ? '结果需要确认' : props.run.status === 'waiting_user' ? '等待补充信息' : wrappingUp.value ? '正在整理成果' : '任务')
</script>
