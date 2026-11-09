import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_PARAMS, type TaskRecord } from '../../types'
import { createStudioJob, getStudioJob, type StudioJobSpec } from '../studioJobsApi'

vi.mock('../galleryApi', async (original) => {
  const actual = await original<typeof import('../galleryApi')>()
  return { ...actual, ensureGalleryIdentity: vi.fn().mockResolvedValue(undefined) }
})

const task: TaskRecord = {
  id: 'studio-task-1',
  prompt: 'A red square',
  params: DEFAULT_PARAMS,
  modelId: 'gpt-image-2',
  modelName: 'GPT Image 2',
  apiModel: 'gpt-image-2',
  inputImageIds: [],
  outputImages: [],
  status: 'running',
  error: null,
  createdAt: 1,
  finishedAt: null,
  elapsed: null,
}

const spec: StudioJobSpec = {
  protocol: 'openai',
  model: 'gpt-image-2',
  prompt: 'A red square',
  params: { size: '2048x2048', n: 1 },
  input_image_ids: [],
  request_input_image_ids: [],
  postprocess: 'none',
  image_count: 1,
}

beforeEach(() => localStorage.setItem('auth_token', 'test-jwt'))
afterEach(() => {
  vi.useRealTimers()
  localStorage.clear()
  vi.unstubAllGlobals()
})

describe('Studio durable job API', () => {
  it('times out a stuck GET without submitting a new generation request', async () => {
    vi.useFakeTimers()
    let signal: AbortSignal | undefined
    const transport = vi.fn().mockImplementation((_url, init: RequestInit) => {
      signal = init.signal as AbortSignal
      return new Promise(() => undefined)
    })
    vi.stubGlobal('fetch', transport)
    const pending = getStudioJob('known-task')
    const failed = expect(pending).rejects.toMatchObject({ code: 'STUDIO_REQUEST_TIMEOUT', status: 408 })
    await vi.advanceTimersByTimeAsync(20_000)
    await failed
    expect(signal?.aborted).toBe(true)
    expect(transport).toHaveBeenCalledOnce()
    expect(transport.mock.calls[0][1].method).toBeUndefined()
  })
  it('submits only the gallery asset IDs and provider parameters under JWT auth', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ code: 0, data: {
      job: { task_id: task.id, status: 'queued', task, calls: [], created_at: '', updated_at: '' },
    } }), { status: 202, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    const response = await createStudioJob(task, spec)
    expect(response.job.status).toBe('queued')
    expect(fetchMock).toHaveBeenCalledOnce()
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/image-studio/jobs')
    expect(init.headers).toMatchObject({ Authorization: 'Bearer test-jwt' })
    const payload = JSON.parse(String(init.body)) as Record<string, unknown>
    expect(payload).toMatchObject({ task_id: task.id, spec: { model: 'gpt-image-2', image_count: 1 } })
    expect(String(init.body)).not.toContain('test-jwt')
    expect(String(init.body)).not.toContain('data:image/')
  })

  it('discards a response after the login changes', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => {
      localStorage.setItem('auth_token', 'another-user-jwt')
      return new Response(JSON.stringify({ code: 0, data: { job: { task_id: task.id } } }), { status: 200 })
    }))
    await expect(getStudioJob(task.id, 'test-jwt')).rejects.toThrow('Studio login changed')
  })

  it('encodes task IDs and propagates server error codes', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      code: 1, error: { code: 'STUDIO_JOB_NOT_FOUND', message: 'not found' },
    }), { status: 404 })))
    await expect(getStudioJob('bad/id')).rejects.toMatchObject({ status: 404, code: 'STUDIO_JOB_NOT_FOUND' })
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe('/api/v1/image-studio/jobs/bad%2Fid')
  })
})
