<template>
  <section class="bs-chat-clarify" aria-label="补充任务信息">
    <header><strong>{{ request.title || '请补充一些信息' }}</strong><button type="button" class="bs-chat-icon" aria-label="跳过剩余问题" :disabled="busy" @click="finish"><Icon name="x" size="sm" /></button></header>
    <template v-if="question">
      <div class="bs-chat-clarify-question"><strong>{{ question.question }}</strong><span v-if="question.options.length">{{ question.multiple ? '多选' : '单选' }}</span><div v-if="request.questions.length > 1" class="bs-chat-question-pages"><button type="button" class="bs-chat-icon" :disabled="page === 0 || busy" aria-label="上一问" @click="page--"><Icon name="chevronLeft" size="sm" /></button>{{ page + 1 }}/{{ request.questions.length }}<button type="button" class="bs-chat-icon" :disabled="page === request.questions.length - 1 || busy" aria-label="下一问" @click="page++"><Icon name="chevronRight" size="sm" /></button></div></div>
      <ol class="bs-chat-clarify-options">
        <li v-for="(option, i) in question.options" :key="i"><button type="button" :class="{ selected: selected.includes(option) }" :disabled="busy" @click="select(option)"><span>{{ i + 1 }}.</span><span>{{ parseOption(option).title }}<small v-if="parseOption(option).description" class="bs-chat-option-description">{{ parseOption(option).description }}</small></span><Icon v-if="selected.includes(option)" name="check" size="sm" /></button></li>
        <li class="bs-chat-custom-answer" :class="{ selected: customActive[question.id] }"><span>{{ question.options.length + 1 }}.</span><TextArea v-model="custom[question.id]" :rows="1" :maxlength="2000" :disabled="busy" :placeholder="customActive[question.id] ? '自行输入（Shift+Enter 换行）' : '自行输入'" aria-label="自定义回答" @focus="activateCustom" @keydown="customKey" @input="resizeCustom" /></li>
      </ol>
    </template>
    <footer><span v-if="error" class="bs-chat-small-error" role="alert">{{ error }}</span><button v-if="page < request.questions.length - 1" type="button" class="bs-chat-text-button" :disabled="busy" @click="skip">跳过 <Icon name="arrowRight" size="sm" /></button><button v-if="page === request.questions.length - 1 || hasAnswer && (question?.multiple || customActive[question?.id || ''])" type="button" class="bs-chat-text-button" :disabled="busy" @click="next">{{ busy ? '正在提交…' : '确定' }}<Icon name="arrowRight" size="sm" /></button></footer>
  </section>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import TextArea from '@/components/common/TextArea.vue'
import type { ChatClarification, ChatClarificationAnswer } from '@/types/workbenchChat'
const props = defineProps<{ request: ChatClarification; busy?: boolean; error?: string }>()
const emit = defineEmits<{ submit: [answers: ChatClarificationAnswer[]] }>()
const page = ref(0), answers = ref<Record<string, string[]>>({}), custom = ref<Record<string, string>>({}), customActive = ref<Record<string, boolean>>({})
const question = computed(() => props.request.questions[page.value])
const selected = computed(() => answers.value[question.value?.id || ''] || [])
const hasAnswer = computed(() => selected.value.length > 0 || !!customActive.value[question.value?.id || ''] && !!custom.value[question.value?.id || '']?.trim())
function parseOption(value: string) { const match = value.match(/^([^(]+)\s*(\([^)]+\))$/); return { title: match?.[1]?.trim() || value, description: match?.[2]?.trim() || '' } }
function activateCustom() { const q = question.value; if (!q) return; customActive.value[q.id] = true; if (!q.multiple) answers.value[q.id] = [] }
function customKey(event: KeyboardEvent) { if (event.key === 'Enter' && !event.shiftKey && !event.isComposing && event.keyCode !== 229 && custom.value[question.value?.id || '']?.trim()) { event.preventDefault(); next() } }
function resizeCustom(event: Event) { const area = event.target as HTMLTextAreaElement; area.style.height = 'auto'; area.style.height = `${Math.min(area.scrollHeight, 160)}px` }
function select(option: string) { const q = question.value; if (!q) return; if (!q.multiple) { customActive.value[q.id] = false; custom.value[q.id] = '' } answers.value[q.id] = q.multiple ? selected.value.includes(option) ? selected.value.filter(v => v !== option) : [...selected.value, option] : [option]; if (!q.multiple) next() }
function next() { if (page.value < props.request.questions.length - 1) page.value++; else finish() }
function skip() { const q = question.value; if (q) { answers.value[q.id] = []; custom.value[q.id] = ''; customActive.value[q.id] = false } next() }
function finish() { emit('submit', props.request.questions.map(q => ({ question_id: q.id, values: [...(answers.value[q.id] || []), ...(customActive.value[q.id] && custom.value[q.id]?.trim() ? [custom.value[q.id].trim()] : [])] }))) }
</script>
