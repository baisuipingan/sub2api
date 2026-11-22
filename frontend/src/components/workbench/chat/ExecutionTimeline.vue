<template>
  <div v-if="episodes.length" class="bso-execution-timeline" data-component="ExecutionTimeline">
    <template v-for="(episode, index) in episodes" :key="`${episode.id}:${index}`">
      <TaskStepRow v-if="episode.task" :episode="episode" />
      <DeepStepGroup v-else :episode="episode" />
    </template>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import DeepStepGroup from './DeepStepGroup.vue'
import TaskStepRow from './TaskStepRow.vue'
import { executionEpisodes } from './taskPresentation'
import type { ChatEvent, ChatRun } from '@/types/workbenchChat'
const props = defineProps<{ events: ChatEvent[]; run: ChatRun }>()
const episodes = computed(() => executionEpisodes(props.events, props.run))
</script>
