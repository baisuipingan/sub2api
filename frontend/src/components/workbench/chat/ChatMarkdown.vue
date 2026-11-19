<template><div class="bs-chat-markdown" v-html="html" @click="onClick"></div></template>
<script setup lang="ts">
import { computed } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import type { ChatArtifact } from '@/types/workbenchChat'
const props = defineProps<{ content: string; files?: ChatArtifact[] }>()
const emit = defineEmits<{ preview: [file: ChatArtifact] }>()
function artifactFor(href: string): ChatArtifact | undefined {
  if (/^(?:https?:|mailto:|javascript:|data:|blob:)/i.test(href)) return
  try {
    const path = decodeURIComponent(href).replace(/^sandbox:/i, '').replace(/^\.\//, '').split(/[?#]/)[0] || ''
    if (!/^(?:\/?output\/|\/?scratch\/|\/mnt\/data\/)/.test(path) && path.includes('/')) return
    return props.files?.find(file => path === file.name || path.endsWith(`/${file.name}`))
  } catch { return }
}
const html = computed(() => {
  const rendered = marked.parse(props.content || '', { breaks: true, gfm: true, async: false, walkTokens(token) { if (token.type === 'link') { const file = artifactFor(token.href); if (file) token.href = `#workbench-file-${file.version_id}` } } }) as string
  return DOMPurify.sanitize(rendered, { FORBID_TAGS: ['iframe', 'object', 'embed', 'form', 'input', 'style'], ADD_ATTR: ['target', 'rel'] })
})
function onClick(event: MouseEvent) {
  const anchor = (event.target as HTMLElement)?.closest('a')
  if (!anchor) return
  const href = anchor.getAttribute('href') || ''
  if (!href) { event.preventDefault(); return }
  if (href.startsWith('#workbench-file-')) { event.preventDefault(); const file = props.files?.find(item => href === `#workbench-file-${item.version_id}`); if (file) emit('preview', file); return }
  if (href.startsWith('#')) return
  event.preventDefault()
  try { const url = new URL(href); if (['http:', 'https:'].includes(url.protocol)) window.open(url.href, '_blank', 'noopener,noreferrer') } catch { /* Unsafe or unresolved model links stay inert. */ }
}
</script>
