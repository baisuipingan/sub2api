<template>
  <div class="bso-workspace-list" data-component="WorkspaceFileList">
    <div v-if="loading" class="bso-workspace-skeleton" role="status" aria-label="正在加载文件"><Skeleton v-for="index in 3" :key="index" :height="48" /></div>
    <div v-else-if="loadFailed" class="bso-workspace-empty"><Icon name="exclamationCircle" size="xl" /><p>文件列表加载失败，请重新打开工作区。</p></div>
    <template v-else-if="files.length">
      <section v-for="group in groups" :key="group.id" class="bso-file-group" :aria-label="group.title">
        <h3><Icon :name="group.id === 'output' ? 'clipboard' : 'document'" size="sm" />{{ group.title }}<span>{{ group.files.length }}</span></h3>
        <div v-for="item in group.files" :key="item.version_id" class="bso-workspace-file-row">
          <button type="button" class="bso-file-open" @click="emit('preview', item)"><WorkspaceFileIcon :file="item" /><span class="bso-file-info"><strong :title="item.name">{{ item.name }}</strong><small>{{ fileType(item).label }}<span>·</span>{{ fileSize(item.bytes) }}</small></span></button>
          <button type="button" class="bso-file-save" :disabled="downloading" :aria-label="`下载${item.name}`" @click="emit('download', item)"><Icon name="download" size="sm" /><span>下载</span></button>
        </div>
      </section>
    </template>
    <div v-else class="bso-workspace-empty"><span class="bso-empty-file-illustration" aria-hidden="true"><Icon name="document" size="xl" /></span><strong>还没有文件</strong><p>对话中上传的资料和生成的成果会显示在这里。</p></div>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import Skeleton from '@/components/common/Skeleton.vue'
import WorkspaceFileIcon from './WorkspaceFileIcon.vue'
import { fileSize, fileType, isOutput } from './filePresentation'
import type { ChatArtifact } from '@/types/workbenchChat'
const props = defineProps<{ files: ChatArtifact[]; loading?: boolean; loadFailed?: boolean; downloading?: boolean }>()
const emit = defineEmits<{ preview: [file: ChatArtifact]; download: [file: ChatArtifact] }>()
const groups = computed(() => [
  { id: 'input', title: '参考资料', files: props.files.filter(file => !isOutput(file)) },
  { id: 'output', title: '生成成果', files: props.files.filter(isOutput) },
].filter(group => group.files.length))
</script>
