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
// the server. The DJ's name is for Now Playing to show.
export const startStation = (djId, tracks, theme = '', djName = '') => {
  const data = toQueueData(tracks)
  return {
    type: PLAYER_PLAY_TRACKS,
    id: Object.keys(data)[0],
    data,
    station: djId,
    theme,
    djName,
  }
}

export const STATION_STOP = 'STATION_STOP'
export const STATION_PICKING = 'STATION_PICKING'
export const STATION_PICKING_FAILED = 'STATION_PICKING_FAILED'
export const STATION_BLURB = 'STATION_BLURB'

// stationPicking says a Station with the DJ is starting: with an LLM, the
// server may take up to a minute picking its first Tracks.
// Each start gets its own token, so the answer to a start given up on (the
// Listener played something else, then started again) is never taken for a
// later start's.
let starts = 0
export const stationPicking = (dj) => ({
  type: STATION_PICKING,
  djId: dj.id,
  djName: dj.name,
  token: ++starts,
})

// stationPickingFailed says the start with the token couldn't, or that the
// Listener gave up on it.
export const stationPickingFailed = (token) => ({
  type: STATION_PICKING_FAILED,
  token,
})

// isPicking reports whether the start with the token is still under way.
export const isPicking = (state, token) =>
  state.station?.picking?.token === token

// stationBlurb records the Blurb ({djName, text}) the DJ showed as the
// queued Track with the uuid started, so Now Playing can show it too.
export const stationBlurb = (uuid, blurb) => ({
  type: STATION_BLURB,
  uuid,
  djName: blurb.djName,
  text: blurb.text,
})

// latestBlurb is the Blurb for the Track playing, if the DJ showed one.
export const latestBlurb = (state) => {
  const blurb = state.station?.blurb
  return blurb && blurb.uuid === state.player?.current?.uuid ? blurb : null
}

// stopStation takes the DJ off air, as when the Listener deletes it, leaving
// the queued Tracks to play out without refills.
export const stopStation = () => ({ type: STATION_STOP })

// addStationTracks appends a refill to the end of the queue.
export const addStationTracks = (tracks) => addTracks(toQueueData(tracks))

// The Station on air, if any. Playing anything else ends it. Each start gets a
// new session, so a refill asked for by an earlier Station is never queued in
// a later one, even with the same DJ. picking is the DJ ({djId, djName}) and
// token of a Station starting, while its first Tracks are picked. Playing anything else
// meanwhile gives up that start. blurb is the latest Blurb shown, with the uuid
// of the queued Track it came with.
export const stationReducer = (
  state = { djId: null, theme: '', session: 0, picking: null, blurb: null },
  action,
) => {
  switch (action.type) {
    case STATION_PICKING:
      return {
        ...state,
        picking: {
          djId: action.djId,
          djName: action.djName,
          token: action.token,
        },
      }
    case STATION_PICKING_FAILED:
      return state.picking?.token === action.token
        ? { ...state, picking: null }
        : state
    case PLAYER_PLAY_TRACKS:
      return action.station
        ? {
            djId: action.station,
            djName: action.djName || '',
            theme: action.theme || '',
            session: (state.session || 0) + 1,
            picking: null,
            blurb: null,
          }
        : { ...state, djId: null, picking: null, blurb: null }
    case PLAYER_SET_TRACK:
    case PLAYER_CLEAR_QUEUE:
      return { ...state, djId: null, picking: null, blurb: null }
    case STATION_BLURB:
      return {
        ...state,
        blurb: { uuid: action.uuid, djName: action.djName, text: action.text },
      }
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
