import { describe, it, expect, vi, afterEach } from 'vitest'
import { createReachability, isAvailableOffline } from './offline'

describe('isAvailableOffline', () => {
  it.each([
    '/',
    '/album',
    '/album/random',
    '/album/al1/show',
    '/playlist',
    '/playlist/pl1/show',
    '/personal',
  ])('opens %s, which Downloads can answer', (path) => {
    expect(isAvailableOffline(path)).toBe(true)
  })

  // Anything else needs the server, including features not built yet
  // (e.g. Stations), so it is unavailable unless listed
  it.each(['/artist', '/song', '/radio', '/station', '/share', '/user/1'])(
    'shows %s as unavailable',
    (path) => {
      expect(isAvailableOffline(path)).toBe(false)
    },
  )
})

// navigator.onLine only notices a lost connection; on a tailnet the server is
// usually unreachable with the network still up (VPN off, host asleep)
describe('createReachability', () => {
  const setup = (probeAnswers = []) => {
    const probe = vi.fn(async () => probeAnswers.shift() ?? false)
    const reachability = createReachability({ probe, retryMs: 1000 })
    const changes = vi.fn()
    reachability.subscribe(changes)
    return { reachability, probe, changes }
  }

  afterEach(() => vi.useRealTimers())

  it('starts reachable', () => {
    expect(setup().reachability.isReachable()).toBe(true)
  })

  it('tells subscribers when the server becomes unreachable, once', () => {
    const { reachability, changes } = setup()
    reachability.markUnreachable()
    reachability.markUnreachable()
    expect(reachability.isReachable()).toBe(false)
    expect(changes).toHaveBeenCalledTimes(1)
  })

  it('keeps checking the server while unreachable, until it answers', async () => {
    vi.useFakeTimers()
    const { reachability, probe, changes } = setup([false, true])
    reachability.markUnreachable()

    await vi.advanceTimersByTimeAsync(1000)
    expect(reachability.isReachable()).toBe(false)
    await vi.advanceTimersByTimeAsync(1000)
    expect(reachability.isReachable()).toBe(true)
    expect(changes).toHaveBeenCalledTimes(2)

    await vi.advanceTimersByTimeAsync(5000)
    expect(probe).toHaveBeenCalledTimes(2) // stops once reachable
  })

  it('stops checking when a request gets through', async () => {
    vi.useFakeTimers()
    const { reachability, probe } = setup()
    reachability.markUnreachable()
    reachability.markReachable()
    await vi.advanceTimersByTimeAsync(5000)
    expect(probe).not.toHaveBeenCalled()
  })
})
