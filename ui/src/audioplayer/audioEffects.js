import { useSelector } from 'react-redux'

// iPhone and iPad, including iPadOS, which presents itself as a desktop Mac
// but has a touch screen (no Mac does).
export const isIOS = ({ userAgent, maxTouchPoints } = navigator) =>
  /iPhone|iPad|iPod/.test(userAgent) ||
  (/Macintosh/.test(userAgent) && maxTouchPoints > 1)

// Whether the equalizer and visuals may use the shared audio chain. On iOS an
// installed app's AudioContext can be suspended when the screen locks, so they
// wait for the Audio effects switch there (ADR-0010). ReplayGain is not gated.
export const selectAudioEffectsAvailable = (state, ios = isIOS()) =>
  !ios || state.settings?.audioEffects === true

export const useAudioEffectsAvailable = () =>
  useSelector((state) => selectAudioEffectsAvailable(state))
