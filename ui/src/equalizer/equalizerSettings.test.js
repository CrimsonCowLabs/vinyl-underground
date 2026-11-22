import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createMemoryHistory } from 'history'
import createAdminStore from '../store/createAdminStore'
import { playerReducer, settingsReducer } from '../reducers'
import { setEqualizer } from '../actions'
import {
  choosePreset,
  deletePreset,
  renamePreset,
  savePreset,
  selectEqualizer,
  setBand,
  setPreamp,
  turnOn,
} from './equalizerSettings'

const FLAT = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
const initial = () => selectEqualizer({ settings: {} })

describe('equalizer settings', () => {
  it('start off and flat, with no custom presets', () => {
    expect(initial()).toEqual({
      on: false,
      bands: FLAT,
      preamp: 0,
      preset: 'flat',
      custom: [],
    })
  })

  it('choosing a built-in preset sets its curve and turns the equalizer on', () => {
    const eq = choosePreset(initial(), 'bassBoost')
    expect(eq.on).toBe(true)
    expect(eq.preset).toBe('bassBoost')
    expect(eq.bands).toEqual([6, 5, 4, 2, 0, 0, 0, 0, 0, 0])
    expect(eq.preamp).toBe(0)

    expect(choosePreset(eq, 'lateNight').bands).toEqual([
      -6, -4, -2, 0, 1, 2, 2, 0, -2, -4,
    ])
    expect(choosePreset(eq, 'flat').bands).toEqual(FLAT)
  })

  it('moving a band leaves the preset, and turns the equalizer on', () => {
    const eq = setBand(choosePreset(initial(), 'vocal'), 9, 12.5)
    expect(eq.preset).toBe(null)
    expect(eq.on).toBe(true)
    expect(eq.bands[9]).toBe(12) // held to ±12 dB
  })

  it('moving the preamp leaves the preset', () => {
    const eq = setPreamp(choosePreset(initial(), 'vocal'), -4)
    expect(eq.preamp).toBe(-4)
    expect(eq.preset).toBe(null)
  })

  it('turning off keeps the curve for turning back on', () => {
    const on = choosePreset(initial(), 'loudness')
    const off = turnOn(on, false)
    expect(off.on).toBe(false)
    expect(turnOn(off, true)).toEqual(on)
  })

  describe('custom presets', () => {
    const saved = () =>
      savePreset(setPreamp(setBand(initial(), 2, 5), -2), '  Car  ')

    it('saves the current curve under a name, and picks it', () => {
      const eq = saved()
      expect(eq.custom).toEqual([
        {
          id: expect.any(String),
          name: 'Car',
          bands: [0, 0, 5, 0, 0, 0, 0, 0, 0, 0],
          preamp: -2,
        },
      ])
      expect(eq.preset).toBe(eq.custom[0].id)
    })

    it('can be picked again after the curve has moved', () => {
      const eq = saved()
      const moved = choosePreset(eq, 'trebleBoost')
      const back = choosePreset(moved, eq.custom[0].id)
      expect(back.bands).toEqual([0, 0, 5, 0, 0, 0, 0, 0, 0, 0])
      expect(back.preamp).toBe(-2)
      expect(back.preset).toBe(eq.custom[0].id)
    })

    it('saving under a name already used replaces that preset', () => {
      const eq = savePreset(setBand(saved(), 0, 3), 'car')
      expect(eq.custom).toHaveLength(1)
      expect(eq.custom[0].bands[0]).toBe(3)
      expect(eq.custom[0].name).toBe('car')
    })

    it('keeps each saved preset apart', () => {
      const eq = savePreset(setBand(saved(), 0, 3), 'Headphones')
      expect(eq.custom.map((p) => p.name)).toEqual(['Car', 'Headphones'])
      expect(new Set(eq.custom.map((p) => p.id)).size).toBe(2)
    })

    it('renames one, keeping its curve', () => {
      const eq = saved()
      const renamed = renamePreset(eq, eq.custom[0].id, 'Kitchen')
      expect(renamed.custom[0]).toEqual({ ...eq.custom[0], name: 'Kitchen' })
    })

    it('deletes one, leaving the curve where it is', () => {
      const eq = saved()
      const deleted = deletePreset(eq, eq.custom[0].id)
      expect(deleted.custom).toEqual([])
      expect(deleted.preset).toBe(null)
      expect(deleted.bands).toEqual(eq.bands)
    })

    it('ignores a blank name', () => {
      const eq = saved()
      expect(savePreset(eq, '   ')).toBe(eq)
      expect(renamePreset(eq, eq.custom[0].id, '')).toBe(eq)
    })
  })

  describe('kept per device', () => {
    beforeEach(() => {
      vi.useFakeTimers()
      localStorage.removeItem('state')
    })
    afterEach(() => {
      vi.useRealTimers()
      localStorage.removeItem('state')
    })

    const appStore = () =>
      createAdminStore({
        authProvider: {},
        dataProvider: {},
        history: createMemoryHistory(),
        customReducers: { settings: settingsReducer, player: playerReducer },
      })

    it('keeps the curve, the toggle and custom presets across a reload', () => {
      const store = appStore()
      const eq = savePreset(setBand(initial(), 4, -7), 'Podcasts')
      store.dispatch(setEqualizer(eq))
      vi.advanceTimersByTime(1500) // the store saves, throttled

      const reloaded = appStore()
      expect(selectEqualizer(reloaded.getState())).toEqual(eq)
    })

    it('reads settings saved before the equalizer existed', () => {
      localStorage.setItem(
        'state',
        JSON.stringify({ settings: { playbackSpeed: 1.5 } }),
      )
      expect(selectEqualizer(appStore().getState())).toEqual(initial())
    })
  })
})
