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
    @click="selectReference"
    @compositionstart="composing = true"
    @compositionend="compositionEnd"
    @paste="paste"
  ></div>
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'
import { composerReferenceSelector, createComposerReference, readComposerDocument } from './composerDocument'
import type { ComposerReference } from './composerDocument'

const props = withDefaults(defineProps<{
  modelValue: string
  references: ComposerReference[]
  placeholder: string
  disabled?: boolean
  maxlength?: number
}>(), { maxlength: 32768 })
const emit = defineEmits<{
  'update:modelValue': [value: string]
  referencesChange: [keys: string[]]
  submit: []
  pasteFiles: [event: ClipboardEvent]
}>()
const root = ref<HTMLDivElement | null>(null), empty = ref(true), composing = ref(false)
let bookmark: Range | null = null

function saveSelection() {
  const selection = window.getSelection(), element = root.value
  if (!element || !selection?.rangeCount || !element.contains(selection.anchorNode)) return
  const range = selection.getRangeAt(0).cloneRange()
  const container = range.startContainer
  const token = (container instanceof HTMLElement ? container : container.parentElement)?.closest(composerReferenceSelector)
  if (range.collapsed && token) {
    range.setStartAfter(token)
    range.collapse(true)
    selection.removeAllRanges()
    selection.addRange(range)
  }
  bookmark = range
}

function restoreSelection() {
  const element = root.value
  if (!element) return
  const saved = bookmark
  const range = saved && element.contains(saved.startContainer) && element.contains(saved.endContainer) ? saved : document.createRange()
  if (range !== saved) { range.selectNodeContents(element); range.collapse(false) }
  element.focus({ preventScroll: true })
  const selection = window.getSelection()
  selection?.removeAllRanges()
  selection?.addRange(range)
}

function publish() {
  if (!root.value) return
  const document = readComposerDocument(root.value)
  empty.value = !document.text && !document.keys.length
  emit('update:modelValue', document.text)
  emit('referencesChange', [...new Set(document.keys)])
  saveSelection()
  // Busy/attachment rules may keep a reference selected after native deletion.
  void nextTick(reconcile)
}

function reconcile() {
  const element = root.value
  if (!element || composing.value) return
  const current = readComposerDocument(element)
  if (current.text !== props.modelValue) {
    const retained = Array.from(element.querySelectorAll<HTMLElement>(composerReferenceSelector))
    element.replaceChildren(...retained, document.createTextNode(props.modelValue))
    bookmark = null
  }
  const wanted = new Set(props.references.map(reference => reference.key))
  for (const token of element.querySelectorAll<HTMLElement>(composerReferenceSelector)) {
    if (!wanted.has(token.dataset.composerReference!)) token.remove()
  }
  const present = new Set(readComposerDocument(element).keys)
  const added = props.references.filter(reference => !present.has(reference.key))
  if (added.length) {
    if (bookmark && !props.disabled) {
      restoreSelection()
      const tokens = added.map(createComposerReference)
      const inserted = typeof document.execCommand === 'function' && document.execCommand('insertHTML', false, tokens.map(token => token.outerHTML).join(''))
      if (!inserted) {
        const selection = window.getSelection(), fragment = document.createDocumentFragment()
        tokens.forEach(token => fragment.append(token))
        if (selection?.rangeCount) { const range = selection.getRangeAt(0); range.deleteContents(); range.insertNode(fragment); range.setStartAfter(tokens.at(-1)!); range.collapse(true); selection.removeAllRanges(); selection.addRange(range) }
      }
      saveSelection()
    } else {
      const fragment = document.createDocumentFragment()
      added.forEach(reference => fragment.append(createComposerReference(reference)))
      const last = fragment.lastChild!
      element.prepend(fragment)
      const range = document.createRange()
      range.setStartAfter(last)
      range.collapse(true)
      bookmark = range
    }
  }
  empty.value = !readComposerDocument(element).text && !props.references.length
}

function onInput() {
  if (props.disabled) return
  if (composing.value) {
    if (root.value) { const current = readComposerDocument(root.value); empty.value = !current.text && !current.keys.length }
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

function selectReference(event: MouseEvent) {
  if (props.disabled || !(event.target instanceof HTMLElement)) return
  const token = event.target.closest(composerReferenceSelector)
  if (!token || !root.value?.contains(token)) return
  const range = document.createRange()
  range.selectNode(token)
  const selection = window.getSelection()
  selection?.removeAllRanges()
  selection?.addRange(range)
  saveSelection()
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

watch(() => [props.modelValue, props.references, props.disabled], reconcile, { deep: true, flush: 'post', immediate: true })
watch(root, reconcile, { flush: 'post' })
onMounted(reconcile)
defineExpose({ focus: restoreSelection })
</script>
