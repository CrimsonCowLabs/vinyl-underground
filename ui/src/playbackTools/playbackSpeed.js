import { useEffect } from 'react'
import { useSelector } from 'react-redux'

// The speeds offered, as playbackRates
export const SPEEDS = [0.75, 1, 1.25, 1.5, 2]

// The chosen speed, kept per device with the other settings. Settings kept
// before speed existed have none, so that is normal speed.
export const selectPlaybackSpeed = (state) => state.settings?.playbackSpeed || 1

export const formatSpeed = (speed) => `${speed}×`

// applyPlaybackSpeed sets the element's speed, keeping the pitch where the
// browser can. defaultPlaybackRate is what a newly loaded source starts at.
export const applyPlaybackSpeed = (audio, speed) => {
  audio.defaultPlaybackRate = speed
  audio.playbackRate = speed
  audio.preservesPitch = true
  audio.mozPreservesPitch = true
  audio.webkitPreservesPitch = true
}

// usePlaybackSpeed keeps the chosen speed on the player's audio element: on
// the Track playing and on each one after it, since loading a new source can
// put the element back to normal speed. Internet Radio, being live, always
// plays at normal speed.
export const usePlaybackSpeed = (audio, isRadio) => {
  const chosen = useSelector(selectPlaybackSpeed)
  const speed = isRadio ? 1 : chosen

  useEffect(() => {
    if (!audio) return
    const apply = () => {
      if (audio.playbackRate !== speed) applyPlaybackSpeed(audio, speed)
    }
    applyPlaybackSpeed(audio, speed)
    const events = ['loadedmetadata', 'play']
    events.forEach((e) => audio.addEventListener(e, apply))
    return () => events.forEach((e) => audio.removeEventListener(e, apply))
  }, [audio, speed])
}
