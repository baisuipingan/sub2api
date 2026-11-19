<template><span class="bso-file-type-icon" :class="`bso-file-${type.family}`" :title="type.label" aria-hidden="true"><Icon name="document" size="lg" /><span>{{ type.short }}</span></span></template>
<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { fileType } from './filePresentation'
import type { ChatArtifact } from '@/types/workbenchChat'
const props = defineProps<{ file: ChatArtifact }>()
const type = computed(() => fileType(props.file))
</script>
