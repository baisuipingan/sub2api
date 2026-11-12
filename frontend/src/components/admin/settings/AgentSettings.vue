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
          <div v-if="!draft.model" class="space-y-4 p-6">
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.unconfigured') }}</p>
            <button type="button" data-test="agent-configure-model" class="btn btn-secondary" @click="configureModel">{{ t('admin.agentSettings.configureModel') }}</button>
          </div>
          <div v-else class="space-y-5 p-6">
            <div class="grid min-w-0 gap-4 sm:grid-cols-2">
              <div><label for="agent-model-id" class="input-label">{{ t('admin.agentSettings.modelId') }}</label><input id="agent-model-id" v-model="draft.model.id" data-test="agent-model-id" class="input" maxlength="128" autocomplete="off" /></div>
              <div><label for="agent-model-name" class="input-label">{{ t('admin.agentSettings.displayName') }}</label><input id="agent-model-name" v-model="draft.model.display_name" data-test="agent-model-name" class="input" maxlength="128" /></div>
            </div>
            <div class="grid min-w-0 gap-4 md:grid-cols-3">
              <div><label for="agent-context" class="input-label">{{ t('admin.agentSettings.contextWindow') }}</label><input id="agent-context" v-model.number="draft.model.context_window" data-test="agent-context" type="number" min="1" max="100000000" step="1" class="input" /></div>
              <div><label for="agent-max-output" class="input-label">{{ t('admin.agentSettings.maxOutputTokens') }}</label><input id="agent-max-output" v-model.number="draft.model.max_output_tokens" data-test="agent-max-output" type="number" min="1" step="1" class="input" /></div>
              <div><label for="agent-default-output" class="input-label">{{ t('admin.agentSettings.defaultMaxOutputTokens') }}</label><input id="agent-default-output" v-model.number="draft.model.default_max_output_tokens" data-test="agent-default-output" type="number" min="1" step="1" class="input" /></div>
            </div>
            <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.budgetHint') }}</p>
            <details class="rounded-xl border border-gray-200 dark:border-dark-700" open>
              <summary class="cursor-pointer px-4 py-3 font-medium">{{ t('admin.agentSettings.advanced') }}</summary>
              <div class="space-y-5 border-t border-gray-100 p-4 dark:border-dark-700">
                <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.capabilitiesHint') }}</p>
                <div class="grid gap-3 sm:grid-cols-2">
                  <label v-for="item in capabilityFields" :key="item.key" class="flex items-start gap-2 text-sm">
                    <input :checked="capabilityValue(item.key)" :data-test="`agent-cap-${item.key}`" type="checkbox" class="mt-0.5 rounded" @change="setCapability(item.key, $event)" />
                    {{ t(`admin.agentSettings.capabilities.${item.label}`) }}
                  </label>
                </div>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.requiredCapabilitiesHint') }}</p>
                <fieldset class="space-y-3">
                  <legend class="input-label">{{ t('admin.agentSettings.reasoningLevels') }}</legend>
                  <div class="flex flex-wrap gap-x-5 gap-y-3">
                    <label v-for="level in DESKTOP_AGENT_REASONING_LEVELS" :key="level" class="flex items-center gap-2 text-sm"><input :checked="draft.model.reasoning_levels.includes(level)" :data-test="`agent-reasoning-${level}`" type="checkbox" :value="level" @change="setReasoningLevel(level, $event)" />{{ level }}</label>
                  </div>
                </fieldset>
                <div>
                  <label for="agent-default-reasoning" class="input-label">{{ t('admin.agentSettings.defaultReasoning') }}</label>
                  <Select id="agent-default-reasoning" v-model="draft.model.default_reasoning" data-test="agent-default-reasoning" :options="reasoningOptions" :aria-label="t('admin.agentSettings.defaultReasoning')" :disabled="loading || saving" />
                  <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.reasoningHint') }}</p>
                </div>
              </div>
            </details>
          </div>
        </div>
      </fieldset>

      <div class="card space-y-4 p-6">
        <ul v-if="submitted && validationCodes.length" role="alert" class="list-inside list-disc space-y-1 text-sm text-red-600 dark:text-red-400"><li v-for="code in validationCodes" :key="code">{{ t(`admin.agentSettings.validation.${code}`) }}</li></ul>
        <button type="button" data-test="agent-save" class="btn btn-primary" :disabled="loading || saving" @click="save">{{ saving ? t('common.saving') : t('admin.agentSettings.save') }}</button>
        <div v-if="validationCodes.length || selectedGroupUnavailable" class="space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.disableOnlyHint') }}</p>
          <button type="button" data-test="agent-disable-only" class="btn btn-secondary" :disabled="loading || saving" @click="disableOnly">{{ t('admin.agentSettings.disableOnly') }}</button>
        </div>
      </div>
    </template>
    <TotpStepUpDialog :controller="stepUp" />
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getDesktopAgentConfig, updateDesktopAgentConfig, DESKTOP_AGENT_REASONING_LEVELS } from '@/api/admin/desktopAgent'
import type { DesktopAgentConfig, DesktopAgentModel, DesktopAgentReasoningLevel, DesktopAgentUpdateRequest } from '@/api/admin/desktopAgent'
import { getAllIncludingInactive } from '@/api/admin/groups'
import type { AdminGroup } from '@/types'
import { isDesktopAgentGroupEligible, validateDesktopAgentConfig } from '@/utils/desktopAgentConfig'
import { extractApiErrorCode } from '@/utils/apiError'
import { isStepUpBlocked, isStepUpCancelled, stepUpBlockReason, StepUpCancelledError, useStepUp } from '@/composables/useStepUp'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import Select from '@/components/common/Select.vue'

const props = withDefaults(defineProps<{ active?: boolean }>(), { active: true })
const { t } = useI18n()
const stepUp = useStepUp()
const loading = ref(false)
const saving = ref(false)
const loaded = ref(false)
const submitted = ref(false)
const errorKey = ref('')
const noticeKey = ref('')
const draft = ref<DesktopAgentConfig>({ enabled: false, group_id: 0, base_url: '' })
const groups = ref<AdminGroup[]>([])
const suggestedGateway = `${window.location.origin}/v1`
let alive = true
let loadGeneration = 0
let saveGeneration = 0

const selectedGroupUnavailable = computed(() => draft.value.group_id > 0 && !groups.value.some(group => group.id === draft.value.group_id && isDesktopAgentGroupEligible(group)))
const groupOptions = computed(() => {
  const options = groups.value.filter(isDesktopAgentGroupEligible).map(group => ({ value: group.id, label: group.name, disabled: false }))
  if (selectedGroupUnavailable.value) {
    const selected = groups.value.find(group => group.id === draft.value.group_id)
    options.unshift({ value: draft.value.group_id, label: `${selected?.name || t('admin.agentSettings.unavailableGroup', { id: draft.value.group_id })} (#${draft.value.group_id})`, disabled: true })
  }
  return options
})
const validationCodes = computed(() => {
  const codes = validateDesktopAgentConfig(draft.value).map(issue => issue.code)
  if (draft.value.enabled && selectedGroupUnavailable.value) codes.push('group_required')
  return [...new Set(codes)]
})
const reasoningOptions = computed(() => DESKTOP_AGENT_REASONING_LEVELS.map(level => ({ value: level, label: level, disabled: !draft.value.model?.reasoning_levels.includes(level) })))
const capabilityFields = [
  { key: 'input.text', label: 'inputText' }, { key: 'input.image', label: 'inputImage' },
  { key: 'input.video', label: 'inputVideo' }, { key: 'input.audio', label: 'inputAudio' },
  { key: 'input.pdf', label: 'inputPdf' }, { key: 'output.text', label: 'outputText' },
  { key: 'supports_streaming', label: 'supportsStreaming' }, { key: 'supports_tool_call', label: 'supportsToolCall' },
  { key: 'supports_reasoning', label: 'supportsReasoning' }, { key: 'requires_mfjs_tool_schema', label: 'requiresMfjsToolSchema' },
  { key: 'supports_json_schema_output', label: 'supportsJsonSchemaOutput' },
  { key: 'supports_native_web_search', label: 'supportsNativeWebSearch' },
  { key: 'supports_mid_conversation_system', label: 'supportsMidConversationSystem' },
] as const

type CapabilityKey = typeof capabilityFields[number]['key']
function capabilityValue(key: CapabilityKey): boolean {
  const model = draft.value.model
  if (!model) return false
  const [parent, child] = key.split('.')
  if (parent === 'input') return model.input[child as keyof DesktopAgentModel['input']]
  if (parent === 'output') return model.output.text
  return model[key as Exclude<CapabilityKey, `input.${string}` | 'output.text'>]
}
function setCapability(key: CapabilityKey, event: Event) {
  const model = draft.value.model
  if (!model || saving.value || loading.value) return
  const value = (event.target as HTMLInputElement).checked
  const [parent, child] = key.split('.')
  if (parent === 'input') model.input[child as keyof DesktopAgentModel['input']] = value
  else if (parent === 'output') model.output.text = value
  else model[key as Exclude<CapabilityKey, `input.${string}` | 'output.text'>] = value
}
function setReasoningLevel(level: DesktopAgentReasoningLevel, event: Event) {
  const model = draft.value.model
  if (!model || saving.value || loading.value) return
  const selected = new Set(model.reasoning_levels)
  if ((event.target as HTMLInputElement).checked) selected.add(level)
  else selected.delete(level)
  model.reasoning_levels = DESKTOP_AGENT_REASONING_LEVELS.filter(item => selected.has(item))
}
function configureModel() {
  if (saving.value || loading.value || draft.value.model) return
  if (!draft.value.base_url) draft.value.base_url = suggestedGateway
  draft.value.model = {
    id: '', display_name: '', context_window: 0, max_output_tokens: 0, default_max_output_tokens: 0,
    reasoning_levels: ['none'], default_reasoning: 'none', supports_reasoning: false,
    supports_streaming: true, supports_tool_call: true, requires_mfjs_tool_schema: false,
    input: { text: true, image: false, video: false, audio: false, pdf: false }, output: { text: true },
    supports_json_schema_output: false, supports_native_web_search: false, supports_mid_conversation_system: false,
  }
}
function applyConfig(config: DesktopAgentConfig) {
  draft.value = JSON.parse(JSON.stringify(config)) as DesktopAgentConfig
  submitted.value = false
}
async function load() {
  if (!props.active || loading.value || saving.value) return
  const generation = ++loadGeneration
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
async function persist(payload: DesktopAgentUpdateRequest, notice: string) {
  if (!loaded.value || saving.value || loading.value || !props.active) return
  const generation = ++saveGeneration
  saving.value = true
  errorKey.value = ''
  noticeKey.value = ''
  try {
    // 原因：二次验证会暂停后重试；本次payload独立于可编辑草稿，避免验证期间被改写。
    const result = await stepUp.run(async () => {
      if (!alive || !props.active || generation !== saveGeneration) throw new StepUpCancelledError()
      try {
        return await updateDesktopAgentConfig(payload)
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
    }
  }
}
function save() {
  if (!loaded.value || saving.value || loading.value || !props.active) return
  submitted.value = true
  noticeKey.value = ''
  if (validationCodes.value.length) return
  return persist(JSON.parse(JSON.stringify(draft.value)) as DesktopAgentConfig, 'admin.agentSettings.saved')
}
function disableOnly() {
  return persist({ enabled: false }, 'admin.agentSettings.disabledOnly')
}
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
  stepUp.onCancel()
})
</script>
