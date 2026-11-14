<template>
  <article class="min-w-0 p-4 sm:p-6" data-test="agent-model-card" :data-model-id="modelValue.id">
    <fieldset :disabled="disabled" class="min-w-0 space-y-4">
      <div class="grid min-w-0 gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(10rem,0.55fr)_minmax(10rem,0.6fr)_auto] lg:items-end">
        <div class="min-w-0 space-y-2">
          <h3 class="break-all font-mono font-semibold text-gray-900 dark:text-white">{{ modelValue.id }}</h3>
          <p data-test="agent-capability-summary" class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ capabilitySummary }}</p>
          <label class="flex items-center gap-2 text-sm"><input data-test="agent-model-default" type="radio" name="agent-default-model" :checked="isDefault" :disabled="disabled" @change="$emit('make-default')" />{{ t('admin.agentSettings.defaultModel') }}</label>
        </div>
        <div>
          <label :for="`agent-default-reasoning-${index}`" class="input-label">{{ t('admin.agentSettings.defaultReasoning') }}</label>
          <Select :id="`agent-default-reasoning-${index}`" :model-value="modelValue.default_reasoning" data-test="agent-default-reasoning" :options="reasoningOptions" :aria-label="t('admin.agentSettings.defaultReasoning')" :disabled="disabled || !profile" @update:model-value="setDefaultReasoning" />
        </div>
        <div>
          <label :for="`agent-default-output-${index}`" class="input-label">{{ t('admin.agentSettings.defaultMaxOutputTokens') }}</label>
          <input :id="`agent-default-output-${index}`" :value="modelValue.default_max_output_tokens" data-test="agent-default-output" type="number" min="1" :max="desktopAgentOutputBudgetCeiling(modelValue) || undefined" step="1" class="input" :disabled="disabled || !profile" @input="update('default_max_output_tokens', Number(inputValue($event)))" />
        </div>
        <button type="button" data-test="agent-model-remove" class="btn btn-secondary btn-sm justify-self-start text-red-600 dark:text-red-400 lg:mb-1" :disabled="disabled" @click="$emit('remove')">{{ t('admin.agentSettings.removeModel') }}</button>
      </div>
      <p v-if="unavailable" data-test="agent-model-unavailable" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">{{ t('admin.agentSettings.modelUnavailable', { id: modelValue.id }) }}</p>
      <div class="space-y-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-800/60">
        <div>
          <label :for="`agent-profile-${index}`" class="input-label">{{ t('admin.agentSettings.profile') }}</label>
          <Select :id="`agent-profile-${index}`" :model-value="profile?.id || null" data-test="agent-profile" :options="profileOptions" :placeholder="t('admin.agentSettings.profilePlaceholder')" :aria-label="t('admin.agentSettings.profile')" :disabled="disabled || !profilesReady || (!!officialProfile && !!profile)" @update:model-value="chooseProfile" />
        </div>
        <p v-if="!profile && profilesReady" data-test="agent-profile-required" class="text-sm text-amber-700 dark:text-amber-300">{{ t('admin.agentSettings.profileRequired') }}</p>
        <template v-if="profile">
          <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.profileSource') }} <a :href="profile.source_url" target="_blank" rel="noopener noreferrer" class="text-primary-600 underline dark:text-primary-400">{{ t('admin.agentSettings.officialDocumentation') }}</a> · {{ t('admin.agentSettings.profileReviewed', { date: profile.reviewed_at, revision: profile.revision }) }}</p>
          <dl data-test="agent-profile-budgets" class="grid min-w-0 gap-3 rounded-lg border border-gray-200 bg-white p-3 text-sm sm:grid-cols-3 dark:border-dark-700 dark:bg-dark-900">
            <div class="min-w-0"><dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.officialContext') }}</dt><dd class="mt-1 break-words font-medium">{{ limitText(profile.official_context_window) }}</dd></div>
            <div class="min-w-0"><dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.officialInput') }}</dt><dd class="mt-1 break-words font-medium">{{ limitText(profile.max_input_tokens) }}</dd></div>
            <div class="min-w-0"><dt class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.officialOutput') }}</dt><dd class="mt-1 break-words font-medium">{{ officialOutputText }}</dd></div>
          </dl>
          <ul class="list-inside list-disc space-y-1 text-xs leading-5 text-gray-500 dark:text-gray-400"><li v-for="note in profileNotes" :key="note" :data-test="`agent-profile-note-${note}`">{{ t(`admin.agentSettings.profileNotes.${note}`, { output: profile.model.max_output_tokens.toLocaleString() }) }}</li></ul>
        </template>
      </div>
      <details class="rounded-xl border border-gray-200 dark:border-dark-700">
        <summary class="cursor-pointer px-4 py-3 text-sm font-medium">{{ t('admin.agentSettings.advanced') }}</summary>
        <div class="space-y-5 border-t border-gray-100 p-4 dark:border-dark-700">
          <div>
            <label :for="`agent-model-name-${index}`" class="input-label">{{ t('admin.agentSettings.displayName') }}</label>
            <input :id="`agent-model-name-${index}`" :value="modelValue.display_name" data-test="agent-model-name" class="input" maxlength="128" @input="update('display_name', inputValue($event))" />
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <label class="flex items-center gap-2 text-sm"><input data-test="agent-profile-override" type="checkbox" :checked="binding?.override === true" :disabled="disabled || !profilesReady || !profile" @change="setOverride" />{{ t('admin.agentSettings.enableOverride') }}</label>
            <button type="button" data-test="agent-restore-profile" class="btn btn-secondary btn-sm" :disabled="disabled || !profilesReady || !profile" @click="$emit('restore-profile')">{{ t('admin.agentSettings.restoreProfile') }}</button>
          </div>
          <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.overrideHint') }}</p>
          <fieldset :disabled="!canOverride" class="min-w-0 space-y-5 disabled:opacity-70">
          <div class="grid min-w-0 gap-4 md:grid-cols-2">
            <div><label :for="`agent-context-${index}`" class="input-label">{{ t(inputOnlyProfile ? 'admin.agentSettings.maxInputTokens' : 'admin.agentSettings.contextWindow') }}</label><input :id="`agent-context-${index}`" :value="modelValue.context_window" data-test="agent-context" type="number" min="1" max="100000000" step="1" class="input" @input="update('context_window', Number(inputValue($event)))" /></div>
            <div><label :for="`agent-max-output-${index}`" class="input-label">{{ t('admin.agentSettings.maxOutputTokens') }}</label><input v-if="outputLimitKnown || canOverride" :id="`agent-max-output-${index}`" :value="outputLimitKnown ? modelValue.max_output_tokens : ''" data-test="agent-max-output" type="number" min="1" step="1" class="input" :placeholder="t('admin.agentSettings.customOutputPlaceholder')" @input="setMaxOutput" /><p v-else :id="`agent-max-output-${index}`" data-test="agent-output-limit-unknown" class="input bg-gray-50 dark:bg-dark-800">{{ modelOutputText }}</p></div>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.budgetHint') }}</p>
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.capabilitiesHint') }}</p>
          <div class="grid gap-3 sm:grid-cols-2">
            <label v-for="item in capabilityFields" :key="item.key" class="flex items-start gap-2 text-sm"><input :checked="capabilityValue(item.key)" :data-test="`agent-cap-${item.key}`" type="checkbox" class="mt-0.5 rounded" @change="setCapability(item.key, $event)" />{{ t(`admin.agentSettings.capabilities.${item.label}`) }}</label>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.requiredCapabilitiesHint') }}</p>
          <fieldset class="space-y-3">
            <legend class="input-label">{{ t('admin.agentSettings.reasoningLevels') }}</legend>
            <div class="flex flex-wrap gap-x-5 gap-y-3"><label v-for="level in DESKTOP_AGENT_REASONING_LEVELS" :key="level" class="flex items-center gap-2 text-sm"><input :checked="modelValue.reasoning_levels.includes(level)" :data-test="`agent-reasoning-${level}`" type="checkbox" :value="level" @change="setReasoningLevel(level, $event)" />{{ reasoningLabel(level) }}</label></div>
          </fieldset>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.reasoningHint') }}</p>
          </fieldset>
        </div>
      </details>
    </fieldset>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { DESKTOP_AGENT_REASONING_LEVELS } from '@/api/admin/desktopAgent'
import type { DesktopAgentModel, DesktopAgentModelProfileBinding, DesktopAgentProfile, DesktopAgentReasoningLevel } from '@/api/admin/desktopAgent'
import { desktopAgentOutputBudgetCeiling, hasDesktopAgentOutputLimit, resolveDesktopAgentProfile } from '@/utils/desktopAgentConfig'
import Select from '@/components/common/Select.vue'

const props = defineProps<{ modelValue: DesktopAgentModel; profiles: DesktopAgentProfile[]; binding?: DesktopAgentModelProfileBinding; profilesReady: boolean; index: number; isDefault: boolean; unavailable: boolean; disabled: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: DesktopAgentModel]; 'profile-change': [id: string]; 'override-change': [enabled: boolean]; 'restore-profile': []; remove: []; 'make-default': [] }>()
const { t } = useI18n()
const profile = computed(() => resolveDesktopAgentProfile(props.profiles, props.modelValue.id, props.binding))
const officialProfile = computed(() => resolveDesktopAgentProfile(props.profiles, props.modelValue.id))
const profileNotes = computed(() => profile.value?.notes.filter(note => [
  'native_web_search_unavailable', 'reasoning_none_unavailable', 'reasoning_auto_only',
  'responses_compatibility_required', 'structured_output_compatibility', 'grok_xhigh_compatibility',
  'official_shared_context', 'official_input_limit', 'context_limit_unpublished',
  'output_limit_unpublished', 'output_limit_unlimited', 'long_decode_required',
].includes(note)) || [])
const canOverride = computed(() => !props.disabled && props.profilesReady && !!profile.value && props.binding?.override === true)
const profileOptions = computed(() => props.profiles.filter(profile => !officialProfile.value || profile.id === officialProfile.value.id).map(profile => ({ value: profile.id, label: profile.display_name })))
const inputOnlyProfile = computed(() => profile.value?.official_context_window === null && !!profile.value.max_input_tokens)
const outputLimitKnown = computed(() => hasDesktopAgentOutputLimit(props.modelValue))
function limitText(value: number | null | undefined) { return typeof value === 'number' && value > 0 ? value.toLocaleString() : t('admin.agentSettings.limitUnpublished') }
function outputText(model: DesktopAgentModel) {
  return model.output_limit_kind === 'unlimited' ? t('admin.agentSettings.limitUnlimited') : model.output_limit_kind === 'not_published' ? t('admin.agentSettings.limitUnpublished') : limitText(model.max_output_tokens)
}
const modelOutputText = computed(() => outputText(props.modelValue))
const officialOutputText = computed(() => {
  if (!profile.value) return t('admin.agentSettings.limitUnpublished')
  return profile.value.official_max_output_tokens === undefined ? outputText(profile.value.model) : profile.value.model.output_limit_kind === 'unlimited' ? t('admin.agentSettings.limitUnlimited') : limitText(profile.value.official_max_output_tokens)
})
const capabilitySummary = computed(() => !profile.value ? t('admin.agentSettings.unverifiedSummary') : [
  t(inputOnlyProfile.value ? 'admin.agentSettings.inputSummary' : 'admin.agentSettings.contextSummary', { count: props.modelValue.context_window.toLocaleString() }),
  t('admin.agentSettings.outputSummary', { count: modelOutputText.value }),
  ...(props.modelValue.supports_tool_call ? [t('admin.agentSettings.summaryTools')] : []),
  ...(props.modelValue.input.image ? [t('admin.agentSettings.summaryImage')] : []),
  ...(props.modelValue.supports_streaming ? [t('admin.agentSettings.summaryStreaming')] : []),
].join(' · '))
function chooseProfile(value: string | number | boolean | null) {
  if (!props.disabled && props.profilesReady && typeof value === 'string' && profileOptions.value.some(item => item.value === value)) emit('profile-change', value)
}
function setOverride(event: Event) {
  if (!props.disabled && props.profilesReady && profile.value) emit('override-change', (event.target as HTMLInputElement).checked)
}
function reasoningLabel(level: DesktopAgentReasoningLevel) { return level === 'none' ? t('admin.agentSettings.reasoningNone') : level }
const reasoningOptions = computed(() => DESKTOP_AGENT_REASONING_LEVELS.map(level => ({ value: level, label: reasoningLabel(level), disabled: !props.modelValue.reasoning_levels.includes(level) })))
const capabilityFields = [
  { key: 'input.text', label: 'inputText' }, { key: 'input.image', label: 'inputImage' }, { key: 'input.video', label: 'inputVideo' },
  { key: 'input.audio', label: 'inputAudio' }, { key: 'input.pdf', label: 'inputPdf' }, { key: 'output.text', label: 'outputText' },
  { key: 'supports_streaming', label: 'supportsStreaming' }, { key: 'supports_tool_call', label: 'supportsToolCall' },
  { key: 'supports_reasoning', label: 'supportsReasoning' }, { key: 'requires_mfjs_tool_schema', label: 'requiresMfjsToolSchema' },
  { key: 'supports_json_schema_output', label: 'supportsJsonSchemaOutput' }, { key: 'supports_native_web_search', label: 'supportsNativeWebSearch' },
  { key: 'supports_mid_conversation_system', label: 'supportsMidConversationSystem' },
] as const

type CapabilityKey = typeof capabilityFields[number]['key']
function inputValue(event: Event) { return (event.target as HTMLInputElement).value }
function update<K extends keyof DesktopAgentModel>(key: K, value: DesktopAgentModel[K]) {
  const operational = key === 'display_name' || key === 'default_reasoning' || key === 'default_max_output_tokens'
  if (!props.disabled && (operational || canOverride.value)) emit('update:modelValue', { ...props.modelValue, [key]: value })
}
function setMaxOutput(event: Event) {
  if (!canOverride.value) return
  const next = { ...props.modelValue, max_output_tokens: Number(inputValue(event)) }
  delete next.output_limit_kind
  emit('update:modelValue', next)
}
function capabilityValue(key: CapabilityKey): boolean {
  const model = props.modelValue
  const [parent, child] = key.split('.')
  if (parent === 'input') return model.input[child as keyof DesktopAgentModel['input']]
  if (parent === 'output') return model.output.text
  return model[key as Exclude<CapabilityKey, `input.${string}` | 'output.text'>]
}
function setCapability(key: CapabilityKey, event: Event) {
  const value = (event.target as HTMLInputElement).checked
  const [parent, child] = key.split('.')
  if (parent === 'input') update('input', { ...props.modelValue.input, [child]: value })
  else if (parent === 'output') update('output', { text: value })
  else update(key as Exclude<CapabilityKey, `input.${string}` | 'output.text'>, value)
}
function setReasoningLevel(level: DesktopAgentReasoningLevel, event: Event) {
  const selected = new Set(props.modelValue.reasoning_levels)
  if ((event.target as HTMLInputElement).checked) selected.add(level)
  else selected.delete(level)
  update('reasoning_levels', DESKTOP_AGENT_REASONING_LEVELS.filter(item => selected.has(item)))
}
function setDefaultReasoning(value: string | number | boolean | null) {
  if (typeof value === 'string' && DESKTOP_AGENT_REASONING_LEVELS.includes(value as DesktopAgentReasoningLevel)) update('default_reasoning', value as DesktopAgentReasoningLevel)
}
</script>
