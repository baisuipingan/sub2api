<template>
  <div class="bs-chat-task-turn">
    <div v-if="run.status === 'queued'" class="bs-chat-queue"><Icon name="clock" size="md" /><div><strong>任务已提交，正在排队</strong><p>轮到后会自动开始，你可以先离开页面。</p></div><button type="button" class="bs-chat-text-button" @click="emit('stop')">取消</button></div>
    <button v-if="(run.task_mode || run.mode === 'office') && !working && !events.length" type="button" class="bs-chat-text-button" :disabled="loadingProcess" @click="loadProcess"><Icon name="clipboard" size="sm" />{{ loadingProcess ? '正在加载过程…' : '查看任务过程' }}</button>
    <details v-if="tools.length" class="bs-chat-execution" :open="open" @toggle="open = ($event.target as HTMLDetailsElement).open">
      <summary><Icon :name="working ? 'refresh' : run.status === 'completed' ? 'checkCircle' : run.status === 'failed' || run.status === 'needs_confirmation' ? 'exclamationCircle' : 'clock'" :class="{ 'bs-chat-spin': working }" size="sm" /><span>{{ working ? '正在执行任务' : '任务过程' }}</span><span class="bs-chat-muted">{{ tools.length }} 个步骤</span><Icon name="chevronDown" size="sm" /></summary>
      <ol class="bs-chat-execution-steps"><li v-for="tool in tools" :key="tool.id"><span class="bs-chat-step-marker"><Icon :name="tool.state === 'running' ? 'refresh' : tool.state === 'failed' ? 'exclamationCircle' : tool.state === 'ended' ? 'clock' : 'checkCircle'" :class="{ 'bs-chat-spin': tool.state === 'running' && working }" size="sm" /></span><div><span>{{ tool.title }}</span><small v-if="tool.message">{{ tool.message }}</small></div></li></ol>
    </details>
    <div v-if="working && !tools.length && run.status !== 'queued' && !ask" class="bs-chat-breathing" role="status"><Icon class="bs-chat-spin" name="refresh" size="sm" /><span>{{ run.status === 'recovering' ? '正在恢复任务' : '正在准备任务' }}</span></div>
    <ClarifyCard v-if="ask" :key="ask.request_id" :request="ask" :busy="answering" :error="clarifyError" @submit="answer" />
    <p v-if="run.status === 'cancelled'" class="bs-chat-muted" role="status">已停止本次任务</p>
    <p v-else-if="run.error_message || run.status === 'failed' || run.status === 'needs_confirmation'" class="bs-chat-run-error" role="alert"><Icon name="exclamationCircle" size="sm" /><span>{{ runErrorMessage(run) }}<small v-if="run.error_code">{{ run.error_code }}</small><button v-if="run.status === 'needs_confirmation'" type="button" class="bs-chat-text-button" @click="emit('stop')">终止本次任务</button></span></p>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import ClarifyCard from './ClarifyCard.vue'
import { clarification, toolItems, runErrorMessage } from './chatPresentation'
import { workbenchChatApi } from '@/api/workbenchChat'
import { chatError, useWorkbenchChatStore } from '@/stores/workbenchChat'
import type { ChatEvent, ChatRun, ChatClarificationAnswer } from '@/types/workbenchChat'
const props = defineProps<{ run: ChatRun; events: ChatEvent[] }>()
const emit = defineEmits<{ stop: []; updated: [] }>()
const working = computed(() => ['queued', 'running', 'recovering'].includes(props.run.status))
const tools = computed(() => toolItems(props.events, props.run))
const ask = computed(() => clarification(props.events, props.run))
const loadingProcess = ref(false)
const store = useWorkbenchChatStore()
async function loadProcess() { loadingProcess.value = true; try { await store.loadRunEvents(props.run.id); open.value = true } catch (e) { clarifyError.value = chatError(e) } finally { loadingProcess.value = false } }
const open = ref(working.value), answering = ref(false), clarifyError = ref('')
watch(working, value => { if (!value) open.value = false })
async function answer(answers: ChatClarificationAnswer[]) { if (!ask.value || answering.value) return; answering.value = true; clarifyError.value = ''; try { await workbenchChatApi.clarify(props.run.id, ask.value.request_id, answers); emit('updated') } catch (e) { clarifyError.value = chatError(e) } finally { answering.value = false } }
</script>
