<template>
  <div class="user-area-switcher px-3 pt-3" :class="{ 'px-2': collapsed }">
    <Select
      v-if="collapsed"
      :model-value="area"
      :options="options"
      :aria-label="t('nav.switchArea', { area: t(area === 'workbench' ? 'nav.workbench' : 'nav.console') })"
      @update:model-value="selectArea"
    >
      <template #selected>
        <Icon :name="area === 'workbench' ? 'cube' : 'grid'" size="md" aria-hidden="true" />
      </template>
      <template #option="{ option }">
        <Icon :name="option.value === 'workbench' ? 'cube' : 'grid'" size="sm" aria-hidden="true" />
        <span>{{ option.label }}</span>
      </template>
    </Select>
    <nav v-else class="grid grid-cols-2 gap-1 rounded-xl bg-gray-100 p-1 dark:bg-dark-800" :aria-label="t('nav.switchAreaLabel')">
      <RouterLink
        v-for="option in options"
        :key="option.value"
        :to="option.value === 'workbench' ? workbenchTo : consoleTo"
        :aria-current="area === option.value ? 'true' : undefined"
        class="flex min-h-9 min-w-0 items-center justify-center gap-1.5 rounded-lg px-1 py-2 text-xs font-medium transition-colors"
        :class="area === option.value ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white' : 'text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-dark-200'"
        @click="emit('navigate', option.value === 'workbench' ? workbenchTo : consoleTo)"
      >
        <Icon :name="option.value === 'workbench' ? 'cube' : 'grid'" size="sm" aria-hidden="true" />
        <span class="truncate">{{ option.label }}</span>
      </RouterLink>
    </nav>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { WorkspaceArea } from '@/router/workspace'

const props = defineProps<{ area: WorkspaceArea; collapsed: boolean; consoleTo: string; workbenchTo: string }>()
const emit = defineEmits<{ navigate: [path: string] }>()
const { t } = useI18n()
const router = useRouter()
const options = computed(() => [
  { value: 'console', label: t('nav.console') },
  { value: 'workbench', label: t('nav.workbench') },
])

function selectArea(value: unknown) {
  if (value !== 'console' && value !== 'workbench') return
  const target = value === 'workbench' ? props.workbenchTo : props.consoleTo
  emit('navigate', target)
  void router.push(target)
}
</script>

<style scoped>
.user-area-switcher :deep(.select-trigger) {
  justify-content: center;
  min-width: 0;
  padding-left: 0;
  padding-right: 0;
}
.user-area-switcher :deep(.select-icon) { display: none; }
.user-area-switcher :deep(.select-value) { flex: none; }
</style>
