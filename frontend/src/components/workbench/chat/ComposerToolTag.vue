<template>
  <button
    type="button"
    class="bs-composer-tool-tag"
    :class="{ 'is-active': active }"
    :aria-label="active ? `关闭${label}` : label"
    :aria-pressed="active"
    :aria-disabled="unavailable || undefined"
    :aria-describedby="descriptionId"
    :title="hint || (active ? `关闭${label}` : `开启${label}`)"
    :disabled="disabled"
    @mousedown.prevent
    @click="emit('toggle')"
  >
    <Icon :name="icon" size="sm" aria-hidden="true" />
    <span>{{ label }}</span>
    <Icon v-if="active" class="bs-composer-tool-remove" name="x" size="xs" aria-hidden="true" />
  </button>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'

defineProps<{
  label: string
  icon: 'globe' | 'clipboard'
  active: boolean
  disabled?: boolean
  unavailable?: boolean
  hint?: string
  descriptionId?: string
}>()
const emit = defineEmits<{ toggle: [] }>()
</script>
