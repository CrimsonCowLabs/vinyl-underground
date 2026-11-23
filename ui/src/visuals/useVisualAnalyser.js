import { useEffect, useState } from 'react'
import { useSelector } from 'react-redux'
import { audioChain } from '../audioplayer/audioChain'

const useIsRadio = () =>
  useSelector((state) => !!state.player?.current?.isRadio)

// Whether visuals hold back for Internet Radio: it's playing, and nothing
// else has built the chain yet
const radioWaits = (isRadio) => isRadio && !audioChain.isBuilt()

// useRadioWaits is whether visuals are holding back for Internet Radio, kept
// up to date as the chain is built
export const useRadioWaits = () => {
  const isRadio = useIsRadio()
  const [, changed] = useState(0)
  useEffect(() => audioChain.onChange(() => changed((n) => n + 1)), [])
  return radioWaits(isRadio)
}

// useVisualAnalyser is the shared chain's AnalyserNode for a visual to read
// (ADR-0010), or null, with `waiting` true while it holds back for Internet
// Radio. Call it only where a visual is shown: asking builds the chain, and
// it lets go of the analyser when unmounted, so only one may be in use.
//
// - Internet Radio: building the chain mid-stream silences a host without
//   CORS, so on radio it only joins a chain already built (by ReplayGain or
//   the equalizer), and otherwise waits for the next Library Track.
// - A player remount (a new audio element) rebuilds the chain, and the old
//   analyser goes quiet, so it asks again whenever the chain changes.
export const useVisualAnalyser = (audioInstance) => {
  const isRadio = useIsRadio()
  const [found, setFound] = useState({ analyser: null, waiting: false })

  useEffect(() => {
    if (!audioInstance) {
      setFound({ analyser: null, waiting: false })
      return
    }
    const ask = () => {
      if (radioWaits(isRadio)) {
        // Let go, so a remount mid-stream doesn't build the chain for us
        audioChain.releaseAnalyser()
        setFound({ analyser: null, waiting: true })
      } else {
        setFound({ analyser: audioChain.getAnalyser(), waiting: false })
      }
    }
    ask()
    const stopListening = audioChain.onChange(ask)
    return () => {
      stopListening()
      audioChain.releaseAnalyser()
    }
  }, [audioInstance, isRadio])

  return found
}
