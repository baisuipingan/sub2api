<template>
  <BaseDialog :show="Boolean(displayedAnnouncement)" :title="displayedAnnouncement?.title || t('announcements.systemTitle')" width="reading" :z-index="120" :close-on-escape="false" :show-close-button="preview" @close="handleDismiss">
    <template v-if="displayedAnnouncement">
      <time :datetime="displayedAnnouncement.created_at" class="mb-4 block text-xs text-gray-500 dark:text-dark-400">{{ formatRelativeWithDateTime(displayedAnnouncement.created_at) }}</time>
      <AnnouncementContent :content="displayedAnnouncement.content" />
      <p v-if="readFailed" role="alert" class="mt-3 text-sm text-red-600 dark:text-red-400">{{ t('announcements.readFailed') }}</p>
    </template>
    <template #footer>
      <button type="button" class="btn btn-primary" data-testid="announcement-popup-dismiss" :disabled="dismissing" @click="handleDismiss">
        <LoadingSpinner v-if="dismissing" size="sm" />{{ preview ? t('common.close') : t('announcements.markRead') }}
      </button>
    </template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAnnouncementStore } from '@/stores/announcements'
import { formatRelativeWithDateTime } from '@/utils/format'
import type { Announcement, UserAnnouncement } from '@/types'
import BaseDialog from './BaseDialog.vue'
import AnnouncementContent from './AnnouncementContent.vue'
import LoadingSpinner from './LoadingSpinner.vue'

type PreviewAnnouncement = Pick<Announcement | UserAnnouncement, 'title' | 'content' | 'created_at'>
const props = withDefaults(defineProps<{ announcement?: PreviewAnnouncement | null; preview?: boolean }>(), { announcement: null, preview: false })
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const announcementStore = useAnnouncementStore()
const displayedAnnouncement = computed(() => props.preview ? props.announcement : announcementStore.centerOpen ? null : announcementStore.currentPopup)
const dismissing = ref(false)
const readFailed = ref(false)
watch(displayedAnnouncement, () => { readFailed.value = false })
async function handleDismiss() {
  if (props.preview) { emit('close'); return }
  if (dismissing.value) return
  dismissing.value = true
  try { readFailed.value = await announcementStore.dismissPopup() === false }
  finally { dismissing.value = false }
}
</script>
