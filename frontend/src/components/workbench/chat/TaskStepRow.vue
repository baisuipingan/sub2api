<template>
  <div class="bso-task-step" data-component="TaskStepRow" :data-task-id="episode.task?.id">
    <DeepStepGroup v-if="episode.steps.length" :episode="episode" />
  </div>
</template>
<script setup lang="ts">
import DeepStepGroup from './DeepStepGroup.vue'
import type { ExecutionEpisode } from './taskPresentation'
// The checklist owns plan names. A task contributes only execution the server
// explicitly attributed to it; empty planned tasks never become fake steps.
defineProps<{ episode: ExecutionEpisode }>()
</script>
