import {
  PLAYER_CLEAR_QUEUE,
  PLAYER_PLAY_TRACKS,
  PLAYER_SET_TRACK,
  addTracks,
} from '../actions'

// Station Tracks keyed for the player queue. Keys hold the position, so a
// Track served twice (a tiny Library) is queued twice, and the "_" keeps the
// server's order: keys that look like numbers would otherwise sort first.
const toQueueData = (tracks) =>
  Object.fromEntries(tracks.map((track, i) => [`_${i}_${track.id}`, track]))

// startStation replaces the play queue with a Station's first Tracks and puts
// its DJ (and optional Theme) on air, so the queue keeps being refilled from
// the server.
export const startStation = (djId, tracks, theme = '') => {
  const data = toQueueData(tracks)
  return {
    type: PLAYER_PLAY_TRACKS,
    id: Object.keys(data)[0],
    data,
    station: djId,
    theme,
  }
}

export const STATION_STOP = 'STATION_STOP'
export const STATION_PICKING = 'STATION_PICKING'
export const STATION_PICKING_FAILED = 'STATION_PICKING_FAILED'

// stationPicking says a Station with the DJ is starting: with an LLM, the
// server may take up to a minute picking its first songs.
export const stationPicking = (dj) => ({
  type: STATION_PICKING,
  djId: dj.id,
  djName: dj.name,
})

// stationPickingFailed says the Station starting couldn't.
export const stationPickingFailed = () => ({ type: STATION_PICKING_FAILED })

// stopStation takes the DJ off air, as when the Listener deletes it, leaving
// the queued Tracks to play out without refills.
export const stopStation = () => ({ type: STATION_STOP })

// addStationTracks appends a refill to the end of the queue.
export const addStationTracks = (tracks) => addTracks(toQueueData(tracks))

// The Station on air, if any. Playing anything else ends it. Each start gets a
// new session, so a refill asked for by an earlier Station is never queued in
// a later one, even with the same DJ. picking is the DJ ({djId, djName}) of a
// Station starting, while its first songs are picked. Playing anything else
// meanwhile gives up that start.
export const stationReducer = (
  state = { djId: null, theme: '', session: 0, picking: null },
  action,
) => {
  switch (action.type) {
    case STATION_PICKING:
      return {
        ...state,
        picking: { djId: action.djId, djName: action.djName },
      }
    case STATION_PICKING_FAILED:
      return { ...state, picking: null }
    case PLAYER_PLAY_TRACKS:
      return action.station
        ? {
            djId: action.station,
            theme: action.theme || '',
            session: (state.session || 0) + 1,
            picking: null,
          }
        : { ...state, djId: null, picking: null }
    case PLAYER_SET_TRACK:
    case PLAYER_CLEAR_QUEUE:
      return { ...state, djId: null, picking: null }
    case STATION_STOP:
      return { ...state, djId: null }
    default:
      return state
  }
}

// upcomingTracks is how many Tracks are queued after the one playing (or about
// to play, right after a new queue starts).
export const upcomingTracks = ({ queue, current, playIndex }) => {
  if (!queue.length) return 0
  let idx = queue.findIndex((item) => item.uuid === current?.uuid)
  if (idx < 0) idx = playIndex || 0
  return Math.max(0, queue.length - 1 - idx)
}
