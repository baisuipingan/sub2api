<template>
  <div
    class="fixed inset-0 z-[100] flex items-center justify-center bg-black/35 p-4 backdrop-blur-sm"
    @click="emit('close')"
  >
    <div
      role="dialog"
      aria-modal="true"
      class="w-full max-w-md rounded-2xl border border-gray-200 bg-white p-5 shadow-2xl dark:border-white/[0.1] dark:bg-dark-900"
      @click.stop
    >
      <h2 class="text-lg font-semibold text-gray-950 dark:text-white">
        {{ t('imageStudio.favoritePicker.title') }}
      </h2>
      <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
        {{ t('imageStudio.favoritePicker.subtitle', { count: taskIds.length }) }}
      </p>

      <div class="mt-4 max-h-72 space-y-1 overflow-y-auto">
        <label
          v-for="collection in collections"
          :key="collection.id"
          class="flex cursor-pointer items-center justify-between gap-3 rounded-lg px-3 py-2.5 text-sm transition hover:bg-gray-50 dark:hover:bg-white/[0.06]"
        >
          <span class="flex min-w-0 items-center gap-2">
            <StudioIcon name="FavoriteIcon" class="h-4 w-4 shrink-0 text-amber-500" />
            <span class="truncate text-gray-800 dark:text-dark-200">{{ collection.name }}</span>
          </span>
          <StudioCheckbox :checked="selected.has(collection.id)" :label="''" @change="toggle(collection.id)" />
        </label>
      </div>

      <div class="mt-5 flex justify-end gap-2 border-t border-gray-100 pt-4 dark:border-white/[0.08]">
        <button
          type="button"
          class="rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-600 hover:bg-gray-50 dark:border-white/[0.1] dark:text-dark-300 dark:hover:bg-white/[0.06]"
          @click="emit('close')"
        >
          {{ t('imageStudio.common.cancel') }}
        </button>
        <button
          type="button"
          class="rounded-lg bg-primary-600 px-4 py-2 text-sm font-medium text-white hover:bg-primary-700 disabled:opacity-40"
          :disabled="saving"
          @click="apply"
        >
          {{ t('imageStudio.favoritePicker.apply') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from '../StudioIcon.vue'
import StudioCheckbox from '../StudioCheckbox.vue'
import { storeState as state } from '../../stores/store'
import { useCloseOnEscape } from '../../composables/useCloseOnEscape'
import { usePreventBackgroundScroll } from '../../composables/usePreventBackgroundScroll'
import {
  ALL_FAVORITES_COLLECTION_ID,
  DEFAULT_FAVORITE_COLLECTION_ID,
  getTaskFavoriteCollectionIds,
} from '../../lib/favoriteState'

const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()

const saving = ref(false)
const selected = ref(new Set<string>())

const taskIds = computed(() => state.favoritePickerTaskIds ?? [])

const collections = computed(() =>
  state.favoriteCollections.filter((c) => c.id !== DEFAULT_FAVORITE_COLLECTION_ID),
)

useCloseOnEscape(() => true, () => emit('close'))
usePreventBackgroundScroll(() => true)

/** 打开时按任务现有的收藏归属预勾选；多任务归属不一致时留空 */
watch(
  taskIds,
  (ids) => {
    if (!ids.length) {
      selected.value = new Set()
      return
    }
    const sets = ids.map((id) => {
      const task = state.tasks.find((item) => item.id === id)
      return task ? getTaskFavoriteCollectionIds(task, state.defaultFavoriteCollectionId) : []
    })
    const first = sets[0] ?? []
    const same = sets.every(
      (list) => list.length === first.length && list.every((id) => first.includes(id)),
    )
    selected.value = new Set(same ? first.filter((id) => id !== ALL_FAVORITES_COLLECTION_ID) : [])
  },
  { immediate: true },
)

function toggle(id: string) {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selected.value = next
}

async function apply() {
  saving.value = true
  try {
    await state.updateTaskFavoriteCollections(taskIds.value, Array.from(selected.value))
    emit('close')
  } finally {
    saving.value = false
  }
}
</script>
