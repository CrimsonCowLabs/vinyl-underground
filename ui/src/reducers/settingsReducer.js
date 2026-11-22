import {
  SET_AUDIO_EFFECTS,
  SET_NOTIFICATIONS_STATE,
  SET_OMITTED_FIELDS,
  SET_PLAYBACK_SPEED,
  SET_SIDEBAR_PLAYLISTS_FAVOURITES,
  SET_TOGGLEABLE_FIELDS,
} from '../actions'

const initialState = {
  notifications: false,
  toggleableFields: {},
  omittedFields: {},
  sidebarPlaylistsOnlyFavourites: false,
  // The iPhone Audio effects switch (ADR-0010).
  audioEffects: false,
  // The player's speed, as a playbackRate (#93).
  playbackSpeed: 1,
}

export const settingsReducer = (previousState = initialState, payload) => {
  const { type, data } = payload
  switch (type) {
    case SET_NOTIFICATIONS_STATE:
      return {
        ...previousState,
        notifications: data,
      }
    case SET_TOGGLEABLE_FIELDS:
      return {
        ...previousState,
        toggleableFields: {
          ...previousState.toggleableFields,
          ...data,
        },
      }
    case SET_OMITTED_FIELDS:
      return {
        ...previousState,
        omittedFields: {
          ...previousState.omittedFields,
          ...data,
        },
      }
    case SET_SIDEBAR_PLAYLISTS_FAVOURITES:
      return {
        ...previousState,
        sidebarPlaylistsOnlyFavourites: data,
      }
    case SET_AUDIO_EFFECTS:
      return {
        ...previousState,
        audioEffects: data,
      }
    case SET_PLAYBACK_SPEED:
      return {
        ...previousState,
        playbackSpeed: data,
      }
    default:
      return previousState
  }
}
