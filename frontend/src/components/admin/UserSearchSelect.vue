<template>
  <div ref="container" class="relative w-full">
    <label :for="inputId" class="input-label mb-1.5 block">{{ label || t('admin.usage.userFilter') }}</label>
    <div class="relative">
      <input
        :id="inputId"
        ref="input"
        :value="keyword"
        type="text"
        autocomplete="off"
        role="combobox"
        aria-autocomplete="list"
        :aria-expanded="open"
        :aria-controls="listId"
        :aria-activedescendant="open && activeIndex >= 0 ? `${listId}-${activeIndex}` : undefined"
        :aria-describedby="error ? `${inputId}-error` : undefined"
        :aria-invalid="Boolean(error)"
        :placeholder="placeholder || t('admin.usage.searchUserPlaceholder')"
        class="input w-full pr-10"
        :class="{ 'input-error ring-2 ring-red-500/20': error }"
        @input="onInput"
        @focus="open = true"
        @keydown="onKeydown"
        @compositionstart="composing = true"
        @compositionend="onCompositionEnd"
      />
      <button
        v-if="keyword || modelValue !== undefined"
        type="button"
        :aria-label="t('common.clear')"
        class="absolute inset-y-0 right-0 flex items-center px-3 text-gray-400 hover:text-gray-600 dark:hover:text-dark-200"
        @mousedown.prevent
        @click="clear"
      ><Icon name="x" size="sm" /></button>
    </div>
    <p v-if="error" :id="`${inputId}-error`" class="input-error-text mt-1.5">{{ error }}</p>
    <div
      v-if="open && (keyword.trim() || results.length)"
      :id="listId"
      role="listbox"
      :aria-label="label || t('admin.usage.userFilter')"
      class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-800"
    >
      <p v-if="loading" role="status" class="px-4 py-2 text-sm text-gray-500">{{ t('common.loading') }}</p>
      <p v-else-if="searchFailed" role="status" class="px-4 py-2 text-sm text-gray-500">{{ t('admin.workbench.filters.userSearchFailed') }}</p>
      <p v-else-if="searched && !results.length" role="status" class="px-4 py-2 text-sm text-gray-500">{{ t('admin.workbench.filters.noUsers') }}</p>
      <button
        v-for="(user, index) in results"
        v-else
        :id="`${listId}-${index}`"
        :key="user.id"
        type="button"
        role="option"
        :aria-selected="modelValue === user.id"
        class="flex w-full items-center justify-between gap-2 px-4 py-2 text-left hover:bg-gray-100 dark:hover:bg-dark-700"
        :class="{ 'bg-gray-100 dark:bg-dark-700': index === activeIndex }"
        @mousedown.prevent
        @mouseenter="activeIndex = index"
        @click="select(user)"
      >
        <span class="min-w-0 break-words">
          <span v-if="user.username" class="block text-sm font-medium">{{ user.username }}</span>
          <span :class="user.username ? 'text-xs text-gray-500' : 'text-sm'">{{ user.email }}</span>
          <span v-if="user.deleted" class="ml-1 text-xs text-gray-400">（{{ t('admin.usage.userDeletedBadge') }}）</span>
        </span>
        <span class="shrink-0 text-xs text-gray-400">#{{ user.id }}</span>
      </button>
    </div>
  </div>
</template>

<script lang="ts">
let userSearchInstance = 0
</script>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { searchUsers, type SimpleUser } from '@/api/admin/usage'
import { getById } from '@/api/admin/users'

type SearchUser = SimpleUser & { username?: string }
const props = defineProps<{ modelValue: number | undefined; id?: string; label?: string; placeholder?: string; error?: string }>()
const emit = defineEmits<{
  (e: 'update:modelValue', value: number | undefined): void
  (e: 'change', user: SearchUser | undefined): void
}>()
const { t } = useI18n()
const instance = ++userSearchInstance
const inputId = computed(() => props.id || `admin-user-search-${instance}`)
const listId = `admin-user-search-results-${instance}`
const container = ref<HTMLElement | null>(null)
const input = ref<HTMLInputElement | null>(null)
const keyword = ref('')
const results = ref<SearchUser[]>([])
const open = ref(false)
const loading = ref(false)
const searched = ref(false)
const searchFailed = ref(false)
const activeIndex = ref(-1)
const unselectedQuery = ref(false)
let composing = false
let disposed = false
let sequence = 0
let timer: ReturnType<typeof setTimeout> | null = null
let ownUpdate: { value: number | undefined } | null = null

function invalidate() {
  if (timer) clearTimeout(timer)
  timer = null
  sequence++
  loading.value = false
}
function update(value: number | undefined) {
  ownUpdate = props.modelValue !== value ? { value } : null
  emit('update:modelValue', value)
}
function labelFor(user: { id: number; email?: string; username?: string }) {
  return [user.username?.trim(), user.email?.trim()].filter(Boolean).join(' · ') || `#${user.id}`
}
function search() {
  invalidate()
  results.value = []
  activeIndex.value = -1
  searched.value = false
  searchFailed.value = false
  const query = keyword.value.trim()
  if (!query) return
  loading.value = true
  const requested = sequence
  timer = setTimeout(async () => {
    timer = null
    try {
      const found = await searchUsers(query)
      if (disposed || requested !== sequence) return
      results.value = [...found].sort((a, b) => Number(a.deleted) - Number(b.deleted))
      searched.value = true
    } catch {
      if (disposed || requested !== sequence) return
      results.value = []
      searchFailed.value = true
    } finally {
      if (!disposed && requested === sequence) loading.value = false
    }
  }, 300)
}
function onInput(event: Event) {
  keyword.value = (event.target as HTMLInputElement).value
  unselectedQuery.value = Boolean(keyword.value.trim())
  update(undefined)
  open.value = true
  if (composing) { invalidate(); results.value = []; return }
  search()
}
function onCompositionEnd() { composing = false; search() }
function select(user: SearchUser) {
  invalidate()
  keyword.value = labelFor(user)
  unselectedQuery.value = false
  results.value = []
  activeIndex.value = -1
  open.value = false
  update(user.id)
  emit('change', user)
}
function clear() {
  invalidate()
  keyword.value = ''
  unselectedQuery.value = false
  results.value = []
  searched.value = false
  searchFailed.value = false
  open.value = false
  activeIndex.value = -1
  update(undefined)
  emit('change', undefined)
}
function onKeydown(event: KeyboardEvent) {
  if (composing || event.isComposing) return
  if (event.key === 'Escape') { open.value = false; event.preventDefault(); return }
  if (event.key === 'Tab') { open.value = false; return }
  if (event.key === 'Enter' && open.value) {
    // A free text query is never converted into a user ID or submitted as a filter.
    event.preventDefault()
    if (activeIndex.value >= 0 && results.value[activeIndex.value]) select(results.value[activeIndex.value]!)
    return
  }
  if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
  event.preventDefault()
  open.value = true
  if (!results.value.length) return
  activeIndex.value = event.key === 'ArrowDown'
    ? (activeIndex.value + 1) % results.value.length
    : (activeIndex.value <= 0 ? results.value.length : activeIndex.value) - 1
  void nextTick(() => document.getElementById(`${listId}-${activeIndex.value}`)?.scrollIntoView?.({ block: 'nearest' }))
}
function outside(event: MouseEvent) {
  if (!container.value?.contains(event.target as Node)) open.value = false
}
watch(() => props.modelValue, async (value) => {
  if (ownUpdate && ownUpdate.value === value) { ownUpdate = null; return }
  ownUpdate = null
  invalidate()
  results.value = []
  open.value = false
  activeIndex.value = -1
  unselectedQuery.value = false
  if (value === undefined) { keyword.value = ''; return }
  keyword.value = `#${value}`
  const requested = sequence
  try {
    const user = await getById(value, true)
    if (disposed || requested !== sequence || props.modelValue !== value) return
    keyword.value = labelFor(user)
  } catch {
    // Keep the exact ID from old links when the label is unavailable.
  }
}, { immediate: true })
onMounted(() => document.addEventListener('click', outside))
onBeforeUnmount(() => { disposed = true; invalidate(); document.removeEventListener('click', outside) })
defineExpose({ hasUnselectedQuery: () => unselectedQuery.value, clear })
</script>
