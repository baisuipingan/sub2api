<template>
  <div v-if="attachments.length || editTarget || uploading" class="bs-composer-attachments">
    <div class="bs-composer-attachment-frame">
      <button v-if="canLeft" type="button" class="bs-composer-attachment-arrow is-left" aria-label="向左查看附件" @click="page(-1)"><Icon name="chevronLeft" size="sm" /></button>
      <div ref="rail" class="bs-composer-attachment-rail" @scroll.passive="updateEdges">
        <div v-if="editTarget" class="bs-composer-chip is-edit-target">
          <span class="bs-composer-resource-icon"><Icon name="edit" size="lg" /></span>
          <span class="bs-composer-resource-body"><span class="bs-composer-resource-name" :title="editTarget.name">{{ editTarget.name }}</span><span class="bs-composer-resource-meta">继续修改此文件</span></span>
          <button type="button" class="bs-composer-resource-remove" :disabled="disabled" aria-label="取消修改此文件" @click="emit('clearEdit')"><Icon name="x" size="xs" /></button>
        </div>
        <div v-for="file in attachments" :key="file.version_id" class="bs-composer-chip">
          <span class="bs-composer-resource-icon"><Icon name="document" size="lg" /></span>
          <span class="bs-composer-resource-body"><span class="bs-composer-resource-name" :title="file.name">{{ file.name }}</span><span class="bs-composer-resource-meta">{{ description(file) }}</span></span>
          <button type="button" class="bs-composer-resource-remove" :disabled="disabled" :aria-label="`移除${file.name}`" @click="emit('remove', file.version_id)"><Icon name="x" size="xs" /></button>
        </div>
        <div v-if="uploading" class="bs-composer-chip" role="status"><span class="bs-composer-resource-icon"><Icon name="refresh" class="bs-chat-spin" size="lg" /></span><span class="bs-composer-resource-body"><span class="bs-composer-resource-name">正在上传文件…</span><span class="bs-composer-resource-meta">上传完成后即可发送</span></span></div>
      </div>
      <div v-if="canLeft" class="bs-composer-attachment-fade is-left"></div><div v-if="canRight" class="bs-composer-attachment-fade is-right"></div>
      <button v-if="canRight" type="button" class="bs-composer-attachment-arrow is-right" aria-label="向右查看附件" @click="page(1)"><Icon name="chevronRight" size="sm" /></button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import type { ChatResource } from '@/types/workbenchChat'
const props = defineProps<{ attachments: ChatResource[]; editTarget?: { version_id: string; name: string } | null; disabled?: boolean; uploading?: boolean }>()
const emit = defineEmits<{ remove: [id: string]; clearEdit: [] }>()
const rail = ref<HTMLElement | null>(null), canLeft = ref(false), canRight = ref(false)
let observer: ResizeObserver | undefined
function description(file: ChatResource) { const format = file.name.split('.').pop()?.toUpperCase() || '文件'; const bytes = file.bytes >= 1024 * 1024 ? `${(file.bytes / 1024 / 1024).toFixed(1)} MB` : file.bytes >= 1024 ? `${Math.ceil(file.bytes / 1024)} KB` : `${file.bytes} B`; return `${format} · ${bytes}` }
function updateEdges() { if (rail.value) { canLeft.value = rail.value.scrollLeft > 1; canRight.value = rail.value.scrollLeft < rail.value.scrollWidth - rail.value.clientWidth - 1 } }
function page(direction: number) { const element = rail.value; if (element) element.scrollBy({ left: direction * element.clientWidth, behavior: 'smooth' }) }
function onWheel(event: WheelEvent) { const element = rail.value; if (!element || element.scrollWidth <= element.clientWidth) return; const delta = Math.abs(event.deltaY) > Math.abs(event.deltaX) ? event.deltaY : event.deltaX; if (delta) { event.preventDefault(); element.scrollLeft += delta; updateEdges() } }
function observe() { observer?.disconnect(); if (rail.value) { observer?.observe(rail.value); rail.value.addEventListener('wheel', onWheel, { passive: false }) } updateEdges() }
watch(() => [props.attachments.at(-1)?.version_id, props.editTarget?.version_id, props.uploading], async () => { await nextTick(); observe(); rail.value?.scrollTo?.({ left: rail.value.scrollWidth, behavior: 'smooth' }); updateEdges() })
onMounted(() => { if (typeof ResizeObserver !== 'undefined') observer = new ResizeObserver(updateEdges); observe() })
onBeforeUnmount(() => { observer?.disconnect(); rail.value?.removeEventListener('wheel', onWheel) })
</script>
