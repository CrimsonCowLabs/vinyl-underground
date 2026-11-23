import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { createMemoryHistory } from 'history'
import { playerReducer, settingsReducer } from '../reducers'
import createAdminStore from '../store/createAdminStore'
import NowPlayingToolbar from '../nowPlaying/NowPlayingToolbar'
import NowPlayingTabs from '../nowPlaying/NowPlayingTabs'
import ArtTab from '../nowPlaying/ArtTab'
import { nowPlayingReducer } from '../nowPlaying/nowPlayingState'
import { VisualsToggle } from '../personal/VisualsToggle'
import { audioChain } from '../audioplayer/audioChain'
// Registers the Visuals tool, as the app does at load
import './index'

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  useTranslate: () => (key, opts) =>
    opts?.name != null ? `${key}:${opts.name}` : key,
}))

const IPHONE =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15'

// A canvas 2D context that accepts any drawing (jsdom has none)
const fakeContext = () => {
  const context = new Proxy(function () {}, {
    get: (target, key) => (key in target ? target[key] : context),
    set: () => true,
    apply: () => context,
  })
  return context
}

const fakeAnalyser = () => ({
  frequencyBinCount: 1024,
  fftSize: 2048,
  context: { sampleRate: 48000 },
  getByteFrequencyData: vi.fn((data) => data.fill(200)),
  getByteTimeDomainData: vi.fn((data) => data.fill(128)),
})

// The app's own store, which keeps settings in localStorage
const appStore = () =>
  createAdminStore({
    authProvider: {},
    dataProvider: {},
    history: createMemoryHistory(),
    customReducers: {
      settings: settingsReducer,
      player: playerReducer,
      nowPlaying: nowPlayingReducer,
    },
  })

const setup = ({
  settings,
  onIOS = false,
  isRadio = false,
  store: given,
} = {}) => {
  if (onIOS) vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(IPHONE)
  const store =
    given ||
    createStore(
      combineReducers({
        settings: settingsReducer,
        nowPlaying: nowPlayingReducer,
        player: (state = { current: { isRadio } }) => state,
      }),
      settings ? { settings } : undefined,
    )
  const onNavigate = vi.fn()
  render(
    <Provider store={store}>
      <ThemeProvider theme={createTheme()}>
        <NowPlayingTabs />
        <ArtTab cover="/cover.jpg" alt="Cover" playing audioInstance={{}} />
        <NowPlayingToolbar onNavigate={onNavigate} />
        <VisualsToggle />
      </ThemeProvider>
    </Provider>,
  )
  return { store, onNavigate }
}

// Hidden from the accessibility tree while a sheet or dialog is over it
const visualsButton = () =>
  screen.queryByRole('button', {
    name: /^nowPlaying\.visuals\.label/,
    hidden: true,
  })
const openSheet = () => fireEvent.click(visualsButton())
const sheet = () =>
  document.querySelector('[aria-label="nowPlaying.visuals.title"]')
const choose = (mode) =>
  fireEvent.click(
    within(sheet()).getByRole('menuitemradio', {
      name: `nowPlaying.visuals.modes.${mode}`,
    }),
  )
const warning = () =>
  screen.queryByRole('dialog', { name: 'nowPlaying.visuals.warningTitle' })
const visual = (mode) =>
  screen.queryByRole('img', {
    name: `nowPlaying.visuals.canvas:nowPlaying.visuals.modes.${mode}`,
    hidden: true,
  })
const cover = () => screen.queryByRole('img', { name: 'Cover', hidden: true })
const artTab = () => screen.getByRole('tab', { selected: true, hidden: true })

const turnOn = (mode) => {
  openSheet()
  choose(mode)
  if (warning()) {
    fireEvent.click(
      within(warning()).getByRole('button', {
        name: 'nowPlaying.visuals.turnOn',
      }),
    )
  }
}

describe('visuals in Now Playing', () => {
  let analyser
  beforeEach(() => {
    analyser = fakeAnalyser()
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(
      fakeContext,
    )
    vi.spyOn(audioChain, 'getAnalyser').mockReturnValue(analyser)
    vi.spyOn(audioChain, 'isBuilt').mockReturnValue(false)
  })
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('shows the album art while visuals are off', () => {
    setup()
    expect(cover()).toBeInTheDocument()
    expect(document.querySelector('canvas')).toBeNull()
    expect(visualsButton()).toHaveAttribute('aria-pressed', 'false')
    expect(artTab()).toHaveTextContent('nowPlaying.tabs.art')
    expect(audioChain.getAnalyser).not.toHaveBeenCalled()
  })

  it('plays the visual chosen from the Visuals tool in the art area', () => {
    setup()
    turnOn('spectrum')

    expect(visual('spectrum')).toBeInTheDocument()
    expect(audioChain.getAnalyser).toHaveBeenCalled()
    expect(visualsButton()).toHaveAttribute('aria-pressed', 'true')
    expect(visualsButton()).toHaveTextContent(
      'nowPlaying.visuals.modes.spectrum',
    )
    expect(artTab()).toHaveTextContent('nowPlaying.tabs.visuals')

    turnOn('waveform')
    expect(visual('waveform')).toBeInTheDocument()
    expect(visual('spectrum')).toBeNull()

    // The glow surrounds the cover rather than covering it
    turnOn('glow')
    expect(visual('glow')).toBeInTheDocument()
    expect(cover()).toBeInTheDocument()
  })

  it('goes back to the album art when Off is chosen', () => {
    setup()
    turnOn('spectrum')
    openSheet()
    choose('off')
    expect(visual('spectrum')).toBeNull()
    expect(cover()).toBeInTheDocument()
    expect(visualsButton()).toHaveAttribute('aria-pressed', 'false')
  })

  it('shows the Art tab when a visual is chosen from another tab', () => {
    const { store } = setup()
    act(() => {
      store.dispatch({ type: 'NOW_PLAYING_TAB', tab: 'queue' })
    })
    turnOn('glow')
    expect(store.getState().nowPlaying.tab).toBe('art')
  })

  describe('the photosensitivity warning', () => {
    it('is shown the first time visuals are turned on, and only then', () => {
      setup()
      openSheet()
      choose('spectrum')
      expect(warning()).toBeInTheDocument()
      // Not on until the Listener agrees
      expect(visual('spectrum')).toBeNull()
      fireEvent.click(
        within(warning()).getByRole('button', {
          name: 'nowPlaying.visuals.turnOn',
        }),
      )
      expect(visual('spectrum')).toBeInTheDocument()

      openSheet()
      choose('off')
      openSheet()
      choose('waveform')
      expect(warning()).toBeNull()
      expect(visual('waveform')).toBeInTheDocument()
    })

    it('leaves visuals off if the Listener cancels', () => {
      setup()
      openSheet()
      choose('glow')
      fireEvent.click(
        within(warning()).getByRole('button', {
          name: 'nowPlaying.visuals.cancel',
        }),
      )
      expect(visual('glow')).toBeNull()
      expect(cover()).toBeInTheDocument()

      // Still to be agreed to next time
      openSheet()
      choose('glow')
      expect(warning()).toBeInTheDocument()
    })
  })

  it('keeps the chosen visual on this device', () => {
    vi.useFakeTimers()
    try {
      localStorage.removeItem('state')
      setup({ store: appStore() })
      turnOn('waveform')
      act(() => vi.advanceTimersByTime(1500)) // the store saves, throttled
      cleanup()

      // A reload starts from what was kept in localStorage
      setup({ store: appStore() })
      expect(visual('waveform')).toBeInTheDocument()
      openSheet()
      choose('spectrum')
      expect(warning()).toBeNull()
    } finally {
      vi.useRealTimers()
      localStorage.removeItem('state')
    }
  })

  describe('the setting that turns visuals off', () => {
    const settingSwitch = () =>
      screen.getByRole('checkbox', {
        name: 'menu.personal.options.visuals.name',
        hidden: true,
      })

    it('is on by default', () => {
      setup()
      expect(settingSwitch()).toBeChecked()
    })

    it('hides the Visuals tool and shows the album art', () => {
      setup()
      turnOn('spectrum')
      fireEvent.click(settingSwitch())

      expect(visualsButton()).toBeNull()
      expect(visual('spectrum')).toBeNull()
      expect(cover()).toBeInTheDocument()
      expect(artTab()).toHaveTextContent('nowPlaying.tabs.art')

      // The visual chosen comes back if they are allowed again
      fireEvent.click(settingSwitch())
      expect(visual('spectrum')).toBeInTheDocument()
    })
  })

  describe('on an iPhone', () => {
    it('points to the Audio effects switch while it is off', () => {
      const { onNavigate } = setup({
        onIOS: true,
        settings: { visuals: { mode: 'spectrum', warned: true } },
      })
      expect(visual('spectrum')).toBeNull()
      expect(cover()).toBeInTheDocument()
      expect(audioChain.getAnalyser).not.toHaveBeenCalled()

      openSheet()
      expect(
        within(sheet()).getByText('nowPlaying.visuals.needsAudioEffects'),
      ).toBeInTheDocument()
      fireEvent.click(
        within(sheet()).getByRole('button', {
          name: 'nowPlaying.visuals.openSettings',
        }),
      )
      expect(onNavigate).toHaveBeenCalledWith('/personal')
    })

    it('plays visuals once Audio effects are on', () => {
      setup({
        onIOS: true,
        settings: {
          audioEffects: true,
          visuals: { mode: 'spectrum', warned: true },
        },
      })
      expect(visual('spectrum')).toBeInTheDocument()
    })
  })

  describe('on Internet Radio', () => {
    it('never builds the audio chain, and says visuals wait', () => {
      setup({
        isRadio: true,
        settings: { visuals: { mode: 'spectrum', warned: true } },
      })
      expect(audioChain.getAnalyser).not.toHaveBeenCalled()
      expect(visual('spectrum')).toBeNull()
      expect(cover()).toBeInTheDocument()
      expect(
        screen.getByText('nowPlaying.visuals.radioWaits'),
      ).toBeInTheDocument()
    })

    it('joins a chain that is already built', () => {
      audioChain.isBuilt.mockReturnValue(true)
      setup({
        isRadio: true,
        settings: { visuals: { mode: 'spectrum', warned: true } },
      })
      expect(visual('spectrum')).toBeInTheDocument()
    })
  })

  it('asks for the analyser again when the chain is rebuilt', () => {
    let rebuilt
    vi.spyOn(audioChain, 'onChange').mockImplementation((listener) => {
      rebuilt = listener
      return () => {}
    })
    setup({ settings: { visuals: { mode: 'spectrum', warned: true } } })
    expect(audioChain.getAnalyser).toHaveBeenCalledTimes(1)

    // The player remounted with a new element
    act(() => rebuilt())
    expect(audioChain.getAnalyser).toHaveBeenCalledTimes(2)
  })

  describe('Full screen', () => {
    const fullScreenButton = () =>
      screen.getByRole('button', { name: 'nowPlaying.visuals.fullScreen' })
    const fullScreen = () =>
      screen.queryByRole('dialog', { name: 'nowPlaying.visuals.fullScreen' })

    it('expands the visual to the whole window, and Esc returns', () => {
      setup({ settings: { visuals: { mode: 'waveform', warned: true } } })
      fireEvent.click(fullScreenButton())
      expect(
        within(fullScreen()).getByRole('img', {
          name: 'nowPlaying.visuals.canvas:nowPlaying.visuals.modes.waveform',
        }),
      ).toBeInTheDocument()

      fireEvent.keyDown(fullScreen(), { key: 'Escape' })
      expect(fullScreen()).toBeNull()
      expect(visual('waveform')).toBeInTheDocument()
    })

    it('uses the browser full screen where it can, and leaves with it', async () => {
      let shown = null
      Object.defineProperty(document, 'fullscreenElement', {
        configurable: true,
        get: () => shown,
      })
      HTMLElement.prototype.requestFullscreen = vi.fn(() => {
        shown = document.body
        return Promise.resolve()
      })
      document.exitFullscreen = vi.fn(() => Promise.resolve())
      try {
        setup({ settings: { visuals: { mode: 'glow', warned: true } } })
        fireEvent.click(fullScreenButton())
        expect(HTMLElement.prototype.requestFullscreen).toHaveBeenCalled()
        expect(shown).not.toBeNull()

        // Esc in browser full screen: the browser leaves it, then tells us
        await act(async () => {
          shown = null
          document.dispatchEvent(new Event('fullscreenchange'))
        })
        expect(fullScreen()).toBeNull()
      } finally {
        delete HTMLElement.prototype.requestFullscreen
        delete document.exitFullscreen
        delete document.fullscreenElement
      }
    })
  })
})
