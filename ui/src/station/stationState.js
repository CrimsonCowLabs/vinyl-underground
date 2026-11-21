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
// its DJ on air, so the queue keeps being refilled from the server.
export const startStation = (djId, tracks) => {
  const data = toQueueData(tracks)
  return {
    type: PLAYER_PLAY_TRACKS,
    id: Object.keys(data)[0],
    data,
    station: djId,
  }
}

// addStationTracks appends a refill to the end of the queue.
export const addStationTracks = (tracks) => addTracks(toQueueData(tracks))

// The Station on air, if any. Playing anything else ends it. Each start gets a
// new session, so a refill asked for by an earlier Station is never queued in
// a later one, even with the same DJ.
export const stationReducer = (state = { djId: null, session: 0 }, action) => {
  switch (action.type) {
    case PLAYER_PLAY_TRACKS:
      return action.station
        ? { djId: action.station, session: (state.session || 0) + 1 }
        : { ...state, djId: null }
    case PLAYER_SET_TRACK:
    case PLAYER_CLEAR_QUEUE:
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
