<template>
  <main class="min-h-screen bg-gray-50 px-4 py-8 dark:bg-dark-950 sm:py-16">
    <section class="card mx-auto max-w-lg space-y-6 p-5 sm:p-8" aria-labelledby="desktop-authorize-title" :aria-busy="submitting">
      <div class="flex items-start gap-3">
        <div class="rounded-xl bg-primary-50 p-3 text-primary-600 dark:bg-primary-900/20 dark:text-primary-400"><Icon name="terminal" size="lg" /></div>
        <div>
          <h1 id="desktop-authorize-title" class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('desktop.authorizeTitle') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('desktop.authorizeDescription') }}</p>
        </div>
      </div>
      <div v-if="!authorization" class="space-y-4">
        <p role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('desktop.invalidRequest') }}</p>
        <RouterLink to="/profile" class="btn btn-secondary">{{ t('desktop.backToAccount') }}</RouterLink>
      </div>
      <div v-else-if="cancelled" class="space-y-4">
        <p role="status" class="text-sm text-gray-600 dark:text-gray-300">{{ t('desktop.cancelled') }}</p>
        <RouterLink to="/profile" class="btn btn-secondary">{{ t('desktop.backToAccount') }}</RouterLink>
      </div>
      <form v-else class="space-y-5" @submit.prevent="approve">
        <dl class="space-y-4 rounded-xl border border-gray-200 bg-gray-50 p-4 text-sm dark:border-dark-700 dark:bg-dark-900">
          <div><dt class="text-gray-500 dark:text-gray-400">{{ t('desktop.application') }}</dt><dd data-test="desktop-client" class="mt-1 font-semibold text-gray-900 dark:text-white">MiloCode</dd></div>
          <div><dt class="text-gray-500 dark:text-gray-400">{{ t('desktop.account') }}</dt><dd class="mt-1 break-all text-gray-900 dark:text-white">{{ authStore.user?.email }}</dd></div>
          <div><dt class="text-gray-500 dark:text-gray-400">{{ t('desktop.returnAddress') }}</dt><dd data-test="desktop-callback" class="mt-1 break-all font-mono text-xs text-gray-700 dark:text-gray-200">{{ authorization.redirect_uri }}</dd></div>
        </dl>
        <p class="text-sm leading-6 text-gray-600 dark:text-gray-300">{{ t('desktop.accessDescription') }}</p>
        <p class="rounded-lg bg-amber-50 p-3 text-sm leading-6 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">{{ t('desktop.authorizeHint') }}</p>
        <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t(error) }}</p>
        <div class="grid gap-3 border-t border-gray-100 pt-5 dark:border-dark-700 sm:flex sm:justify-end">
          <button type="button" data-test="desktop-deny" class="btn btn-secondary" :disabled="submitting" @click="deny">{{ t('common.cancel') }}</button>
          <button type="submit" data-test="desktop-approve" class="btn btn-primary" :disabled="submitting">{{ submitting ? t('common.processing') : t('desktop.approve') }}</button>
        </div>
      </form>
    </section>
  </main>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { authorizeDesktop, type DesktopAuthorization } from '@/api/desktop'
import { Icon } from '@/components/icons'
import { useAuthStore } from '@/stores/auth'
import { desktopAuthorizationDeniedRedirect, parseDesktopAuthorization, validateDesktopAuthorizationRedirect } from '@/utils/desktopAuthorization'

const route = useRoute()
const { t } = useI18n()
const authStore = useAuthStore()
const authorization = ref<Readonly<DesktopAuthorization> | null>(null)
const submitting = ref(false)
const cancelled = ref(false)
const error = ref('')
let generation = 0
watch(() => route.fullPath, () => {
  generation++
  submitting.value = false
  cancelled.value = false
  error.value = ''
  const parsed = parseDesktopAuthorization(route.query)
  authorization.value = parsed ? Object.freeze(parsed) : null
}, { immediate: true })

async function approve() {
  if (!authorization.value || submitting.value || cancelled.value) return
  const request = { ...authorization.value }
  const current = generation
  submitting.value = true
  error.value = ''
  try {
    const result = await authorizeDesktop(request)
    // 原因：授权结果仅属于当前页面；换路由、卸载或身份变化后的旧结果不能跳回应用。
    if (current !== generation) return
    if (!validateDesktopAuthorizationRedirect(result.redirect_url, request)) {
      error.value = 'desktop.invalidRedirect'
      return
    }
    window.location.assign(result.redirect_url)
  } catch {
    if (current === generation) error.value = 'desktop.authorizeFailed'
  } finally {
    if (current === generation) submitting.value = false
  }
}
function deny() {
  if (!authorization.value || submitting.value) return
  cancelled.value = true
  // 原因：取消仅返回已校验的本机回调和state，让桌面立即结束等待，不签发授权码。
  window.location.assign(desktopAuthorizationDeniedRedirect(authorization.value))
}
watch(() => authStore.user?.id, () => {
  generation++
  submitting.value = false
  cancelled.value = true
})
onBeforeUnmount(() => { generation++ })
</script>
