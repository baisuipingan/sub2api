<template>
  <div class="bs-chat-result" data-component="ResultSection">
    <ChatMarkdown v-if="text" :content="text" :files="files" :citations="citations" @preview="emit('preview', $event)" @reference="emit('reference', $event)" />
    <ul v-if="files.length" class="bs-chat-deliverables" aria-label="生成文件">
      <li v-for="file in files" :key="file.version_id" class="bs-chat-deliverable">
        <button type="button" class="bso-file-open" @click="emit('preview', file)">
          <WorkspaceFileIcon :file="file" />
          <span class="bso-file-info">
            <strong :title="file.name">{{ file.name }}</strong>
            <small v-if="file.bytes">{{ formatBytes(file.bytes, 1) }}</small>
          </span>
        </button>
        <button type="button" class="bs-chat-icon" :aria-label="`下载${file.name}`" title="下载文件" @click="emit('download', file)">
          <Icon name="download" size="sm" />
        </button>
      </li>
    </ul>
  </div>
</template>
<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import ChatMarkdown from './ChatMarkdown.vue'
import WorkspaceFileIcon from './WorkspaceFileIcon.vue'
import { formatBytes } from '@/utils/format'
import type { ChatArtifact, ChatCitation } from '@/types/workbenchChat'
import './workbenchArtifacts.css'
defineProps<{ text: string; files: ChatArtifact[]; citations?: ChatCitation[] }>()
const emit = defineEmits<{ preview: [file: ChatArtifact]; download: [file: ChatArtifact]; reference: [citation: ChatCitation] }>()
</script>
