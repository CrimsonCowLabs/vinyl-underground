import { BANDS } from './equalizerCurve'

// The equalizer's settings, kept per device in settings.equalizer:
//   on      whether it shapes the sound at all
//   bands   each band's gain in dB, ±12, lowest band first
//   preamp  the Listener's own preamp in dB, -12 to 0: it only lowers, so it
//           can't clip (the automatic lowering for boosts is on top; see preampGain)
//   preset  the preset the curve came from, built-in or custom, or null once
//           the Listener has moved a slider
//   custom  the Listener's own presets: { id, name, bands, preamp }
// Every change goes through the functions below, which return new settings
// for setEqualizer to store.

export const MAX_DB = 12

const FLAT = BANDS.map(() => 0)

// The built-in presets, in the order they are offered. Each name is
// nowPlaying.eq.presets.<id>.
export const BUILT_IN_PRESETS = [
  { id: 'flat', bands: FLAT },
  { id: 'bassBoost', bands: [6, 5, 4, 2, 0, 0, 0, 0, 0, 0] },
  { id: 'trebleBoost', bands: [0, 0, 0, 0, 0, 0, 2, 4, 5, 6] },
  { id: 'vocal', bands: [-2, -2, -1, 0, 2, 4, 4, 3, 1, 0] },
  // A smile: lifts the lows and highs the ear hears less of when quiet
  { id: 'loudness', bands: [6, 4, 2, 0, -1, -1, 0, 2, 4, 5] },
  // Takes the boom and the hiss out, for listening quietly
  { id: 'lateNight', bands: [-6, -4, -2, 0, 1, 2, 2, 0, -2, -4] },
].map((preset) => ({ ...preset, preamp: 0 }))

const DEFAULTS = {
  on: false,
  bands: FLAT,
  preamp: 0,
  preset: 'flat',
  custom: [],
}

// selectEqualizer is the stored settings over the defaults. It returns the
// same object while they are unchanged, so components and effects using it
// only update when the equalizer does.
let lastStored
let lastSelected
export const selectEqualizer = (state) => {
  const stored = state.settings?.equalizer
  if (!lastSelected || stored !== lastStored) {
    lastStored = stored
    lastSelected = { ...DEFAULTS, ...stored }
    if (lastSelected.bands?.length !== BANDS.length) {
      lastSelected.bands = DEFAULTS.bands
    }
  }
  return lastSelected
}

const clamp = (db, max = MAX_DB) =>
  Math.max(-MAX_DB, Math.min(max, Math.round(db)))

const findPreset = (eq, id) =>
  BUILT_IN_PRESETS.find((p) => p.id === id) ||
  eq.custom.find((p) => p.id === id)

export const setOn = (eq, on) => ({ ...eq, on })

export const choosePreset = (eq, id) => {
  const preset = findPreset(eq, id)
  if (!preset) return eq
  return {
    ...eq,
    on: true,
    preset: id,
    bands: preset.bands,
    preamp: preset.preamp,
  }
}

export const setBand = (eq, index, db) => ({
  ...eq,
  on: true,
  preset: null,
  bands: eq.bands.map((gain, i) => (i === index ? clamp(db) : gain)),
})

export const setPreamp = (eq, db) => ({
  ...eq,
  on: true,
  preset: null,
  preamp: clamp(db, 0),
})

const sameName = (a, b) =>
  a.trim().toLocaleLowerCase() === b.trim().toLocaleLowerCase()

// nameTaken: whether a custom preset other than exceptId is called name
export const nameTaken = (eq, name, exceptId) =>
  eq.custom.some((p) => p.id !== exceptId && sameName(p.name, name))

// pickedCustomPreset is the custom preset the curve came from, if any
export const pickedCustomPreset = (eq) =>
  eq.custom.find((p) => p.id === eq.preset)

// presetName is what the curve is called: its preset's name, or "Custom"
// once the Listener has moved a slider.
export const presetName = (eq, translate) => {
  if (BUILT_IN_PRESETS.some((p) => p.id === eq.preset)) {
    return translate(`nowPlaying.eq.presets.${eq.preset}`)
  }
  return pickedCustomPreset(eq)?.name || translate('nowPlaying.eq.custom')
}

const nextId = (custom) =>
  `custom-${
    custom.reduce(
      (max, { id }) => Math.max(max, Number(id.split('-')[1]) || 0),
      0,
    ) + 1
  }`

// savePreset keeps the current curve as a custom preset called name, and
// picks it. Saving under a name already used replaces that preset.
export const savePreset = (eq, name) => {
  const trimmed = name.trim()
  if (!trimmed) return eq
  const existing = eq.custom.find((p) => sameName(p.name, trimmed))
  const preset = {
    id: existing?.id || nextId(eq.custom),
    name: trimmed,
    bands: eq.bands,
    preamp: eq.preamp,
  }
  return {
    ...eq,
    preset: preset.id,
    custom: existing
      ? eq.custom.map((p) => (p.id === existing.id ? preset : p))
      : [...eq.custom, preset],
  }
}

// renamePreset renames a custom preset, unless another one has the name
export const renamePreset = (eq, id, name) => {
  const trimmed = name.trim()
  if (!trimmed || nameTaken(eq, trimmed, id)) return eq
  return {
    ...eq,
    custom: eq.custom.map((p) => (p.id === id ? { ...p, name: trimmed } : p)),
  }
}

// deletePreset forgets a custom preset. The curve stays as it is.
export const deletePreset = (eq, id) => ({
  ...eq,
  preset: eq.preset === id ? null : eq.preset,
  custom: eq.custom.filter((p) => p.id !== id),
})
