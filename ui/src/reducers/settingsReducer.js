import {
  SET_AUDIO_EFFECTS,
  SET_EQUALIZER,
  SET_NOTIFICATIONS_STATE,
  SET_OMITTED_FIELDS,
  SET_PLAYBACK_SPEED,
  SET_SIDEBAR_PLAYLISTS_FAVOURITES,
  SET_TOGGLEABLE_FIELDS,
  SET_VISUALS,
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
  // Now Playing's visuals (#96): the visual chosen, whether the
  // photosensitivity warning has been agreed to, and whether visuals are
  // allowed at all (Personal). See visuals/visualsSettings.
  visuals: { mode: 'off', warned: false, enabled: true },
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
    // The equalizer (#95). Left out of initialState: selectEqualizer gives
    // its defaults, for settings saved before it existed too.
    case SET_EQUALIZER:
      return {
        ...previousState,
        equalizer: data,
      }
    case SET_VISUALS:
      return {
        ...previousState,
        visuals: { ...previousState.visuals, ...data },
      }
    default:
      return previousState
  }
}
