import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { TaskRecord } from '../../types'

const { state, removeTask } = vi.hoisted(() => ({
  state: {
    tasks: [] as TaskRecord[],
    setConfirmDialog: vi.fn(),
    showToast: vi.fn(),
  },
  removeTask: vi.fn(),
}))
vi.mock('../../stores/store', () => ({ storeState: state, removeTask }))
vi.mock('../i18nMessage', () => ({ libT: (key: string) => key }))
import { confirmTaskDeletion } from '../taskDeletion'

function confirmation() {
  return state.setConfirmDialog.mock.calls[0][0] as { action: () => Promise<void> }
}

beforeEach(() => {
  vi.clearAllMocks()
  state.tasks = [{ id: 'batch', status: 'done', outputImages: ['one', 'two', 'three'] } as TaskRecord]
  removeTask.mockReset().mockResolvedValue(undefined)
})

describe('task deletion confirmation', () => {
  it('waits for confirmation before deleting the complete batch', async () => {
    confirmTaskDeletion('batch')
    expect(removeTask).not.toHaveBeenCalled()
    expect(state.setConfirmDialog).toHaveBeenCalledWith(expect.objectContaining({ tone: 'danger' }))
    await confirmation().action()
    expect(removeTask).toHaveBeenCalledWith(state.tasks[0])
  })

  it('does not confirm deletion of a running or missing task', () => {
    state.tasks[0].status = 'running'
    confirmTaskDeletion('batch')
    confirmTaskDeletion('missing')
    expect(state.setConfirmDialog).not.toHaveBeenCalled()
    expect(removeTask).not.toHaveBeenCalled()
  })

  it('ignores repeated confirmation while deletion is pending', async () => {
    let complete!: () => void
    removeTask.mockReturnValueOnce(new Promise<void>((resolve) => { complete = resolve }))
    confirmTaskDeletion('batch')
    const { action } = confirmation()
    const first = action()
    await action()
    expect(removeTask).toHaveBeenCalledTimes(1)
    complete()
    await first
  })

  it('does not act on a task removed while the confirmation was open', async () => {
    confirmTaskDeletion('batch')
    state.tasks = []
    await confirmation().action()
    expect(removeTask).not.toHaveBeenCalled()
  })

  it('handles persistence errors without leaving an unhandled rejection', async () => {
    removeTask.mockRejectedValueOnce(new Error('Storage unavailable'))
    confirmTaskDeletion('batch')
    await expect(confirmation().action()).resolves.toBeUndefined()
    expect(state.showToast).toHaveBeenCalledWith('imageStudio.store.deleteFailed', 'error')
  })

  it('distinguishes an image cleanup failure after the task was deleted', async () => {
    removeTask.mockImplementationOnce(async () => { state.tasks = []; throw new Error('Cleanup failed') })
    confirmTaskDeletion('batch')
    await confirmation().action()
    expect(state.showToast).toHaveBeenCalledWith('imageStudio.store.imageCleanupFailed', 'error')
  })
})
