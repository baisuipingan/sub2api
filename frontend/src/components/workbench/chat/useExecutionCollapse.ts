import { ref, watch, type Ref } from 'vue'

const storageKey = 'workbench:execution-folds:v1'
const maximumEntries = 160

// Bisheng keeps a user's manual timeline fold when switching conversations.
// Persist only operation IDs and booleans, never conversation/file contents.
export function useExecutionCollapse(key: Ref<string>) {
  const open = ref(false)
  const read = (): Record<string, boolean> => {
    try {
      const value: unknown = JSON.parse(sessionStorage.getItem(storageKey) || '{}')
      if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
      return Object.fromEntries(Object.entries(value).filter(([, expanded]) => typeof expanded === 'boolean').slice(-maximumEntries))
    } catch { return {} }
  }
  watch(key, id => { open.value = id ? read()[id] === true : false }, { immediate: true })
  function toggle() {
    open.value = !open.value
    if (!key.value) return
    const entries = read()
    delete entries[key.value]
    entries[key.value] = open.value
    try { sessionStorage.setItem(storageKey, JSON.stringify(Object.fromEntries(Object.entries(entries).slice(-maximumEntries)))) } catch { /* A blocked/full storage must not break the control. */ }
  }
  return { open, toggle }
}
