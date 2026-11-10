<template>
  <button type="button" class="relative flex h-9 w-9 items-center justify-center rounded-lg text-gray-600 transition-colors hover:bg-gray-100 focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-dark-300 dark:hover:bg-dark-800" :aria-label="t('announcements.title')" @click="openCenter">
    <Icon name="bell" size="md" />
    <span v-if="unreadCount" class="absolute right-1 top-1 h-2 w-2 rounded-full bg-red-500" :aria-label="t('announcements.newCount', { count: unreadCount })" />
  </button>

  <BaseDialog :show="centerOpen" :title="t('announcements.systemTitle')" width="reading" :z-index="100" @close="announcementStore.closeCenter()">
    <div class="sticky -top-3 z-10 -mx-1 bg-white pb-4 pt-1 sm:-top-4 dark:bg-dark-800">
      <p class="mb-4 text-xs text-gray-500 dark:text-dark-400">{{ t('announcements.centerDescription') }}</p>
      <div role="tablist" :aria-label="t('announcements.systemTitle')" class="flex rounded-full bg-gray-100 p-1 dark:bg-dark-900">
        <button id="announcements-notice-tab" type="button" role="tab" :aria-selected="centerTab === 'notice'" aria-controls="announcements-notice-panel" :tabindex="centerTab === 'notice' ? 0 : -1" class="flex min-w-0 flex-1 items-center justify-center gap-2 rounded-full px-3 py-2.5 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:ring-primary-500" :class="tabClass('notice')" @click="selectTab('notice')" @keydown="navigateTabs($event, 'notice')">
          <Icon name="bell" size="sm" />{{ t('announcements.noticeTab') }}
        </button>
        <button id="announcements-timeline-tab" type="button" role="tab" :aria-selected="centerTab === 'timeline'" aria-controls="announcements-timeline-panel" :tabindex="centerTab === 'timeline' ? 0 : -1" class="flex min-w-0 flex-1 items-center justify-center gap-2 rounded-full px-3 py-2.5 text-sm font-medium transition-colors focus-visible:ring-2 focus-visible:ring-primary-500" :class="tabClass('timeline')" @click="selectTab('timeline')" @keydown="navigateTabs($event, 'timeline')">
          <Icon name="clock" size="sm" />{{ t('announcements.timelineTab') }}
          <span v-if="unreadCount" class="rounded-full bg-red-50 px-1.5 py-0.5 text-[11px] leading-none text-red-600 dark:bg-red-500/15 dark:text-red-400">{{ unreadCount > 99 ? '99+' : unreadCount }}</span>
        </button>
      </div>
    </div>

    <section v-if="centerTab === 'notice'" id="announcements-notice-panel" role="tabpanel" aria-labelledby="announcements-notice-tab" class="min-h-32 space-y-4 py-1" :aria-busy="noticeLoading">
      <LoadingSpinner v-if="noticeLoading && !notices.length" class="mx-auto my-10 block" />
      <article v-for="item in notices" :key="item.id" class="rounded-2xl border border-gray-200 p-4 sm:p-5 dark:border-dark-600">
        <h4 class="mb-4 text-base font-semibold text-gray-900 dark:text-white">{{ item.title }}</h4>
        <AnnouncementContent :content="item.content" />
      </article>
      <p v-if="!noticeLoading && !noticeError && !notices.length" class="py-10 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('announcements.emptyNotices') }}</p>
      <div v-if="noticeError" role="alert" class="flex flex-wrap items-center justify-center gap-3 py-3 text-sm text-gray-500 dark:text-dark-400">
        {{ t('announcements.loadFailed') }}<button type="button" class="btn btn-secondary !py-1.5 text-xs" @click="announcementStore.loadCenterPage('notice', noticePage < noticePages)">{{ t('common.refresh') }}</button>
      </div>
      <button v-else-if="noticePage < noticePages" type="button" class="btn btn-secondary mx-auto !flex" :disabled="noticeLoading" @click="announcementStore.loadCenterPage('notice', true)"><LoadingSpinner v-if="noticeLoading" size="sm" />{{ t('announcements.loadMore') }}</button>
    </section>

    <section v-else id="announcements-timeline-panel" role="tabpanel" aria-labelledby="announcements-timeline-tab" class="min-h-32 py-1" :aria-busy="timelineLoading">
      <p class="mb-5 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('announcements.timelineHint') }}</p>
      <LoadingSpinner v-if="timelineLoading && !announcements.length" class="mx-auto my-10 block" />
      <ol v-if="announcements.length" class="ml-1 border-l border-gray-200 pl-5 dark:border-dark-600">
        <li v-for="item in announcements" :key="item.id" class="relative pb-6 last:pb-1">
          <span class="absolute -left-[25px] top-1 h-2 w-2 rounded-full ring-4 ring-white dark:ring-dark-800" :class="item.read_at ? 'bg-gray-300 dark:bg-dark-500' : 'bg-red-500'" />
          <time :datetime="item.created_at" class="mb-2 block text-xs tabular-nums text-gray-500 dark:text-dark-400">{{ formatDateTime(item.created_at) }}</time>
          <article class="overflow-hidden rounded-2xl border border-gray-200 bg-gray-50/40 dark:border-dark-600 dark:bg-dark-900/20">
            <button type="button" class="flex w-full items-center gap-2 px-4 py-4 text-left focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500" :aria-expanded="expandedIds.has(item.id)" :aria-controls="`announcement-content-${item.id}`" @click="toggle(item.id)">
              <span class="min-w-0 flex-1 break-words text-sm font-semibold text-gray-900 dark:text-white">{{ item.title }}</span>
              <span v-if="!item.read_at" class="shrink-0 rounded-full bg-red-50 px-1.5 py-0.5 text-[11px] text-red-600 dark:bg-red-500/15 dark:text-red-400">{{ t('announcements.unread') }}</span>
              <Icon name="chevronDown" size="sm" class="shrink-0 text-gray-400 transition-transform" :class="{ 'rotate-180': expandedIds.has(item.id) }" />
            </button>
            <div v-if="expandedIds.has(item.id)" :id="`announcement-content-${item.id}`" class="space-y-4 border-t border-gray-100 p-4 dark:border-dark-700">
              <AnnouncementContent :content="item.content" />
              <button v-if="!item.read_at" type="button" class="btn btn-secondary !py-1.5 text-xs" :disabled="readingIds.includes(item.id) || markingAll" @click="markAsRead(item.id)"><LoadingSpinner v-if="readingIds.includes(item.id)" size="sm" />{{ t('announcements.markRead') }}</button>
            </div>
          </article>
        </li>
      </ol>
      <p v-if="!timelineLoading && !timelineError && !announcements.length" class="py-10 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('announcements.emptyTimeline') }}</p>
      <div v-if="timelineError" role="alert" class="flex flex-wrap items-center justify-center gap-3 py-3 text-sm text-gray-500 dark:text-dark-400">
        {{ t('announcements.loadFailed') }}<button type="button" class="btn btn-secondary !py-1.5 text-xs" @click="announcementStore.loadCenterPage('timeline', timelinePage < timelinePages)">{{ t('common.refresh') }}</button>
      </div>
      <button v-else-if="timelinePage < timelinePages" type="button" class="btn btn-secondary mx-auto mt-4 !flex" :disabled="timelineLoading" @click="announcementStore.loadCenterPage('timeline', true)"><LoadingSpinner v-if="timelineLoading" size="sm" />{{ t('announcements.loadMore') }}</button>
    </section>
    <template #footer>
      <div class="flex w-full items-center justify-between gap-3">
        <button v-if="centerTab === 'timeline'" type="button" class="rounded-lg px-2 py-2 text-sm text-gray-500 transition-colors hover:text-primary-600 disabled:cursor-default disabled:opacity-50 dark:text-dark-400" :disabled="!unreadCount || markingAll || readingIds.length > 0 || !snapshot" @click="markAllAsRead"><LoadingSpinner v-if="markingAll" size="sm" class="mr-1 align-middle" />{{ t('announcements.markAllRead') }}</button>
        <span v-else />
        <button type="button" class="btn btn-primary" @click="announcementStore.closeCenter()">{{ t('common.close') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import BaseDialog from './BaseDialog.vue'
import LoadingSpinner from './LoadingSpinner.vue'
import AnnouncementContent from './AnnouncementContent.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useAnnouncementStore } from '@/stores/announcements'
import type { AnnouncementCenterTab } from '@/api/announcements'

const { t, locale } = useI18n()
const appStore = useAppStore()
const announcementStore = useAnnouncementStore()
const { announcements, notices, centerOpen, centerTab, unreadCount, noticeLoading, timelineLoading, noticeError, timelineError, noticePage, timelinePage, noticePages, timelinePages, markingAll, readingIds, snapshot } = storeToRefs(announcementStore)
const expandedIds = ref(new Set<number>())
const dateFormatter = computed(() => new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }))
function formatDateTime(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) ? value : dateFormatter.value.format(date) }
function tabClass(tab: AnnouncementCenterTab) { return centerTab.value === tab ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white' : 'text-gray-500 dark:text-dark-400' }
function toggle(id: number) { const ids = new Set(expandedIds.value); if (ids.has(id)) ids.delete(id); else ids.add(id); expandedIds.value = ids }
async function openCenter() { await announcementStore.openCenter() }
async function selectTab(tab: AnnouncementCenterTab) { await announcementStore.selectCenterTab(tab) }
function navigateTabs(event: KeyboardEvent, tab: AnnouncementCenterTab) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const next = event.key === 'Home' ? 'notice' : event.key === 'End' ? 'timeline' : tab === 'notice' ? 'timeline' : 'notice'
  void selectTab(next)
  void nextTick(() => document.getElementById(`announcements-${next}-tab`)?.focus())
}
async function markAsRead(id: number) { if (!await announcementStore.markAsRead(id)) appStore.showError(t('announcements.readFailed')) }
async function markAllAsRead() {
  try { await announcementStore.markAllAsRead() }
  catch (error) {
    const reason = (error as { response?: { data?: { reason?: string } } })?.response?.data?.reason
    appStore.showError(t(reason === 'ANNOUNCEMENT_SNAPSHOT_INVALID' ? 'announcements.snapshotChanged' : 'announcements.readFailed'))
  }
}
watch(centerOpen, opened => { if (opened) expandedIds.value = new Set() })
</script>
