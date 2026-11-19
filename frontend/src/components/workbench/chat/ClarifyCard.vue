<template>
  <section ref="card" class="bs-chat-clarify bso-clarify" aria-label="补充任务信息">
    <header><strong>{{ request.title || '请补充一些信息' }}</strong><button type="button" class="bs-chat-icon" aria-label="跳过剩余问题" :disabled="disabled" @click="finish"><Icon name="x" size="sm" /></button></header>
    <template v-if="question">
      <div class="bs-chat-clarify-question"><strong>{{ question.question }}</strong><span v-if="question.options.length">{{ question.multiple ? '多选' : '单选' }}</span><div v-if="request.questions.length > 1" class="bs-chat-question-pages"><button type="button" class="bs-chat-icon" :disabled="page === 0 || disabled" aria-label="上一问" @click="page--"><Icon name="chevronLeft" size="sm" /></button>{{ page + 1 }}/{{ request.questions.length }}<button type="button" class="bs-chat-icon" :disabled="isLast || disabled" aria-label="下一问" @click="page++"><Icon name="chevronRight" size="sm" /></button></div></div>
      <ol class="bs-chat-clarify-options">
        <li v-for="(option, i) in question.options" :key="i"><button type="button" :class="{ selected: selected.includes(option) }" :disabled="disabled" @click="select(option)"><span>{{ i + 1 }}.</span><span>{{ parseOption(option).title }}<small v-if="parseOption(option).description" class="bs-chat-option-description">{{ parseOption(option).description }}</small></span><Icon v-if="selected.includes(option)" name="check" size="sm" /></button></li>
        <li class="bs-chat-custom-answer" :class="{ selected: customActive[question.id] }"><span>{{ question.options.length + 1 }}.</span><TextArea v-model="custom[question.id]" :rows="1" :maxlength="2000" :disabled="disabled" :placeholder="customActive[question.id] ? '自行输入（Shift+Enter 换行）' : '自行输入'" aria-label="自定义回答" @focus="activateCustom" @keydown="customKey" @input="resizeCustom" /><button v-if="!question.multiple && customActive[question.id] && custom[question.id]?.trim()" type="button" class="bso-custom-confirm" :disabled="disabled" @click="next">确定<span class="bso-return-hint" aria-hidden="true">↵</span></button></li>
      </ol>
    </template>
    <footer><span v-if="error" class="bs-chat-small-error" role="alert">{{ error }}</span><button v-if="!isLast" type="button" class="bs-chat-text-button" :disabled="disabled" @click="skip">跳过<Icon v-if="!question?.multiple" name="arrowRight" size="sm" /></button><span v-if="!isLast && footerConfirm" class="bso-footer-divider" aria-hidden="true" /><button v-if="footerConfirm" type="button" class="bs-chat-text-button" :class="{ 'is-last': isLast }" :disabled="disabled" @click="next">{{ busy ? '正在提交…' : isLast ? '确定' : '下一题' }}<span class="bso-return-hint" aria-hidden="true">↵</span></button></footer>
  </section>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import TextArea from '@/components/common/TextArea.vue'
import type { ChatClarification, ChatClarificationAnswer } from '@/types/workbenchChat'
import './workbenchArtifacts.css'
const props = defineProps<{ request: ChatClarification; busy?: boolean; error?: string }>()
const emit = defineEmits<{ submit: [answers: ChatClarificationAnswer[]] }>()
const page = ref(0), answers = ref<Record<string, string[]>>({}), custom = ref<Record<string, string>>({}), customActive = ref<Record<string, boolean>>({})
const card = ref<HTMLElement>(), submitted = ref(false)
const disabled = computed(() => !!props.busy || submitted.value)
const question = computed(() => props.request.questions[page.value])
const selected = computed(() => answers.value[question.value?.id || ''] || [])
const hasAnswer = computed(() => selected.value.length > 0 || !!customActive.value[question.value?.id || ''] && !!custom.value[question.value?.id || '']?.trim())
const isLast = computed(() => page.value >= props.request.questions.length - 1)
const footerConfirm = computed(() => isLast.value || hasAnswer.value && !!question.value?.multiple)
function parseOption(value: string) { const match = value.match(/^([^(]+)\s*(\([^)]+\))$/); return { title: match?.[1]?.trim() || value, description: match?.[2]?.trim() || '' } }
function activateCustom() { const q = question.value; if (!q || disabled.value) return; customActive.value[q.id] = true; if (!q.multiple) answers.value[q.id] = [] }
function customKey(event: KeyboardEvent) { if (event.key === 'Enter' && !event.shiftKey && !event.isComposing && event.keyCode !== 229 && custom.value[question.value?.id || '']?.trim()) { event.preventDefault(); next() } }
function resizeCustom(event: Event) { const area = event.target as HTMLTextAreaElement; area.style.height = 'auto'; area.style.height = `${Math.min(area.scrollHeight, 160)}px` }
function select(option: string) { const q = question.value; if (!q || disabled.value) return; if (!q.multiple) { customActive.value[q.id] = false; custom.value[q.id] = '' } answers.value[q.id] = q.multiple ? selected.value.includes(option) ? selected.value.filter(v => v !== option) : [...selected.value, option] : [option]; if (!q.multiple) next() }
function next() { if (disabled.value) return; if (!isLast.value) page.value++; else finish() }
function skip() { if (disabled.value) return; const q = question.value; if (q) { answers.value[q.id] = []; custom.value[q.id] = ''; customActive.value[q.id] = false } next() }
function finish() { if (disabled.value) return; submitted.value = true; emit('submit', props.request.questions.map(q => ({ question_id: q.id, values: [...(answers.value[q.id] || []), ...(customActive.value[q.id] && custom.value[q.id]?.trim() ? [custom.value[q.id].trim()] : [])] }))) }
function confirmKey(event: KeyboardEvent) {
  if (event.defaultPrevented || event.key !== 'Enter' || event.shiftKey || event.isComposing || event.keyCode === 229 || disabled.value || !footerConfirm.value) return
  const target = document.activeElement as HTMLElement | null
  if (target?.matches('input, textarea, select, button') || target?.isContentEditable) return
  event.preventDefault(); next()
}
watch(page, async () => { await nextTick(); const area = card.value?.querySelector('textarea'); if (area) { area.style.height = 'auto'; area.style.height = `${Math.min(area.scrollHeight, 160)}px` } })
watch(() => props.request.request_id, () => { page.value = 0; answers.value = {}; custom.value = {}; customActive.value = {}; submitted.value = false })
// Keep successful submissions locked until the waiting request disappears;
// a failed request explicitly reopens the card for a deliberate retry.
watch([() => props.busy, () => props.error], () => { if (!props.busy && props.error) submitted.value = false })
onMounted(() => window.addEventListener('keydown', confirmKey))
onBeforeUnmount(() => window.removeEventListener('keydown', confirmKey))
</script>
