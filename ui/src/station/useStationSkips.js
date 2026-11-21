import { useCallback, useMemo, useRef } from 'react'
import { useSelector } from 'react-redux'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

// Leaving a Station Track before SKIP_BEFORE of it played is a skip, which
// makes that Station play the Track and its artist a little less for a while.
// Past it, the Track counts as heard: Station plays count in play history from
// about half way.
export const SKIP_BEFORE = 0.5

export const reportStationSkip = (djId, trackId) =>
  httpClient(`${REST_URL}/station/skip`, {
    method: 'POST',
    body: JSON.stringify({ dj: djId, track: trackId }),
  })

// useStationSkips tells the server when the Listener skips a Track on the
// Station on air. The Player calls trackStarted when a Track starts and
// trackLeft when it moves to another one. Only a Track started and left on the
// same Station is a skip: ending the Station, or starting another one, isn't.
export const useStationSkips = () => {
  const station = useSelector((state) => state.station)
  const onAir = useRef()
  onAir.current = station?.djId ? station : null
  const playing = useRef(null) // the Track playing, and the Station it started on

  const trackStarted = useCallback((trackId, durationSec) => {
    playing.current = { trackId, durationSec, station: onAir.current }
  }, [])

  const trackLeft = useCallback((trackId, positionMs) => {
    const started = playing.current
    playing.current = null
    const now = onAir.current
    if (
      !started?.station ||
      !now ||
      started.trackId !== trackId ||
      started.station.djId !== now.djId ||
      started.station.session !== now.session ||
      !(started.durationSec > 0) ||
      positionMs >= started.durationSec * 1000 * SKIP_BEFORE
    ) {
      return
    }
    reportStationSkip(now.djId, trackId).catch((e) => {
      // The Station may not have served it (e.g. a Track added to the queue).
      // eslint-disable-next-line no-console
      console.debug('Station skip not recorded:', e)
    })
  }, [])

  return useMemo(() => ({ trackStarted, trackLeft }), [trackStarted, trackLeft])
}
