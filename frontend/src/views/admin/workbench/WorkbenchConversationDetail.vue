<template>
  <div v-if="previewFile" class="bs-chat h-[68dvh] overflow-hidden">
    <ResourcePreview :key="previewFile.version_id" :file="previewFile" :files="[previewFile]"
      :load-access="getWorkbenchChatResourceAccess" read-only hide-fullscreen @close="previewFile = null"
      @preview="file => { if (!file) previewFile = null }" />
  </div>
  <div v-show="!previewFile" class="min-w-0">
    <header class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-200 pb-3 dark:border-dark-700">
      <div class="min-w-0">
        <h4 class="break-words text-sm font-semibold">{{ title || '会话记录' }}</h4>
        <p v-if="trace" class="mt-1 break-all text-xs text-gray-500">{{ trace.user_name || trace.user_email }}</p>
      </div>
      <div class="flex gap-1 md:hidden">
        <button type="button" class="btn btn-secondary px-2 py-1 text-xs" :aria-pressed="!showCalls" @click="showCalls = false">会话</button>
        <button type="button" class="btn btn-secondary px-2 py-1 text-xs" :aria-pressed="showCalls" @click="showCalls = true">调用</button>
      </div>
    </header>

    <div class="grid h-[62dvh] min-h-56 min-w-0 md:grid-cols-[minmax(0,1.55fr)_minmax(0,1fr)]">
      <section ref="scroll" aria-label="会话消息" tabindex="0" class="min-w-0 overflow-auto overscroll-contain py-3 md:pr-4"
        :class="{ 'hidden md:block': showCalls }" @scroll.passive="onScroll">
        <div v-if="hasMore || historyError" class="mb-3 flex flex-wrap items-center justify-center gap-2 text-xs">
          <p v-if="historyError" role="alert" class="text-red-600 dark:text-red-400">{{ historyError }}</p>
          <button type="button" class="btn btn-secondary px-2 py-1 text-xs" :disabled="historyLoading" @click="loadHistory">
            {{ historyLoading ? '载入中…' : historyError ? '重试' : '加载更早的消息' }}
          </button>
        </div>
        <LoadingSpinner v-if="historyLoading && !messages.length" />
        <p v-else-if="!messages.length && !historyError" class="py-6 text-center text-sm text-gray-500">暂无会话消息</p>
        <ol class="divide-y divide-gray-100 dark:divide-dark-700">
          <li v-for="message in messages" :key="message.id" :data-message-id="message.id"
            class="min-w-0 px-2 py-3 sm:px-3" :class="message.run_id === selectedRun ? 'bg-primary-50/70 dark:bg-primary-950/30' : ''">
            <div class="mb-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
              <strong class="text-gray-800 dark:text-gray-200">{{ message.role === 'user' ? '用户' : '助手' }}</strong>
              <time :datetime="message.created_at">{{ officeTime(message.created_at) }}</time>
              <span v-if="message.role === 'assistant'">{{ runs.get(message.run_id)?.model }}</span>
              <span v-if="message.role === 'user' && runs.get(message.run_id)?.regeneration_of">重试</span>
              <button v-if="message.role === 'user'" type="button" class="ml-auto text-primary-600 hover:underline dark:text-primary-400"
                :aria-pressed="selectedRun === message.run_id" @click="selectRun(message.run_id)">调用明细</button>
            </div>
            <p v-if="message.role === 'user'" class="whitespace-pre-wrap break-words text-sm leading-relaxed">{{ chatText(message.content) }}</p>
            <ChatMarkdown v-else :content="chatText(message.content)" :files="filesFor(message)" class="text-sm" @preview="openPreview" />
            <div v-if="filesFor(message).length" class="mt-2 flex flex-wrap gap-2">
              <button v-for="file in filesFor(message)" :key="file.version_id" type="button"
                class="btn btn-secondary gap-1 px-2 py-1 text-xs" @click="openPreview(file)">
                <Icon name="document" size="sm" /><span class="break-all">{{ file.name }}</span>
              </button>
            </div>
          </li>
        </ol>
      </section>

      <aside class="min-w-0 overflow-auto overscroll-contain py-3 md:border-l md:border-gray-200 md:pl-4 md:dark:border-dark-700"
        :class="{ 'hidden md:block': !showCalls }" aria-label="本轮调用明细">
        <LoadingSpinner v-if="traceLoading" />
        <p v-else-if="traceError" role="alert" class="text-sm text-red-600 dark:text-red-400">
          {{ traceError }} <button type="button" class="font-medium text-primary-600" @click="loadTrace(selectedRun)">重试</button>
        </p>
        <template v-else-if="trace">
          <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h5 class="break-all text-sm font-semibold">{{ trace.run.model }}</h5>
            <StatusBadge :status="officeStatusVariant(trace.run.status)" :label="officeStatusLabels[trace.run.status] || trace.run.status" />
          </div>
          <p class="mt-1 text-xs text-gray-500">{{ officeTime(trace.run.created_at) }} · {{ officeDuration(trace.run) }}</p>
          <p class="mt-1 text-xs text-gray-500">模型 {{ trace.run.model_calls || 0 }} · 工具 {{ trace.run.tool_calls || 0 }} · {{ officeBilling(totalCredits) }}</p>
          <p v-if="selectedPromptOutsidePage" class="mt-2 whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-2 text-xs text-gray-700 dark:bg-dark-800 dark:text-gray-300">
            <strong class="mb-1 block">本轮用户输入</strong>{{ chatText(selectedPromptOutsidePage.content) }}
          </p>
          <p v-if="trace.run.error_code || trace.run.error_message" class="mt-2 break-words text-xs text-amber-700 dark:text-amber-300">{{ runErrorMessage(trace.run) }}</p>
          <p v-if="!trace.operations.length" class="mt-4 text-xs text-gray-500">尚无模型或工具调用</p>
          <ol v-else class="mt-3 divide-y divide-gray-100 border-y border-gray-100 dark:divide-dark-700 dark:border-dark-700">
            <li v-for="operation in trace.operations" :key="operation.id" class="min-w-0 py-1.5">
              <ToolOperationDetails v-if="operation.tool" :run-id="trace.run.id" :operation-id="operation.id"
                :title="operation.tool.title" :status="operation.tool.state" :load-detail="getWorkbenchChatToolDetail"
                :can-preview="file => !!file.mime" show-version @preview="openPreview" />
              <details v-else class="py-1 text-xs">
                <summary class="cursor-pointer break-words leading-relaxed">
                  <strong>{{ operation.kind === 'model' ? '模型调用' : '工具调用' }}</strong>
                  <span class="ml-2 text-gray-500">{{ operationLabel(operation) }}<template v-if="operation.usage?.duration_ms != null"> · {{ (operation.usage.duration_ms / 1000).toFixed(1) }} 秒</template></span>
                  <span v-if="operation.usage?.settled" class="ml-2 text-gray-500">{{ officeBilling(operation.usage.charge_credits) }}</span>
                </summary>
                <div class="mt-1.5 space-y-1 pl-4">
                  <p class="break-all font-mono text-gray-500">{{ operation.id }} · {{ officeTime(operation.dispatched_at || operation.created_at) }}</p>
                  <p v-for="(upstream, index) in operation.usage?.upstreams" :key="index" class="break-words">
                    {{ upstream.account_name || '#' + upstream.account_id }}<template v-if="upstream.upstream_model"> · {{ upstream.upstream_model }}</template>
                    <span v-if="upstream.outcome !== 'usage'" class="text-red-600 dark:text-red-400"> · 调用失败 {{ upstream.status_code || '' }}</span>
                  </p>
                  <p v-if="operation.usage?.settled" class="text-gray-600 dark:text-gray-400">
                    输入 {{ officeNumber(operation.usage.input_tokens) }} · 输出 {{ officeNumber(operation.usage.output_tokens) }} · 缓存命中 {{ officeNumber(operation.usage.cache_read_tokens) }}
                  </p>
                  <p v-else-if="operation.kind === 'model'" class="text-gray-500">暂无结算记录</p>
                </div>
              </details>
            </li>
          </ol>
        </template>
      </aside>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  getWorkbenchChatResourceAccess, getWorkbenchChatRun, getWorkbenchChatToolDetail, getWorkbenchConversationPage,
  type WorkbenchChatOperation, type WorkbenchChatRunDetail,
} from '@/api/admin/workbench'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import ChatMarkdown from '@/components/workbench/chat/ChatMarkdown.vue'
import ResourcePreview from '@/components/workbench/chat/ResourcePreview.vue'
import ToolOperationDetails from '@/components/workbench/chat/ToolOperationDetails.vue'
import { runErrorMessage } from '@/components/workbench/chat/chatPresentation'
import { chatError } from '@/stores/workbenchChat'
import { chatArtifacts, chatText, type ChatArtifact, type ChatMessage, type ChatRun, type ChatToolArtifact } from '@/types/workbenchChat'
import { officeBilling, officeDuration, officeNumber, officeStatusLabels, officeStatusVariant, officeTime } from './workbenchChatDisplay'
import '@/components/workbench/chat/chat.css'

const props = defineProps<{ runId: string }>()
const title = ref('')
const messages = ref<ChatMessage[]>([])
const runs = ref(new Map<string, ChatRun>())
const cursor = ref<number>()
const hasMore = ref(false)
const historyLoading = ref(false)
const historyError = ref('')
const initialPrompt = ref<ChatMessage>()
const scroll = ref<HTMLElement | null>(null)
const selectedRun = ref(props.runId)
const trace = ref<WorkbenchChatRunDetail>()
const traceLoading = ref(false)
const traceError = ref('')
const showCalls = ref(false)
const previewFile = ref<ChatArtifact | null>(null)
let historyRequest: AbortController | undefined
let traceRequest: AbortController | undefined

const selectedPromptOutsidePage = computed(() => {
  const prompt = initialPrompt.value
  return prompt?.run_id === selectedRun.value && !messages.value.some(message => message.id === prompt.id) ? prompt : undefined
})
const totalCredits = computed(() => trace.value?.operations.reduce((sum, operation) => sum + (operation.usage?.charge_credits || 0), 0) ?? 0)

function filesFor(message: ChatMessage) {
  if (message.role === 'user') return chatArtifacts(message.content)
  return runs.value.get(message.run_id)?.artifacts ?? chatArtifacts(message.content)
}

function openPreview(file: ChatToolArtifact) {
  if (file.mime) previewFile.value = { ...file, id: file.id || file.version_id, mime: file.mime, bytes: file.bytes ?? 0 }
}

function operationLabel(operation: WorkbenchChatOperation) {
  const labels: Record<string, string> = { prepared: '已准备', dispatched: '已派发', completed: '已完成', unknown: '结果待确认' }
  return labels[operation.state] || operation.state
}

async function loadHistory() {
  if (historyLoading.value) return
  const request = new AbortController()
  historyRequest = request
  historyLoading.value = true
  historyError.value = ''
  const element = scroll.value
  const previousHeight = element?.scrollHeight ?? 0
  const previousTop = element?.scrollTop ?? 0
  const before = cursor.value
  try {
    const result = await getWorkbenchConversationPage(props.runId, before, request.signal)
    if (request.signal.aborted) return
    title.value = result.conversation_title
    if (result.selected_user_message) initialPrompt.value = result.selected_user_message
    for (const run of result.runs) runs.value.set(run.id, run)
    const existing = new Set(messages.value.map(message => message.id))
    messages.value = [...result.messages.filter(message => !existing.has(message.id)), ...messages.value]
    cursor.value = result.next_before
    hasMore.value = Boolean(result.has_more && result.next_before && result.next_before !== before)
    await nextTick()
    if (!element || request.signal.aborted) return
    if (before) {
      element.scrollTop = previousTop + element.scrollHeight - previousHeight
      return
    }
    const focus = [...element.querySelectorAll<HTMLElement>('[data-message-id]')]
      .find(item => item.dataset.messageId === initialPrompt.value?.id)
    element.scrollTop = focus ? focus.getBoundingClientRect().top - element.getBoundingClientRect().top : element.scrollHeight
  } catch (cause) {
    if (!request.signal.aborted) historyError.value = chatError(cause)
  } finally {
    if (historyRequest === request) {
      historyRequest = undefined
      historyLoading.value = false
    }
  }
}

function onScroll() {
  if (scroll.value && scroll.value.scrollTop <= 72 && hasMore.value && !historyError.value) void loadHistory()
}

async function loadTrace(id: string) {
  traceRequest?.abort()
  const request = new AbortController()
  traceRequest = request
  trace.value = undefined
  traceError.value = ''
  traceLoading.value = true
  try {
    const result = await getWorkbenchChatRun(id, request.signal)
    if (!request.signal.aborted) trace.value = result
  } catch (cause) {
    if (!request.signal.aborted) traceError.value = chatError(cause)
  } finally {
    if (traceRequest === request) {
      traceRequest = undefined
      traceLoading.value = false
    }
  }
}

function selectRun(id: string) {
  showCalls.value = true
  if (selectedRun.value === id) return
  selectedRun.value = id
  void loadTrace(id)
}

onMounted(() => {
  void loadHistory()
  void loadTrace(props.runId)
})
onBeforeUnmount(() => {
  historyRequest?.abort()
  traceRequest?.abort()
})
</script>

<style scoped>
section[aria-label="会话消息"] { overflow-anchor: none; }
</style>
