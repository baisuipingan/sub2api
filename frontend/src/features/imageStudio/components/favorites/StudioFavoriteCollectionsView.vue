<template>
  <div data-favorite-collections-root>
    <div class="mb-5 flex flex-wrap items-end justify-between gap-3">
      <div>
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">
          {{ t('imageStudio.favorites.title') }}
        </h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
          {{ t('imageStudio.favorites.favoriteCount', { count: favoriteTaskCount }) }}
        </p>
      </div>

      <form
        class="flex w-full max-w-xs items-center gap-2 sm:w-auto"
        @submit.prevent="handleCreate"
      >
        <input
          v-model="draft"
          :placeholder="t('imageStudio.favorites.newPlaceholder')"
          :aria-label="t('imageStudio.favorites.newPlaceholder')"
          class="min-w-0 flex-1 rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm text-gray-900 outline-none transition focus:border-primary-500 focus:ring-2 focus:ring-primary-500/15 dark:border-white/[0.1] dark:bg-white/[0.04] dark:text-white"
        />
        <button
          type="submit"
          class="rounded-lg bg-primary-600 p-2 text-white transition hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-40"
          :disabled="!draft.trim()"
          :aria-label="t('imageStudio.favorites.newPlaceholder')"
          :title="t('imageStudio.favorites.newPlaceholder')"
        >
          <StudioIcon name="PlusIcon" class="h-5 w-5" />
        </button>
      </form>
    </div>

    <div
      v-if="favoriteTaskCount === 0"
      class="mb-5 flex items-center gap-3 border-b border-gray-200 pb-4 text-sm text-gray-500 dark:border-white/[0.08] dark:text-dark-400"
    >
      <StudioIcon name="FavoriteIcon" class="h-5 w-5 shrink-0 text-amber-400" />
      <span>{{ t('imageStudio.favorites.emptyHint') }}</span>
    </div>

    <div class="grid grid-cols-1 gap-3 pb-4">
      <StudioCollectionCard
        v-for="card in cards"
        :key="card.id"
        :card="card"
        :is-virtual-all="card.id === ALL_FAVORITES_COLLECTION_ID"
        :is-default="card.id !== ALL_FAVORITES_COLLECTION_ID && card.id === state.defaultFavoriteCollectionId"
        :can-delete="state.favoriteCollections.length > 1"
        :is-editing="card.id !== ALL_FAVORITES_COLLECTION_ID && editingId === card.id"
        :editing-name="editingName"
        @open="emit('openCollection', card.id)"
        @update:editing-name="editingName = $event"
        @start-rename="card.collection && startRename(card.collection)"
        @confirm-rename="confirmRename"
        @cancel-rename="cancelRename"
        @toggle-default="toggleDefault(card)"
        @delete="card.collection && handleDelete(card.collection)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from '../StudioIcon.vue'
import StudioCollectionCard from './StudioCollectionCard.vue'
import type { FavoriteCollection } from '../../types'
import {
  storeState as state,
  createFavoriteCollection,
  deleteFavoriteCollection,
  renameFavoriteCollection,
} from '../../stores/store'
import {
  ALL_FAVORITES_COLLECTION_ID,
  DEFAULT_FAVORITE_COLLECTION_ID,
} from '../../lib/favoriteState'
import { getCollectionTasks, type CollectionCard } from './favoriteUtils'

const emit = defineEmits<{ openCollection: [collectionId: string] }>()
const { t } = useI18n()

const draft = ref('')
const editingId = ref<string | null>(null)
const editingName = ref('')

const favoriteTaskCount = computed(() => state.tasks.filter((task) => task.isFavorite).length)

const cards = computed<CollectionCard[]>(() => [
  {
    id: ALL_FAVORITES_COLLECTION_ID,
    name: t('imageStudio.favorites.all'),
    tasks: getCollectionTasks(
      ALL_FAVORITES_COLLECTION_ID,
      state.tasks,
      state.defaultFavoriteCollectionId,
    ),
  },
  ...state.favoriteCollections
    .filter((collection) => collection.id !== DEFAULT_FAVORITE_COLLECTION_ID)
    .map((collection) => ({
      id: collection.id,
      name: collection.name,
      collection,
      tasks: getCollectionTasks(
        collection.id,
        state.tasks,
        state.defaultFavoriteCollectionId,
      ),
    })),
])

function handleCreate() {
  const collection = createFavoriteCollection(draft.value)
  if (!collection) return
  draft.value = ''
}

function startRename(collection: FavoriteCollection) {
  editingId.value = collection.id
  editingName.value = collection.name
}

function confirmRename() {
  if (editingId.value && editingName.value.trim()) {
    renameFavoriteCollection(editingId.value, editingName.value)
  }
  editingId.value = null
  editingName.value = ''
}

function cancelRename() {
  editingId.value = null
  editingName.value = ''
}

function toggleDefault(card: CollectionCard) {
  if (!card.collection) return
  const isDefault = card.id === state.defaultFavoriteCollectionId
  state.setDefaultFavoriteCollectionId(isDefault ? null : card.id)
}

function handleDelete(collection: FavoriteCollection) {
  if (state.favoriteCollections.length <= 1) return
  state.setConfirmDialog({
    title: t('imageStudio.favorites.deleteTitle'),
    message: t('imageStudio.favorites.deleteMessage', { name: collection.name }),
    tone: 'danger',
    confirmText: t('imageStudio.favorites.deleteConfirm'),
    cancelText: t('imageStudio.common.cancel'),
    action: () => {
      void deleteFavoriteCollection(collection.id)
    },
  })
}
</script>
