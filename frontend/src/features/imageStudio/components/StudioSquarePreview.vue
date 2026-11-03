<template>
  <!--
    固定正方形相框。

    为什么要固定尺寸：不同比例的图（1:1 / 16:9 / 9:16）放进同一个框，
    切换历史时画面不跳动，眼睛有稳定的落点。
    图片用 object-contain：以「最短边贴合相框」缩放，另一方向留白居中，
    不裁切也不拉伸。
  -->
  <div
    class="studio-frame relative flex items-center justify-center overflow-hidden rounded-2xl bg-gray-100/80 ring-1 ring-inset ring-black/[0.04] dark:bg-white/[0.04] dark:ring-white/[0.06]"
    role="region"
    :aria-label="t('imageStudio.result.previewAria')"
  >
    <!-- 生成中 -->
    <template v-if="task.status === 'running'">
      <img
        v-if="streamPreview"
        :src="streamPreview"
        :alt="t('imageStudio.task.streamPreview')"
        class="h-full w-full object-contain"
      />
      <div v-else class="flex flex-col items-center gap-3 text-sm text-primary-600 dark:text-primary-400">
        <span class="h-9 w-9 animate-spin rounded-full border-2 border-primary-200 border-t-primary-600" />
        {{ t('imageStudio.result.generating') }}
      </div>
      <span class="absolute bottom-3 left-3 rounded-full bg-black/60 px-2.5 py-1 text-[11px] text-white">
        {{ t('imageStudio.result.rendering') }}
      </span>
    </template>

    <!-- 已生成：点击放大 -->
    <button
      v-else-if="source"
      type="button"
      class="group h-full w-full outline-none"
      :aria-label="t('imageStudio.result.zoomAria')"
      @click="openLightbox"
    >
      <img
        :src="source"
        :alt="t('imageStudio.result.imageAlt')"
        class="h-full w-full object-contain transition-transform duration-200 group-hover:scale-[1.01]"
      />
    </button>

    <!-- 失败 / 无图 / 加载中 -->
    <div v-else class="flex flex-col items-center gap-2 px-6 text-center text-sm text-gray-400">
      <StudioIcon
        v-if="task.status === 'error'"
        name="ImageUploadIcon"
        class="h-10 w-10 text-gray-300 dark:text-dark-600"
      />
      <span v-else-if="task.status === 'done'" class="h-8 w-8 animate-spin rounded-full border-2 border-gray-200 border-t-primary-500" />
      <span>
        {{ task.status === 'error' ? t('imageStudio.result.noOutput') : t('imageStudio.result.unavailable') }}
      </span>
    </div>

    <!-- 多图切换：浮在框内侧，不占布局空间 -->
    <template v-if="task.outputImages.length > 1 && source">
      <button
        type="button"
        class="absolute left-2 top-1/2 flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded-full bg-black/45 text-white backdrop-blur transition hover:bg-black/65 disabled:pointer-events-none disabled:opacity-0"
        :aria-label="t('imageStudio.result.prevImage')"
        :disabled="imageIndex <= 0"
        @click="selectImage(imageIndex - 1)"
      >
        <StudioIcon name="ChevronLeftIcon" class="h-4 w-4" />
      </button>
      <button
        type="button"
        class="absolute right-2 top-1/2 flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded-full bg-black/45 text-white backdrop-blur transition hover:bg-black/65 disabled:pointer-events-none disabled:opacity-0"
        :aria-label="t('imageStudio.result.nextImage')"
        :disabled="imageIndex >= task.outputImages.length - 1"
        @click="selectImage(imageIndex + 1)"
      >
        <StudioIcon name="ChevronRightIcon" class="h-4 w-4" />
      </button>
      <span
        class="pointer-events-none absolute bottom-3 right-3 rounded-full bg-black/55 px-2 py-0.5 text-[11px] tabular-nums text-white"
      >
        {{ imageIndex + 1 }} / {{ task.outputImages.length }}
      </span>
    </template>
  </div>
</template>

<script setup lang="ts">
/**
 * 方形预览框。
 *
 * 边长完全由 CSS 决定（见下方 .studio-frame）：取「父容器可用宽」与
 * 「父容器可用高」的较小值。这样浏览器缩放到 67% 或 150%、窗口任意拉伸，
 * 相框都恰好填满但不溢出，不需要任何 JS 测量。
 *
 * 图片走 imageCache：先读缓存同步出图，未命中再异步补，避免闪白。
 */
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { TaskRecord } from '../types'
import { storeState as state } from '../stores/store'
import { ensureImageCached, getCachedImage } from '../lib/imageCache'
import StudioIcon from './StudioIcon.vue'

const props = defineProps<{
  task: TaskRecord
  /** 当前显示的图片 id，由父级持有，和右侧历史栏联动 */
  imageId?: string
}>()

const emit = defineEmits<{
  selectImage: [index: number]
}>()

const { t } = useI18n()

const source = ref<string | null>(null)
let cancelled = false

const imageIndex = computed(() => {
  const id = props.imageId
  return id ? Math.max(0, props.task.outputImages.indexOf(id)) : 0
})

const streamPreview = computed(() => state.streamPreviews[props.task.id]?.image)

function load(id: string | undefined) {
  cancelled = false
  source.value = id ? getCachedImage(id) ?? null : null
  if (!id || source.value) return

  void ensureImageCached(id)
    .then((dataUrl) => {
      if (cancelled || !dataUrl) return
      source.value = dataUrl
    })
    .catch(() => {
      /* 取不到就停在占位态，不阻塞其它交互 */
    })
}

watch(
  () => props.imageId,
  (id) => {
    cancelled = true
    load(id)
  },
  { immediate: true },
)

onUnmounted(() => {
  cancelled = true
})

function selectImage(index: number) {
  emit('selectImage', index)
}

function openLightbox() {
  if (!props.imageId) return
  state.setLightboxImageId(props.imageId, props.task.outputImages)
}
</script>

<style scoped>
/**
 * 方形相框尺寸。
 *
 * 关键点：父级给了 `container-type: size` 后，这里的 cqw / cqh 就是父容器的
 * 内容宽高。相框取两者的较小值 → 永远是正方形，且永不超过父容器。
 *
 * 为什么不用 JS 算：之前是「视口高度 - 172px」这种硬编码估算，
 * 结果 100% 缩放时算大了溢出、67% 缩放时被 560 上限卡住显得空。
 * 交给 CSS 后，缩放比例、窗口大小、历史栏展开收起都不需要重新计算。
 *
 * 预留 62px：提示词一行 + 操作键一行 + 元信息一行的紧凑占位。
 */
.studio-frame {
  width: min(100cqw, calc(100cqh - 62px));
  max-width: 720px;
  aspect-ratio: 1 / 1;
  /* 有些窄屏会算出很大值，用 vmin 兜一层，防止超过视口 */
  max-height: 100cqh;
}

/* 容器查询不可用时退化为纯宽高比方案，保证仍能看到图 */
@supports not (width: 1cqw) {
  .studio-frame {
    width: 100%;
    max-width: min(560px, 62vh);
  }
}
</style>
