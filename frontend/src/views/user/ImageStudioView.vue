<template>
  <!-- 与其他用户页面一致：视图自身包一层 AppLayout，才能带上侧栏与顶栏 -->
  <AppLayout>
    <!-- 桌面端固定为「视口高度 - 顶部栏」，整体不滚动（顶部栏 64px 内容 + 1px 下边框）：
         左右两栏各自内部滚动，避免整页上下拖。移动端保持普通文档流。 -->
    <div
      class="image-studio-root -m-4 flex flex-col md:-m-6 lg:-m-8 lg:h-[calc(100dvh-4rem-1px)] lg:overflow-hidden"
    >
      <StudioProductHeader />

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
import { onMounted, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import StudioProductHeader from '@/features/imageStudio/components/StudioProductHeader.vue'
import StudioCreationPanel from '@/features/imageStudio/components/StudioCreationPanel.vue'
import StudioResultPanel from '@/features/imageStudio/components/StudioResultPanel.vue'
import StudioDetailModal from '@/features/imageStudio/components/StudioDetailModal.vue'
import StudioLightbox from '@/features/imageStudio/components/StudioLightbox.vue'
import StudioMaskEditorModal from '@/features/imageStudio/components/StudioMaskEditorModal.vue'
import StudioSettingsModal from '@/features/imageStudio/components/StudioSettingsModal.vue'
import StudioConfirmDialog from '@/features/imageStudio/components/StudioConfirmDialog.vue'
import StudioImageContextMenu from '@/features/imageStudio/components/StudioImageContextMenu.vue'
import StudioToast from '@/features/imageStudio/components/StudioToast.vue'
import { storeState as state, initStore } from '@/features/imageStudio/stores/store'
import { loadImagePricing, collectBillingModels } from '@/features/imageStudio/lib/studioPricing'
import { ensureStudioApiKey } from '@/features/imageStudio/lib/studioAuth'

/**
 * 生图工作台主视图。
 *
 * 鉴权说明：不再让用户手填 API Key。进入页面时向后端换取本用户专属的
 * 「生图工作台专用」密钥并注入 store；用户无感，计费仍走原有网关链路。
 */
onMounted(async () => {
  await ensureStudioApiKey()
  initStore()
})

/**
 * 模型目录就绪后拉取单价表。
 *
 * 放在这里而不是 ensureStudioApiKey 里：用户点「重新获取模型」时目录会再变，
 * 需要按新的模型集合重新取价。价格只影响显示，失败也不影响出图。
 */
watch(
  () => [state.catalogStatus, state.modelCatalog.length] as const,
  async ([status, count]) => {
    if (status !== 'ready' || count === 0) return
    const pricing = await loadImagePricing(collectBillingModels(state.modelCatalog))
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
