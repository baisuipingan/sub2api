<template>
  <div class="min-w-44 max-w-64 whitespace-normal break-words text-xs">
    <template v-if="observations.length">
      <p class="font-medium text-gray-900 dark:text-white">{{ latest.account_name || t('admin.workbench.routes.account', { id: latest.account_id }) }} <span v-if="latest.account_name" class="font-normal text-gray-500">#{{ latest.account_id }}</span></p>
      <p class="mt-1 font-mono text-gray-500 dark:text-dark-400">{{ latest.upstream_model || t('admin.workbench.routes.modelUnknown') }}</p>
      <details class="mt-2">
        <summary class="cursor-pointer select-none text-primary-600 dark:text-primary-400">{{ t('admin.workbench.routes.details', { calls: calls.length, records: observations.length }) }}</summary>
        <ol class="mt-2 max-h-64 space-y-3 overflow-y-auto pr-1">
          <li v-for="call in calls" :key="call.ordinal">
            <p class="font-medium">{{ t('admin.workbench.routes.call', { number: call.ordinal + 1 }) }}</p>
            <ol v-if="call.upstreams.length" class="mt-1 space-y-2 border-l border-gray-200 pl-2 dark:border-dark-600">
              <li v-for="(upstream, index) in call.upstreams" :key="index">
                <p>{{ upstream.account_name || t('admin.workbench.routes.account', { id: upstream.account_id }) }} <span v-if="upstream.account_name" class="text-gray-500">#{{ upstream.account_id }}</span></p>
                <p class="font-mono text-gray-500 dark:text-dark-400">{{ upstream.upstream_model || t('admin.workbench.routes.modelUnknown') }}</p>
                <p :class="upstream.outcome === 'failed' ? 'text-amber-700 dark:text-amber-400' : 'text-gray-500 dark:text-dark-400'">{{ t('admin.workbench.routes.' + upstream.outcome) }}<span v-if="upstream.status_code"> · HTTP {{ upstream.status_code }}</span></p>
              </li>
            </ol>
            <p v-else class="mt-1 text-gray-500 dark:text-dark-400">{{ stateLabel(call.state) }}</p>
          </li>
        </ol>
      </details>
    </template>
    <p v-else class="text-gray-500 dark:text-dark-400">{{ emptyLabel }}</p>
    <p v-if="hasUnrecorded && observations.length" class="mt-1 text-gray-500 dark:text-dark-400">{{ t('admin.workbench.routes.incomplete') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { WorkbenchTaskRouteCall } from '@/api/admin/workbench'

const props = defineProps<{ calls: WorkbenchTaskRouteCall[] }>()
const { t } = useI18n()
const observations = computed(() => props.calls.flatMap(call => call.upstreams))
// Prefer the last metered result for the compact summary; expanded details keep
// every observed failure/retry and all child calls in their original order.
const latest = computed(() => observations.value.filter(item => item.outcome === 'usage').at(-1) || observations.value.at(-1)!)
const hasUnrecorded = computed(() => props.calls.some(call => !call.upstreams.length))
function stateLabel(state: WorkbenchTaskRouteCall['state']) { return t('admin.workbench.routes.' + state) }
const emptyLabel = computed(() => {
  if (props.calls.length && props.calls.every(call => call.state === 'not_dispatched')) return stateLabel('not_dispatched')
  if (props.calls.some(call => call.state === 'pending')) return stateLabel('pending')
  return stateLabel('unknown')
})
</script>
