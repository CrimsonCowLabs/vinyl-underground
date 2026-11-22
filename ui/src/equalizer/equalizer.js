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
// The filters are made inside the chain's createFilters callback, which the
// chain calls again whenever it rebuilds (a new player element after a
// logout and login), so they always take the current curve, and the nodes
// held for live changes are always the chain's current ones.
export const createEqualizer = (chain) => {
  let current = null
  let inSlot = false
  // The nodes the chain last built from createNodes. The chain calls it again
  // whenever it is rebuilt, so these are always the live ones.
  let nodes = null

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
        available &&
        eq.on &&
        !isFlat(eq) &&
        // Building the chain while a radio stream plays silences a stream
        // from a host without CORS until it reloads, so on Internet Radio
        // the equalizer only joins a chain that is already there, and
        // otherwise waits for the next Library Track.
        (inSlot || !isRadio || chain.isBuilt())
      if (wanted && inSlot && nodes) adjust()
      else if (wanted !== inSlot) {
        inSlot = wanted
        nodes = null
        chain.setEqualizer(wanted ? createNodes : null)
      }
    },
  }
}

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
