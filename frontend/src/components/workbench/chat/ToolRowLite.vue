<template>
  <div class="bso-tool-row" :data-state="step.state">
    <div class="bso-tool-rail" aria-hidden="true"><Icon :name="icon" :class="{ 'bs-chat-spin': step.state === 'running' }" size="sm" /></div>
    <div class="bso-tool-content">
      <button v-if="step.message" type="button" class="bso-tool-trigger" :aria-expanded="open" @click="open = !open"><span>{{ step.title }}</span><span class="bso-tool-state">{{ stepStatus(step) }}</span><Icon name="chevronDown" class="bso-fold-arrow" :class="{ open }" size="sm" /></button>
      <div v-else class="bso-tool-trigger"><span>{{ step.title }}</span><span class="bso-tool-state">{{ stepStatus(step) }}</span></div>
      <div v-if="open && step.message" class="bso-tool-detail">{{ step.message }}</div>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { stepStatus, type ExecutionStep } from './taskPresentation'
const props = defineProps<{ step: ExecutionStep }>()
const open = ref(false)
const icon = computed(() => props.step.state === 'running' ? 'refresh' : props.step.state === 'failed' ? 'exclamationCircle' : /web_|search/.test(props.step.name) ? 'search' : /write|edit/.test(props.step.name) ? 'edit' : props.step.state === 'ended' || props.step.state === 'paused' ? 'clock' : 'document')
</script>
