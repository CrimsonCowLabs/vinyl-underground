import { useEffect, useState } from 'react'
import { useSelector } from 'react-redux'
import { audioChain } from '../audioplayer/audioChain'

// useVisualAnalyser is the shared chain's AnalyserNode for a visual to read
// (ADR-0010), or null, with `waiting` true while it holds back for Internet
// Radio. Call it only where a visual is shown: asking builds the chain.
//
// - Internet Radio: building the chain mid-stream silences a host without
//   CORS, so on radio it only joins a chain already built (by ReplayGain or
//   the equalizer), and otherwise waits for the next Library Track.
// - A player remount (a new audio element) rebuilds the chain, and the old
//   analyser goes quiet, so it asks again whenever the chain changes.
export const useVisualAnalyser = (audioInstance) => {
  const isRadio = useSelector((state) => !!state.player?.current?.isRadio)
  const [found, setFound] = useState({ analyser: null, waiting: false })

  useEffect(() => {
    if (!audioInstance) {
      setFound({ analyser: null, waiting: false })
      return
    }
    const ask = () => {
      if (isRadio && !audioChain.isBuilt()) {
        setFound({ analyser: null, waiting: true })
      } else {
        setFound({ analyser: audioChain.getAnalyser(), waiting: false })
      }
    }
    ask()
    return audioChain.onChange(ask)
  }, [audioInstance, isRadio])

  return found
}
