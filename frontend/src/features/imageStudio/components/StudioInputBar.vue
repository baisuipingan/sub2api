<template>
  <div
    class="relative flex min-h-0 flex-col"
    :class="
      embedded
        ? 'lg:h-full'
        : 'mx-auto w-full max-w-3xl rounded-2xl border border-gray-200 bg-white p-4 shadow-sm dark:border-white/[0.1] dark:bg-white/[0.03]'
    "
    @dragenter.prevent="isDragging = true"
    @dragover.prevent
    @dragleave="onDragLeave"
    @drop="handleDrop"
  >
    <StudioDragUploadOverlay
      :visible="isDragging"
      :at-image-limit="inputImages.length >= maxInputImages"
      :max-images="maxInputImages"
    />

    <div
      class="space-y-5"
      :class="embedded ? 'px-4 py-4 lg:min-h-0 lg:flex-1 lg:overflow-y-auto' : 'pb-4'"
    >
      <!-- 提示词编辑区 -->
      <div>
        <label
          for="image-prompt"
          class="mb-2 block text-sm font-medium text-gray-700 dark:text-dark-200"
        >
          {{ t('imageStudio.input.promptLabel') }}
        </label>

        <div
          data-prompt-composer
          class="rounded-lg border border-gray-200 bg-gray-50 transition focus-within:border-primary-500 focus-within:ring-2 focus-within:ring-primary-500/15 dark:border-white/[0.08] dark:bg-black/10"
        >
          <div class="relative grid">
            <!-- @ 提及参考图菜单 -->
            <div
              v-if="showMentionMenu"
              class="absolute top-full z-30 mt-2 w-64 overflow-hidden rounded-xl border border-gray-200 bg-white p-1.5 shadow-xl dark:border-white/[0.1] dark:bg-dark-900"
              :style="{ left: `${mentionMenuLeft}px` }"
              role="listbox"
              :aria-label="t('imageStudio.input.mentionAria')"
            >
              <p class="px-2 pb-1 pt-0.5 text-[11px] text-gray-400">
                {{ t('imageStudio.input.mentionHint') }}
              </p>
              <div class="max-h-56 overflow-y-auto">
                <button
                  v-for="(option, optionIndex) in mentionOptions"
                  :key="option.image.id"
                  type="button"
                  role="option"
                  class="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-xs transition"
                  :class="
                    optionIndex === mentionMenuIndex
                      ? 'bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300'
                      : 'text-gray-700 hover:bg-gray-50 dark:text-dark-300 dark:hover:bg-white/[0.06]'
                  "
                  :aria-selected="optionIndex === mentionMenuIndex"
                  @mousedown.prevent="selectMention(option.imageIndex)"
                  @mouseenter="mentionMenuIndex = optionIndex"
                >
                  <span
                    class="h-9 w-9 shrink-0 overflow-hidden rounded-md border border-gray-200 bg-gray-100 dark:border-white/[0.08]"
                  >
                    <img :src="option.image.dataUrl" alt="" class="h-full w-full object-cover" />
                  </span>
                  <span class="font-medium">{{ option.label }}</span>
                </button>
              </div>
            </div>

            <div
              id="image-prompt"
              ref="editorRef"
              contenteditable="true"
              role="textbox"
              aria-multiline="true"
              aria-autocomplete="list"
              :aria-label="t('imageStudio.input.promptAria')"
              class="col-start-1 row-start-1 max-h-80 min-h-40 w-full overflow-y-auto whitespace-pre-wrap break-words rounded-lg px-3 py-3 pr-10 text-sm leading-6 text-gray-900 outline-none dark:text-white"
              @input="onEditorInput"
              @keydown="handleKeyDown"
              @paste="onPaste"
              @copy="onCopy"
              @click="onEditorClick"
            />

            <p
              v-if="!visiblePrompt"
              class="pointer-events-none col-start-1 row-start-1 px-3 py-3 pr-10 text-sm leading-6 text-gray-400 dark:text-dark-500"
            >
              {{ t('imageStudio.input.placeholder') }}
            </p>

            <button
              v-if="visiblePrompt"
              type="button"
              class="absolute right-2 top-2 rounded-md p-1.5 text-gray-400 transition hover:bg-gray-200 hover:text-gray-700 dark:hover:bg-white/[0.08] dark:hover:text-dark-200"
              :aria-label="t('imageStudio.input.clearPrompt')"
              :title="t('imageStudio.input.clearPrompt')"
              @click="clearPrompt"
            >
              <StudioIcon name="CloseIcon" class="h-4 w-4" />
            </button>
          </div>
        </div>
      </div>

      <!-- 参考图 -->
      <div v-if="capabilities.referenceImages || inputImages.length > 0">
        <h3 class="mb-2 text-sm font-medium text-gray-700 dark:text-dark-200">
          {{ t('imageStudio.input.referenceTitle') }}<span class="ml-2 text-xs font-normal text-gray-400">{{
            t('imageStudio.input.optional')
          }}</span>
        </h3>

        <div
          v-if="inputImages.length > 0"
          class="mb-2 flex items-center justify-between gap-3 px-0.5"
        >
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('imageStudio.input.referenceCount', {
              count: inputImages.length,
              max: maxInputImages || inputImages.length,
            }) }}
            · {{ inputImages.length === 1 ? t('imageStudio.input.autoReference') : t('imageStudio.input.useAtHint') }}
          </span>
          <button
            type="button"
            class="text-xs text-gray-400 transition hover:text-red-500"
            @click="handleClearImages"
          >
            {{ t('imageStudio.input.clearAll') }}
          </button>
        </div>

        <div class="flex gap-2 overflow-x-auto pb-0.5">
          <div
            v-for="(image, index) in inputImages"
            :key="image.id"
            class="group relative h-[4.5rem] w-[4.5rem] shrink-0 overflow-hidden rounded-lg border border-gray-200 bg-gray-100 dark:border-white/[0.1] dark:bg-black/20"
          >
            <button
              type="button"
              class="h-full w-full"
              :aria-label="t('imageStudio.input.viewReferenceN', { index: index + 1 })"
              @click="state.setLightboxImageId(image.id, inputImages.map((item) => item.id))"
            >
              <img
                :src="image.dataUrl"
                :data-image-id="image.id"
                alt=""
                class="h-full w-full object-cover"
              />
            </button>

            <button
              type="button"
              class="absolute bottom-1 left-1 rounded bg-primary-600/90 px-1.5 py-0.5 text-[10px] font-medium text-white transition hover:bg-primary-700"
              :aria-label="t('imageStudio.input.insertMentionN', { index: index + 1 })"
              :title="t('imageStudio.input.insertToPrompt')"
              @click="insertMentionAtCursor(index)"
            >
              {{ getImageMentionLabel(index) }}
            </button>

            <button
              v-if="capabilities.mask && index === 0"
              type="button"
              class="absolute bottom-1 right-1 rounded p-1 text-white transition hover:bg-primary-600"
              :class="maskDraft?.targetImageId === image.id ? 'bg-primary-600' : 'bg-black/60'"
              :aria-label="t('imageStudio.input.editFirstMask')"
              :title="t('imageStudio.lightbox.editMask')"
              @click="state.setMaskEditorImageId(image.id)"
            >
              <StudioIcon name="EditIcon" class="h-3 w-3" />
            </button>

            <button
              type="button"
              class="absolute right-1 top-1 rounded-full bg-black/65 p-1 text-white opacity-100 transition sm:opacity-0 sm:group-hover:opacity-100"
              :aria-label="t('imageStudio.input.removeReferenceN', { index: index + 1 })"
              :title="t('imageStudio.input.removeReference')"
              @click="removeImage(index)"
            >
              <StudioIcon name="CloseIcon" class="h-3 w-3" />
            </button>
          </div>

          <button
            v-if="capabilities.referenceImages && inputImages.length < maxInputImages"
            type="button"
            :data-reference-upload-empty="inputImages.length ? undefined : 'true'"
            :class="
              inputImages.length
                ? 'flex h-[4.5rem] min-w-[4.5rem] shrink-0 flex-col items-center justify-center gap-1 rounded-lg border border-dashed border-gray-300 px-2 text-xs text-gray-500 transition hover:border-primary-400 hover:bg-primary-50 hover:text-primary-600 dark:border-white/[0.14] dark:text-dark-400'
                : 'group flex min-h-24 w-full items-center justify-center gap-3 rounded-xl border border-dashed border-primary-200 bg-primary-50/30 px-4 py-4 text-left transition hover:border-primary-400 hover:bg-primary-50/70 dark:border-primary-400/20 dark:bg-primary-500/[0.04]'
            "
            :aria-label="t('imageStudio.input.addReference')"
            @click="fileInputRef?.click()"
          >
            <template v-if="inputImages.length">
              <StudioIcon name="PlusIcon" class="h-4 w-4" />
              {{ t('imageStudio.input.add') }}
            </template>
            <template v-else>
              <span
                class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-white text-gray-400 shadow-sm ring-1 ring-gray-200 transition group-hover:text-primary-500 dark:bg-white/[0.06] dark:ring-white/[0.08]"
              >
                <StudioIcon name="ImageUploadIcon" class="h-6 w-6" />
              </span>
              <span class="min-w-0">
                <span class="block text-sm font-medium text-gray-600 dark:text-dark-300">
                  {{ t('imageStudio.input.dropHint') }}
                  <span class="text-primary-600 dark:text-primary-400">{{ t('imageStudio.input.clickUpload') }}</span>
                </span>
                <span class="mt-1 block text-xs text-gray-400 dark:text-dark-500">
                  {{ t('imageStudio.input.uploadFormats') }}
                </span>
              </span>
            </template>
          </button>
        </div>
      </div>

      <input
        ref="fileInputRef"
        type="file"
        accept="image/*"
        multiple
        class="hidden"
        @change="onFilePick"
      />

      <StudioInputParamsPanel :params="params" :model="model" @set-params="state.setParams" />
    </div>

    <!-- 底部提交区 -->
    <div
      class="shrink-0 border-t border-gray-100 bg-white px-4 pb-[max(1rem,env(safe-area-inset-bottom))] pt-3 dark:border-white/[0.06] dark:bg-dark-950"
    >
      <div
        v-if="estimate"
        aria-live="polite"
        class="mb-3 grid grid-cols-2 rounded-lg bg-primary-50/70 px-3 py-2.5 dark:bg-primary-500/[0.08]"
      >
        <div class="min-w-0 pr-3">
          <span class="flex items-center gap-1.5 text-[11px] text-gray-500 dark:text-dark-400">
            <StudioIcon name="CreditIcon" class="h-3.5 w-3.5 text-primary-500" />
            {{ t('imageStudio.input.estimatePerImage') }}
          </span>
          <span
            class="mt-1 block text-sm font-medium tabular-nums text-gray-800 dark:text-dark-100"
          >≈ {{ estimate.creditsPerImage.toFixed(4) }}</span>
        </div>
        <div class="min-w-0 border-l border-primary-100 pl-3 dark:border-primary-400/15">
          <span class="flex items-center gap-1.5 text-[11px] text-gray-500 dark:text-dark-400">
            <StudioIcon name="ClockIcon" class="h-3.5 w-3.5 text-primary-500" />
            {{ t('imageStudio.input.estimateDuration') }}
          </span>
          <span
            class="mt-1 block text-sm font-medium tabular-nums text-gray-800 dark:text-dark-100"
          >{{ t(`imageStudio.input.${estimate.durationKey}`) }}</span>
        </div>
      </div>

      <button
        type="button"
        class="w-full rounded-lg px-4 py-3 text-sm font-medium text-white transition disabled:cursor-not-allowed disabled:opacity-50"
        :class="isGenerating ? 'bg-red-500 hover:bg-red-600' : 'bg-primary-600 hover:bg-primary-700'"
        :disabled="buttonDisabled"
        @click="handleSubmit"
      >
        {{ buttonLabel }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StudioIcon from './StudioIcon.vue'
import StudioDragUploadOverlay from './StudioDragUploadOverlay.vue'
import StudioInputParamsPanel from './StudioInputParamsPanel.vue'
import {
  storeState as state,
  addImageFromFile,
  deleteImageIfUnreferenced,
  stopTaskGeneration,
  submitTask,
} from '../stores/store'
import {
  getAtImageQuery,
  getImageMentionLabel,
  getPromptIndexFromVisibleIndex,
  getPromptMentionParts,
  getSelectedImageMentionLabel,
  imageMentionMatches,
  insertImageMentionAtVisibleRange,
  isCursorInSelectedImageMention,
  stripImageMentionMarkers,
} from '../lib/promptImageMentions'
import {
  getContentEditablePlainText,
  getContentEditableSelection,
  insertContentEditableMention,
  insertContentEditableText,
  setContentEditableSelection,
  syncMentionTagSelection,
} from '../lib/contentEditableMentions'
import { getImageModelCapabilities } from '../lib/imageModelCapabilities'
import { getGenerationEstimate } from '../lib/generationEstimate'
import { resolveUnitPrice } from '../lib/studioPricing'
import { getImageModel, getModelResolution } from '../lib/modelRegistry'

withDefaults(defineProps<{ embedded?: boolean }>(), { embedded: false })

const { t } = useI18n()

const fileInputRef = ref<HTMLInputElement | null>(null)
const editorRef = ref<HTMLDivElement | null>(null)
/** 标记这次 prompt 变化来自用户输入，避免回写编辑器导致光标跳动 */
const isUserInput = ref(false)

const isDragging = ref(false)
const cursorPosition = ref(0)
const mentionMenuIndex = ref(0)
const mentionMenuDismissed = ref(false)
const mentionMenuLeft = ref(12)

const prompt = computed(() => state.prompt)
const inputImages = computed(() => state.inputImages)
const params = computed(() => state.params)
const maskDraft = computed(() => state.maskDraft)

const model = computed(() => getImageModel(state.settings.selectedModelId, state.modelCatalog))
const capabilities = computed(() =>
  getImageModelCapabilities(state.settings.selectedModelId, state.modelCatalog),
)
const maxInputImages = computed(() => model.value?.capabilities.referenceImages ?? 0)

const runningTask = computed(() => state.tasks.find((task) => task.status === 'running'))
const isGenerating = computed(() => Boolean(runningTask.value))
const connected = computed(() => state.settings.miloRouter.connectionStatus === 'connected')
const checking = computed(() => state.settings.miloRouter.connectionStatus === 'checking')

const visiblePrompt = computed(() => stripImageMentionMarkers(prompt.value))

/**
 * 当前配置下的单张实价。
 *
 * 优先用后端算出的价格（已含分组单价与倍率），拿不到时回退内置值。
 * 依赖 state.imagePricing，所以价格一到就会自动刷新。
 */
const unitPrice = computed(() =>
  resolveUnitPrice(
    state.imagePricing,
    model.value,
    params.value.size,
    params.value.resolution,
  ),
)

const estimate = computed(() =>
  state.catalogStatus === 'ready' && model.value
    ? getGenerationEstimate(
        getModelResolution(model.value, params.value.size, params.value.resolution),
        unitPrice.value,
      )
    : null,
)

const buttonLabel = computed(() => {
  if (isGenerating.value) return t('imageStudio.input.stopGenerating')
  if (checking.value) return t('imageStudio.header.connecting')
  if (!connected.value) return t('imageStudio.input.connectAndGenerate')
  if (state.catalogStatus === 'loading') return t('imageStudio.input.loadingModels')
  if (state.catalogStatus !== 'ready') {
    // 余额不足这类问题点「重新获取」是没用的，直接把原因写在按钮上，
    // 避免用户反复点击困惑（他需要的是去充值，不是重试）。
    if (isBalanceIssue.value) return t('imageStudio.errors.insufficientBalance')
    return t('imageStudio.input.refetchModels')
  }
  if (!model.value) return t('imageStudio.input.pickModel')
  return t('imageStudio.input.generateCount', { count: params.value.n })
})

/** 模型取不到是不是因为余额不足（此时重试无意义，需要用户去充值） */
const isBalanceIssue = computed(() => {
  if (state.catalogStatus !== 'error') return false
  const message = String(state.catalogError ?? '')
  return /余额不足|insufficient/i.test(message)
})

const buttonDisabled = computed(() => {
  if (isGenerating.value) return false
  if (checking.value) return true
  // 余额不足时禁用主按钮：点它只会再次失败，正确动作是去充值
  if (isBalanceIssue.value) return true
  if (connected.value && state.catalogStatus === 'loading') return true
  if (connected.value && state.catalogStatus === 'ready') {
    return !model.value || !prompt.value.trim()
  }
  if (!connected.value) return !prompt.value.trim()
  return false
})

const mentionQuery = computed(() => {
  if (isCursorInSelectedImageMention(prompt.value, cursorPosition.value)) return null
  return getAtImageQuery(visiblePrompt.value, cursorPosition.value, inputImages.value.length)
})

const mentionOptions = computed(() => {
  const query = mentionQuery.value
  if (!query) return []
  return inputImages.value
    .map((image, imageIndex) => ({
      image,
      imageIndex,
      label: getImageMentionLabel(imageIndex),
    }))
    .filter((option) => imageMentionMatches(query.query, option.imageIndex))
})

const showMentionMenu = computed(
  () => !mentionMenuDismissed.value && mentionOptions.value.length > 0,
)

/* ── 文件处理 ───────────────────────────────────────── */

async function handleFiles(files: FileList | File[]) {
  const available = maxInputImages.value - inputImages.value.length
  if (available <= 0) {
    state.showToast(
      maxInputImages.value
        ? t('imageStudio.input.limitReached', { max: maxInputImages.value })
        : t('imageStudio.input.modelNoReference'),
      'error',
    )
    return
  }

  for (const file of Array.from(files).slice(0, available)) {
    try {
      await addImageFromFile(file)
    } catch (error) {
      state.showToast(
        error instanceof Error ? error.message : t('imageStudio.input.readFailed'),
        'error',
      )
    }
  }
}

function onFilePick(event: Event) {
  const input = event.target as HTMLInputElement
  if (input.files) void handleFiles(input.files)
  input.value = ''
}

function removeImage(index: number) {
  const image = inputImages.value[index]
  state.removeInputImage(index)
  if (image) void deleteImageIfUnreferenced(image.id)
}

function handleClearImages() {
  const imageIds = inputImages.value.map((image) => image.id)
  state.clearInputImages()
  imageIds.forEach((imageId) => void deleteImageIfUnreferenced(imageId))
}

function handleDrop(event: DragEvent) {
  event.preventDefault()
  isDragging.value = false
  if (event.dataTransfer?.files.length) void handleFiles(event.dataTransfer.files)
}

function onDragLeave(event: DragEvent) {
  if (event.currentTarget === event.target) isDragging.value = false
}

/* ── 提交 ──────────────────────────────────────────── */

function handleSubmit() {
  if (isGenerating.value && runningTask.value) {
    stopTaskGeneration(runningTask.value.id)
    return
  }
  if (connected.value && state.catalogStatus !== 'ready') {
    void state.refreshImageModelCatalog()
    return
  }
  void submitTask()
}

/* ── 编辑器同步 ─────────────────────────────────────── */

function syncPromptFromEditor() {
  const editor = editorRef.value
  if (!editor) return
  isUserInput.value = true
  const selection = getContentEditableSelection(editor)
  cursorPosition.value = selection.start
  syncMentionTagSelection(editor)
  state.setPrompt(getContentEditablePlainText(editor))
}

function onEditorInput() {
  syncPromptFromEditor()
  mentionMenuIndex.value = 0
  mentionMenuDismissed.value = false
}

function insertTextAtSelection(text: string) {
  const editor = editorRef.value
  if (editor && !text.includes('\n')) {
    editor.focus()
    if (insertContentEditableText(editor, text)) {
      syncPromptFromEditor()
      return
    }
  }

  const selection = editor
    ? getContentEditableSelection(editor)
    : { start: visiblePrompt.value.length, end: visiblePrompt.value.length }
  const promptStart = getPromptIndexFromVisibleIndex(prompt.value, selection.start)
  const promptEnd = getPromptIndexFromVisibleIndex(prompt.value, selection.end)
  state.setPrompt(
    `${prompt.value.slice(0, promptStart)}${text}${prompt.value.slice(promptEnd)}`,
  )

  const nextCursor = selection.start + text.length
  cursorPosition.value = nextCursor
  window.setTimeout(() => {
    if (!editorRef.value) return
    editorRef.value.focus()
    setContentEditableSelection(editorRef.value, nextCursor, nextCursor)
  }, 0)
}

function selectMention(imageIndex: number) {
  const editor = editorRef.value
  const cursor = editor
    ? getContentEditableSelection(editor).start
    : visiblePrompt.value.length
  const query = getAtImageQuery(visiblePrompt.value, cursor, inputImages.value.length)

  mentionMenuDismissed.value = true
  mentionMenuIndex.value = 0
  if (!query) return

  if (editor) {
    editor.focus()
    setContentEditableSelection(editor, query.start, cursor)
    if (
      insertContentEditableMention(
        editor,
        getImageMentionLabel(imageIndex),
        getSelectedImageMentionLabel(imageIndex),
      )
    ) {
      syncPromptFromEditor()
      return
    }
  }

  const next = insertImageMentionAtVisibleRange(
    prompt.value,
    query.start,
    cursor,
    imageIndex,
  )
  state.setPrompt(next.prompt)
  cursorPosition.value = next.cursor
}

function insertMentionAtCursor(imageIndex: number) {
  const editor = editorRef.value
  if (!editor) return

  const selection = window.getSelection()
  const range = selection?.rangeCount ? selection.getRangeAt(0) : null
  const cursor =
    range && editor.contains(range.commonAncestorContainer)
      ? getContentEditableSelection(editor).start
      : visiblePrompt.value.length

  editor.focus()
  setContentEditableSelection(editor, cursor, cursor)
  if (
    insertContentEditableMention(
      editor,
      getImageMentionLabel(imageIndex),
      getSelectedImageMentionLabel(imageIndex),
    )
  ) {
    syncPromptFromEditor()
  }
}

function clearPrompt() {
  state.setPrompt('')
  editorRef.value?.focus()
}

/* ── 键盘 ──────────────────────────────────────────── */

function handleKeyDown(event: KeyboardEvent) {
  // 输入法组合中不处理，避免打断中文输入
  if (event.isComposing) return

  if (showMentionMenu.value) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      mentionMenuIndex.value = (mentionMenuIndex.value + 1) % mentionOptions.value.length
      return
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      mentionMenuIndex.value =
        (mentionMenuIndex.value - 1 + mentionOptions.value.length) %
        mentionOptions.value.length
      return
    }
    if ((event.key === 'Enter' && !event.shiftKey) || event.key === 'Tab') {
      event.preventDefault()
      selectMention(
        mentionOptions.value[mentionMenuIndex.value]?.imageIndex ??
          mentionOptions.value[0].imageIndex,
      )
      return
    }
    if (event.key === 'Escape') {
      event.preventDefault()
      mentionMenuDismissed.value = true
      return
    }
  }

  if (event.key !== 'Enter') return
  event.preventDefault()
  insertTextAtSelection('\n')
}

/* ── 粘贴 / 复制 ───────────────────────────────────── */

function onPaste(event: ClipboardEvent) {
  const files = Array.from(event.clipboardData?.files ?? []).filter((file) =>
    file.type.startsWith('image/'),
  )
  if (files.length) {
    event.preventDefault()
    void handleFiles(files)
    return
  }

  const text = event.clipboardData?.getData('text/plain')
  if (!text) return
  event.preventDefault()
  insertTextAtSelection(text.replace(/\r\n?/g, '\n'))
}

function onCopy(event: ClipboardEvent) {
  const editor = editorRef.value
  if (!editor) return
  const selection = getContentEditableSelection(editor)
  if (selection.start === selection.end) return

  const promptStart = getPromptIndexFromVisibleIndex(prompt.value, selection.start)
  const promptEnd = getPromptIndexFromVisibleIndex(prompt.value, selection.end)
  event.preventDefault()
  event.clipboardData?.setData(
    'text/plain',
    stripImageMentionMarkers(prompt.value.slice(promptStart, promptEnd)),
  )
}

function onEditorClick(event: MouseEvent) {
  const target = event.target as HTMLElement
  if (target.classList.contains('mention-tag')) {
    const selection = window.getSelection()
    const range = document.createRange()
    range.selectNode(target)
    selection?.removeAllRanges()
    selection?.addRange(range)
  }
  if (editorRef.value) syncMentionTagSelection(editorRef.value)
}

/* ── 把 prompt 渲染进 contentEditable ────────────────── */

watch(
  [() => state.inputImages, () => state.prompt],
  async () => {
    const editor = editorRef.value
    if (!editor) return

    // 用户刚输入的内容不要回写，否则光标会跳
    if (isUserInput.value) {
      isUserInput.value = false
      return
    }

    await nextTick()
    const nodes: Node[] = getPromptMentionParts(prompt.value, inputImages.value).map((part) => {
      if (part.type === 'text') return document.createTextNode(part.text)
      const mention = document.createElement('span')
      mention.contentEditable = 'false'
      mention.className = 'mention-tag'
      mention.dataset.mentionText = getSelectedImageMentionLabel(part.imageIndex)
      mention.textContent = part.text
      return mention
    })

    // 末尾换行需要占位 br，否则看不见最后一行
    if (prompt.value.endsWith('\n')) {
      const sentinel = document.createElement('br')
      sentinel.dataset.sentinelBr = 'true'
      nodes.push(sentinel)
    }
    editor.replaceChildren(...nodes)
  },
)

/** 跟踪光标位置，用于 @ 菜单定位 */
function onSelectionChange() {
  const editor = editorRef.value
  const selection = window.getSelection()
  if (!editor || !selection || selection.rangeCount === 0) return

  const range = selection.getRangeAt(0)
  if (!editor.contains(range.commonAncestorContainer)) {
    syncMentionTagSelection(editor)
    return
  }

  const nextSelection = getContentEditableSelection(editor)
  cursorPosition.value = nextSelection.start
  syncMentionTagSelection(editor)

  if (typeof range.getBoundingClientRect !== 'function') return
  const rangeRect = range.getBoundingClientRect()
  const editorRect = editor.getBoundingClientRect()
  if (!editorRect.width) return
  mentionMenuLeft.value = Math.max(
    12,
    Math.min(rangeRect.left - editorRect.left, editorRect.width - 272),
  )
}

document.addEventListener('selectionchange', onSelectionChange)
onUnmounted(() => document.removeEventListener('selectionchange', onSelectionChange))
</script>
