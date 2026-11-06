import { removeTask, storeState as state } from '../stores/store'
import { libT } from './i18nMessage'

/** Both task deletion entry points share the same confirmation and error handling. */
export function confirmTaskDeletion(taskId: string) {
  const task = state.tasks.find((item) => item.id === taskId)
  if (!task) return
  if (task.status === 'running') {
    state.showToast(libT('imageStudio.store.waitGeneration'), 'info')
    return
  }
  let deleting = false
  state.setConfirmDialog({
    title: libT('imageStudio.grid.deleteTitle'),
    message: libT('imageStudio.grid.deleteMessage'),
    tone: 'danger',
    confirmText: libT('imageStudio.task.delete'),
    cancelText: libT('imageStudio.common.cancel'),
    action: async () => {
      if (deleting) return
      const current = state.tasks.find((item) => item.id === taskId)
      if (!current) return
      deleting = true
      try {
        await removeTask(current)
      } catch {
        state.showToast(libT(state.tasks.some((item) => item.id === taskId)
          ? 'imageStudio.store.deleteFailed'
          : 'imageStudio.store.imageCleanupFailed'), 'error')
      } finally {
        deleting = false
      }
    },
  })
}
