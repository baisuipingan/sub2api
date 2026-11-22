<template>
  <div
    ref="root"
    class="bs-composer-editor"
    role="textbox"
    aria-label="消息"
    aria-multiline="true"
    :aria-disabled="disabled || undefined"
    :contenteditable="!disabled"
    :data-placeholder="placeholder"
    :data-empty="empty"
    data-testid="ai-chat-input"
    spellcheck="false"
    @input="onInput"
    @beforeinput="beforeInput"
    @keydown="onKeyDown"
    @keyup="saveSelection"
    @mouseup="saveSelection"
    @blur="saveSelection"
    @compositionstart="composing = true"
    @compositionend="compositionEnd"
    @paste="paste"
  ></div>
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'
import { readComposerDocument } from './composerDocument'

const props = withDefaults(defineProps<{
  modelValue: string
  placeholder: string
  disabled?: boolean
  maxlength?: number
}>(), { maxlength: 32768 })
const emit = defineEmits<{
  'update:modelValue': [value: string]
  submit: []
  pasteFiles: [event: ClipboardEvent]
}>()
const root = ref<HTMLDivElement | null>(null), empty = ref(true), composing = ref(false)
let bookmark: Range | null = null

function saveSelection() {
  const selection = window.getSelection(), element = root.value
  if (!element || !selection?.rangeCount || !element.contains(selection.anchorNode)) return
  bookmark = selection.getRangeAt(0).cloneRange()
}

function restoreSelection() {
  const element = root.value
  if (!element || props.disabled || composing.value) return
  const selection = window.getSelection()
  const current = selection?.rangeCount ? selection.getRangeAt(0) : null
  if (document.activeElement === element && current && element.contains(current.startContainer) && element.contains(current.endContainer)) return
  const saved = bookmark
  const range = saved && element.contains(saved.startContainer) && element.contains(saved.endContainer) ? saved : document.createRange()
  if (range !== saved) { range.selectNodeContents(element); range.collapse(false) }
  element.focus({ preventScroll: true })
  selection?.removeAllRanges()
  selection?.addRange(range)
}

function publish() {
  if (!root.value) return
  const text = readComposerDocument(root.value)
  empty.value = !text
  emit('update:modelValue', text)
  saveSelection()
}

function reconcile() {
  const element = root.value
  if (!element || composing.value) return
  if (readComposerDocument(element) !== props.modelValue) {
    element.textContent = props.modelValue
    bookmark = null
  }
  empty.value = !props.modelValue
}

function onInput() {
  if (props.disabled) return
  if (composing.value) {
    if (root.value) empty.value = !readComposerDocument(root.value)
  } else publish()
}
function compositionEnd() { composing.value = false; if (!props.disabled) publish(); void nextTick(reconcile) }

function beforeInput(event: InputEvent) {
  if (props.disabled) { event.preventDefault(); return }
  if (composing.value || !event.inputType.startsWith('insert') || !event.data) return
  const selection = window.getSelection()
  const selected = root.value?.contains(selection?.anchorNode || null) ? selection?.toString().length || 0 : 0
  if (props.modelValue.length - selected + event.data.length > props.maxlength) event.preventDefault()
}

function onKeyDown(event: KeyboardEvent) {
  if (event.key === 'Enter' && !event.shiftKey && !event.isComposing && !composing.value && event.keyCode !== 229) {
    event.preventDefault()
    if (!props.disabled) emit('submit')
  }
}

function paste(event: ClipboardEvent) {
  emit('pasteFiles', event)
  if (event.defaultPrevented || props.disabled) return
  event.preventDefault()
  const element = root.value
  if (!element) return
  restoreSelection()
  const selection = window.getSelection()
  const selected = selection?.toString().length || 0
  const text = (event.clipboardData?.getData('text/plain') || '').slice(0, Math.max(0, props.maxlength - props.modelValue.length + selected))
  if (!text) return
  // The native command preserves browser undo. No pasted HTML or resource IDs enter the DOM.
  const inserted = typeof document.execCommand === 'function' && document.execCommand('insertText', false, text)
  if (!inserted && selection?.rangeCount) {
    const range = selection.getRangeAt(0)
    range.deleteContents()
    const node = document.createTextNode(text)
    range.insertNode(node)
    range.setStartAfter(node)
    range.collapse(true)
    selection.removeAllRanges()
    selection.addRange(range)
  }
  publish()
}

watch(() => props.modelValue, reconcile, { flush: 'post' })
onMounted(reconcile)
defineExpose({ focus: restoreSelection })
</script>
