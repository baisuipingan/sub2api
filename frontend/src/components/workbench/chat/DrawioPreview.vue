<template>
  <iframe :srcdoc="markup" :title="title" class="bs-chat-docx-preview" sandbox="allow-scripts" referrerpolicy="no-referrer" />
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ xml: string; title: string }>()

const markup = computed(() => {
  const policy = "default-src 'none'; script-src " + window.location.origin + "; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; base-uri 'none'; form-action 'none'"
  const config = JSON.stringify({ xml: props.xml, nav: true, toolbar: 'zoom', resize: true, 'check-visible': false })
  const node = document.createElement('div')
  node.className = 'mxgraph'
  node.setAttribute('data-mxgraph', config)
  const script = document.createElement('script')
  script.src = new URL(import.meta.env.BASE_URL + 'drawio-preview/viewer-static.js', window.location.origin).href
  const header = document.createElement('meta')
  header.httpEquiv = 'Content-Security-Policy'
  header.content = policy
  return '<!doctype html><html><head><meta charset="utf-8">' + header.outerHTML +
    '<style>body{margin:0;padding:12px}.mxgraph{width:100%}</style></head><body>' +
    node.outerHTML + script.outerHTML + '</body></html>'
})
</script>
