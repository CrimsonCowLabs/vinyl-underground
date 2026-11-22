import { useCallback, useEffect, useState } from 'react'

const read = (audio, fallbackDuration) => ({
  playing: !!audio && !audio.paused,
  currentTime: audio?.currentTime || 0,
  duration:
    audio && Number.isFinite(audio.duration) && audio.duration > 0
      ? audio.duration
      : fallbackDuration || 0,
})

// usePlayback follows the player package's audio element: whether it is
// playing, where it is and how long the Track is. It returns that with the
// package's own controls, so Now Playing drives the same playback and queue
// as the player bar.
export const usePlayback = (audio, fallbackDuration) => {
  const [state, setState] = useState(() => read(audio, fallbackDuration))

  useEffect(() => {
    const update = () => setState(read(audio, fallbackDuration))
    update()
    if (!audio) return
    const events = [
      'timeupdate',
      'play',
      'playing',
      'pause',
      'durationchange',
      'loadedmetadata',
      'seeked',
      'emptied',
    ]
    events.forEach((e) => audio.addEventListener(e, update))
    return () => events.forEach((e) => audio.removeEventListener(e, update))
  }, [audio, fallbackDuration])

  const seek = useCallback(
    (seconds) => {
      if (!audio) return
      audio.currentTime = seconds
      setState(read(audio, fallbackDuration))
    },
    [audio, fallbackDuration],
  )

  return {
    ...state,
    seek,
    togglePlay: () => audio?.togglePlay(),
    next: () => audio?.playNext(),
    previous: () => audio?.playPrev(),
  }
}
