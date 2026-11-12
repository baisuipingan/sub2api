<template>
  <section class="space-y-6" data-test="agent-settings" :aria-label="t('admin.agentSettings.title')">
    <div class="card p-6">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.agentSettings.title') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.description') }}</p>
      <p v-if="errorKey" role="alert" class="mt-4 text-sm text-red-600 dark:text-red-400">{{ t(errorKey) }}</p>
      <p v-if="noticeKey" role="status" class="mt-4 text-sm text-green-700 dark:text-green-400">{{ t(noticeKey) }}</p>
      <p v-if="loading" class="mt-4" role="status">{{ t('common.loading') }}</p>
      <div class="mt-4 flex flex-wrap items-center gap-3">
        <button type="button" data-test="agent-reload" class="btn btn-secondary" :disabled="loading || saving" @click="load">{{ t('admin.agentSettings.reload') }}</button>
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.reloadHint') }}</span>
      </div>
    </div>

    <template v-if="loaded">
      <fieldset :disabled="loading || saving" class="min-w-0 space-y-6">
        <div class="card">
          <h3 class="border-b border-gray-100 px-6 py-4 font-semibold dark:border-dark-700">{{ t('admin.agentSettings.connectionTitle') }}</h3>
          <div class="space-y-5 p-6">
            <label class="flex items-start gap-3">
              <input v-model="draft.enabled" data-test="agent-enabled" type="checkbox" class="mt-1 rounded" />
              <span><span class="block font-medium">{{ t('admin.agentSettings.enabled') }}</span><span class="mt-1 block text-sm text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.enabledHint') }}</span></span>
            </label>
            <p class="rounded-xl bg-amber-50 p-4 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">{{ t('admin.agentSettings.revocationHint') }}</p>
            <div>
              <label for="agent-group" class="input-label">{{ t('admin.agentSettings.group') }}</label>
              <Select id="agent-group" v-model="draft.group_id" data-test="agent-group" :options="groupOptions" :placeholder="t('admin.agentSettings.groupPlaceholder')" :aria-label="t('admin.agentSettings.group')" :disabled="loading || saving" :error="selectedGroupUnavailable" />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.groupHint') }}</p>
              <p v-if="selectedGroupUnavailable" data-test="agent-group-warning" class="mt-2 text-sm text-amber-700 dark:text-amber-300">{{ t('admin.agentSettings.groupUnavailable') }}</p>
            </div>
            <div>
              <label for="agent-gateway" class="input-label">{{ t('admin.agentSettings.gateway') }}</label>
              <input id="agent-gateway" v-model="draft.base_url" data-test="agent-gateway" type="url" class="input" :placeholder="suggestedGateway" autocomplete="off" />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.gatewayHint') }}</p>
            </div>
          </div>
        </div>

        <div class="card">
          <h3 class="border-b border-gray-100 px-6 py-4 font-semibold dark:border-dark-700">{{ t('admin.agentSettings.modelTitle') }}</h3>
          <div class="space-y-4 p-6">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.modelHint') }}</p>
            <div class="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-end">
              <div class="min-w-0 flex-1">
                <label for="agent-model-picker" class="input-label">{{ t('admin.agentSettings.addModelLabel') }}</label>
                <Select :key="pickerKey" id="agent-model-picker" :model-value="pickerSelection" data-test="agent-model-picker" :options="modelOptions" :placeholder="t('admin.agentSettings.modelPlaceholder')" :aria-label="t('admin.agentSettings.addModelLabel')" :searchable="true" :creatable="false" :loading="catalog.status === 'loading'" :disabled="loading || saving || !catalogCurrentReady || modelOptions.length === 0" @update:model-value="selectModel" />
              </div>
              <button type="button" data-test="agent-add-model" class="btn btn-secondary shrink-0" :disabled="loading || saving || !canAddModel" @click="addModel">{{ t('admin.agentSettings.addModel') }}</button>
              <button type="button" data-test="agent-refresh-models" class="btn btn-secondary shrink-0" :disabled="loading || saving || catalog.status === 'loading' || !selectedGroupEligible" @click="refreshModels">{{ t('admin.agentSettings.refreshModels') }}</button>
            </div>
            <p data-test="agent-model-catalog-status" class="text-sm" :class="catalog.status === 'error' ? 'text-red-600 dark:text-red-400' : 'text-gray-500 dark:text-gray-400'">{{ t(catalogMessageKey) }}</p>
            <p v-if="draft.models.length === 0" class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.noModels') }}</p>
          </div>
        </div>

        <AgentModelSettingsCard v-for="(model, index) in draft.models" :key="model.id" :model-value="model" :index="index" :is-default="draft.default_model_id === model.id" :unavailable="catalogCurrentReady && !catalog.models.includes(model.id)" :disabled="loading || saving" @update:model-value="updateModel(index, $event)" @remove="removeModel(index)" @make-default="makeDefault(model.id)" />
      </fieldset>

      <div class="card space-y-4 p-6">
        <ul v-if="submitted && validationCodes.length" role="alert" class="list-inside list-disc space-y-1 text-sm text-red-600 dark:text-red-400"><li v-for="code in validationCodes" :key="code">{{ t(`admin.agentSettings.validation.${code}`) }}</li></ul>
        <button type="button" data-test="agent-save" class="btn btn-primary" :disabled="loading || saving" @click="save">{{ saving ? t('common.saving') : t('admin.agentSettings.save') }}</button>
        <div class="space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.disableHint') }}</p>
          <button type="button" data-test="agent-disable" class="btn btn-secondary" :disabled="loading || saving" @click="disableConnections">{{ t('admin.agentSettings.disable') }}</button>
        </div>
      </div>
    </template>
    <TotpStepUpDialog :controller="stepUp" />
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { disableDesktopAgentConfig, getDesktopAgentConfig, getDesktopAgentModels, updateDesktopAgentConfig } from '@/api/admin/desktopAgent'
import type { DesktopAgentConfig, DesktopAgentModel } from '@/api/admin/desktopAgent'
import { getAllIncludingInactive } from '@/api/admin/groups'
import type { AdminGroup } from '@/types'
import { isDesktopAgentGroupEligible, validateDesktopAgentConfig } from '@/utils/desktopAgentConfig'
import { extractApiErrorCode } from '@/utils/apiError'
import { isStepUpBlocked, isStepUpCancelled, stepUpBlockReason, StepUpCancelledError, useStepUp } from '@/composables/useStepUp'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import Select from '@/components/common/Select.vue'
import AgentModelSettingsCard from './AgentModelSettingsCard.vue'

const props = withDefaults(defineProps<{ active?: boolean }>(), { active: true })
const { t } = useI18n()
const stepUp = useStepUp()
const loading = ref(false)
const saving = ref(false)
const loaded = ref(false)
const submitted = ref(false)
const errorKey = ref('')
const noticeKey = ref('')
const draft = ref<DesktopAgentConfig>({ enabled: false, group_id: 0, base_url: '', models: [], default_model_id: '' })
const groups = ref<AdminGroup[]>([])
const catalog = ref<{ status: 'idle' | 'loading' | 'ready' | 'error'; groupId: number; models: string[] }>({ status: 'idle', groupId: 0, models: [] })
const pickerSelection = ref<string | null>(null)
const pickerKey = ref(0)
const suggestedGateway = `${window.location.origin}/v1`
let alive = true
let loadGeneration = 0
let saveGeneration = 0
let catalogGeneration = 0

const selectedGroupEligible = computed(() => groups.value.some(group => group.id === draft.value.group_id && isDesktopAgentGroupEligible(group)))
const selectedGroupUnavailable = computed(() => draft.value.group_id > 0 && !selectedGroupEligible.value)
const groupOptions = computed(() => {
  const options = groups.value.filter(isDesktopAgentGroupEligible).map(group => ({ value: group.id, label: group.name, disabled: false }))
  if (selectedGroupUnavailable.value) {
    const selected = groups.value.find(group => group.id === draft.value.group_id)
    options.unshift({ value: draft.value.group_id, label: `${selected?.name || t('admin.agentSettings.unavailableGroup', { id: draft.value.group_id })} (#${draft.value.group_id})`, disabled: true })
  }
  return options
})
const catalogCurrentReady = computed(() => catalog.value.status === 'ready' && catalog.value.groupId === draft.value.group_id)
const modelOptions = computed(() => catalogCurrentReady.value ? catalog.value.models.filter(id => !draft.value.models.some(model => model.id === id)).map(id => ({ value: id, label: id })) : [])
const canAddModel = computed(() => draft.value.models.length < 32 && modelOptions.value.some(option => option.value === pickerSelection.value))
const catalogMessageKey = computed(() => {
  if (!selectedGroupEligible.value) return 'admin.agentSettings.catalogChooseGroup'
  if (catalog.value.status === 'loading') return 'admin.agentSettings.catalogLoading'
  if (catalog.value.status === 'error') return 'admin.agentSettings.catalogFailed'
  if (catalogCurrentReady.value && catalog.value.models.length === 0) return 'admin.agentSettings.catalogEmpty'
  return 'admin.agentSettings.catalogHint'
})
const validationCodes = computed(() => {
  const codes: string[] = validateDesktopAgentConfig(draft.value).map(issue => issue.code)
  if (selectedGroupUnavailable.value) codes.push('group_required')
  if (draft.value.models.length > 0) {
    if (!catalogCurrentReady.value) codes.push('catalog_required')
    else if (draft.value.models.some(model => !catalog.value.models.includes(model.id))) codes.push('model_unavailable')
  }
  return [...new Set(codes)]
})
function canEdit() { return alive && props.active && loaded.value && !loading.value && !saving.value }
function resetCatalog() {
  catalogGeneration += 1
  catalog.value = { status: 'idle', groupId: 0, models: [] }
  pickerSelection.value = null
  // 原因：Select的下拉挂载到body；重建能关闭旧分组的下拉和搜索，防止选入过期候选。
  pickerKey.value += 1
}
async function refreshModels() {
  if (!alive || !props.active || !loaded.value || loading.value || saving.value || !selectedGroupEligible.value) return
  resetCatalog()
  const generation = catalogGeneration
  const groupId = draft.value.group_id
  catalog.value = { status: 'loading', groupId, models: [] }
  try {
    const result = await getDesktopAgentModels(groupId)
    // 原因：分组切换、刷新和隐藏不会取消网络响应；仅当前激活分组的本代目录可以成为保存依据。
    if (!alive || !props.active || generation !== catalogGeneration || draft.value.group_id !== groupId) return
    if (result.group_id !== groupId || !Array.isArray(result.models) || !result.models.every(id => typeof id === 'string' && id.length > 0) || new Set(result.models).size !== result.models.length) throw new Error('Invalid model catalog')
    catalog.value = { status: 'ready', groupId, models: [...result.models] }
  } catch {
    if (alive && props.active && generation === catalogGeneration && draft.value.group_id === groupId) catalog.value = { status: 'error', groupId, models: [] }
  }
}
function selectModel(value: string | number | boolean | null) {
  if (canEdit()) pickerSelection.value = typeof value === 'string' && modelOptions.value.some(option => option.value === value) ? value : null
}
function addModel() {
  if (!canEdit() || !canAddModel.value || !pickerSelection.value) return
  const id = pickerSelection.value
  // 新模型从显式空预算和基础必需能力开始，不能复制其他模型能力或擅自成为默认模型。
  draft.value.models.push({
    id, display_name: id, context_window: 0, max_output_tokens: 0, default_max_output_tokens: 0,
    reasoning_levels: ['none'], default_reasoning: 'none', supports_reasoning: false,
    supports_streaming: true, supports_tool_call: true, requires_mfjs_tool_schema: false,
    input: { text: true, image: false, video: false, audio: false, pdf: false }, output: { text: true },
    supports_json_schema_output: false, supports_native_web_search: false, supports_mid_conversation_system: false,
  })
  if (!draft.value.base_url) draft.value.base_url = suggestedGateway
  pickerSelection.value = null
}
function updateModel(index: number, model: DesktopAgentModel) {
  if (canEdit() && draft.value.models[index]?.id === model.id) draft.value.models[index] = model
}
function removeModel(index: number) {
  if (!canEdit()) return
  const model = draft.value.models[index]
  if (!model) return
  draft.value.models.splice(index, 1)
  if (draft.value.default_model_id === model.id) draft.value.default_model_id = ''
}
function makeDefault(id: string) {
  if (canEdit() && draft.value.models.some(model => model.id === id)) draft.value.default_model_id = id
}
function applyConfig(config: DesktopAgentConfig) {
  draft.value = JSON.parse(JSON.stringify(config)) as DesktopAgentConfig
  submitted.value = false
}
async function load() {
  if (!props.active || loading.value || saving.value) return
  const generation = ++loadGeneration
  resetCatalog()
  loading.value = true
  loaded.value = false
  errorKey.value = ''
  noticeKey.value = ''
  try {
    const [config, allGroups] = await Promise.all([getDesktopAgentConfig(), getAllIncludingInactive()])
    if (!alive || generation !== loadGeneration || !props.active) return
    groups.value = allGroups
    applyConfig(config)
    loaded.value = true
  } catch {
    if (alive && generation === loadGeneration) errorKey.value = 'admin.agentSettings.loadFailed'
  } finally {
    if (alive && generation === loadGeneration) loading.value = false
  }
}
function reportSaveError(error: unknown) {
  if (isStepUpCancelled(error)) return
  if (isStepUpBlocked(error)) {
    errorKey.value = stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled'
    return
  }
  const code = extractApiErrorCode(error)
  errorKey.value = code === 'DESKTOP_AGENT_INVALID' ? 'admin.agentSettings.configRejected' : code === 'FORBIDDEN' ? 'admin.agentSettings.adminRequired' : 'admin.agentSettings.saveFailed'
}
async function persist(action: () => Promise<DesktopAgentConfig>, notice: string) {
  if (!canEdit()) return
  const generation = ++saveGeneration
  saving.value = true
  errorKey.value = ''
  noticeKey.value = ''
  try {
    const result = await stepUp.run(async () => {
      if (!alive || !props.active || generation !== saveGeneration) throw new StepUpCancelledError()
      try {
        return await action()
      } catch (error) {
        // 原因：离开后才返回的403不能重新唤起隐藏栏的验证弹窗或重试旧操作。
        if (!alive || !props.active || generation !== saveGeneration) throw new StepUpCancelledError()
        throw error
      }
    })
    if (!alive) return
    if (!props.active || generation !== saveGeneration) {
      loaded.value = false
      return
    }
    applyConfig(result)
    noticeKey.value = notice
  } catch (error) {
    if (alive && generation === saveGeneration && props.active) reportSaveError(error)
  } finally {
    if (alive) {
      saving.value = false
      if (props.active && !loaded.value) void load()
      else if (props.active && catalog.value.status === 'idle') void refreshModels()
    }
  }
}
function save() {
  if (!canEdit()) return
  submitted.value = true
  noticeKey.value = ''
  if (validationCodes.value.length) return
  // 原因：二次验证会暂停后重试；本次payload独立于可编辑草稿，避免验证期间被改写。
  const payload = JSON.parse(JSON.stringify(draft.value)) as DesktopAgentConfig
  return persist(() => updateDesktopAgentConfig(payload), 'admin.agentSettings.saved')
}
function disableConnections() {
  return persist(disableDesktopAgentConfig, 'admin.agentSettings.disabled')
}
watch(() => [props.active, loaded.value, loading.value, draft.value.group_id] as const, () => {
  resetCatalog()
  if (props.active && loaded.value && !loading.value && selectedGroupEligible.value) void refreshModels()
})
watch(() => props.active, active => {
  if (active) {
    if (!loaded.value && !loading.value) void load()
  } else {
    // 原因：v-show不会卸载，离开时废弃在途读取，防止回来后旧响应覆盖新草稿。
    loadGeneration += 1
    saveGeneration += 1
    loading.value = false
    stepUp.onCancel()
  }
}, { immediate: true })
onBeforeUnmount(() => {
  alive = false
  loadGeneration += 1
  saveGeneration += 1
  resetCatalog()
  stepUp.onCancel()
})
</script>
