<template>
  <details class="tool-operation" :data-state="state" @toggle="onToggle">
    <summary :title="summaryPreview || undefined">
      <Icon name="chevronRight" size="sm" class="tool-chevron" aria-hidden="true" />
      <span class="tool-title">{{ title }}</span>
      <span class="tool-preview">{{ summaryPreview }}</span>
      <span class="tool-status tool-row-status">{{ stateLabels[state] }}</span>
    </summary>
    <p v-if="loading" class="tool-status" role="status">正在加载调用详情…</p>
    <p v-else-if="error" class="tool-error" role="alert">
      {{ error }}
      <button type="button" class="tool-link" @click="load">重试</button>
    </p>
    <div v-else-if="detail" class="tool-details">
      <dl v-if="fields.length" class="tool-fields">
        <div v-for="field in fields" :key="field.label">
          <dt>{{ field.label }}</dt><dd>{{ field.value }}</dd>
        </div>
      </dl>
      <p v-if="detail.error_message" class="tool-error">{{ detail.error_message }}</p>
      <ul v-if="detail.artifacts?.length" class="tool-artifacts">
        <li v-for="file in detail.artifacts" :key="file.version_id">
          <button v-if="canPreview && canPreview(file)" type="button" class="tool-link" @click="emit('preview', file)">
            <Icon name="document" size="sm" />{{ file.name }}
          </button>
          <span v-else>{{ file.name }}</span>
          <span v-if="showVersion" class="tool-status">{{ file.version_id }}</span>
        </li>
      </ul>
      <ul v-if="detail.files?.length" class="tool-artifacts">
        <li v-for="file in detail.files" :key="file.name">
          <span>{{ file.name }}</span>
          <span class="tool-status">{{ fileLabels[file.status] || stateLabels.ended }}<template v-if="file.replacements != null"> · {{ file.replacements }} 处</template></span>
          <span v-if="file.message" class="tool-status">{{ file.message }}</span>
        </li>
      </ul>
      <p v-if="!fields.length && !detail.error_message && !detail.artifacts?.length && !detail.files?.length" class="tool-status">这次调用没有更多记录。</p>
    </div>
  </details>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { chatError } from '@/stores/workbenchChat'
import type { ChatToolArtifact, ChatToolDetail, ChatToolStatus } from '@/types/workbenchChat'
import { formatSearchCharge } from '@/utils/workbenchSearchBilling'

const props = defineProps<{
  runId: string
  operationId: string
  title: string
  status: ChatToolStatus
  preview?: string
  loadDetail: (run: string, operation: string, signal: AbortSignal) => Promise<ChatToolDetail>
  canPreview?: (file: ChatToolArtifact) => boolean
  showVersion?: boolean
}>()
const emit = defineEmits<{ preview: [file: ChatToolArtifact] }>()
const stateLabels: Record<ChatToolStatus, string> = {
  pending: '等待执行', running: '进行中', completed: '已完成', partial: '部分完成',
  failed: '失败', cancelled: '已停止', unknown: '结果待确认', ended: '已结束',
}
const fileLabels: Record<string, string> = { completed: '已完成', modified: '已修改', skipped: '已跳过', failed: '失败' }
const scopeLabels: Record<string, string> = { all: '全文', body: '正文', header: '页眉', footer: '页脚', title: '标题' }
const detail = ref<ChatToolDetail>(), loading = ref(false), error = ref(''), open = ref(false)
const state = computed(() => detail.value?.state || props.status)
const summaryPreview = computed(() => props.preview || detail.value?.query || detail.value?.source || '')
let current: AbortController | undefined
const fields = computed(() => {
  const item = detail.value
  if (!item) return []
  return [
    { label: '搜索词', value: item.query },
    { label: '供应商', value: item.provider },
    { label: '结果数', value: item.result_count == null ? undefined : `${item.result_count} 条` },
    { label: '本次搜索扣费', value: item.search_charge ? formatSearchCharge(item.search_charge) : undefined },
    { label: '来源', value: item.source },
    { label: '文件', value: item.file_name },
    { label: '输入版本', value: props.showVersion ? item.source_version_id : undefined },
    { label: '范围', value: item.scope ? scopeLabels[item.scope] : undefined },
    { label: '替换', value: item.replacements == null ? undefined : `${item.replacements} 处` },
    { label: '修改单元格', value: item.cells_changed == null ? undefined : `${item.cells_changed} 个` },
    { label: '已重算公式', value: item.calculated == null ? undefined : `${item.calculated} 个` },
    { label: '公式错误', value: item.formula_errors == null ? undefined : `${item.formula_errors} 个` },
    { label: '完成', value: item.processed == null ? undefined : `${item.processed} 项` },
    { label: '跳过', value: item.skipped == null ? undefined : `${item.skipped} 项` },
    { label: '失败', value: item.failed == null ? undefined : `${item.failed} 项` },
    { label: '耗时', value: item.duration_ms == null ? undefined : item.duration_ms < 1000 ? `${item.duration_ms} 毫秒` : `${(item.duration_ms / 1000).toFixed(1)} 秒` },
  ].filter(field => field.value)
})

function onToggle(event: Event) {
  open.value = (event.currentTarget as HTMLDetailsElement).open
  if (open.value && !detail.value && !loading.value) void load()
}

async function load() {
  current?.abort()
  const request = new AbortController()
  current = request
  loading.value = true
  error.value = ''
  try {
    const item = await props.loadDetail(props.runId, props.operationId, request.signal)
    if (current === request) detail.value = item
  } catch (cause) {
    if (current === request) error.value = chatError(cause)
  } finally {
    if (current === request) {
      current = undefined
      loading.value = false
    }
  }
}

watch(() => [props.runId, props.operationId, props.status], () => {
  current?.abort()
  current = undefined
  loading.value = false
  detail.value = undefined
  error.value = ''
  if (open.value) void load()
})
onBeforeUnmount(() => {
  current?.abort()
  current = undefined
})
</script>

<style scoped>
.tool-operation { width: 100%; min-width: 0; color: inherit; }
.tool-operation > summary { cursor: pointer; display: flex; align-items: center; gap: .5rem; min-height: 28px; padding-inline: 4px; border-radius: 5px; list-style: none; }
.tool-operation > summary::-webkit-details-marker { display: none; }
.tool-operation > summary:hover { background: var(--bc-fill, transparent); }
.tool-operation > summary:focus-visible { outline: 2px solid var(--bc-blue, currentColor); outline-offset: 2px; }
.tool-chevron { align-self: center; flex: none; color: var(--bc-muted, #64748b); }
.tool-operation[open] .tool-chevron { transform: rotate(90deg); }
.tool-title { flex: 0 1 auto; min-width: 0; max-width: 48%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tool-preview { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--bc-muted, #64748b); }
.tool-status { color: var(--bc-muted, #64748b); overflow-wrap: anywhere; }
.tool-row-status { flex: none; white-space: nowrap; font-size: 12px; }
.tool-operation:is([data-state="failed"], [data-state="partial"], [data-state="unknown"]) .tool-row-status { color: var(--bc-warning, #a46120); }
.tool-details, .tool-operation > p { margin: .45rem 0 .2rem 1.5rem; overflow-wrap: anywhere; }
.tool-fields, .tool-artifacts { margin: 0; padding: 0; display: grid; gap: .3rem; }
.tool-fields > div, .tool-artifacts > li { display: flex; align-items: baseline; gap: .5rem; flex-wrap: wrap; }
.tool-fields dt { color: var(--bc-muted, #64748b); }
.tool-fields dd { margin: 0; }
.tool-artifacts { margin-top: .35rem; list-style: none; }
.tool-error { margin-block: .3rem; color: var(--bc-danger, #b91c1c); }
.tool-link { appearance: none; border: 0; background: transparent; cursor: pointer; color: var(--bc-blue, #2563eb); display: inline-flex; align-items: center; gap: .3rem; padding: 0; text-align: start; }
.tool-link:hover { text-decoration: underline; }
</style>
