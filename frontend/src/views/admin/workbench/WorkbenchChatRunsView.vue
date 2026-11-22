<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters><div class="card p-4 sm:p-6"><header class="mb-4 flex flex-wrap items-start justify-between gap-3"><div><h2 class="text-lg font-semibold">对话记录</h2><p class="mt-1 text-xs text-gray-500 dark:text-dark-400">查看用户会话、模型与工具调用、结算记录。</p></div><button class="btn btn-secondary" type="button" :disabled="loading" @click="load"><Icon name="refresh" :class="{ 'animate-spin': loading }" size="sm" />刷新</button></header><form class="flex flex-wrap items-end gap-3" @submit.prevent="search"><div class="w-full sm:w-64"><UserSearchSelect ref="userPicker" id="office-runs-user" v-model="userId" label="用户" placeholder="搜索名称、昵称或邮箱" :error="filterError" @change="onUserChange" /></div><div class="w-full sm:w-36"><label for="office-runs-mode" class="input-label">模式</label><Select id="office-runs-mode" v-model="filters.execution" :options="modeOptions" /></div><div class="w-full sm:w-40"><label for="office-runs-status" class="input-label">状态</label><Select id="office-runs-status" v-model="filters.status" :options="statusOptions" /></div><div class="min-w-40 flex-1"><Input id="office-runs-model" v-model="filters.model" label="模型" placeholder="模型 ID" /></div><div class="w-36"><label for="office-runs-period" class="input-label">统计时段</label><Select id="office-runs-period" v-model="filters.days" :options="dayOptions" /></div><button type="submit" class="btn btn-primary" :disabled="loading">搜索</button><button type="button" class="btn btn-secondary" @click="reset">重置</button></form><p v-if="error" role="alert" class="mt-3 text-sm text-red-600">{{ error }}</p></div></template>
      <template #table><DataTable :data="items" :columns="columns" :loading="loading" row-key="id">
        <template #cell-id="{ row }"><button type="button" class="max-w-64 break-all text-left font-mono text-xs text-primary-600 hover:underline" @click="openDetail(row.id)">{{ row.id }}</button><p class="mt-1 text-xs">{{ row.user_name || row.user_email || `#${row.user_id}` }}</p><p v-if="row.user_name" class="text-xs text-gray-500">{{ row.user_email }}</p></template>
        <template #cell-model="{ row }"><p class="max-w-52 break-words font-medium">{{ row.model }}</p><p class="mt-1 text-xs text-gray-500">{{ row.browser_driven ? '当前工作台' : '历史任务' }}<span v-if="row.search_enabled"> · 联网搜索</span></p></template>
        <template #cell-status="{ row }"><StatusBadge :status="officeStatusVariant(row.status)" :label="officeStatusLabels[row.status as ChatRunStatus] || row.status" /><p v-if="row.error_message || row.error_code" class="mt-2 max-w-64 whitespace-pre-wrap break-words text-xs text-amber-700 dark:text-amber-400">{{ runErrorMessage(row) }}</p></template>
        <template #cell-calls="{ row }"><p class="whitespace-nowrap text-xs">模型 {{ row.model_calls || 0 }} / 工具 {{ row.tool_calls || 0 }}</p></template>
        <template #cell-usage="{ row }"><p class="whitespace-nowrap text-xs">输入 {{ officeNumber(row.input_tokens) }}</p><p class="mt-1 whitespace-nowrap text-xs text-gray-500">输出 {{ officeNumber(row.output_tokens) }}</p><p class="mt-1 text-xs">{{ officeBilling(row.charge_credits) }}</p></template>
        <template #cell-created_at="{ row }"><p class="whitespace-nowrap text-xs">{{ officeTime(row.created_at) }}</p><p class="mt-1 text-xs text-gray-500">耗时 {{ officeDuration(row) }}</p></template>
        <template #cell-actions="{ row }"><button type="button" class="btn btn-secondary whitespace-nowrap text-xs" @click="openDetail(row.id)">查看会话</button></template>
        <template #empty><EmptyState title="没有匹配的对话记录" description="调整筛选条件，或等待用户创建对话任务。" /></template>
      </DataTable></template>
      <template #pagination><Pagination v-if="total" :total="total" :page="page" :page-size="pageSize" @update:page="setPage" @update:page-size="setPageSize" /></template>
    </TablePageLayout>
    <BaseDialog :show="!!detailId" title="会话详情" width="full" @close="detailId = ''">
      <WorkbenchConversationDetail v-if="detailId" :key="detailId" :run-id="detailId" />
    </BaseDialog>
  </AppLayout>
</template>
<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Input from '@/components/common/Input.vue'
import UserSearchSelect from '@/components/admin/UserSearchSelect.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import WorkbenchConversationDetail from './WorkbenchConversationDetail.vue'
import { OFFICE_RUN_STATUSES, listWorkbenchChatRuns, type WorkbenchChatRun, type WorkbenchChatRunQuery, type WorkbenchDays } from '@/api/admin/workbench'
import { officeStatusLabels, officeStatusVariant, officeBilling, officeTime, officeNumber, officeDuration } from './workbenchChatDisplay'
import { runErrorMessage } from '@/components/workbench/chat/chatPresentation'
import type { ChatRunStatus } from '@/types/workbenchChat'
import { chatError } from '@/stores/workbenchChat'
const items = ref<WorkbenchChatRun[]>([]), loading = ref(false), total = ref(0), page = ref(1), pageSize = ref(20), error = ref(''), filterError = ref('')
const userId = ref<number>(), userPicker = ref<InstanceType<typeof UserSearchSelect> | null>(null)
const filters = reactive({ model: '', execution: '', status: '', days: '7' }), applied = ref<WorkbenchChatRunQuery>({ days: 7 })
let sequence = 0, controller: AbortController | undefined, resetting = false
const modeOptions = [{ value: '', label: '全部记录' }, { value: 'browser', label: '当前工作台' }, { value: 'historical', label: '历史任务' }]
const statusOptions = [{ value: '', label: '全部状态' }, ...OFFICE_RUN_STATUSES.map(value => ({ value, label: officeStatusLabels[value] }))]
const dayOptions = [7, 30, 90].map(value => ({ value: String(value), label: `最近 ${value} 天` }))
const columns = [{ key: 'id', label: '任务 / 用户' }, { key: 'model', label: '模型 / 执行方式' }, { key: 'status', label: '状态' }, { key: 'calls', label: '调用' }, { key: 'usage', label: '用量 / 扣费' }, { key: 'created_at', label: '时间' }, { key: 'actions', label: '详情' }]
async function load() { controller?.abort(); controller = new AbortController(); const own = ++sequence; loading.value = true; error.value = ''; try { const value = await listWorkbenchChatRuns({ ...applied.value, page: page.value, page_size: pageSize.value }, controller.signal); if (own !== sequence) return; items.value = value.items; total.value = value.total; if (value.pages > 0 && page.value > value.pages) { page.value = value.pages; void load() } } catch (e) { if (own === sequence) { items.value = []; total.value = 0; error.value = chatError(e) } } finally { if (own === sequence) loading.value = false } }
function search() { filterError.value = ''; if (userPicker.value?.hasUnselectedQuery()) { filterError.value = '请从搜索结果中选择用户'; return } applied.value = { ...(userId.value ? { user_id: userId.value } : {}), ...(filters.model.trim() ? { model: filters.model.trim() } : {}), ...(filters.execution ? { execution: filters.execution as 'browser' | 'historical' } : {}), ...(filters.status ? { status: filters.status as ChatRunStatus } : {}), days: Number(filters.days) as WorkbenchDays }; page.value = 1; void load() }
function onUserChange() { if (!resetting) search() }
function reset() { resetting = true; filters.model = ''; filters.execution = ''; filters.status = ''; filters.days = '7'; userId.value = undefined; userPicker.value?.clear(); resetting = false; search() }
function setPage(value: number) { page.value = value; void load() }
function setPageSize(value: number) { pageSize.value = value; page.value = 1; void load() }
const detailId = ref('')
function openDetail(id: string) { detailId.value = id }
onMounted(() => { void load() })
onBeforeUnmount(() => { ++sequence; controller?.abort() })
</script>
