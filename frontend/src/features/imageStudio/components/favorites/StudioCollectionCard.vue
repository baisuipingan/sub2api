<template>
  <article
    class="min-w-0 overflow-hidden rounded-lg border border-gray-200 bg-white transition hover:border-primary-300 dark:border-white/[0.08] dark:bg-dark-900"
  >
    <button type="button" class="block w-full text-left" @click="emit('open')">
      <div class="relative aspect-[16/9] overflow-hidden bg-gray-100 dark:bg-black/20">
        <StudioCollectionCover :task="coverTask" :is-virtual-all="isVirtualAll" />
        <span
          class="absolute bottom-2 left-2 rounded bg-black/55 px-2 py-1 text-xs text-white"
        >
          {{ t('imageStudio.resultPanel.taskCount', { count: card.tasks.length }) }}
        </span>
      </div>
    </button>

    <div class="p-3.5">
      <div class="flex min-w-0 items-center gap-2">
        <StudioIcon
          v-if="isVirtualAll"
          name="FavoriteIcon"
          class="h-4 w-4 shrink-0 fill-current text-amber-500"
        />
        <StudioIcon v-else name="ImageUploadIcon" class="h-4 w-4 shrink-0 text-gray-400" />

        <input
          v-if="isEditing"
          :value="editingName"
          :aria-label="t('imageStudio.favorites.nameAria')"
          class="min-w-0 flex-1 rounded border border-primary-400 bg-white px-1.5 py-0.5 text-sm text-gray-900 outline-none focus:border-primary-500 focus:ring-2 focus:ring-primary-500/15 dark:border-primary-400/60 dark:bg-dark-950 dark:text-white"
          autofocus
          @input="emit('update:editingName', ($event.target as HTMLInputElement).value)"
          @keydown.enter.prevent="emit('confirmRename')"
          @keydown.escape.prevent="emit('cancelRename')"
          @blur="emit('confirmRename')"
        />
        <button
          v-else
          type="button"
          class="min-w-0 truncate text-left text-sm font-semibold text-gray-900 hover:text-primary-600 dark:text-dark-100 dark:hover:text-primary-400"
          :title="card.name"
          @click="emit('open')"
        >
          {{ card.name }}
        </button>

        <span
          v-if="isDefault"
          class="shrink-0 rounded bg-amber-50 px-1.5 py-0.5 text-[11px] font-medium text-amber-600 dark:bg-amber-500/10 dark:text-amber-400"
        >
          {{ t('imageStudio.favorites.defaultBadge') }}
        </span>
      </div>

      <p class="mt-1.5 text-xs text-gray-500 dark:text-dark-400">
        {{ t('imageStudio.resultPanel.taskCount', { count: card.tasks.length }) }}
      </p>

      <div
        v-if="!isVirtualAll && card.collection"
        class="mt-3 flex items-center justify-end gap-1 border-t border-gray-100 pt-2 dark:border-white/[0.06]"
      >
        <button
          type="button"
          class="rounded-md p-1.5 transition hover:bg-amber-50 dark:hover:bg-amber-500/10"
          :class="isDefault ? 'text-amber-500' : 'text-gray-400 hover:text-amber-500'"
          :aria-label="isDefault ? t('imageStudio.favorites.unsetDefault') : t('imageStudio.favorites.setDefault')"
          :title="isDefault ? t('imageStudio.favorites.unsetDefault') : t('imageStudio.favorites.setDefault')"
          @click.stop="emit('toggleDefault')"
        >
          <StudioIcon
            name="FavoriteIcon"
            class="h-4 w-4"
            :class="isDefault ? 'fill-current' : ''"
          />
        </button>

        <button
          v-if="isEditing"
          type="button"
          class="rounded-md p-1.5 text-emerald-500 transition hover:bg-emerald-50 dark:text-emerald-400 dark:hover:bg-emerald-500/10"
          :aria-label="t('imageStudio.favorites.confirmRename')"
          :title="t('imageStudio.favorites.confirmRename')"
          @mousedown.prevent.stop="emit('confirmRename')"
        >
          <StudioIcon name="PlusIcon" class="h-4 w-4" />
        </button>
        <button
          v-else
          type="button"
          class="rounded-md p-1.5 text-gray-400 transition hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-white/[0.06] dark:hover:text-white"
          :aria-label="t('imageStudio.favorites.rename')"
          :title="t('imageStudio.favorites.rename')"
          @click.stop="emit('startRename')"
        >
          <StudioIcon name="EditIcon" class="h-4 w-4" />
        </button>

        <button
          type="button"
          class="rounded-md p-1.5 text-gray-400 transition hover:bg-red-50 hover:text-red-600 disabled:cursor-not-allowed disabled:opacity-30 dark:hover:bg-red-500/10 dark:hover:text-red-400"
          :disabled="!canDelete"
          :aria-label="canDelete ? t('imageStudio.favorites.delete') : t('imageStudio.favorites.keepOne')"
          :title="canDelete ? t('imageStudio.favorites.delete') : t('imageStudio.favorites.keepOne')"
          @click.stop="emit('delete')"
        >
          <StudioIcon name="TrashIcon" class="h-4 w-4" />
        </button>
      </div>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from '../StudioIcon.vue'
import StudioCollectionCover from './StudioCollectionCover.vue'
import { getLatestCoverTask, type CollectionCard } from './favoriteUtils'

const props = defineProps<{
  card: CollectionCard
  isVirtualAll: boolean
  isDefault: boolean
  canDelete: boolean
  isEditing: boolean
  editingName: string
}>()

const emit = defineEmits<{
  open: []
  'update:editingName': [value: string]
  startRename: []
  confirmRename: []
  cancelRename: []
  toggleDefault: []
  delete: []
}>()

const { t } = useI18n()

const coverTask = computed(() => getLatestCoverTask(props.card.tasks))
</script>
