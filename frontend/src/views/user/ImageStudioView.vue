<template>
  <!-- 与其他用户页面一致：视图自身包一层 AppLayout，才能带上侧栏与顶栏 -->
  <AppLayout>
    <!-- 桌面端固定为「视口高度 - 顶部栏」，整体不滚动（顶部栏 64px 内容 + 1px 下边框）：
         左右两栏各自内部滚动，避免整页上下拖。移动端保持普通文档流。 -->
    <div
      class="image-studio-root -m-4 flex flex-col md:-m-6 lg:-m-8 lg:h-[calc(100dvh-4rem-1px)] lg:overflow-hidden"
    >
      <main
        class="mx-auto grid w-full max-w-[1600px] flex-1 gap-0 lg:min-h-0 lg:grid-cols-[clamp(280px,26vw,340px)_minmax(0,1fr)]"
      >
        <StudioCreationPanel />
        <StudioResultPanel />
      </main>

    <!-- 全局浮层 -->
      <StudioDetailModal />
      <StudioLightbox />
      <StudioMaskEditorModal />
      <StudioSettingsModal />
      <StudioConfirmDialog />
      <StudioImageContextMenu />
      <StudioToast />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import StudioCreationPanel from '@/features/imageStudio/components/StudioCreationPanel.vue'
import StudioResultPanel from '@/features/imageStudio/components/StudioResultPanel.vue'
import StudioDetailModal from '@/features/imageStudio/components/StudioDetailModal.vue'
import StudioLightbox from '@/features/imageStudio/components/StudioLightbox.vue'
import StudioMaskEditorModal from '@/features/imageStudio/components/StudioMaskEditorModal.vue'
import StudioSettingsModal from '@/features/imageStudio/components/StudioSettingsModal.vue'
import StudioConfirmDialog from '@/features/imageStudio/components/StudioConfirmDialog.vue'
import StudioImageContextMenu from '@/features/imageStudio/components/StudioImageContextMenu.vue'
import StudioToast from '@/features/imageStudio/components/StudioToast.vue'
import { storeState as state, initStore, retryPendingGalleryTasks } from '@/features/imageStudio/stores/store'
import { loadImagePricing, collectBillingModels } from '@/features/imageStudio/lib/studioPricing'
import { ensureStudioApiKey } from '@/features/imageStudio/lib/studioAuth'
import { captureStudioIdentity, isStudioIdentityCurrent } from '@/features/imageStudio/lib/studioKeySession'
import { ensureGalleryIdentity } from '@/features/imageStudio/lib/galleryApi'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

function retryGalleryOnReconnect() {
  const identity = captureStudioIdentity()
  if (identity.token && identity.userId) void retryPendingGalleryTasks()
}

/**
 * 生图工作台主视图。
 *
 * 鉴权说明：不再让用户手填 API Key。进入页面时向后端换取本用户专属的
 * 「生图工作台专用」密钥并注入 store；用户无感，计费仍走原有网关链路。
 */
onMounted(async () => {
  // Clear prior-account state synchronously before a new key request can fail.
  void initStore()
  window.addEventListener('online', retryGalleryOnReconnect)
  window.addEventListener('focus', retryGalleryOnReconnect)
  await ensureStudioApiKey()
})

onUnmounted(() => {
  window.removeEventListener('online', retryGalleryOnReconnect)
  window.removeEventListener('focus', retryGalleryOnReconnect)
})

/** A refreshed JWT needs fresh gallery verification and a key bound to that JWT. */
watch(
  () => [authStore.token, authStore.user?.id] as const,
  async ([token, userId], [previousToken, previousUserId], onCleanup) => {
    if (token === previousToken && userId === previousUserId) return
    let cancelled = false
    onCleanup(() => { cancelled = true })
    if (userId !== previousUserId) void initStore()
    if (!token || !userId) return
    const identity = captureStudioIdentity()
    if (identity.token !== token || identity.userId !== userId) return
    void ensureStudioApiKey()
    try { await ensureGalleryIdentity(token) } catch { return }
    if (!cancelled && isStudioIdentityCurrent(identity)) void retryPendingGalleryTasks()
  },
)

/**
 * 模型目录就绪后拉取单价表。
 *
 * 放在这里而不是 ensureStudioApiKey 里：用户点「重新获取模型」时目录会再变，
 * 需要按新的模型集合重新取价。价格只影响显示，失败也不影响出图。
 */
watch(
  () => [state.catalogStatus, state.modelCatalog, state.settings.miloRouter.apiKey] as const,
  async ([status, catalog, studioKey], _previous, onCleanup) => {
    if (status !== 'ready' || catalog.length === 0 || !studioKey) return
    const session = captureStudioIdentity()
    let cancelled = false
    onCleanup(() => { cancelled = true })
    const pricing = await loadImagePricing(collectBillingModels(catalog))
    if (cancelled || !isStudioIdentityCurrent(session)
      || state.catalogStatus !== 'ready' || state.modelCatalog !== catalog
      || state.settings.miloRouter.apiKey !== studioKey) return
    if (Object.keys(pricing).length) state.setImagePricing(pricing)
  },
  { immediate: true },
)

/** 主题跟随：sub2api 用 class 控制暗色，这里同步到工作台根节点 */
watch(
  () => document.documentElement.classList.contains('dark'),
  (isDark) => {
    document.documentElement.classList.toggle('dark', isDark)
  },
)
</script>

<style scoped>
.image-studio-root {
  min-height: 100%;
}
</style>
