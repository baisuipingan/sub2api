<template>
  <section class="card p-4 sm:p-6" aria-labelledby="office-overview-title">
    <header class="mb-5 flex flex-wrap items-start justify-between gap-3"><div><h3 id="office-overview-title" class="font-semibold text-gray-900 dark:text-white">对话与办公</h3><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">仅统计工作台对话任务；文件容量为当前存量。</p></div><div class="flex items-center gap-3"><RouterLink to="/admin/workbench/chat" class="text-sm text-primary-600 hover:underline dark:text-primary-400">对话记录</RouterLink><RouterLink to="/admin/workbench/files" class="text-sm text-primary-600 hover:underline dark:text-primary-400">用户文件</RouterLink><button type="button" class="btn btn-secondary" :disabled="loading" @click="load"><Icon name="refresh" :class="{ 'animate-spin': loading }" size="sm" /><span class="sr-only">刷新对话统计</span></button></div></header>
    <p v-if="error" role="alert" class="text-sm text-red-600">对话统计读取失败，请重试。</p>
    <LoadingSpinner v-else-if="loading && !overview" />
    <template v-else-if="overview">
      <dl class="grid grid-cols-2 gap-5 sm:grid-cols-4">
        <div v-for="item in primary" :key="item.label"><dt class="text-sm text-gray-500 dark:text-dark-400">{{ item.label }}</dt><dd class="mt-2 text-2xl font-semibold tabular-nums">{{ item.value }}</dd></div>
      </dl>
      <dl class="mt-5 grid grid-cols-2 gap-3 rounded-xl bg-gray-50 p-4 text-sm dark:bg-dark-900/50 sm:grid-cols-4 xl:grid-cols-8"><div v-for="item in secondary" :key="item.label"><dt class="text-gray-500 dark:text-dark-400">{{ item.label }}</dt><dd class="mt-1 font-medium tabular-nums">{{ item.value }}</dd></div></dl>
      <div class="mt-5 border-t border-gray-100 pt-4 dark:border-dark-700"><div class="flex flex-wrap items-center justify-between gap-2"><h4 class="text-sm font-medium">执行服务</h4><p class="text-xs text-gray-500">最后心跳不代表当前任务一定可运行</p></div><p v-if="!overview.workers.length" class="mt-3 text-sm text-gray-500">尚未收到执行服务心跳。</p><ul v-else class="mt-3 grid gap-3 sm:grid-cols-2"><li v-for="worker in overview.workers" :key="worker.id" class="rounded-xl border border-gray-100 p-3 text-xs dark:border-dark-700"><div class="flex flex-wrap items-center justify-between gap-2"><strong class="break-all font-mono">{{ worker.id }}</strong><StatusBadge :status="worker.executor_ready === true ? 'success' : worker.executor_ready === false ? 'warning' : 'neutral'" :label="worker.executor_ready === true ? '执行器就绪' : worker.executor_ready === false ? '执行器未就绪' : '无需办公执行器'" /></div><p class="mt-2 text-gray-500">{{ worker.preset }} · {{ officeTime(worker.last_seen_at) }}</p></li></ul></div>
    </template>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { getWorkbenchChatOverview, type WorkbenchChatOverview, type WorkbenchDays } from '@/api/admin/workbench'
import { formatBytes } from '@/utils/format'
import { officeBilling, officeNumber, officeTime } from './workbenchChatDisplay'
const props = defineProps<{ days: WorkbenchDays }>()
const overview = ref<WorkbenchChatOverview | null>(null), loading = ref(false), error = ref(false)
let epoch = 0, controller: AbortController | undefined
const primary = computed(() => overview.value ? [{ label: '对话任务', value: officeNumber(overview.value.total_runs) }, { label: '已完成', value: officeNumber(overview.value.completed) }, { label: '模型调用', value: officeNumber(overview.value.model_calls) }, { label: '已扣费用', value: officeBilling(overview.value.charge_credits) }] : [])
const secondary = computed(() => overview.value ? [{ label: '普通对话', value: officeNumber(overview.value.chat_runs) }, { label: '办公任务', value: officeNumber(overview.value.office_runs) }, { label: '排队 / 执行', value: `${overview.value.queued} / ${overview.value.running}` }, { label: '待回答 / 待确认', value: `${overview.value.waiting_user} / ${overview.value.needs_confirmation}` }, { label: '工具调用', value: officeNumber(overview.value.tool_calls) }, { label: '输入 / 输出 Token', value: `${officeNumber(overview.value.input_tokens)} / ${officeNumber(overview.value.output_tokens)}` }, { label: '当前文件', value: officeNumber(overview.value.resource_count) }, { label: '当前文件容量', value: formatBytes(overview.value.used_bytes) }] : [])
async function load() { controller?.abort(); controller = new AbortController(); const own = ++epoch; loading.value = true; error.value = false; overview.value = null; try { const value = await getWorkbenchChatOverview(props.days, controller.signal); if (own === epoch) overview.value = value } catch { if (own === epoch) error.value = true } finally { if (own === epoch) loading.value = false } }
watch(() => props.days, () => { void load() }, { immediate: true })
onBeforeUnmount(() => { ++epoch; controller?.abort() })
</script>
