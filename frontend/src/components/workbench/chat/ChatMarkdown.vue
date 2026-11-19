<template><div class="bs-chat-markdown" v-html="html" @click="onClick"></div></template>
<script setup lang="ts">
import { computed } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import type { ChatArtifact, ChatCitation } from '@/types/workbenchChat'
const props = defineProps<{ content: string; files?: ChatArtifact[]; citations?: ChatCitation[] }>()
const emit = defineEmits<{ preview: [file: ChatArtifact]; reference: [citation: ChatCitation] }>()
function artifactFor(href: string): ChatArtifact | undefined {
  if (/^(?:https?:|mailto:|javascript:|data:|blob:)/i.test(href)) return
  try {
    const path = decodeURIComponent(href).replace(/^sandbox:/i, '').replace(/^\.\//, '').split(/[?#]/)[0] || ''
    if (!/^(?:\/?output\/|\/?scratch\/|\/mnt\/data\/)/.test(path) && path.includes('/')) return
    return props.files?.find(file => path === file.name || path.endsWith(`/${file.name}`))
  } catch { return }
}
const html = computed(() => {
  const rendered = marked.parse(props.content || '', { breaks: true, gfm: true, async: false, walkTokens(token) { if (token.type === 'link') { const file = artifactFor(token.href); if (file) token.href = `#workbench-file-${file.version_id}`; else { const citation = props.citations?.find(source => source.url === token.href); if (citation) token.href = `#workbench-source-${citation.id}` } } } }) as string
  const clean = DOMPurify.sanitize(rendered, { USE_PROFILES: { html: true }, ALLOW_DATA_ATTR: false, FORBID_TAGS: ['iframe', 'object', 'embed', 'form', 'input', 'style', 'button', 'img', 'video', 'audio', 'source', 'picture'], ADD_ATTR: ['target', 'rel'] })
  const fragment = document.createElement('template')
  fragment.innerHTML = clean
  for (const anchor of fragment.content.querySelectorAll<HTMLAnchorElement>('a')) {
    const href = anchor.getAttribute('href') || ''
    if (props.files?.some(file => href === `#workbench-file-${file.version_id}`)) continue
    const source = props.citations?.find(item => href === `#workbench-source-${item.id}`)
    if (source) {
      anchor.classList.add('bs-chat-inline-citation')
      anchor.title = source.title
      anchor.setAttribute('aria-label', `查看来源：${source.title}`)
      continue
    }
    try {
      const url = new URL(href)
      if (['http:', 'https:'].includes(url.protocol) && !url.username && !url.password) {
        anchor.href = url.href
        anchor.target = '_blank'
        anchor.rel = 'noopener noreferrer'
        anchor.setAttribute('referrerpolicy', 'no-referrer')
        continue
      }
    } catch { /* Non-authorized relative links have no navigation target. */ }
    anchor.removeAttribute('href')
    anchor.removeAttribute('target')
  }
  for (const pre of fragment.content.querySelectorAll('pre')) {
    const code = pre.querySelector('code')
    if (!code) continue
    const toolbar = document.createElement('div')
    toolbar.className = 'bs-chat-code-toolbar'
    const language = document.createElement('span')
    language.textContent = [...code.classList].find(value => value.startsWith('language-'))?.slice(9) || '代码'
    const copy = document.createElement('button')
    copy.type = 'button'; copy.dataset.copyCode = ''; copy.textContent = '复制代码'
    toolbar.append(language, copy)
    pre.prepend(toolbar)
  }
  return fragment.innerHTML
})
async function onClick(event: MouseEvent) {
  const copy = (event.target as HTMLElement)?.closest<HTMLButtonElement>('[data-copy-code]')
  if (copy) { try { await navigator.clipboard.writeText(copy.closest('pre')?.querySelector('code')?.textContent || ''); copy.textContent = '已复制' } catch { copy.textContent = '复制失败' } return }
  const anchor = (event.target as HTMLElement)?.closest('a')
  if (!anchor) return
  const href = anchor.getAttribute('href') || ''
  if (!href) { event.preventDefault(); return }
  if (href.startsWith('#workbench-file-')) { event.preventDefault(); const file = props.files?.find(item => href === `#workbench-file-${item.version_id}`); if (file) emit('preview', file); return }
  if (href.startsWith('#workbench-source-')) { event.preventDefault(); const citation = props.citations?.find(item => href === `#workbench-source-${item.id}`); if (citation) emit('reference', citation); return }
  if (href.startsWith('#')) return
  event.preventDefault()
  try { const url = new URL(href); if (['http:', 'https:'].includes(url.protocol) && !url.username && !url.password) window.open(url.href, '_blank', 'noopener,noreferrer') } catch { /* Unsafe or unresolved model links stay inert. */ }
}
</script>
