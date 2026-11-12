<template>
  <article class="card min-w-0" data-test="agent-model-card" :data-model-id="modelValue.id">
    <div class="flex flex-wrap items-start justify-between gap-3 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <div class="min-w-0 space-y-2">
        <h3 class="break-all font-mono font-semibold text-gray-900 dark:text-white">{{ modelValue.id }}</h3>
        <label class="flex items-center gap-2 text-sm"><input data-test="agent-model-default" type="radio" name="agent-default-model" :checked="isDefault" :disabled="disabled" @change="$emit('make-default')" />{{ t('admin.agentSettings.defaultModel') }}</label>
      </div>
      <button type="button" data-test="agent-model-remove" class="btn btn-secondary btn-sm text-red-600 dark:text-red-400" :disabled="disabled" @click="$emit('remove')">{{ t('admin.agentSettings.removeModel') }}</button>
    </div>
    <fieldset :disabled="disabled" class="min-w-0 space-y-5 p-6">
      <p v-if="unavailable" data-test="agent-model-unavailable" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">{{ t('admin.agentSettings.modelUnavailable', { id: modelValue.id }) }}</p>
      <div>
        <label :for="`agent-model-name-${index}`" class="input-label">{{ t('admin.agentSettings.displayName') }}</label>
        <input :id="`agent-model-name-${index}`" :value="modelValue.display_name" data-test="agent-model-name" class="input" maxlength="128" @input="update('display_name', inputValue($event))" />
      </div>
      <div class="grid min-w-0 gap-4 md:grid-cols-3">
        <div><label :for="`agent-context-${index}`" class="input-label">{{ t('admin.agentSettings.contextWindow') }}</label><input :id="`agent-context-${index}`" :value="modelValue.context_window" data-test="agent-context" type="number" min="1" max="100000000" step="1" class="input" @input="update('context_window', Number(inputValue($event)))" /></div>
        <div><label :for="`agent-max-output-${index}`" class="input-label">{{ t('admin.agentSettings.maxOutputTokens') }}</label><input :id="`agent-max-output-${index}`" :value="modelValue.max_output_tokens" data-test="agent-max-output" type="number" min="1" step="1" class="input" @input="update('max_output_tokens', Number(inputValue($event)))" /></div>
        <div><label :for="`agent-default-output-${index}`" class="input-label">{{ t('admin.agentSettings.defaultMaxOutputTokens') }}</label><input :id="`agent-default-output-${index}`" :value="modelValue.default_max_output_tokens" data-test="agent-default-output" type="number" min="1" step="1" class="input" @input="update('default_max_output_tokens', Number(inputValue($event)))" /></div>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.budgetHint') }}</p>
      <details class="rounded-xl border border-gray-200 dark:border-dark-700">
        <summary class="cursor-pointer px-4 py-3 font-medium">{{ t('admin.agentSettings.advanced') }}</summary>
        <div class="space-y-5 border-t border-gray-100 p-4 dark:border-dark-700">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.capabilitiesHint') }}</p>
          <div class="grid gap-3 sm:grid-cols-2">
            <label v-for="item in capabilityFields" :key="item.key" class="flex items-start gap-2 text-sm"><input :checked="capabilityValue(item.key)" :data-test="`agent-cap-${item.key}`" type="checkbox" class="mt-0.5 rounded" @change="setCapability(item.key, $event)" />{{ t(`admin.agentSettings.capabilities.${item.label}`) }}</label>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.requiredCapabilitiesHint') }}</p>
          <fieldset class="space-y-3">
            <legend class="input-label">{{ t('admin.agentSettings.reasoningLevels') }}</legend>
            <div class="flex flex-wrap gap-x-5 gap-y-3"><label v-for="level in DESKTOP_AGENT_REASONING_LEVELS" :key="level" class="flex items-center gap-2 text-sm"><input :checked="modelValue.reasoning_levels.includes(level)" :data-test="`agent-reasoning-${level}`" type="checkbox" :value="level" @change="setReasoningLevel(level, $event)" />{{ level }}</label></div>
          </fieldset>
          <div>
            <label :for="`agent-default-reasoning-${index}`" class="input-label">{{ t('admin.agentSettings.defaultReasoning') }}</label>
            <Select :id="`agent-default-reasoning-${index}`" :model-value="modelValue.default_reasoning" data-test="agent-default-reasoning" :options="reasoningOptions" :aria-label="t('admin.agentSettings.defaultReasoning')" :disabled="disabled" @update:model-value="setDefaultReasoning" />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.agentSettings.reasoningHint') }}</p>
          </div>
        </div>
      </details>
    </fieldset>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { DESKTOP_AGENT_REASONING_LEVELS } from '@/api/admin/desktopAgent'
import type { DesktopAgentModel, DesktopAgentReasoningLevel } from '@/api/admin/desktopAgent'
import Select from '@/components/common/Select.vue'

const props = defineProps<{ modelValue: DesktopAgentModel; index: number; isDefault: boolean; unavailable: boolean; disabled: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: DesktopAgentModel]; remove: []; 'make-default': [] }>()
const { t } = useI18n()
const reasoningOptions = computed(() => DESKTOP_AGENT_REASONING_LEVELS.map(level => ({ value: level, label: level, disabled: !props.modelValue.reasoning_levels.includes(level) })))
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
  if (!props.disabled) emit('update:modelValue', { ...props.modelValue, [key]: value })
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
