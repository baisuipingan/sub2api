<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="card p-5 sm:p-6" data-testid="workbench-access-settings" :aria-busy="loading">
        <div class="mb-5 flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.workbenchSettings.accessTitle') }}</h2>
            <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.accessHint') }}</p>
          </div>
          <button type="button" class="btn btn-ghost btn-sm" :disabled="loading || saving" @click="loadConfig">{{ t('common.refresh') }}</button>
        </div>
        <p v-if="loadError" class="mb-4 break-words text-sm text-red-600 dark:text-red-400" role="alert">{{ loadError }}</p>
        <fieldset class="min-w-0 space-y-5" :disabled="loading || saving || !loaded">
          <div class="flex items-start justify-between gap-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
            <div>
              <label for="workbench-entry-enabled" class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.workbenchSettings.entryEnabled') }}</label>
              <p id="workbench-entry-description" class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.entryHint') }}</p>
            </div>
            <Toggle id="workbench-entry-enabled" v-model="form.image_studio_enabled" :aria-label="t('admin.workbenchSettings.entryEnabled')" aria-describedby="workbench-entry-description" />
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
 <div v-for="feature in features" :key="feature.key" class="flex items-start justify-between gap-3 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
 <div><label :for="feature.key" class="text-sm font-medium">{{ feature.label }}</label><p class="mt-1 text-xs leading-5 text-gray-500">{{ feature.hint }}</p></div>
 <Toggle :id="feature.key" :model-value="form[feature.key]" :aria-label="feature.label" @update:model-value="setFeature(feature.key, $event)" />
 </div></div>
 <div class="max-w-xl">
            <label for="workbench-group" class="mb-2 block text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('admin.workbenchSettings.group') }}</label>
            <Select id="workbench-group" :model-value="form.image_studio_group_id" :options="groupOptions" :searchable="groupOptions.length > 6" :aria-label="t('admin.workbenchSettings.group')" :placeholder="t('admin.workbenchSettings.groupPlaceholder')" @update:model-value="form.image_studio_group_id = Number($event) || 0" />
            <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.groupHint') }}</p>
            <p v-if="loaded && !selectedGroupAvailable" class="mt-2 text-xs leading-5 text-amber-700 dark:text-amber-300" role="note">{{ t('admin.workbenchSettings.groupRequired') }}</p>
          </div>
          <details v-if="budgetForm" class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
            <summary class="cursor-pointer text-sm font-medium">执行限额</summary>
            <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-dark-400">按用户限制同时运行的任务，并限制每个任务的调用次数和时长。修改后仅影响新任务；模型官方上下文与输出能力不受这些设置改写。</p>
            <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <Input v-for="field in budgetFields" :id="`workbench-${field.key}`" :key="field.key" :model-value="budgetForm[field.key]" type="number" :min="1" :max="deploymentLimits?.[field.key]" step="1" :label="field.label" :hint="deploymentLimits ? `部署上限 ${deploymentLimits[field.key].toLocaleString()}` : undefined" @update:model-value="budgetForm[field.key] = Number($event)" />
              <Input id="workbench-max-run-amount" :model-value="budgetAmount" type="number" min="0" step="0.01" :label="`单任务费用预算（${billingSymbol}）`" hint="0 表示不额外限制；每次模型调用仍检查用户可用额度。" @update:model-value="budgetAmount = $event" />
            </div>
            <p class="mt-3 text-xs leading-5 text-amber-700 dark:text-amber-300">费用预算用于停止后续调用，正在执行的一次调用仍可能使任务费用超过预算。达到限制时保留已完成的结果和文件。</p>
            <p v-if="budgetError" role="alert" class="mt-3 text-sm text-red-600">{{ budgetError }}</p>
          </details>
          <details class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
            <summary class="cursor-pointer text-sm font-medium">自定义模型能力</summary>
            <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-dark-400">官方模型名会自动识别。仅在分组使用自定义公开别名时，将它关联到对应的官方能力档案；不会修改模型路由、价格或官方参数上限。</p>
            <p v-if="profileLoadError" role="alert" class="mt-3 text-sm text-amber-700 dark:text-amber-300">{{ profileLoadError }} <button type="button" class="underline" :disabled="profilesLoading" @click="loadProfiles">重新加载</button></p>
            <div v-for="(row, index) in profileRows" :key="row.key" class="mt-4 grid items-end gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]" data-testid="workbench-model-profile">
              <Input :id="`workbench-model-alias-${row.key}`" v-model="row.modelId" :label="`公开模型 ID ${index + 1}`" placeholder="分组向用户展示的模型 ID" />
              <div>
                <label :for="`workbench-model-profile-${row.key}`" class="mb-2 block text-sm font-medium text-gray-700 dark:text-dark-200">官方能力档案</label>
                <Select :id="`workbench-model-profile-${row.key}`" v-model="row.profileId" :options="profileOptions(row.profileId)" searchable :aria-label="`官方能力档案 ${index + 1}`" placeholder="选择官方模型" :disabled="!profilesReady" />
              </div>
              <button type="button" class="btn btn-ghost btn-sm" :aria-label="`删除模型能力关联 ${index + 1}`" @click="profileRows.splice(index, 1)">删除</button>
            </div>
            <button type="button" class="btn btn-secondary btn-sm mt-4" :disabled="!profilesReady || profileRows.length >= 128" @click="addProfile">添加模型别名</button>
            <p v-if="profileError" role="alert" class="mt-3 text-sm text-red-600">{{ profileError }}</p>
          </details>
          <div class="flex flex-wrap items-center gap-3">
            <button type="button" data-testid="workbench-access-save" class="btn btn-primary btn-sm" :disabled="(form.image_studio_enabled || form.chat_enabled) && !selectedGroupAvailable" @click="saveConfig">{{ saving ? t('common.loading') : t('common.save') }}</button>
            <router-link to="/admin/groups" class="text-sm text-primary-600 hover:text-primary-700 dark:text-primary-400">{{ t('admin.workbenchSettings.manageGroups') }}</router-link>
          </div>
        </fieldset>
      </section>

      <ImageStorageSettings />

      <section class="rounded-xl border border-gray-200 p-5 dark:border-dark-700">
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.workbenchSettings.routingTitle') }}</h2>
        <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.routingHint') }}</p>
        <div class="mt-3 flex flex-wrap gap-3">
          <router-link to="/admin/accounts" class="btn btn-secondary btn-sm">{{ t('admin.workbenchSettings.manageAccounts') }}</router-link>
          <router-link to="/admin/channels" class="btn btn-secondary btn-sm">{{ t('admin.workbenchSettings.manageChannels') }}</router-link>
        </div>
      </section>
      <TotpStepUpDialog :controller="stepUp" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api'
import { getWorkbenchConfig, updateWorkbenchConfig, type WorkbenchConfig, type WorkbenchExecutionLimits } from '@/api/admin/workbench'
import { getDesktopAgentProfiles, type DesktopAgentProfile } from '@/api/admin/desktopAgent'
import { resolveDesktopAgentProfile } from '@/utils/desktopAgentConfig'
import type { AdminGroup } from '@/types'
import { useAppStore } from '@/stores'
import AppLayout from '@/components/layout/AppLayout.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Input from '@/components/common/Input.vue'
import { useBillingCurrency } from '@/composables/useBillingCurrency'
import ImageStorageSettings from '@/components/admin/workbench/ImageStorageSettings.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'

const { t } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()
const { symbol: billingSymbol, view: billingView, creditsToDisplay, displayToUSD } = useBillingCurrency()
const deploymentLimits = ref<WorkbenchExecutionLimits | null>(null)
const budgetForm = ref<WorkbenchExecutionLimits | null>(null)
const budgetAmount = ref('0'), budgetError = ref('')
const profiles = ref<DesktopAgentProfile[]>([])
const profilesReady = ref(false), profilesLoading = ref(false)
const profileLoadError = ref(''), profileError = ref('')
const profileRows = ref<{ key: number; modelId: string; profileId: string }[]>([])
let profileRowKey = 0
function loadBindings(config: WorkbenchConfig) {
  profileRows.value = Object.entries(config.model_profiles || {}).map(([modelId, binding]) => ({ key: ++profileRowKey, modelId, profileId: binding.profile_id }))
  profileError.value = ''
}
async function loadProfiles() {
  profilesLoading.value = true
  profileLoadError.value = ''
  try {
    const result = await getDesktopAgentProfiles()
    if (disposed) return
    profiles.value = result.profiles
    profilesReady.value = true
  } catch {
    if (!disposed) profileLoadError.value = '官方能力档案暂时无法加载，已有模型关联会保留。'
  } finally { if (!disposed) profilesLoading.value = false }
}
function profileOptions(selected: string) {
  return [
    { value: '', label: '选择官方模型' },
    ...(selected && !profiles.value.some(profile => profile.id === selected) ? [{ value: selected, label: `${selected}（档案暂不可用）` }] : []),
    ...profiles.value.map(profile => ({ value: profile.id, label: profile.display_name })),
  ]
}
function addProfile() { profileRows.value.push({ key: ++profileRowKey, modelId: '', profileId: '' }) }
function profilePayload(): NonNullable<WorkbenchConfig['model_profiles']> {
  const bindings: NonNullable<WorkbenchConfig['model_profiles']> = Object.create(null)
  if (profileRows.value.length > 128) throw new Error('最多配置 128 个模型别名')
  for (const row of profileRows.value) {
    const modelId = row.modelId.trim()
    if (!modelId || !row.profileId) throw new Error('请填写公开模型 ID 并选择官方能力档案，或删除空行')
    if (Object.prototype.hasOwnProperty.call(bindings, modelId)) throw new Error(`公开模型 ID 重复：${modelId}`)
    const binding = { profile_id: row.profileId, override: false as const }
    if (profilesReady.value && !resolveDesktopAgentProfile(profiles.value, modelId, binding)) throw new Error(`模型 ${modelId} 的官方能力档案不匹配`)
    bindings[modelId] = binding
  }
  return bindings
}
const budgetFields: { key: keyof WorkbenchExecutionLimits; label: string }[] = [
  { key: 'chat_concurrent_runs', label: '每用户并行对话任务' }, { key: 'office_concurrent_runs', label: '每用户并行办公任务' },
  { key: 'max_model_calls', label: '每任务最多模型调用' }, { key: 'max_tool_calls', label: '每任务最多工具调用' },
  { key: 'max_output_tokens', label: '每次模型输出 Token 上限' }, { key: 'max_run_seconds', label: '任务最长时长（秒）' },
]
function loadBudgets(config: WorkbenchConfig) {
  const limits = config.deployment_limits
  if (!limits || !budgetFields.every(field => Number.isSafeInteger(config[field.key]) && Number.isSafeInteger(limits[field.key]))) { budgetForm.value = null; deploymentLimits.value = null; return }
  deploymentLimits.value = limits
  budgetForm.value = Object.fromEntries(budgetFields.map(field => [field.key, config[field.key]])) as unknown as WorkbenchExecutionLimits
  budgetAmount.value = String(creditsToDisplay(config.max_run_credits || 0)); budgetError.value = ''
}
function budgetPayload() {
  if (!budgetForm.value || !deploymentLimits.value) return {}
  for (const field of budgetFields) { const value = budgetForm.value[field.key]; if (!Number.isSafeInteger(value) || value < 1 || value > deploymentLimits.value[field.key]) throw new Error(`${field.label}须为 1 到 ${deploymentLimits.value[field.key]} 的整数`) }
  const amount = Number(budgetAmount.value)
  const credits = Math.round(displayToUSD(amount) * billingView.value.creditsPerUSD)
  if (!budgetAmount.value.trim() || !Number.isFinite(amount) || amount < 0 || !Number.isSafeInteger(credits) || credits < 0 || amount > 0 && credits === 0) throw new Error('请输入有效的单任务费用预算')
  return { ...budgetForm.value, max_run_credits: credits }
}
const form = ref({ image_studio_enabled: true, image_studio_group_id: 0, chat_enabled:false,office_enabled:false,search_enabled:false,files_enabled:false })
const features = [
 {key:'chat_enabled' as const,label:'对话助手',hint:'开放云端对话；关闭时同时关闭搜索和办公，保留历史会话和文件。'},
 {key:'search_enabled' as const,label:'联网搜索',hint:'启用时同时开启对话助手；使用系统搜索供应商，计入任务工具次数。'},
 {key:'files_enabled' as const,label:'我的文件',hint:'私有资料、成果和版本，需配置对象存储；关闭时同时关闭办公任务。'},
 {key:'office_enabled' as const,label:'办公任务',hint:'启用时同时开启对话助手和我的文件；需已就绪的办公执行环境。'},
]
type FeatureKey = typeof features[number]['key']
function setFeature(key: FeatureKey, value: boolean) {
  form.value[key] = value
  if (key === 'chat_enabled' && !value) { form.value.search_enabled = false; form.value.office_enabled = false }
  if (key === 'files_enabled' && !value) form.value.office_enabled = false
  if ((key === 'search_enabled' || key === 'office_enabled') && value) form.value.chat_enabled = true
  if (key === 'office_enabled' && value) form.value.files_enabled = true
}
const groups = ref<AdminGroup[]>([])
const loaded = ref(false)
const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
let disposed = false

const eligibleGroups = computed(() => groups.value.filter(group => group.status === 'active' && (!form.value.image_studio_enabled || group.allow_image_generation)))
const selectedGroupAvailable = computed(() => eligibleGroups.value.some(group => group.id === form.value.image_studio_group_id))
const groupOptions = computed(() => [
  { value: 0, label: t('admin.workbenchSettings.groupPlaceholder') },
  ...(form.value.image_studio_group_id > 0 && !selectedGroupAvailable.value
    ? [{ value: form.value.image_studio_group_id, label: t('admin.workbenchSettings.groupUnavailable', { id: form.value.image_studio_group_id }), disabled: true }]
    : []),
  ...eligibleGroups.value.map(group => ({ value: group.id, label: group.name })),
])

function errorMessage(error: unknown) {
  if (isStepUpBlocked(error)) return t(stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled')
  return (error as { message?: string })?.message || t('errors.networkError')
}

async function loadConfig() {
  if (disposed) return
  loading.value = true
  loadError.value = ''
  try {
    const [config, availableGroups] = await Promise.all([getWorkbenchConfig(), adminAPI.groups.getAll()])
    if (disposed) return
    form.value = { image_studio_enabled: config.image_studio_enabled, image_studio_group_id: config.image_studio_group_id,chat_enabled:!!config.chat_enabled,office_enabled:!!config.office_enabled,search_enabled:!!config.search_enabled,files_enabled:!!config.files_enabled }
    loadBudgets(config)
    loadBindings(config)
    groups.value = availableGroups
    loaded.value = true
  } catch (error) {
    if (!disposed) { loaded.value = false; loadError.value = errorMessage(error) }
  } finally { if (!disposed) loading.value = false }
}

async function saveConfig() {
  if (!loaded.value || loading.value || saving.value || ((form.value.image_studio_enabled || form.value.chat_enabled) && !selectedGroupAvailable.value)) return
  let budgets: Partial<WorkbenchConfig>
  let modelProfiles: NonNullable<WorkbenchConfig['model_profiles']>
  budgetError.value = ''
  profileError.value = ''
  try { budgets = budgetPayload() } catch (error) { budgetError.value = errorMessage(error); appStore.showError(budgetError.value); return }
  try { modelProfiles = profilePayload() } catch (error) { profileError.value = errorMessage(error); appStore.showError(profileError.value); return }
  saving.value = true
  const payload = { ...form.value, ...budgets, model_profiles: modelProfiles }
  try {
    const saved = await stepUp.run(() => updateWorkbenchConfig(payload))
    if (disposed) return
    form.value = { image_studio_enabled: saved.image_studio_enabled, image_studio_group_id: saved.image_studio_group_id,chat_enabled:!!saved.chat_enabled,office_enabled:!!saved.office_enabled,search_enabled:!!saved.search_enabled,files_enabled:!!saved.files_enabled }
    loadBudgets(saved)
    loadBindings(saved)
    appStore.showSuccess(t('admin.workbenchSettings.saved'))
    await appStore.fetchPublicSettings(true)
  } catch (error) {
    if (!disposed && !isStepUpCancelled(error)) appStore.showError(errorMessage(error))
  } finally { if (!disposed) saving.value = false }
}

onMounted(() => { void loadConfig(); void loadProfiles() })
onBeforeUnmount(() => { disposed = true })
</script>
