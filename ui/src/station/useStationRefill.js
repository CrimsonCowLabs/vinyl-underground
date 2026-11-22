import { useEffect, useRef, useState } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'
import { addStationTracks, stopStation, upcomingTracks } from './stationState'
import { stationErrorCode } from './stationErrors'

// The Station refills when fewer than REFILL_BELOW Tracks are queued after the
// one playing, REFILL_COUNT at a time, so a skip always has a Track to go to.
export const REFILL_BELOW = 3
export const REFILL_COUNT = 3
// A failed refill is tried again after RETRY_MS, or sooner if the next Track
// starts.
export const RETRY_MS = 15000

// fetchStationTracks asks for a Station's next Tracks. The Theme is only sent
// when there is one (only with an LLM configured). For a Station's first
// Tracks (start), the server may wait up to a minute for the LLM's picks.
export const fetchStationTracks = (djId, count, theme = '', start = false) =>
  httpClient(`${REST_URL}/station/next`, {
    method: 'POST',
    body: JSON.stringify({
      dj: djId,
      count,
      ...(theme && { theme }),
      ...(start && { start }),
    }),
  }).then(({ json }) => json.tracks || [])

// useStationRefill keeps the Station on air endless: while a DJ is on air and
// the queue runs low, it asks the server for the next Tracks and queues them.
export const useStationRefill = () => {
  const dispatch = useDispatch()
  const djId = useSelector((state) => state.station?.djId)
  const theme = useSelector((state) => state.station?.theme)
  const session = useSelector((state) => state.station?.session)
  const upcoming = useSelector((state) => upcomingTracks(state.player))
  const [retries, setRetries] = useState(0)
  const onAir = useRef()
  const inFlight = useRef(null) // the session a refill is pending for
  onAir.current = djId && session

  useEffect(() => {
    if (!djId || upcoming >= REFILL_BELOW || inFlight.current === session) {
      return
    }
    inFlight.current = session
    let retry
    fetchStationTracks(djId, REFILL_COUNT, theme)
      .then((tracks) => {
        // The Listener may have stopped or changed Station meanwhile.
        if (onAir.current === session && tracks.length) {
          dispatch(addStationTracks(tracks))
        }
      })
      .catch((e) => {
        // The DJ is gone (deleted, maybe on another device): the Station
        // can't refill any more, so it ends and what's queued plays out.
        if (stationErrorCode(e) === 'unknownDJ') {
          if (onAir.current === session) dispatch(stopStation())
          return
        }
        // eslint-disable-next-line no-console
        console.warn('Station refill failed:', e)
        retry = setTimeout(() => setRetries((n) => n + 1), RETRY_MS)
      })
      .finally(() => {
        if (inFlight.current === session) inFlight.current = null
      })
    return () => clearTimeout(retry)
  }, [djId, theme, session, upcoming, retries, dispatch])
}
