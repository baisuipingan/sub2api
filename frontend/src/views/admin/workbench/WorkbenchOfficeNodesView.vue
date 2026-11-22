<template>
  <AppLayout>
    <div class="space-y-5" :aria-busy="loading">
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('nav.workbenchOfficeNodes') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.workbench.nodes.hint') }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-secondary" :disabled="loading || busy" @click="load">
            <Icon name="refresh" size="sm" class="mr-1.5" aria-hidden="true" />{{ t('common.refresh') }}
          </button>
          <button type="button" class="btn btn-primary" :disabled="loading || busy" @click="openEditor()">
            <Icon name="plus" size="sm" class="mr-1.5" aria-hidden="true" />{{ t('admin.workbench.nodes.add') }}
          </button>
        </div>
      </header>

      <p v-if="loadError" class="card p-4 text-sm text-red-600 dark:text-red-400" role="alert">{{ loadError }}</p>
      <LoadingSpinner v-if="loading && !nodes.length" />
      <section v-else-if="!nodes.length && !loadError" class="card px-5 py-10 text-center">
        <Icon name="link" size="lg" class="mx-auto mb-3 text-gray-400" aria-hidden="true" />
        <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.workbench.nodes.empty') }}</h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.workbench.nodes.emptyHint') }}</p>
      </section>

      <ul v-if="nodes.length" class="space-y-3">
        <li v-for="node in nodes" :key="node.id" class="card p-4 sm:p-5">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="min-w-0">
              <h3 class="break-words font-semibold text-gray-900 dark:text-white">{{ node.name }}</h3>
              <p class="mt-0.5 break-all font-mono text-xs text-gray-500 dark:text-dark-400">{{ node.url }}</p>
            </div>
            <div class="flex flex-wrap gap-x-4 gap-y-1">
              <StatusBadge :status="healthBadge[node.health]" :label="t(`admin.workbench.nodes.health.${node.health}`)" />
              <StatusBadge :status="stateBadge[node.state]" :label="t(`admin.workbench.nodes.states.${node.state}`)" />
            </div>
          </div>

          <dl class="mt-4 grid grid-cols-2 gap-x-5 gap-y-2 border-t border-gray-100 pt-3 dark:border-dark-700 sm:grid-cols-4 lg:grid-cols-7">
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.workbench.nodes.running') }}</dt>
              <dd class="mt-0.5 text-sm font-medium tabular-nums">{{ snapshot(node)?.running ?? '—' }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.workbench.nodes.queued') }}</dt>
              <dd class="mt-0.5 text-sm font-medium tabular-nums">{{ snapshot(node)?.queued ?? '—' }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.workbench.nodes.awaitingUpload') }}</dt>
              <dd class="mt-0.5 text-sm font-medium tabular-nums">{{ snapshot(node)?.awaiting_upload ?? '—' }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.workbench.nodes.assigned') }}</dt>
              <dd class="mt-0.5 text-sm font-medium tabular-nums">{{ node.assigned }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.workbench.nodes.maxConcurrency') }}</dt>
              <dd class="mt-0.5 text-sm font-medium tabular-nums">{{ node.max_concurrent }}</dd>
              <dd v-if="concurrencyStatus(node)" class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
                {{ concurrencyStatus(node) }}
              </dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.workbench.nodes.lastChecked') }}</dt>
              <dd class="mt-0.5 text-sm font-medium tabular-nums">
                <time v-if="node.last_checked_at" :datetime="node.last_checked_at">{{ checkedAt(node.last_checked_at) }}</time>
                <span v-else>—</span>
              </dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.workbench.nodes.version') }}</dt>
              <dd class="mt-0.5 break-words text-sm font-medium">{{ node.status?.version || '—' }}</dd>
            </div>
          </dl>

          <div class="mt-3 flex flex-wrap items-center gap-1.5">
            <button type="button" class="btn btn-secondary btn-sm" :disabled="busy || loading" @click="check(node)">{{ t('admin.workbench.nodes.check') }}</button>
            <button type="button" class="btn btn-ghost btn-sm" :disabled="busy || loading" @click="openEditor(node)">{{ t('common.edit') }}</button>
            <button v-if="node.state === 'active'" type="button" class="btn btn-ghost btn-sm" :disabled="busy || loading" @click="updateState(node, 'draining')">{{ t('admin.workbench.nodes.drain') }}</button>
            <button v-else type="button" class="btn btn-ghost btn-sm" :disabled="busy || loading" @click="updateState(node, 'active')">{{ t('admin.workbench.nodes.enable') }}</button>
            <button v-if="node.state === 'draining'" type="button" class="btn btn-ghost btn-sm" :disabled="busy || loading || node.assigned > 0" @click="updateState(node, 'disabled')">{{ t('admin.workbench.nodes.disable') }}</button>
            <button v-if="node.state === 'disabled'" type="button" class="btn btn-ghost btn-sm text-red-600 dark:text-red-400" :disabled="busy || loading || node.assigned > 0" @click="deleting = node">{{ t('common.delete') }}</button>
          </div>
        </li>
      </ul>
    </div>

    <BaseDialog :show="editorOpen" :title="t(editing ? 'admin.workbench.nodes.edit' : 'admin.workbench.nodes.add')" :close-on-escape="!busy" :show-close-button="!busy" @close="closeEditor">
      <form id="office-node-form" class="space-y-4" @submit.prevent="save">
        <fieldset class="space-y-4" :disabled="busy">
          <Input id="office-node-name" v-model="draft.name" :label="t('admin.workbench.nodes.name')" :maxlength="120" required />
          <Input id="office-node-url" v-model="draft.url" type="url" :label="t('admin.workbench.nodes.url')" placeholder="https://worker.example.com" :maxlength="512" :hint="t('admin.workbench.nodes.urlHint')" required />
          <Input id="office-node-token" v-model="draft.token" type="password" autocomplete="new-password" :label="t('admin.workbench.nodes.token')" :maxlength="256" :required="!editing || !editing.token_configured" :hint="t(editing?.token_configured ? 'admin.workbench.nodes.keepToken' : 'admin.workbench.nodes.tokenHint')" />
          <Input id="office-node-concurrency" v-model="draft.concurrent" type="number" :min="1" :max="8" :step="1" :label="t('admin.workbench.nodes.maxConcurrency')" :hint="concurrencyHint" required />
        </fieldset>
        <p v-if="formError" class="break-words text-sm text-red-600 dark:text-red-400" role="alert">{{ formError }}</p>
      </form>
      <template #footer>
        <div class="flex flex-wrap justify-end gap-2">
          <button type="button" class="btn btn-secondary" :disabled="busy" @click="closeEditor">{{ t('common.cancel') }}</button>
          <button type="submit" form="office-node-form" class="btn btn-primary" :disabled="busy">{{ t(editing ? 'common.save' : 'admin.workbench.nodes.addAndCheck') }}</button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="!!deleting"
      :title="t('admin.workbench.nodes.deleteTitle')"
      :message="t('admin.workbench.nodes.deleteMessage', { name: deleting?.name || '' })"
      :confirm-text="t('common.delete')"
      :loading="busy"
      danger
      @cancel="deleting = undefined"
      @confirm="remove"
    />
    <TotpStepUpDialog :controller="stepUp" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { workbenchOfficeNodesApi, type OfficeNode, type OfficeNodeInput, type OfficeNodeState } from '@/api/admin/workbenchOfficeNodes'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Input from '@/components/common/Input.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import StatusBadge from '@/components/common/StatusBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import { isStepUpBlocked, isStepUpCancelled, stepUpBlockReason, useStepUp } from '@/composables/useStepUp'
import { useAppStore } from '@/stores/app'

const { t, locale } = useI18n()
const app = useAppStore()
const stepUp = useStepUp()
const nodes = ref<OfficeNode[]>([])
const loading = ref(true)
const busy = ref(false)
const loadError = ref('')
const formError = ref('')
const editorOpen = ref(false)
const editing = ref<OfficeNode>()
const deleting = ref<OfficeNode>()
const draft = ref({ name: '', url: '', token: '', concurrent: '1' })
const stateBadge = { active: 'success', draining: 'warning', disabled: 'neutral' }
const healthBadge = { online: 'success', unavailable: 'error', unchecked: 'neutral' }
const officeNodeErrors = {
  OFFICE_NODE_INVALID: 'invalid',
  OFFICE_NODE_BUSY: 'busy',
  OFFICE_NODE_UNAVAILABLE: 'unavailable',
  OFFICE_NODE_DUPLICATE: 'duplicate',
  OFFICE_NODE_SECRET_UNAVAILABLE: 'secret',
  OFFICE_NODE_IDENTITY_CHANGED: 'identity',
  OFFICE_NODE_CONFLICT: 'conflict',
  OFFICE_NODE_NOT_FOUND: 'notFound',
} as const
let controller: AbortController | undefined
let disposed = false

function snapshot(node: OfficeNode) {
  return node.health === 'online' ? node.status : undefined
}

function concurrencyStatus(node: OfficeNode) {
  const status = snapshot(node)
  if (!status) return t('admin.workbench.nodes.concurrencyUnconfirmed')
  if (status.concurrency_revision === undefined) {
    return t('admin.workbench.nodes.legacyConcurrency', { limit: Math.min(status.concurrency, node.max_concurrent) })
  }
  if (status.concurrency_revision === 0) return t('admin.workbench.nodes.concurrencyPending')
  if (status.concurrency !== node.max_concurrent) {
    return t('admin.workbench.nodes.concurrencyPendingWithLimit', { limit: status.concurrency })
  }
  return ''
}

const concurrencyHint = computed(() => {
  const status = editing.value && snapshot(editing.value)
  if (status && status.concurrency_revision === undefined) {
    return t('admin.workbench.nodes.legacyConcurrencyHint')
  }
  return t('admin.workbench.nodes.concurrencyHint')
})

function checkedAt(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString(locale.value)
}

function message(cause: unknown) {
  if (isStepUpBlocked(cause)) {
    return t(stepUpBlockReason(cause) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled')
  }
  const error = cause as { reason?: string; code?: string } | null
  const code = error?.reason || error?.code
  const key = code && officeNodeErrors[code as keyof typeof officeNodeErrors]
  return t(`admin.workbench.nodes.errors.${key || 'general'}`)
}

async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loadError.value = ''
  loading.value = true
  try {
    const data = await workbenchOfficeNodesApi.list(request.signal)
    if (!request.signal.aborted) nodes.value = data
  } catch {
    if (!request.signal.aborted) loadError.value = t('admin.workbench.nodes.errors.load')
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}

function replaceNode(node: OfficeNode) {
  const index = nodes.value.findIndex(item => item.id === node.id)
  if (index === -1) nodes.value = [...nodes.value, node]
  else nodes.value.splice(index, 1, node)
}

function openEditor(node?: OfficeNode) {
  editing.value = node
  draft.value = { name: node?.name || '', url: node?.url || '', token: '', concurrent: String(node?.max_concurrent ?? 1) }
  formError.value = ''
  editorOpen.value = true
}

function closeEditor() {
  if (busy.value) return
  editorOpen.value = false
  editing.value = undefined
  draft.value.token = ''
}

function inputFor(node: OfficeNode, state: OfficeNodeState): OfficeNodeInput {
  return { name: node.name, url: node.url, state, max_concurrent: node.max_concurrent }
}

async function perform(action: () => Promise<void>, onError: (value: string) => void = value => app.showError(value)) {
  if (busy.value || loading.value) return
  busy.value = true
  try {
    await action()
  } catch (cause) {
    if (!disposed && !isStepUpCancelled(cause)) onError(message(cause))
  } finally {
    busy.value = false
  }
}

async function save() {
  const current = editing.value
  const token = draft.value.token.trim()
  const payload: OfficeNodeInput = {
    name: draft.value.name.trim(),
    url: draft.value.url.trim(),
    state: current?.state ?? 'disabled',
    max_concurrent: Number(draft.value.concurrent),
    ...(token && { token }),
  }
  formError.value = ''
  const validToken = /^[!-~]{32,256}$/
  if (!payload.name || new TextEncoder().encode(payload.name).length > 120 ||
      !Number.isInteger(payload.max_concurrent) || payload.max_concurrent < 1 || payload.max_concurrent > 8 ||
      (token ? !validToken.test(token) : !current?.token_configured)) {
    formError.value = t('admin.workbench.nodes.errors.invalid')
    return
  }
  await perform(async () => {
    const saved = await stepUp.run(() => current
      ? workbenchOfficeNodesApi.update(current.id, payload)
      : workbenchOfficeNodesApi.create(payload))
    if (disposed) return
    replaceNode(saved)
    editorOpen.value = false
    draft.value.token = ''
    if (current) {
      app.showSuccess(t('common.saved'))
    } else {
      // The registered node remains available for correction when a probe fails.
      try {
        await probe(saved.id)
      } catch (cause) {
        if (!disposed && !isStepUpCancelled(cause)) app.showError(message(cause))
      }
    }
  }, value => { formError.value = value })
}

async function probe(id: string) {
  const checked = await stepUp.run(() => workbenchOfficeNodesApi.check(id))
  if (disposed) return
  replaceNode(checked)
  if (checked.health === 'online') app.showSuccess(t('admin.workbench.nodes.checked'))
  else app.showError(t('admin.workbench.nodes.errors.unavailable'))
}

async function check(node: OfficeNode) {
  await perform(() => probe(node.id))
}

async function updateState(node: OfficeNode, state: OfficeNodeState) {
  await perform(async () => {
    const updated = await stepUp.run(() => workbenchOfficeNodesApi.update(node.id, inputFor(node, state)))
    if (disposed) return
    replaceNode(updated)
    app.showSuccess(t('common.saved'))
  })
}

async function remove() {
  const node = deleting.value
  if (!node) return
  await perform(async () => {
    await stepUp.run(() => workbenchOfficeNodesApi.remove(node.id))
    if (disposed) return
    nodes.value = nodes.value.filter(item => item.id !== node.id)
    deleting.value = undefined
    app.showSuccess(t('common.deleted'))
  })
}

onMounted(load)
onBeforeUnmount(() => {
  disposed = true
  controller?.abort()
  stepUp.onCancel()
  draft.value.token = ''
})
</script>
