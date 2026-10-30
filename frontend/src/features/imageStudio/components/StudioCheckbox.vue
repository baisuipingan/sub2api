<template>
  <label class="group flex cursor-pointer items-center gap-2.5">
    <span class="relative flex items-center justify-center">
      <input
        type="checkbox"
        :checked="checked"
        class="peer h-4 w-4 cursor-pointer appearance-none rounded-[4px] border bg-white transition-all focus:outline-none focus:ring-2 focus:ring-offset-1 focus:ring-offset-white dark:bg-white/5 dark:focus:ring-offset-dark-900"
        :class="toneClasses"
        @change="onChange(($event.target as HTMLInputElement).checked)"
      />
      <svg
        class="pointer-events-none absolute h-2.5 w-2.5 scale-50 text-white opacity-0 transition-all duration-200 peer-checked:scale-100 peer-checked:opacity-100"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2.5"
      >
        <path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.5l3.5 3.5L19 6.5" />
      </svg>
    </span>
    <span
      v-if="label"
      class="select-none text-[13px] font-medium text-gray-700 transition-colors group-hover:text-gray-900 dark:text-dark-200 dark:group-hover:text-dark-100"
    >
      {{ label }}
    </span>
  </label>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    checked: boolean
    label?: string
    tone?: 'primary' | 'danger'
  }>(),
  { tone: 'primary', label: undefined },
)

const emit = defineEmits<{ change: [checked: boolean] }>()

const toneClasses = computed(() =>
  props.tone === 'danger'
    ? 'border-red-300/80 hover:border-red-400/80 checked:border-transparent checked:bg-red-500/85 focus:ring-red-500/20 dark:border-red-500/30 dark:hover:border-red-500/50 dark:checked:border-transparent dark:checked:bg-red-500/60'
    : 'border-gray-300 hover:border-gray-400 checked:border-primary-500 checked:bg-primary-500 focus:ring-primary-500/20 dark:border-white/15 dark:hover:border-white/30 dark:checked:border-transparent dark:checked:bg-primary-500',
)

function onChange(next: boolean) {
  emit('change', next)
}
</script>
