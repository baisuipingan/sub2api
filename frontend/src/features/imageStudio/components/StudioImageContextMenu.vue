<template>
  <div
    v-if="menuInfo"
    ref="menuRef"
    class="animate-fade-in fixed z-[9999] w-[120px] overflow-hidden rounded-lg border border-gray-100 bg-white py-1 shadow-xl dark:border-dark-700 dark:bg-dark-800"
    :style="{ left: `${left}px`, top: `${top}px` }"
    @contextmenu.prevent
  >
    <button
      v-if="menuInfo.canCopyImage"
      type="button"
      class="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-gray-700 transition-colors hover:bg-gray-50 dark:text-dark-200 dark:hover:bg-dark-700/50"
      @click="handleCopy"
    >
      <StudioIcon name="CopyIcon" class="h-4 w-4 flex-shrink-0" />{{ t('imageStudio.contextMenu.copy') }}
    </button>
    <button
      type="button"
      class="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-gray-700 transition-colors hover:bg-gray-50 dark:text-dark-200 dark:hover:bg-dark-700/50"
      @click="handleDownload"
    >
      <StudioIcon name="DownloadIcon" class="h-4 w-4 flex-shrink-0" />{{ t('imageStudio.contextMenu.download') }}
    </button>
    <button
      type="button"
      class="flex w-full items-center gap-2 px-4 py-2 text-left text-sm text-gray-700 transition-colors hover:bg-gray-50 dark:text-dark-200 dark:hover:bg-dark-700/50"
      @click="handleEdit"
    >
      <StudioIcon name="EditIcon" class="h-4 w-4 flex-shrink-0" />{{ t('imageStudio.contextMenu.edit') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
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

interface MenuInfo {
  src: string
  imageId?: string
  canCopyImage: boolean
  x: number
  y: number
}

const { t } = useI18n()
const menuInfo = ref<MenuInfo | null>(null)
const menuRef = ref<HTMLDivElement | null>(null)

/** 图片右键菜单：复制 / 下载 / 加入参考图 */
function onContextMenu(e: MouseEvent) {
  const target = e.target as HTMLElement | null
  if (!target || target.tagName !== 'IMG') return

  const img = target as HTMLImageElement
  if (!img.src) return

  e.preventDefault()
  menuInfo.value = {
    src: img.src,
    imageId: img.dataset.imageId,
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

watch(menuInfo, (info) => {
  if (info) {
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
  window.removeEventListener('mousedown', onClose, { capture: true })
  window.removeEventListener('touchstart', onClose, { capture: true })
  window.removeEventListener('wheel', onClose, { capture: true })
  window.removeEventListener('scroll', onClose, { capture: true })
  window.removeEventListener('resize', onClose)
})

/** 保证菜单不超出视口 */
const MENU_WIDTH = 120
const MENU_HEIGHT = computed(() => ((menuInfo.value?.canCopyImage ? 1 : 0) + 2) * 32 + 32)

const left = computed(() => {
  const x = menuInfo.value?.x ?? 0
  return x + MENU_WIDTH > window.innerWidth ? x - MENU_WIDTH : x
})

const top = computed(() => {
  const y = menuInfo.value?.y ?? 0
  return y + MENU_HEIGHT.value > window.innerHeight ? y - MENU_HEIGHT.value : y
})

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
