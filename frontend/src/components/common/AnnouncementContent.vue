<template>
  <div class="markdown-body min-w-0 break-words" v-html="rendered" />
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import '@/styles/announcement-markdown.css'
const props = defineProps<{ content: string }>()
const rendered = computed(() => DOMPurify.sanitize(marked.parse(props.content || '', { breaks: true, gfm: true }) as string))
</script>
<style scoped>
.markdown-body :deep(table) { display: block; max-width: 100%; overflow-x: auto; }
.markdown-body :deep(pre) { max-width: 100%; }
.markdown-body :deep(img) { height: auto; }
</style>
