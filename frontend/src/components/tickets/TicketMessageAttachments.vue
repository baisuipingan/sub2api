<template>
  <div v-if="attachments.length" class="flex flex-wrap gap-2 pt-1">
    <div v-for="(attachment, index) in attachments" :key="attachment.id" role="button" tabindex="0" class="overflow-hidden rounded-lg border border-gray-200 bg-white transition-colors hover:border-primary-400 focus-visible:ring-2 focus-visible:ring-primary-500 dark:border-dark-600 dark:bg-dark-800" :aria-label="t('tickets.attachments.view', { number: index + 1 })" @click="selected = index" @keydown.enter.self="selected = index" @keydown.space.self.prevent="selected = index">
      <TicketAttachmentImage :ticket-id="ticketId" :owner-id="ownerId" :attachment-id="attachment.id" variant="thumbnail" :admin="admin" :alt="t('tickets.attachments.screenshot', { number: index + 1 })" />
    </div>
    <BaseDialog :show="selected !== null" :title="t('tickets.attachments.screenshot', { number: (selected ?? 0) + 1 })" width="extra-wide" :z-index="70" @close="selected = null">
      <TicketAttachmentImage v-if="current" :key="current.id" :ticket-id="ticketId" :owner-id="ownerId" :attachment-id="current.id" variant="preview" :admin="admin" :alt="t('tickets.attachments.screenshot', { number: (selected ?? 0) + 1 })" />
      <template v-if="attachments.length > 1" #footer>
        <div class="flex w-full items-center justify-between gap-3">
          <button type="button" class="btn btn-secondary" :disabled="selected === 0" @click="selected = Math.max(0, (selected ?? 0) - 1)">{{ t('tickets.attachments.previous') }}</button>
          <span class="text-xs text-gray-500">{{ (selected ?? 0) + 1 }} / {{ attachments.length }}</span>
          <button type="button" class="btn btn-secondary" :disabled="selected === attachments.length - 1" @click="selected = Math.min(attachments.length - 1, (selected ?? 0) + 1)">{{ t('tickets.attachments.next') }}</button>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import TicketAttachmentImage from './TicketAttachmentImage.vue'
import type { TicketAttachment } from '@/api/tickets'

const props = withDefaults(defineProps<{ ticketId: number; ownerId: number; attachments?: TicketAttachment[]; admin?: boolean }>(), { attachments: () => [], admin: false })
const { t } = useI18n()
const selected = ref<number | null>(null)
const current = computed(() => selected.value === null ? undefined : props.attachments[selected.value])
</script>
