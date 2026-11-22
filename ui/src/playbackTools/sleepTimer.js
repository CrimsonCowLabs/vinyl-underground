// The sleep timer: after a number of minutes, or at the end of the Track
// playing, it fades playback out and pauses it. It runs on the player
// package's own audio element, so it keeps going while Now Playing is closed,
// on a Station and for Downloads offline. It lives in memory only, so it is
// gone after a reload.
//
// The fade lowers the element's volume, as the package's own 200ms fade does,
// and puts it back once paused. The package takes every volumechange for the
// Listener's own volume (its slider, the stored volume, and the level it
// fades back in to on play), so the timer stops the events its fade causes
// before the package hears them: its listener is a capture one, which runs
// first at the element.
const SLEEP_FADE_MS = 6000
const FADE_STEP_MS = 100

// The timer's modes, besides null for off
export const MINUTES = 'minutes'
export const END_OF_TRACK = 'endOfTrack'
// "End of this Track" pauses this long before the Track's end, so the player
// never gets to start the next one
const END_OF_TRACK_MARGIN_MS = 400

export const createSleepTimer = ({ fadeMs = SLEEP_FADE_MS } = {}) => {
  let audio = null
  let mode = null // null, MINUTES or END_OF_TRACK
  let minutes = null // for MINUTES, as chosen
  let endsAt = null // for MINUTES, as Date.now()
  let fadeTimeout = null
  let fade = null // { from, interval }
  let restoring = null // the volume being put back, until it is announced
  let lastSet = null // the volume the timer last gave the element
  const listeners = new Set()

  const notify = () => listeners.forEach((listener) => listener())

  const setVolume = (volume) => {
    audio.volume = volume
    lastSet = audio.volume
  }

  const restoreVolume = (volume) => {
    if (!audio || audio.volume === volume) return
    restoring = volume
    setVolume(volume)
  }

  const stopFade = ({ restore }) => {
    if (!fade) return
    clearInterval(fade.interval)
    const { from } = fade
    fade = null
    if (restore) restoreVolume(from)
  }

  const clear = () => {
    clearTimeout(fadeTimeout)
    fadeTimeout = null
    mode = null
    minutes = null
    endsAt = null
  }

  const finish = () => {
    const from = fade.from
    stopFade({ restore: false })
    clear()
    audio.pause()
    restoreVolume(from)
    notify()
  }

  const beginFade = (duration) => {
    if (fade || !audio) return
    if (audio.paused || duration <= 0) {
      fade = { from: audio.volume, interval: null }
      finish()
      return
    }
    const startedAt = Date.now()
    fade = {
      from: audio.volume,
      interval: setInterval(() => {
        const progress = (Date.now() - startedAt) / duration
        if (progress >= 1) finish()
        else setVolume(fade.from * (1 - progress))
      }, FADE_STEP_MS),
    }
    notify()
  }

  // How long the Track playing has left, in real time at its speed
  const trackLeftMs = () => {
    if (!audio || !Number.isFinite(audio.duration)) return null
    const rate = audio.playbackRate || 1
    return (Math.max(0, audio.duration - audio.currentTime) / rate) * 1000
  }

  // Each timeupdate checks the clock too, in case a background tab held the
  // timeout back
  const onTimeUpdate = () => {
    if (fade) {
      // Seeking back, or on to another Track, puts the end further off
      const left = mode === END_OF_TRACK && trackLeftMs()
      if (left && left - END_OF_TRACK_MARGIN_MS > fadeMs) {
        stopFade({ restore: true })
        notify()
      }
      return
    }
    if (mode === END_OF_TRACK) {
      const left = trackLeftMs()
      if (left !== null && left - END_OF_TRACK_MARGIN_MS <= fadeMs) {
        beginFade(left - END_OF_TRACK_MARGIN_MS)
      }
    } else if (mode === MINUTES && Date.now() >= endsAt - fadeMs) {
      clearTimeout(fadeTimeout)
      beginFade(endsAt - Date.now())
    }
  }

  // The Listener paused during the fade: the timer's work is done, and the
  // package's own fade-out may have left the volume where ours had got to
  const onPause = () => {
    if (fade) api.cancel()
  }

  // A volumechange from the fade, or from putting the volume back, never
  // reaches the package. Events are announced late, so each reads the volume
  // as it is by then; once one reads the restored volume, the rest are the
  // package's again.
  //
  // A volume the timer didn't set is the Listener's (their slider, or the
  // package's own fade as they pause): it goes through, and the timer stops.
  const onVolumeChange = (event) => {
    if (!fade && restoring === null) return
    if (audio.volume !== lastSet) {
      restoring = null
      if (fade) {
        stopFade({ restore: false })
        clear()
        notify()
      }
      return
    }
    event.stopImmediatePropagation()
    if (!fade && audio.volume === restoring) restoring = null
  }

  const api = {
    // Hands the timer the player's audio element.
    attach(newElement) {
      if (newElement === audio) return
      audio?.removeEventListener('timeupdate', onTimeUpdate)
      audio?.removeEventListener('pause', onPause)
      audio?.removeEventListener('volumechange', onVolumeChange, true)
      audio = newElement
      audio?.addEventListener('timeupdate', onTimeUpdate)
      audio?.addEventListener('pause', onPause)
      audio?.addEventListener('volumechange', onVolumeChange, true)
    },

    // Starts (or restarts) the timer for the given number of minutes.
    start(chosenMinutes) {
      api.cancel()
      const ms = chosenMinutes * 60 * 1000
      mode = MINUTES
      minutes = chosenMinutes
      endsAt = Date.now() + ms
      fadeTimeout = setTimeout(
        () => beginFade(Math.min(fadeMs, ms)),
        Math.max(0, ms - fadeMs),
      )
      notify()
    },

    // Starts the timer for the rest of the Track playing.
    startEndOfTrack() {
      api.cancel()
      mode = END_OF_TRACK
      notify()
    },

    // Turns the timer off, and puts the volume back if it was fading.
    cancel() {
      stopFade({ restore: true })
      const wasOn = mode !== null
      clear()
      if (wasOn) notify()
    },

    state: () => ({ mode, minutes, fading: !!fade }),

    // Whether the end of the Track playing is known, which it isn't on
    // Internet Radio
    trackEndKnown: () => trackLeftMs() !== null,

    // The whole seconds left before playback pauses, or null when the timer
    // is off or the end of the Track can't be known (Internet Radio).
    remainingSeconds() {
      if (mode === MINUTES) {
        return Math.max(0, Math.ceil((endsAt - Date.now()) / 1000))
      }
      if (mode === END_OF_TRACK) {
        const left = trackLeftMs()
        return left === null ? null : Math.ceil(left / 1000)
      }
      return null
    },

    // Calls listener whenever the timer starts, stops or begins to fade.
    // Returns a function that stops the calls.
    subscribe(listener) {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
  }
  return api
}

export const sleepTimer = createSleepTimer()
