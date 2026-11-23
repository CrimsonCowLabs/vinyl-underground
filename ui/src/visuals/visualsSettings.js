import { shallowEqual, useSelector } from 'react-redux'
import { selectAudioEffectsAvailable } from '../audioplayer/audioEffects'
import { OFF, visualMode } from './visualModes'

// selectVisuals is the visuals settings, kept per device in the settings
// slice: the visual chosen (`mode`, or 'off'), whether the photosensitivity
// warning has been agreed to (`warned`), and whether visuals are allowed at
// all (`enabled`, the switch in Personal). Settings saved before visuals, or
// naming a visual no longer offered, read as off.
export const selectVisuals = (state) => {
  const saved = state.settings?.visuals || {}
  return {
    mode: visualMode(saved.mode) ? saved.mode : OFF,
    warned: saved.warned === true,
    enabled: saved.enabled !== false,
  }
}

// selectVisualShown is the visual Now Playing plays, or null: none chosen,
// visuals turned off in Personal, or an iPhone with Audio effects off
// (ADR-0010).
export const selectVisualShown = (state, ios) => {
  const { mode, enabled } = selectVisuals(state)
  return enabled && mode !== OFF && selectAudioEffectsAvailable(state, ios)
    ? mode
    : null
}

export const useVisuals = () => useSelector(selectVisuals, shallowEqual)
export const useVisualShown = () =>
  useSelector((state) => selectVisualShown(state))
