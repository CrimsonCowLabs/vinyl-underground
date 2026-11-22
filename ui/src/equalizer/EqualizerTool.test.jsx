import React from 'react'
import { Provider } from 'react-redux'
import { combineReducers, legacy_createStore as createStore } from 'redux'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import { ThemeProvider, createTheme } from '@material-ui/core/styles'
import { settingsReducer } from '../reducers'
import { selectEqualizer } from './equalizerSettings'
import EqualizerTool from './EqualizerTool'

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  useTranslate: () => (key, opts) =>
    opts?.name != null
      ? `${key}:${opts.name}`
      : opts?.db != null
        ? `${key}:${opts.db}`
        : key,
}))

const IPHONE =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15'

const setup = ({ settings, onIOS = false } = {}) => {
  if (onIOS) vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(IPHONE)
  const store = createStore(
    combineReducers({ settings: settingsReducer }),
    settings ? { settings } : undefined,
  )
  const onNavigate = vi.fn()
  render(
    <Provider store={store}>
      <ThemeProvider theme={createTheme()}>
        <EqualizerTool onNavigate={onNavigate} />
      </ThemeProvider>
    </Provider>,
  )
  const eq = () => selectEqualizer(store.getState())
  return { store, eq, onNavigate }
}

// Hidden from the accessibility tree while the sheet is open over it
const eqButton = () =>
  screen.getByRole('button', { name: /^nowPlaying\.eq\.label/, hidden: true })
const sheet = () => document.querySelector('[aria-label="nowPlaying.eq.title"]')
const preset = (name) => within(sheet()).getByRole('radio', { name })
const onSwitch = () =>
  within(sheet()).getByRole('checkbox', { name: 'nowPlaying.eq.on' })
const bandSlider = (label) =>
  within(sheet()).getByRole('slider', { name: label })

describe('the EQ tool', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('shows the equalizer as off until it is turned on', () => {
    setup()
    expect(eqButton()).toHaveAttribute('aria-pressed', 'false')
    expect(eqButton()).toHaveTextContent('nowPlaying.eq.label')
  })

  it('picks a built-in preset, turning the equalizer on', () => {
    const { eq } = setup()
    fireEvent.click(eqButton())
    fireEvent.click(preset('nowPlaying.eq.presets.bassBoost'))

    expect(eq().on).toBe(true)
    expect(eq().bands).toEqual([6, 5, 4, 2, 0, 0, 0, 0, 0, 0])
    expect(preset('nowPlaying.eq.presets.bassBoost')).toHaveAttribute(
      'aria-checked',
      'true',
    )
    expect(eqButton()).toHaveAttribute('aria-pressed', 'true')
    expect(eqButton()).toHaveTextContent('nowPlaying.eq.presets.bassBoost')
  })

  it('shows each band and the preamp as a slider', () => {
    setup({
      settings: {
        equalizer: {
          on: true,
          bands: [6, 5, 4, 2, 0, 0, 0, 0, 0, -3],
          preamp: 0,
          preset: null,
          custom: [],
        },
      },
    })
    fireEvent.click(eqButton())

    expect(bandSlider('31 Hz')).toHaveAttribute('aria-valuenow', '6')
    expect(bandSlider('1 kHz')).toHaveAttribute('aria-valuenow', '0')
    expect(bandSlider('16 kHz')).toHaveAttribute('aria-valuenow', '-3')
    expect(bandSlider('nowPlaying.eq.preamp')).toHaveAttribute(
      'aria-valuenow',
      '0',
    )
    // and how far the level is lowered automatically for the boost
    expect(sheet()).toHaveTextContent('nowPlaying.eq.autoPreamp:6.2')
  })

  it('moves a band from the keyboard', () => {
    const { eq } = setup({
      settings: {
        equalizer: {
          on: true,
          bands: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
          preamp: 0,
          preset: 'flat',
          custom: [],
        },
      },
    })
    fireEvent.click(eqButton())
    fireEvent.keyDown(bandSlider('4 kHz'), { key: 'ArrowUp' })

    expect(eq().bands[7]).toBe(1)
    expect(eq().preset).toBe(null)
    expect(eqButton()).toHaveTextContent('nowPlaying.eq.custom')
  })

  it('turns off and on again with the switch, keeping the curve', () => {
    const { eq } = setup()
    fireEvent.click(eqButton())
    fireEvent.click(preset('nowPlaying.eq.presets.vocal'))

    fireEvent.click(onSwitch())
    expect(eq().on).toBe(false)
    expect(eqButton()).toHaveAttribute('aria-pressed', 'false')

    fireEvent.click(onSwitch())
    expect(eq().on).toBe(true)
    expect(eq().preset).toBe('vocal')
  })

  describe('custom presets', () => {
    const saveAs = (name) => {
      fireEvent.click(
        within(sheet()).getByRole('button', { name: 'nowPlaying.eq.save' }),
      )
      const dialog = screen.getByRole('dialog', {
        name: 'nowPlaying.eq.saveTitle',
      })
      fireEvent.change(within(dialog).getByRole('textbox'), {
        target: { value: name },
      })
      fireEvent.click(
        within(dialog).getByRole('button', {
          name: 'nowPlaying.eq.saveButton',
        }),
      )
    }

    it('saves the curve under a name, and offers it as a preset', () => {
      const { eq } = setup()
      fireEvent.click(eqButton())
      fireEvent.click(preset('nowPlaying.eq.presets.loudness'))
      saveAs('Car')

      expect(eq().custom.map((p) => p.name)).toEqual(['Car'])
      expect(preset('Car')).toHaveAttribute('aria-checked', 'true')
      expect(eqButton()).toHaveTextContent('Car')
    })

    it('renames the preset picked', () => {
      const { eq } = setup()
      fireEvent.click(eqButton())
      saveAs('Car')

      fireEvent.click(
        within(sheet()).getByRole('button', { name: 'nowPlaying.eq.rename' }),
      )
      const dialog = screen.getByRole('dialog', {
        name: 'nowPlaying.eq.renameTitle',
      })
      const name = within(dialog).getByRole('textbox')
      expect(name).toHaveValue('Car')
      fireEvent.change(name, { target: { value: 'Van' } })
      fireEvent.click(
        within(dialog).getByRole('button', {
          name: 'nowPlaying.eq.saveButton',
        }),
      )

      expect(eq().custom.map((p) => p.name)).toEqual(['Van'])
      expect(preset('Van')).toBeInTheDocument()
    })

    it('deletes the preset picked', () => {
      const { eq } = setup()
      fireEvent.click(eqButton())
      saveAs('Car')
      fireEvent.click(
        within(sheet()).getByRole('button', { name: 'nowPlaying.eq.delete' }),
      )

      expect(eq().custom).toEqual([])
      expect(within(sheet()).queryByRole('radio', { name: 'Car' })).toBeNull()
    })

    it("doesn't offer renaming or deleting a built-in preset", () => {
      setup()
      fireEvent.click(eqButton())
      fireEvent.click(preset('nowPlaying.eq.presets.vocal'))
      expect(
        within(sheet()).queryByRole('button', { name: 'nowPlaying.eq.rename' }),
      ).toBeNull()
      expect(
        within(sheet()).queryByRole('button', { name: 'nowPlaying.eq.delete' }),
      ).toBeNull()
    })
  })

  describe('on an iPhone', () => {
    it('points to the Audio effects switch while it is off', () => {
      const { onNavigate } = setup({ onIOS: true })
      fireEvent.click(eqButton())

      expect(sheet()).toHaveTextContent('nowPlaying.eq.needsAudioEffects')
      expect(within(sheet()).queryAllByRole('slider')).toHaveLength(0)
      fireEvent.click(
        within(sheet()).getByRole('button', {
          name: 'nowPlaying.eq.openSettings',
        }),
      )
      expect(onNavigate).toHaveBeenCalledWith('/personal')
    })

    it("doesn't show a saved equalizer as on while the switch is off", () => {
      setup({
        onIOS: true,
        settings: {
          equalizer: {
            on: true,
            bands: [6, 5, 4, 2, 0, 0, 0, 0, 0, 0],
            preamp: 0,
            preset: 'bassBoost',
            custom: [],
          },
        },
      })
      expect(eqButton()).toHaveAttribute('aria-pressed', 'false')
    })

    it('offers the equalizer once the switch is on', () => {
      setup({ onIOS: true, settings: { audioEffects: true } })
      fireEvent.click(eqButton())
      expect(within(sheet()).getAllByRole('slider')).toHaveLength(11)
    })
  })
})
