import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { settingsReducer } from '../reducers'
import NowPlayingToolbar from '../nowPlaying/NowPlayingToolbar'
import { sleepTimer } from './sleepTimer'
import { usePlaybackSpeed } from './playbackSpeed'
// Registers the Sleep and Speed tools, as the app does at load
import './index'

vi.mock('react-admin', () => ({
  useTranslate: () => (key, opts) =>
    opts?.smart_count != null ? `${key}:${opts.smart_count}` : key,
}))

// Stands in for the player package's audio element
const fakeAudio = () => {
  const audio = new EventTarget()
  return Object.assign(audio, {
    currentTime: 40,
    duration: 252,
    paused: false,
    volume: 1,
    playbackRate: 1,
    defaultPlaybackRate: 1,
    pause: vi.fn(() => {
      audio.paused = true
      audio.dispatchEvent(new Event('pause'))
    }),
  })
}

// The player's part: it hands the element to the sleep timer and keeps the
// chosen speed on it
const PlayerWiring = ({ audio, isRadio }) => {
  usePlaybackSpeed(audio, isRadio)
  return null
}

const setup = ({ settings, isRadio = false } = {}) => {
  const store = createStore(
    combineReducers({ settings: settingsReducer }),
    settings ? { settings } : undefined,
  )
  const audio = fakeAudio()
  sleepTimer.attach(audio)
  render(
    <Provider store={store}>
      <ThemeProvider theme={createTheme()}>
        <PlayerWiring audio={audio} isRadio={isRadio} />
        <NowPlayingToolbar onNavigate={vi.fn()} />
      </ThemeProvider>
    </Provider>,
  )
  return { store, audio }
}

const toolbar = () => screen.getByRole('toolbar', { name: 'nowPlaying.tools' })
const sleepButton = () =>
  screen.getByRole('button', { name: /^nowPlaying\.sleep\.label/ })
const speedButton = () =>
  screen.getByRole('button', { name: /^nowPlaying\.speed\.label/ })
const choose = (name) => {
  fireEvent.click(screen.getByRole('menuitemradio', { name }))
}

describe('Sleep and Speed tools', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    sleepTimer.cancel()
    cleanup()
    vi.useRealTimers()
  })

  it('show the Now Playing toolbar, Sleep before Speed', () => {
    setup()
    const labels = Array.from(toolbar().querySelectorAll('button')).map((b) =>
      b.getAttribute('aria-label'),
    )
    expect(labels).toEqual(['nowPlaying.sleep.label', 'nowPlaying.speed.label'])
  })

  describe('Sleep', () => {
    it('counts down the minutes chosen in the toolbar', () => {
      setup()
      fireEvent.click(sleepButton())
      choose('nowPlaying.sleep.minutes:15')

      expect(sleepButton()).toHaveTextContent('15:00')
      expect(sleepButton()).toHaveAttribute('aria-pressed', 'true')

      act(() => vi.advanceTimersByTime(61 * 1000))
      expect(sleepButton()).toHaveTextContent('13:59')
    })

    it('shows the time left in the Track for "End of this Track"', () => {
      setup()
      fireEvent.click(sleepButton())
      choose('nowPlaying.sleep.endOfTrack')

      expect(sleepButton()).toHaveTextContent('3:32') // 252 - 40 seconds
    })

    it('can be cancelled from the toolbar', () => {
      const { audio } = setup()
      fireEvent.click(sleepButton())
      choose('nowPlaying.sleep.minutes:30')

      fireEvent.click(sleepButton())
      choose('nowPlaying.sleep.off')

      expect(sleepButton()).toHaveAttribute('aria-pressed', 'false')
      expect(sleepButton()).toHaveTextContent('nowPlaying.sleep.label')
      act(() => vi.advanceTimersByTime(31 * 60 * 1000))
      expect(audio.pause).not.toHaveBeenCalled()
    })

    it("doesn't offer the end of the Track on Internet Radio", () => {
      const { audio } = setup({ isRadio: true })
      audio.duration = Infinity
      fireEvent.click(sleepButton())

      expect(
        screen.getByRole('menuitemradio', {
          name: 'nowPlaying.sleep.endOfTrack',
        }),
      ).toHaveAttribute('aria-disabled', 'true')
    })
  })

  describe('Speed', () => {
    it('plays the Track at the speed chosen, keeping its pitch', () => {
      const { audio } = setup()
      fireEvent.click(speedButton())
      choose('1.5×')

      expect(audio.playbackRate).toBe(1.5)
      expect(audio.preservesPitch).toBe(true)
      expect(speedButton()).toHaveTextContent('1.5×')
      expect(speedButton()).toHaveAttribute('aria-pressed', 'true')
    })

    it('keeps the speed for the Tracks after it', () => {
      const { audio } = setup()
      fireEvent.click(speedButton())
      choose('2×')

      // Loading the next Track resets the element's rate
      audio.playbackRate = 1
      audio.dispatchEvent(new Event('loadedmetadata'))

      expect(audio.playbackRate).toBe(2)
    })

    it('is kept on this device', () => {
      const { store } = setup()
      fireEvent.click(speedButton())
      choose('0.75×')
      expect(store.getState().settings.playbackSpeed).toBe(0.75)
      cleanup()

      // A reload starts from the settings kept in localStorage
      const { audio } = setup({ settings: store.getState().settings })

      expect(audio.playbackRate).toBe(0.75)
      expect(speedButton()).toHaveTextContent('0.75×')
    })

    it('plays Internet Radio at normal speed', () => {
      const { audio } = setup({
        settings: { playbackSpeed: 1.5 },
        isRadio: true,
      })
      expect(audio.playbackRate).toBe(1)
    })
  })
})
