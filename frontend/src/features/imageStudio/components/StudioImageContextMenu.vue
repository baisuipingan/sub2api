<template>
  <div
    v-if="menuInfo"
    ref="menuRef"
    role="menu"
    :aria-label="t('imageStudio.contextMenu.aria')"
    class="animate-fade-in fixed z-[9999] w-40 overflow-hidden rounded-lg border border-gray-100 bg-white py-1 shadow-xl dark:border-dark-700 dark:bg-dark-800"
    :style="{ left: `${left}px`, top: `${top}px` }"
    @contextmenu.prevent.stop
  >
    <button
      v-if="menuInfo.src && menuInfo.canCopyImage"
      type="button"
      role="menuitem"
      class="btn btn-sm btn-ghost flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-gray-700 transition-colors hover:bg-gray-50 dark:text-dark-200 dark:hover:bg-dark-700/50"
      @click="handleCopy"
    >
      <StudioIcon name="CopyIcon" class="h-4 w-4 flex-shrink-0" />{{ t('imageStudio.contextMenu.copy') }}
    </button>
    <button
      v-if="menuInfo.src"
      type="button"
      role="menuitem"
      class="btn btn-sm btn-ghost flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-gray-700 transition-colors hover:bg-gray-50 dark:text-dark-200 dark:hover:bg-dark-700/50"
      @click="handleDownload"
    >
      <StudioIcon name="DownloadIcon" class="h-4 w-4 flex-shrink-0" />{{ t('imageStudio.contextMenu.download') }}
    </button>
    <button
      v-if="menuInfo.src"
      type="button"
      role="menuitem"
      class="btn btn-sm btn-ghost flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-gray-700 transition-colors hover:bg-gray-50 dark:text-dark-200 dark:hover:bg-dark-700/50"
      @click="handleEdit"
    >
      <StudioIcon name="EditIcon" class="h-4 w-4 flex-shrink-0" />{{ t('imageStudio.contextMenu.edit') }}
    </button>
    <div v-if="menuInfo.taskId && menuInfo.src" class="my-1 border-t border-gray-100 dark:border-dark-700" role="separator" />
    <button
      v-if="menuInfo.taskId"
      type="button"
      role="menuitem"
      data-testid="delete-history-task"
      class="btn btn-sm btn-ghost flex w-full items-center gap-2 px-4 py-2 text-left text-sm !text-red-600 hover:!bg-red-50 disabled:opacity-40 dark:!text-red-400 dark:hover:!bg-red-500/10"
      :disabled="!deletableTask"
      :title="!deletableTask ? t('imageStudio.store.waitGeneration') : undefined"
      @click="handleDelete"
    >
      <StudioIcon name="TrashIcon" class="h-4 w-4 flex-shrink-0" />{{ t('imageStudio.task.delete') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import { storeState as state, addImageFromUrl } from '../stores/store'
import {
  canCopyImageToClipboard,
  copyImageSourceToClipboard,
  getClipboardFailureMessage,
} from '../lib/clipboard'
import { downloadImageSource } from '../lib/downloadImage'
import { ensureImageCached } from '../lib/imageCache'
import { confirmTaskDeletion } from '../lib/taskDeletion'
import { useCloseOnEscape } from '../composables/useCloseOnEscape'

interface MenuInfo {
  src: string
  imageId?: string
  taskId?: string
  canCopyImage: boolean
  x: number
  y: number
}

const { t } = useI18n()
const menuInfo = ref<MenuInfo | null>(null)
const menuRef = ref<HTMLDivElement | null>(null)
const deletableTask = computed(() => {
  const task = state.tasks.find((item) => item.id === menuInfo.value?.taskId)
  return Boolean(task && task.status !== 'running')
})
let menuTrigger: HTMLElement | null = null
useCloseOnEscape(() => Boolean(menuInfo.value), () => {
  menuInfo.value = null
  menuTrigger?.focus({ preventScroll: true })
})

/** History entries add task deletion to the existing image actions. */
function onContextMenu(e: MouseEvent) {
  const target = e.target
  if (!(target instanceof Element)) return
  const historyItem = target.closest<HTMLElement>('[data-studio-history-task-id]')
  const taskId = historyItem?.dataset.studioHistoryTaskId
  const task = taskId ? state.tasks.find((item) => item.id === taskId) : undefined
  const img = target instanceof HTMLImageElement ? target : historyItem?.querySelector('img')
  if (!task && !img?.src) return

  e.preventDefault()
  menuTrigger = historyItem ?? img ?? null
  menuInfo.value = {
    src: img?.src ?? '',
    imageId: img?.dataset.imageId ?? task?.outputImages[0],
    taskId: task?.id,
    canCopyImage: canCopyImageToClipboard(),
    x: e.clientX,
    y: e.clientY,
  }
}

// 原实现用 isEmbeddedPage() 判断在 iframe 里就禁用；
// 改为原生页面后不再需要该判断，因为右键菜单是本功能的一部分。
window.addEventListener('contextmenu', onContextMenu)

/** 点击别处 / 滚动 / 缩放都收起 */
function onClose(e: Event) {
  if (menuRef.value && e.target instanceof Node && menuRef.value.contains(e.target)) return
  menuInfo.value = null
}

function onKeyDown(event: KeyboardEvent) {
  if (!menuInfo.value) return
  if (event.key === 'Tab') {
    menuInfo.value = null
  } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    const buttons = Array.from(menuRef.value?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])
    if (!buttons.length) return
    event.preventDefault()
    const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
    buttons[(index + (event.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length]?.focus()
  }
}
window.addEventListener('keydown', onKeyDown)

watch(menuInfo, (info) => {
  if (info) {
    void nextTick(() => {
      if (menuInfo.value === info) menuRef.value?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus({ preventScroll: true })
    })
    window.addEventListener('mousedown', onClose, { capture: true })
    window.addEventListener('touchstart', onClose, { capture: true })
    window.addEventListener('wheel', onClose, { capture: true })
    window.addEventListener('scroll', onClose, { capture: true })
    window.addEventListener('resize', onClose)
  } else {
    window.removeEventListener('mousedown', onClose, { capture: true })
    window.removeEventListener('touchstart', onClose, { capture: true })
    window.removeEventListener('wheel', onClose, { capture: true })
    window.removeEventListener('scroll', onClose, { capture: true })
    window.removeEventListener('resize', onClose)
  }
})

onUnmounted(() => {
  window.removeEventListener('contextmenu', onContextMenu)
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('mousedown', onClose, { capture: true })
  window.removeEventListener('touchstart', onClose, { capture: true })
  window.removeEventListener('wheel', onClose, { capture: true })
  window.removeEventListener('scroll', onClose, { capture: true })
  window.removeEventListener('resize', onClose)
})

/** 保证菜单不超出视口 */
const MENU_WIDTH = 160
const MENU_HEIGHT = computed(() => ((menuInfo.value?.src ? (menuInfo.value.canCopyImage ? 3 : 2) : 0) + (menuInfo.value?.taskId ? 1 : 0)) * 40 + 20)

const left = computed(() => {
  const x = menuInfo.value?.x ?? 0
  return Math.max(8, Math.min(x, window.innerWidth - MENU_WIDTH - 8))
})

const top = computed(() => {
  const y = menuInfo.value?.y ?? 0
  return Math.max(8, Math.min(y, window.innerHeight - MENU_HEIGHT.value - 8))
})

function handleDelete() {
  const taskId = menuInfo.value?.taskId
  if (!taskId || !deletableTask.value) return
  menuInfo.value = null
  menuTrigger?.focus({ preventScroll: true })
  confirmTaskDeletion(taskId)
}

async function getOriginalImageSrc(info: MenuInfo) {
  if (!info.imageId) return info.src
  return (await ensureImageCached(info.imageId)) ?? info.src
}

async function handleCopy(e: MouseEvent) {
  e.stopPropagation()
  const info = menuInfo.value
  if (!info) return
  menuInfo.value = null
  try {
    const target = await getOriginalImageSrc(info)
    await copyImageSourceToClipboard(target)
    state.showToast(t('imageStudio.contextMenu.copied'), 'success')
  } catch (err) {
    console.error(err)
    state.showToast(getClipboardFailureMessage(t('imageStudio.contextMenu.copyFailed'), err), 'error')
  }
}

async function handleEdit(e: MouseEvent) {
  e.stopPropagation()
  const info = menuInfo.value
  if (!info) return
  menuInfo.value = null

  if (state.inputImages.length >= 16) {
    state.showToast(t('imageStudio.contextMenu.referenceLimit'), 'error')
    return
  }

  try {
    const src = await getOriginalImageSrc(info)
    await addImageFromUrl(src)
    state.setDetailTaskId(null)
    state.setLightboxImageId(null)
    state.setMaskEditorImageId(null)
    state.showToast(t('imageStudio.contextMenu.addedToReference'), 'success')
  } catch (err) {
    console.error(err)
    state.showToast(
      t('imageStudio.contextMenu.addFailed', {
        error: err instanceof Error ? err.message : String(err),
      }),
      'error',
    )
  }
}

async function handleDownload(e: MouseEvent) {
  e.stopPropagation()
  const info = menuInfo.value
  if (!info) return
  menuInfo.value = null
  try {
    const target = await getOriginalImageSrc(info)
    await downloadImageSource(target)
    state.showToast(t('imageStudio.lightbox.downloaded'), 'success')
  } catch (err) {
    console.error(err)
    state.showToast(t('imageStudio.lightbox.downloadFailed'), 'error')
  }
}
</script>
