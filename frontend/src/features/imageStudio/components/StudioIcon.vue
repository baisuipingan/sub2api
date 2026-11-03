<template>
  <svg
    fill="none"
    stroke="currentColor"
    viewBox="0 0 24 24"
    aria-hidden="true"
    v-html="paths[name]"
  />
</template>

<script setup lang="ts">
/**
 * 生图工作台图标集。
 *
 * 原实现是 17 个独立的 React 函数组件；迁到 Vue 后合并为单个组件 +
 * 路径数据表，减少文件数量，也便于统一维护。
 *
 * 用法：<StudioIcon name="CopyIcon" /> ，样式通过 class 传入。
 */
defineProps<{
  name: keyof typeof paths
}>()

const paths = {
  CopyIcon: `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />`,
  TrashIcon: `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />`,
  PlusIcon: `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />`,
  CloseIcon: `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />`,
  ChevronLeftIcon: `<polyline points="15 18 9 12 15 6"></polyline>`,
  ChevronRightIcon: `<polyline points="9 18 15 12 9 6"></polyline>`,
  ExportIcon: `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />`,
  ImportIcon: `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />`,
  ImageUploadIcon: `<rect x="3" y="4" width="18" height="16" rx="2.5" stroke-width="1.8" /> <circle cx="8.5" cy="9" r="1.5" stroke-width="1.8" /> <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="m5.5 17 4.25-4.25 2.75 2.75 2.25-2.25L18.5 17M16.5 4v5m-2-2 2 2 2-2" />`,
  DownloadIcon: `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" />`,
  EditIcon: `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />`,
  RefreshIcon: `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />`,
  DragHandleIcon: `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 8h16M4 16h16" />`,
  SettingsIcon: `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" /> <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />`,
  CreditIcon: `<circle cx="12" cy="12" r="9" stroke-width="2" /> <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 9.5c0-1.1 1.343-2 3-2s3 .9 3 2-1.343 2-3 2-3 .9-3 2 1.343 2 3 2 3-.9 3-2M12 5.5v2m0 8v2" />`,
  SearchIcon: `<circle cx="11" cy="11" r="7" stroke-width="2" /> <path stroke-linecap="round" stroke-width="2" d="m20 20-3.5-3.5" />`,
  ClockIcon: `<circle cx="12" cy="12" r="9" stroke-width="2" /> <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 7v5l3 2" />`,
}
</script>
