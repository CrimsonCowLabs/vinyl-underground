import { useEffect, useRef } from 'react'
import { useDispatch, useSelector } from 'react-redux'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'
import { addStationTracks, upcomingTracks } from './stationState'

// The Station refills when fewer than REFILL_BELOW Tracks are queued after the
// one playing, REFILL_COUNT at a time, so a skip always has a Track to go to.
export const REFILL_BELOW = 3
export const REFILL_COUNT = 3

export const fetchStationTracks = (djId, count) =>
  httpClient(`${REST_URL}/station/next`, {
    method: 'POST',
    body: JSON.stringify({ dj: djId, count }),
  }).then(({ json }) => json.tracks || [])

// useStationRefill keeps the Station on air endless: while a DJ is on air and
// the queue runs low, it asks the server for the next Tracks and queues them.
// A failed refill is tried again when the next Track starts.
export const useStationRefill = () => {
  const dispatch = useDispatch()
  const djId = useSelector((state) => state.station?.djId)
  const session = useSelector((state) => state.station?.session)
  const upcoming = useSelector((state) => upcomingTracks(state.player))
  const onAir = useRef()
  const inFlight = useRef(null) // the session a refill is pending for
  onAir.current = djId && session

  useEffect(() => {
    if (!djId || upcoming >= REFILL_BELOW || inFlight.current === session) {
      return
    }
    inFlight.current = session
    fetchStationTracks(djId, REFILL_COUNT)
      .then((tracks) => {
        // The Listener may have stopped or changed Station meanwhile.
        if (onAir.current === session && tracks.length) {
          dispatch(addStationTracks(tracks))
        }
      })
      .catch((e) => {
        // eslint-disable-next-line no-console
        console.warn('Station refill failed:', e)
      })
      .finally(() => {
        if (inFlight.current === session) inFlight.current = null
      })
  }, [djId, session, upcoming, dispatch])
}
