import { useEffect } from 'react'
import { useSelector } from 'react-redux'
import { audioChain } from '../audioplayer/audioChain'
import { useAudioEffectsAvailable } from '../audioplayer/audioEffects'
import {
  BANDS,
  PEAKING_Q,
  bandType,
  isFlat,
  preampGain,
} from './equalizerCurve'
import { selectEqualizer } from './equalizerSettings'

// How quickly a live change glides to its new value, in seconds, so moving a
// slider doesn't click.
const GLIDE = 0.015

// createEqualizer puts the equalizer into the audio chain's equalizer slot
// (ADR-0010): a preamp gain, then the ten band filters in series. It fills
// the slot only while the equalizer is on, isn't flat and audio effects are
// available, so it never builds the chain for nothing; otherwise the slot is
// empty and passes the audio straight through.
//
// The filters are made in createNodes, the callback handed to
// chain.setEqualizer, which the chain calls again whenever it rebuilds (a
// new player element after a logout and login). So they always take the
// current curve, and the nodes held for live changes are the chain's own.
export const createEqualizer = (chain) => {
  let current = null
  let inSlot = false
  let nodes = null // the ones createNodes made last

  const createNodes = (context) => {
    const gain = context.createGain()
    gain.gain.value = preampGain(current, context.sampleRate)
    const filters = BANDS.map((frequency, index) => {
      const filter = context.createBiquadFilter()
      filter.type = bandType(index)
      filter.frequency.value = frequency
      if (filter.type === 'peaking') filter.Q.value = PEAKING_Q
      filter.gain.value = current.bands[index]
      return filter
    })
    nodes = { context, gain, filters }
    return [gain, ...filters]
  }

  const adjust = () => {
    const { context, gain, filters } = nodes
    const now = context.currentTime
    gain.gain.setTargetAtTime(
      preampGain(current, context.sampleRate),
      now,
      GLIDE,
    )
    filters.forEach((filter, index) =>
      filter.gain.setTargetAtTime(current.bands[index], now, GLIDE),
    )
  }

  return {
    apply(eq, { available, isRadio }) {
      current = eq
      const wanted =
        wantsTheChain(eq, available) &&
        !waitsForTrack(eq, { available, isRadio, built: chain.isBuilt() })
      if (wanted && inSlot && nodes) adjust()
      else if (wanted !== inSlot) {
        inSlot = wanted
        nodes = null
        chain.setEqualizer(wanted ? createNodes : null)
      }
    },
  }
}

// Whether the equalizer has anything to do: on, not flat, and allowed.
const wantsTheChain = (eq, available) => available && eq.on && !isFlat(eq)

// waitsForTrack: whether the equalizer is holding back for Internet Radio.
// Building the chain while a radio stream plays silences a stream from a
// host without CORS until it reloads, so on Internet Radio the equalizer
// only joins a chain that is already built, and otherwise waits for the next
// Library Track. (Once it is in the chain, the chain is built.)
export const waitsForTrack = (eq, { available, isRadio, built }) =>
  wantsTheChain(eq, available) && isRadio && !built

export const equalizer = createEqualizer(audioChain)

// useEqualizer keeps the stored equalizer on the player's audio, for
// whatever it plays: Library Tracks, Stations, Downloads offline, with or
// without ReplayGain. The player calls it once.
export const useEqualizer = (isRadio) => {
  const eq = useSelector(selectEqualizer)
  const available = useAudioEffectsAvailable()
  useEffect(() => {
    equalizer.apply(eq, { available, isRadio })
  }, [eq, available, isRadio])
}
