import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createSleepTimer } from './sleepTimer'

// Stands in for the player package's audio element. As in a browser, a
// volume change is announced later, by a volumechange event.
const fakeAudio = ({ currentTime = 40, duration = 252 } = {}) => {
  const audio = new EventTarget()
  let volume = 0.64
  Object.defineProperty(audio, 'volume', {
    get: () => volume,
    set: (value) => {
      if (value === volume) return
      volume = value
      setTimeout(() => audio.dispatchEvent(new Event('volumechange')))
    },
  })
  return Object.assign(audio, {
    currentTime,
    duration,
    paused: false,
    playbackRate: 1,
    pause: vi.fn(() => {
      audio.paused = true
      audio.dispatchEvent(new Event('pause'))
    }),
  })
}

const minutes = (n) => n * 60 * 1000

describe('sleep timer', () => {
  let timer, audio
  beforeEach(() => {
    vi.useFakeTimers()
    audio = fakeAudio()
    timer = createSleepTimer({ fadeMs: 4000 })
    timer.attach(audio)
  })
  afterEach(() => {
    timer.cancel()
    vi.useRealTimers()
  })

  it('fades out over its last seconds, pauses when it runs out, then restores the volume', () => {
    timer.start(15)
    vi.advanceTimersByTime(minutes(15) - 4000)
    expect(audio.volume).toBeCloseTo(0.64)

    vi.advanceTimersByTime(2000)
    expect(audio.volume).toBeCloseTo(0.32, 1)
    expect(audio.pause).not.toHaveBeenCalled()

    vi.advanceTimersByTime(2000)
    expect(audio.pause).toHaveBeenCalledTimes(1)
    expect(audio.volume).toBeCloseTo(0.64)
    expect(timer.state().mode).toBe(null)
  })

  it("keeps its fade from the player package, which would take it for the Listener's volume", () => {
    // The package listens before the timer, and keeps each volume it hears
    const player = fakeAudio()
    const stored = []
    player.addEventListener('volumechange', () => stored.push(player.volume))
    timer.attach(player)

    timer.start(15)
    vi.advanceTimersByTime(minutes(15) + 1000)
    expect(player.pause).toHaveBeenCalled()
    expect(player.volume).toBeCloseTo(0.64)
    expect(stored).toEqual([])

    // The Listener's own changes are stored again
    player.volume = 0.5
    vi.advanceTimersByTime(0)
    expect(stored).toEqual([0.5])
  })

  it('lets the Listener take the volume back during the fade, and stops', () => {
    const player = fakeAudio()
    const heard = []
    player.addEventListener('volumechange', () => heard.push(player.volume))
    timer.attach(player)
    timer.start(15)
    vi.advanceTimersByTime(minutes(15) - 2000)

    player.volume = 0.5 // the Listener's slider
    vi.advanceTimersByTime(0)

    expect(heard).toEqual([0.5])
    expect(timer.state().mode).toBe(null)
    vi.advanceTimersByTime(5000)
    expect(player.volume).toBe(0.5)
    expect(player.pause).not.toHaveBeenCalled()
  })

  it('waits for the new end when the Listener seeks back during an "End of this Track" fade', () => {
    timer.startEndOfTrack()
    audio.currentTime = 249
    audio.dispatchEvent(new Event('timeupdate'))
    vi.advanceTimersByTime(1000)
    expect(audio.volume).toBeLessThan(0.6)

    audio.currentTime = 100
    audio.dispatchEvent(new Event('timeupdate'))
    vi.advanceTimersByTime(3000)

    expect(audio.pause).not.toHaveBeenCalled()
    expect(audio.volume).toBeCloseTo(0.64)
    expect(timer.state().mode).toBe('endOfTrack')
  })

  it('at "End of this Track", fades as the Track ends and pauses before the next one starts', () => {
    timer.startEndOfTrack()
    const playTo = (seconds) => {
      audio.currentTime = seconds
      audio.dispatchEvent(new Event('timeupdate'))
    }
    playTo(247)
    vi.advanceTimersByTime(1000)
    expect(audio.volume).toBeCloseTo(0.64)

    playTo(249) // 3s left, inside the 4s fade
    vi.advanceTimersByTime(1500)
    expect(audio.volume).toBeCloseTo(0.32, 1)

    vi.advanceTimersByTime(1500)
    expect(audio.pause).toHaveBeenCalledTimes(1)
    expect(audio.volume).toBeCloseTo(0.64)
    expect(timer.state().mode).toBe(null)
  })

  it('does nothing once cancelled', () => {
    const listener = vi.fn()
    timer.subscribe(listener)
    timer.start(30)
    vi.advanceTimersByTime(minutes(10))
    expect(timer.remainingSeconds()).toBe(20 * 60)

    timer.cancel()
    expect(timer.state().mode).toBe(null)
    expect(timer.remainingSeconds()).toBe(null)
    expect(listener).toHaveBeenCalledTimes(2)

    vi.advanceTimersByTime(minutes(30))
    audio.dispatchEvent(new Event('timeupdate'))
    expect(audio.pause).not.toHaveBeenCalled()
    expect(audio.volume).toBeCloseTo(0.64)
  })

  it('puts the volume back when cancelled while fading, and keeps playing', () => {
    timer.start(15)
    vi.advanceTimersByTime(minutes(15) - 2000)
    expect(timer.state().fading).toBe(true)
    expect(audio.volume).toBeLessThan(0.64)

    timer.cancel()
    expect(timer.state().fading).toBe(false)
    expect(audio.volume).toBeCloseTo(0.64)
    vi.advanceTimersByTime(5000)
    expect(audio.pause).not.toHaveBeenCalled()
    expect(audio.volume).toBeCloseTo(0.64)
  })

  it('stops, with the volume put back, when the Listener pauses during the fade', () => {
    timer.start(15)
    vi.advanceTimersByTime(minutes(15) - 2000)
    audio.volume = 0.1 // the package's own fade-out ending where ours was
    audio.pause()

    expect(audio.volume).toBeCloseTo(0.64)
    expect(timer.state().mode).toBe(null)
    vi.advanceTimersByTime(5000)
    expect(audio.volume).toBeCloseTo(0.64)
  })

  it('counts the end of this Track in real time, at the playback speed', () => {
    audio.playbackRate = 2
    timer.startEndOfTrack()
    expect(timer.remainingSeconds()).toBe(106) // (252 - 40) / 2

    audio.currentTime = 245 // 7 Track seconds, 3.5s real, left
    audio.dispatchEvent(new Event('timeupdate'))
    vi.advanceTimersByTime(3500)
    expect(audio.pause).toHaveBeenCalledTimes(1)
  })
})
