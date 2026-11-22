<template>
  <section class="bso-deep-group" data-component="DeepStepGroup">
    <button type="button" class="bso-group-trigger" :class="{ 'is-open': open, 'is-live': episode.active }" :aria-expanded="open" @click="toggle">
      <Icon :name="episode.active && !open ? 'refresh' : writing ? 'edit' : 'clipboard'" :class="{ 'bs-chat-spin': episode.active && !open }" size="sm" />
      <span>{{ activity || (episode.active ? '正在执行任务' : '任务过程') }}</span>
      <Icon name="chevronDown" class="bso-fold-arrow" :class="{ open }" size="sm" />
    </button>
    <div class="bso-group-collapse" :class="{ open }" :inert="!open"><div><div class="bso-group-body"><ToolRowLite v-for="step in episode.steps" :key="step.id" :step="step" /></div></div></div>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import ToolRowLite from './ToolRowLite.vue'
import { episodeActivity, type ExecutionEpisode } from './taskPresentation'
import { useExecutionCollapse } from './useExecutionCollapse'
const props = defineProps<{ episode: ExecutionEpisode }>()
// Like the source, live and historical episodes both start collapsed. Changing
// a tool's running flag never toggles the user's fold or invents reasoning text.
const { open, toggle } = useExecutionCollapse(computed(() => props.episode.steps[0]?.id || ''))
const activity = computed(() => episodeActivity(props.episode.steps))
const writing = computed(() => props.episode.steps.some(step => /write_file|edit_file|execute_office_python/.test(step.name)))
</script>
