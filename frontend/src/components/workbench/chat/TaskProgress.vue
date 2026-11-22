<template>
  <div class="bs-chat-task-turn bso-task-turn" data-component="TaskTurnPanel">
    <div v-if="working && !ask && (!hasText || currentTool)" class="bso-breathing" role="status">
      <Icon class="bs-chat-spin" name="refresh" size="sm" />
      <span>{{ currentTool?.message || currentTool?.title || progressLabel }}</span>
    </div>
    <details v-if="hasProcess" class="bs-chat-process" @toggle="onProcessToggle">
      <summary>
        <Icon name="chevronRight" size="sm" />
        <span>工具过程</span>
        <span v-if="toolCount">{{ toolCount }} 项</span>
      </summary>
      <p v-if="loadingProcess" class="bs-chat-muted" role="status">正在加载工具过程…</p>
      <button v-if="processError" type="button" class="bs-chat-text-button" @click="loadProcess">重新加载</button>
      <p v-if="processError" class="bs-chat-small-error" role="alert">{{ processError }}</p>
      <ul v-if="(run.browser_driven || history.length) && tools.length" class="bs-chat-process-steps">
        <li v-for="tool in tools" :key="tool.id">
          <ToolOperationDetails v-if="run.browser_driven || persistedToolIDs.has(tool.id)" :run-id="run.id" :operation-id="tool.id" :title="tool.title" :status="tool.state"
            :load-detail="workbenchChatApi.toolDetail" :can-preview="canPreview"
            @preview="previewArtifact" />
          <span v-else>{{ tool.title }}<small v-if="tool.message">{{ tool.message }}</small></span>
        </li>
      </ul>
      <ExecutionTimeline v-else-if="!run.browser_driven" :events="events" :run="run" />
      <p v-if="processLoaded && !tools.length" class="bs-chat-muted">没有记录到工具调用</p>
    </details>
    <ClarifyCard v-if="ask" :key="ask.request_id" :request="ask" :busy="answering" :error="clarifyError" @submit="answer" />
    <p v-if="run.status === 'cancelled'" class="bs-chat-muted" role="status">已停止本次任务</p>
    <p v-else-if="run.error_message || run.status === 'failed' || run.status === 'needs_confirmation'" class="bs-chat-run-error" role="alert"><Icon name="exclamationCircle" size="sm" /><span>{{ runErrorMessage(run) }}<small v-if="run.error_code">{{ run.error_code }}</small><button v-if="run.status === 'needs_confirmation'" type="button" class="bs-chat-text-button" @click="emit('stop')">终止本次任务</button></span></p>
  </div>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import ClarifyCard from './ClarifyCard.vue'
import ExecutionTimeline from './ExecutionTimeline.vue'
import ToolOperationDetails from './ToolOperationDetails.vue'
import './workbenchArtifacts.css'
import { clarification, runArtifacts, toolItems, runErrorMessage } from './chatPresentation'
import { workbenchChatApi } from '@/api/workbenchChat'
import { chatError, useWorkbenchChatStore } from '@/stores/workbenchChat'
import type { ChatArtifact, ChatEvent, ChatRun, ChatClarificationAnswer, ChatToolArtifact, ChatToolSummary } from '@/types/workbenchChat'
const props = defineProps<{ run: ChatRun; events: ChatEvent[]; hasText?: boolean }>()
const emit = defineEmits<{ stop: []; updated: []; preview: [file: ChatArtifact] }>()
const working = computed(() => ['queued', 'running', 'recovering'].includes(props.run.status))
const history = ref<ChatToolSummary[]>([])
const persistedToolIDs = computed(() => new Set(history.value.map(tool => tool.id)))
const tools = computed(() => {
  const items = new Map(toolItems(props.events, props.run).map(tool => [tool.id, tool]))
  for (const tool of history.value) {
    const previous = items.get(tool.id)
    items.set(tool.id, { message: previous?.message || '', created_at: previous?.created_at || '', ...tool })
  }
  return [...items.values()]
})
const files = computed(() => new Map(runArtifacts(props.events, props.run).map(file => [file.version_id, file])))
function canPreview(file: ChatToolArtifact) { return files.value.has(file.version_id) }
function previewArtifact(file: ChatToolArtifact) {
  const saved = files.value.get(file.version_id)
  if (saved) emit('preview', saved)
}
const currentTool = computed(() => [...tools.value].reverse().find(tool => tool.state === 'running'))
const toolCount = computed(() => Math.max(props.run.tool_calls || 0, tools.value.length))
const hasProcess = computed(() => !!toolCount.value || !working.value && !props.run.browser_driven && (props.run.task_mode || props.run.mode === 'office'))
const progressLabel = computed(() => {
  if (props.run.status === 'recovering') return '正在恢复对话'
  if (props.run.status === 'queued' && !props.run.browser_driven) return '正在等待执行'
  return '正在生成回答'
})
const ask = computed(() => clarification(props.events, props.run))
const loadingProcess = ref(false), processLoaded = ref(false), processError = ref(''), processOpen = ref(false)
let processRequest: AbortController | undefined
const store = useWorkbenchChatStore()
function onProcessToggle(event: Event) {
  processOpen.value = (event.currentTarget as HTMLDetailsElement).open
  if (processOpen.value && !working.value && !processLoaded.value) void loadProcess()
}
async function loadProcess() {
  if (loadingProcess.value) return
  const request = new AbortController()
  processRequest = request
  loadingProcess.value = true
  processError.value = ''
  const id = props.run.id
  try {
    if (props.run.browser_driven || !!props.run.tool_calls) {
      const items = await workbenchChatApi.tools(id, request.signal)
      if (processRequest === request) history.value = items
    }
    if (!props.run.browser_driven && processRequest === request && !tools.value.length) {
      await store.loadRunEvents(id)
    }
    if (processRequest === request) processLoaded.value = true
  } catch (e) {
    if (processRequest === request) processError.value = chatError(e)
  } finally {
    if (processRequest === request) {
      processRequest = undefined
      loadingProcess.value = false
    }
  }
}
const answering = ref(false), clarifyError = ref('')
watch(() => props.run.id, () => {
  processRequest?.abort()
  processRequest = undefined
  history.value = []
  clarifyError.value = ''
  processError.value = ''
  loadingProcess.value = false
  processLoaded.value = false
  if (processOpen.value && !working.value && (props.run.browser_driven || !!props.run.tool_calls)) void loadProcess()
})
watch(working, active => {
  if (!active && processOpen.value && !processLoaded.value) void loadProcess()
})
onBeforeUnmount(() => {
  processRequest?.abort()
  processRequest = undefined
})
async function answer(answers: ChatClarificationAnswer[]) { if (!ask.value || answering.value) return; answering.value = true; clarifyError.value = ''; try { await workbenchChatApi.clarify(props.run.id, ask.value.request_id, answers); emit('updated') } catch (e) { clarifyError.value = chatError(e) } finally { answering.value = false } }
</script>
