export const SET_NOTIFICATIONS_STATE = 'SET_NOTIFICATIONS_STATE'
export const SET_TOGGLEABLE_FIELDS = 'SET_TOGGLEABLE_FIELDS'
export const SET_OMITTED_FIELDS = 'SET_OMITTED_FIELDS'
export const SET_SIDEBAR_PLAYLISTS_FAVOURITES =
  'SET_SIDEBAR_PLAYLISTS_FAVOURITES'

export const SET_AUDIO_EFFECTS = 'SET_AUDIO_EFFECTS'
export const SET_PLAYBACK_SPEED = 'SET_PLAYBACK_SPEED'
export const SET_EQUALIZER = 'SET_EQUALIZER'
export const SET_VISUALS = 'SET_VISUALS'

export const setNotificationsState = (enabled) => ({
  type: SET_NOTIFICATIONS_STATE,
  data: enabled,
})

export const setToggleableFields = (obj) => ({
  type: SET_TOGGLEABLE_FIELDS,
  data: obj,
})

export const setOmittedFields = (obj) => ({
  type: SET_OMITTED_FIELDS,
  data: obj,
})

export const setSidebarPlaylistsOnlyFavourites = (enabled) => ({
  type: SET_SIDEBAR_PLAYLISTS_FAVOURITES,
  data: enabled,
})

export const setAudioEffects = (enabled) => ({
  type: SET_AUDIO_EFFECTS,
  data: enabled,
})

export const setPlaybackSpeed = (speed) => ({
  type: SET_PLAYBACK_SPEED,
  data: speed,
})

// setEqualizer stores the equalizer's settings, as made by the functions in
// equalizer/equalizerSettings.js.
export const setEqualizer = (equalizer) => ({
  type: SET_EQUALIZER,
  data: equalizer,
})

// Merges `changes` into the visuals settings: { mode, warned, enabled }
export const setVisuals = (changes) => ({
  type: SET_VISUALS,
  data: changes,
})
