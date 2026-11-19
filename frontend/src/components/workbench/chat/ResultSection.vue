<template>
  <div class="bs-chat-result">
    <div v-if="files.length" class="bs-chat-report-ready"><span>{{ files.length > 1 ? '已生成' : '文件已准备好：' }}</span><button type="button" class="bs-chat-file-link" @click="emit('preview', files[0])"><Icon name="document" size="sm" /><span>{{ files[0].name }}</span></button><span v-if="files.length > 1">等 {{ files.length }} 个文件</span><button v-else type="button" class="bs-chat-icon" aria-label="下载文件" @click="emit('download', files[0])"><Icon name="download" size="sm" /></button></div>
    <ChatMarkdown v-if="text" :content="text" :files="files" @preview="emit('preview', $event)" />
    <div v-if="files.length > 1" class="bs-chat-result-files"><div class="bs-chat-result-files-title"><Icon name="document" size="sm" />输出文件 <span>{{ files.length }}</span></div><div v-for="file in files" :key="file.version_id" class="bs-chat-file-row"><button type="button" @click="emit('preview', file)"><span>{{ file.name }}</span></button><button type="button" class="bs-chat-text-button" @click="emit('download', file)"><Icon name="download" size="sm" />下载</button></div></div>
  </div>
</template>
<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import ChatMarkdown from './ChatMarkdown.vue'
import type { ChatArtifact } from '@/types/workbenchChat'
defineProps<{ text: string; files: ChatArtifact[] }>()
const emit = defineEmits<{ preview: [file: ChatArtifact]; download: [file: ChatArtifact] }>()
</script>
