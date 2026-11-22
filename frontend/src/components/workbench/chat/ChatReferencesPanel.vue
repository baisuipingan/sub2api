<template>
  <section class="bs-chat-workspace bs-chat-references" aria-label="参考资料">
    <header>
      <strong>参考资料 <span class="bs-chat-file-count">{{ sources.length }}</span></strong>
      <button type="button" class="bs-chat-icon" aria-label="关闭参考资料" @click="emit('close')"><Icon name="x" size="sm" /></button>
    </header>
    <div ref="list" class="bs-chat-references-list">
      <article v-for="(source, index) in sources" :key="source.id" :id="`reference-${source.id}`" :data-source-id="source.id" class="bs-chat-reference" :class="{ active: source.id === activeId }">
        <div class="bs-chat-reference-domain"><span>{{ index + 1 }}</span>{{ domain(source.url) }}</div>
        <a :href="safeURL(source.url)" target="_blank" rel="noopener noreferrer" referrerpolicy="no-referrer">{{ source.title }}<Icon name="externalLink" size="sm" /></a>
        <p v-if="source.excerpt">{{ source.excerpt }}</p>
      </article>
    </div>
  </section>
</template>
<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import type { ChatCitation } from '@/types/workbenchChat'
const props = defineProps<{ citations: ChatCitation[]; activeId?: string }>()
const emit = defineEmits<{ close: [] }>()
function safeURL(raw?: string) { try { const value = new URL(raw || ''); if (['https:', 'http:'].includes(value.protocol) && !value.username && !value.password) return value.href } catch { /* Invalid source metadata is not a link. */ } return undefined }
const sources = computed(() => [...new Map(props.citations.filter(source => safeURL(source.url)).map(source => [source.id, source])).values()])
const list = ref<HTMLElement | null>(null)
watch(() => [props.activeId, props.citations], async () => { await nextTick(); const target = [...list.value?.querySelectorAll<HTMLElement>('[data-source-id]') || []].find(element => element.dataset.sourceId === props.activeId); target?.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' }) }, { immediate: true })
function domain(raw?: string) { try { return new URL(raw || '').hostname } catch { return '' } }
</script>
