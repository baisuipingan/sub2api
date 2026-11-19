<template>
  <button ref="trigger" type="button" class="bs-composer-add" title="添加附件和工具" aria-label="添加附件和工具" aria-haspopup="menu" :aria-expanded="open" :aria-controls="open ? menuId : undefined" :disabled="disabled" @pointerdown="pointerOpened = true" @keydown="triggerKeyDown" @click="toggle">
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" aria-hidden="true"><path d="M8 2V14M2 8H14" stroke="currentColor" stroke-width="1.3" /></svg>
  </button>
  <Teleport to="body">
    <div v-if="open" :id="menuId" ref="panel" class="bs-chat bs-composer-menu bs-composer-command-panel" :style="anchorStyle" role="menu" aria-label="附件和工具" @keydown="menuKeyDown">
      <div class="bs-composer-command-scroll-frame" :class="{ 'has-overflow': overflowBelow }"><div ref="viewport" class="bs-composer-command-scroll" @scroll.passive="updateOverflow"><div ref="content">
        <template v-if="page === 'tools'">
          <button type="button" role="menuitem" aria-label="返回附件和工具" class="bs-composer-command-row" @click="setPage('root')"><Icon name="chevronLeft" size="sm" /><span class="bs-composer-command-name">工具</span></button>
          <div class="bs-composer-command-submenu">
            <button v-if="searchEnabled" type="button" role="menuitemcheckbox" :aria-checked="webSearch" class="bs-composer-command-row" :disabled="disabled" @click="emit('update:webSearch', !webSearch)"><span class="bs-composer-command-name">联网搜索</span><span class="bs-composer-command-description">查找并引用互联网资料</span><Icon v-if="webSearch" name="check" size="sm" /></button>
          </div>
        </template>
        <template v-else>
          <button v-if="officeEnabled" type="button" role="menuitemcheckbox" :aria-checked="taskMode" class="bs-composer-command-row" :disabled="disabled || taskLocked" :title="taskLocked ? '移除附件后可退出任务模式' : undefined" @click="selectTask"><Icon name="clipboard" size="sm" /><span class="bs-composer-command-name">任务模式</span><span class="bs-composer-command-description">规划并完成多步办公任务</span><Icon v-if="taskMode" name="check" size="sm" /></button>
          <button v-if="searchEnabled" type="button" role="menuitem" aria-haspopup="menu" class="bs-composer-command-row" :disabled="disabled" @click="setPage('tools')"><Icon name="cog" size="sm" /><span class="bs-composer-command-name">工具</span><Icon class="bs-composer-command-end" name="chevronRight" size="sm" /></button>
          <button v-if="uploadsEnabled" type="button" role="menuitem" class="bs-composer-command-row" :disabled="disabled || uploading" @click="select('upload')"><Icon name="upload" size="sm" /><span class="bs-composer-command-name">上传文件</span><span class="bs-composer-command-description">添加需要阅读或处理的文件</span></button>
          <button v-if="resourcesEnabled" type="button" role="menuitem" class="bs-composer-command-row" :disabled="disabled || uploading" @click="select('resources')"><Icon name="document" size="sm" /><span class="bs-composer-command-name">从我的文件选取</span><span class="bs-composer-command-description">使用已有资料或生成的成果</span></button>
          <p v-if="!officeEnabled && !searchEnabled && !uploadsEnabled && !resourcesEnabled" class="bs-composer-command-empty">当前未启用其他工具</p>
        </template>
      </div></div></div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
const props = defineProps<{ disabled?: boolean; uploading?: boolean; taskMode?: boolean; webSearch?: boolean; taskLocked?: boolean; officeEnabled?: boolean; searchEnabled?: boolean; uploadsEnabled?: boolean; resourcesEnabled?: boolean }>()
const emit = defineEmits<{ 'update:taskMode': [value: boolean]; 'update:webSearch': [value: boolean]; upload: []; resources: []; focusEditor: [] }>()
const menuId = `bs-composer-menu-${useId()}`
const open = ref(false), page = ref<'root' | 'tools'>('root'), pointerOpened = ref(false), overflowBelow = ref(false)
const trigger = ref<HTMLButtonElement | null>(null), panel = ref<HTMLElement | null>(null), viewport = ref<HTMLElement | null>(null), content = ref<HTMLElement | null>(null)
const anchor = ref({ left: 12, top: 12, width: 248, maxHeight: 360 })
const anchorStyle = computed(() => ({ left: `${anchor.value.left}px`, top: `${anchor.value.top}px`, width: `${anchor.value.width}px`, maxHeight: `${anchor.value.maxHeight}px` }))
let observer: ResizeObserver | undefined
function updateOverflow() { const element = viewport.value; overflowBelow.value = !!element && element.scrollHeight - element.clientHeight - element.scrollTop > 1 }
function measure() {
  const element = trigger.value?.closest('.bs-chat-composer'), popup = panel.value
  if (!element || !popup) return
  const rect = element.getBoundingClientRect(), width = Math.min(rect.width, window.innerWidth - 24)
  const availableAbove = Math.max(0, rect.top - 16), availableBelow = Math.max(0, window.innerHeight - rect.bottom - 16)
  const above = availableAbove >= Math.min(200, popup.scrollHeight) || availableAbove >= availableBelow
  const maxHeight = Math.min(360, above ? availableAbove : availableBelow)
  const height = Math.min(popup.scrollHeight, maxHeight)
  anchor.value = { left: Math.max(12, Math.min(rect.left, window.innerWidth - width - 12)), top: above ? Math.max(12, rect.top - 4 - height) : rect.bottom + 4, width, maxHeight }
  if (viewport.value) viewport.value.style.maxHeight = `${Math.max(0, maxHeight - 8)}px`
  updateOverflow()
}
function focusFirst() { panel.value?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus({ preventScroll: true }) }
function close(restore = true) { open.value = false; page.value = 'root'; if (restore) void nextTick(() => { if (pointerOpened.value) emit('focusEditor'); else trigger.value?.focus({ preventScroll: true }) }) }
function toggle() { if (props.disabled) return; if (open.value) close(); else { page.value = 'root'; open.value = true } }
function triggerKeyDown(event: KeyboardEvent) { pointerOpened.value = false; if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); if (!props.disabled) open.value = true } }
function selectTask() { if (props.disabled || props.taskLocked) return; emit('update:taskMode', !props.taskMode); close() }
function select(kind: 'upload' | 'resources') { if (props.disabled || props.uploading) return; close(false); if (kind === 'upload') emit('upload'); else emit('resources') }
async function setPage(value: 'root' | 'tools') { page.value = value; await nextTick(); measure(); focusFirst() }
function menuKeyDown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); if (page.value !== 'root') void setPage('root'); else close(); return }
  if (event.key === 'ArrowLeft' && page.value !== 'root') { event.preventDefault(); void setPage('root'); return }
  if (event.key === 'Tab') { close(false); return }
  if (event.key === 'ArrowRight' && document.activeElement?.getAttribute('aria-haspopup') === 'menu') { event.preventDefault(); (document.activeElement as HTMLButtonElement).click(); return }
  const buttons = Array.from(panel.value?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') || [])
  const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
  if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
    event.preventDefault()
    const target = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : (index + (event.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length
    buttons[target]?.focus()
  }
}
function outside(event: PointerEvent) { const target = event.target as Node; if (!panel.value?.contains(target) && !trigger.value?.contains(target)) close(false) }
function dispose() { observer?.disconnect(); window.removeEventListener('resize', measure); window.removeEventListener('scroll', measure, true); document.removeEventListener('pointerdown', outside) }
watch(open, async value => {
  dispose()
  if (!value) return
  await nextTick()
  if (!open.value) return
  measure(); focusFirst()
  if (typeof ResizeObserver !== 'undefined') { observer = new ResizeObserver(measure); const composer = trigger.value?.closest('.bs-chat-composer'); if (composer) observer.observe(composer); if (content.value) observer.observe(content.value) }
  window.addEventListener('resize', measure); window.addEventListener('scroll', measure, true); document.addEventListener('pointerdown', outside)
})
watch(() => props.disabled, value => { if (value) close(false) })
onBeforeUnmount(dispose)
</script>
