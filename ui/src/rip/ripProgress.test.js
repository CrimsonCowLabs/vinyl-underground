import { describe, it, expect } from 'vitest'
import { ripProgress } from './ripProgress'

const job = (...statuses) => ({
  status: 'downloading',
  items: statuses.map((status, i) => ({ id: `i${i}`, status })),
})

describe('ripProgress', () => {
  it('counts the Tracks that landed, failed, and are still to do', () => {
    expect(
      ripProgress(job('ready', 'failed', 'downloading', 'queued')),
    ).toEqual({ total: 4, ready: 1, failed: 1, done: 2 })
  })

  it('is all zeros for a job without items', () => {
    expect(ripProgress({ status: 'failed' })).toEqual({
      total: 0,
      ready: 0,
      failed: 0,
      done: 0,
    })
  })
})
