<template>
  <div class="bs-chat-task-turn bso-task-turn" data-component="TaskTurnPanel">
    <details v-if="hasProcess" class="bs-chat-process bs-chat-turn-process" :open="processOpen">
      <summary ref="processSummary" :aria-label="processLabel === '执行过程' ? processLabel : '执行过程，' + processLabel" :aria-disabled="keepProcessOpen || undefined" @click.prevent="toggleProcess">
        <span>{{ processLabel }}</span>
        <Icon name="chevronRight" size="sm" aria-hidden="true" />
      </summary>
      <div ref="processBody" class="bs-chat-turn-process-body">
        <template v-for="group in groups" :key="group.id">
          <p v-if="group.commentary.trim()" class="bs-chat-step-commentary">{{ group.commentary }}</p>
          <details v-if="group.hasTools" class="bs-chat-process" @toggle="onProcessToggle(group.id, $event)">
            <summary>
              <Icon name="chevronRight" size="sm" />
              <span>{{ groupTitle(group.tools) }}</span>
              <span v-if="group.count" class="bs-chat-tool-count">{{ group.count }} 项</span>
            </summary>
            <p v-if="loadingProcess" class="bs-chat-muted" role="status">正在加载工具过程…</p>
            <button v-if="processError" type="button" class="bs-chat-text-button" @click="loadProcess">重新加载</button>
            <p v-if="processError" class="bs-chat-small-error" role="alert">{{ processError }}</p>
            <ul v-if="(run.browser_driven || history.length) && group.tools.length" class="bs-chat-process-steps">
              <li v-for="tool in group.tools" :key="tool.id">
                <ToolOperationDetails v-if="run.browser_driven || persistedToolIDs.has(tool.id)" :run-id="run.id" :operation-id="tool.id" :title="tool.title" :status="tool.state"
                  :load-detail="workbenchChatApi.toolDetail" :can-preview="canPreview"
                  @preview="previewArtifact" />
                <span v-else>{{ tool.title }}<small v-if="tool.message">{{ tool.message }}</small></span>
              </li>
            </ul>
            <ExecutionTimeline v-else-if="!run.browser_driven" :events="events" :run="run" />
            <p v-if="processLoaded && !group.tools.length && run.browser_driven" class="bs-chat-muted">没有记录到工具调用</p>
          </details>
        </template>
        <details v-if="reasoningSummary" class="bs-chat-process">
          <summary><Icon name="chevronRight" size="sm" />推理摘要</summary>
          <p class="bs-chat-reasoning-summary">{{ reasoningSummary }}</p>
        </details>
        <slot name="streaming-answer" />
        <div v-if="working || waitingForAnswer" class="bso-breathing" role="status">
          <Icon :class="{ 'bs-chat-spin': !connectionInterrupted }" :name="connectionInterrupted ? 'exclamationCircle' : 'refresh'" size="sm" aria-hidden="true" />
          <span>{{ waitingForAnswer ? '正在整理回答…' : progressLabel }}</span>
        </div>
      </div>
    </details>
    <slot />
    <ClarifyCard v-if="ask" :key="ask.request_id" :request="ask" :busy="answering" :error="clarifyError" @submit="answer" />
    <p v-if="run.status === 'waiting_user' && !ask" class="bs-chat-muted" role="status">等待你的补充</p>
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
import { clarification, runArtifacts, toolItems, runErrorMessage, runStatusLabel, type ToolItem } from './chatPresentation'
import { isExecuting, workbenchProgress, type WorkbenchProgress } from './taskPresentation'
import { workbenchChatApi } from '@/api/workbenchChat'
import { chatError, useWorkbenchChatStore } from '@/stores/workbenchChat'
import type { ChatArtifact, ChatEvent, ChatRun, ChatClarificationAnswer, ChatToolArtifact, ChatToolSummary } from '@/types/workbenchChat'
const props = defineProps<{
  run: ChatRun
  events: ChatEvent[]
  progress?: WorkbenchProgress
  reasoningSummary?: string
  awaitingFinalAnswer?: boolean
  connectionInterrupted?: boolean
}>()
const emit = defineEmits<{ stop: []; updated: []; preview: [file: ChatArtifact] }>()
const working = computed(() => isExecuting(props.run))
const progress = computed(() => props.progress ?? workbenchProgress(props.events))
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
const groups = computed(() => {
  const toolMap = new Map(tools.value.map(tool => [tool.id, tool]))
  const displayed = new Set<string>()
  const groups = props.run.browser_driven ? progress.value.groups.map(group => {
    const groupTools = group.toolIds.map(id => {
      displayed.add(id)
      return toolMap.get(id)
    }).filter((tool): tool is ToolItem => !!tool)
    return { id: group.id, commentary: group.commentary, tools: groupTools, count: groupTools.length, hasTools: groupTools.length > 0 }
  }) : []
  const remaining = tools.value.filter(tool => !displayed.has(tool.id))
  const count = Math.max((props.run.tool_calls || 0) - displayed.size, remaining.length)
  const legacy = !props.run.browser_driven && !working.value && (props.run.task_mode || props.run.mode === 'office')
  if (count || legacy) {
    // Events can still be loading when the historical process is expanded.
    // Merge any remaining tool receipts into the last known group.
    const last = groups.at(-1)
    if (last?.hasTools) {
      last.tools.push(...remaining)
      last.count += count
    } else groups.push({ id: 'recorded-tools', commentary: '', tools: remaining, count, hasTools: true })
  }
  return groups
})
const ranHere = ref(working.value)
const waitingForAnswer = computed(() => ranHere.value && props.awaitingFinalAnswer &&
  (props.run.status === 'completed' || props.run.status === 'partially_completed'))
const keepProcessOpen = computed(() => working.value || waitingForAnswer.value || props.run.status === 'waiting_user')
const processExpanded = ref(false)
const hasProcess = computed(() => working.value || waitingForAnswer.value || !!groups.value.length || !!props.reasoningSummary)
const processOpen = computed(() => keepProcessOpen.value || processExpanded.value)
const processSummary = ref<HTMLElement | null>(null)
const processBody = ref<HTMLElement | null>(null)

const processLabel = computed(() => {
  if (working.value || waitingForAnswer.value) return '执行过程'
  if (props.run.status === 'completed' && props.run.finished_at) {
    const seconds = Math.floor((Date.parse(props.run.finished_at) - Date.parse(props.run.created_at)) / 1000)
    if (seconds > 0) {
      const minutes = Math.floor(seconds / 60)
      const remaining = seconds % 60
      return `用时 ${minutes ? minutes + ' 分钟 ' : ''}${remaining} 秒`
    }
  }
  return runStatusLabel[props.run.status]
})

function toggleProcess() {
  if (!keepProcessOpen.value) processExpanded.value = !processExpanded.value
}

watch(processOpen, open => {
  if (!open && processBody.value?.contains(document.activeElement)) processSummary.value?.focus()
})
function groupTitle(groupTools: ToolItem[]) {
  if (!props.run.browser_driven || !groupTools.length) return '工具过程'
  const titles = [...new Set(groupTools.map(tool => tool.title))]
  return titles.length > 2 ? `${titles.slice(0, 2).join(' · ')}等` : titles.join(' · ')
}
const files = computed(() => new Map(runArtifacts(props.events, props.run).map(file => [file.version_id, file])))
function canPreview(file: ChatToolArtifact) { return files.value.has(file.version_id) }
function previewArtifact(file: ChatToolArtifact) {
  const saved = files.value.get(file.version_id)
  if (saved) emit('preview', saved)
}
const progressLabel = computed(() => {
  if (props.connectionInterrupted) return '连接暂时异常，等待同步'
  if (props.run.status === 'recovering') return '正在恢复对话'
  if (props.run.status === 'queued') return '正在等待执行…'
  const activeGroup = progress.value.groups.at(-1)
  const runningTools = tools.value.filter(tool => tool.state === 'running' &&
    (!props.run.browser_driven || activeGroup?.toolIds.includes(tool.id) && progress.value.phase === 'tools'))
  if (runningTools.length > 1) return `正在执行 ${runningTools.length} 项工具…`
  const tool = runningTools[0]
  if (tool) return tool.message || tool.title
  if (progress.value.phase === 'tools') return '准备调用工具…'
  if (progress.value.phase === 'tool-results') return '正在整理工具结果…'
  if (progress.value.phase === 'answer') return '正在整理回答…'
  return '正在生成回答…'
})
const ask = computed(() => clarification(props.events, props.run))
const loadingProcess = ref(false), processLoaded = ref(false), processError = ref('')
const openGroups = new Set<string>()
let processRequest: AbortController | undefined
const store = useWorkbenchChatStore()
function onProcessToggle(id: string, event: Event) {
  if ((event.currentTarget as HTMLDetailsElement).open) openGroups.add(id)
  else openGroups.delete(id)
  if (processOpen.value && openGroups.size && !working.value && !processLoaded.value) void loadProcess()
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
  openGroups.clear()
  ranHere.value = working.value
  processExpanded.value = false
})
watch(working, active => {
  if (active) {
    ranHere.value = true
    processExpanded.value = false
  }
})
watch([working, processOpen], ([active, expanded]) => {
  if (!active && expanded && openGroups.size && !processLoaded.value) void loadProcess()
})
onBeforeUnmount(() => {
  processRequest?.abort()
  processRequest = undefined
})
async function answer(answers: ChatClarificationAnswer[]) { if (!ask.value || answering.value) return; answering.value = true; clarifyError.value = ''; try { await workbenchChatApi.clarify(props.run.id, ask.value.request_id, answers); emit('updated') } catch (e) { clarifyError.value = chatError(e) } finally { answering.value = false } }
</script>
