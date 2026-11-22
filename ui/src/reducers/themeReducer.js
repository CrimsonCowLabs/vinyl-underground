import { CHANGE_ACCENT, CHANGE_THEME } from '../actions'
import { AUTO_THEME_ID, AUTO_THEME_CONFIG_VALUE } from '../consts'
import config from '../config'
import themes from '../themes'
import {
  accents,
  ART_ACCENT,
  DEFAULT_ACCENT,
} from '../themes/vinylUndergroundPalette'

// The colour theme a Listener chose on this device, or null if they never
// chose one, so the default (the Vinyl Underground theme, through "Auto")
// can still change under them. `accent`, once chosen, is the Vinyl
// Underground theme's accent, kept while another theme is shown.
const NO_CHOICE = { choice: null }

const configuredDefaultId = () => {
  if (config.defaultTheme === AUTO_THEME_CONFIG_VALUE) {
    return AUTO_THEME_ID
  }
  return (
    Object.keys(themes).find(
      (t) => themes[t].themeName === config.defaultTheme,
    ) || AUTO_THEME_ID
  )
}

// Before the Vinyl Underground theme, the state was the theme's id, saved
// even when the Listener never chose one. So a saved default is taken as
// never chosen: Dark, the built-in default then, or the theme the server's
// DefaultTheme names. Anything else, Auto included, was picked on purpose.
const LEGACY_DEFAULT = 'DarkTheme'
const migrate = (legacyId) =>
  legacyId === LEGACY_DEFAULT ||
  (legacyId !== AUTO_THEME_ID && legacyId === configuredDefaultId())
    ? NO_CHOICE
    : { choice: legacyId }

const isKnown = (id) =>
  id === AUTO_THEME_ID || Object.prototype.hasOwnProperty.call(themes, id)

// The id of the theme to show: AUTO_THEME_ID or a key of `themes`.
export const currentThemeId = (state) => {
  const choice = state.theme?.choice
  return isKnown(choice) ? choice : configuredDefaultId()
}

// Whether the Vinyl Underground theme is shown (through Auto, or chosen), the
// only theme whose accent can be chosen
export const showsVinylUnderground = (state) => {
  const id = currentThemeId(state)
  return id === AUTO_THEME_ID || !!themes[id].palette.vu
}

// The id of the Vinyl Underground theme's accent: a key of its accents, or
// ART_ACCENT ("From album art").
export const currentAccentId = (state) => {
  const accent = state.theme?.accent
  return accent === ART_ACCENT ||
    Object.prototype.hasOwnProperty.call(accents, accent)
    ? accent
    : DEFAULT_ACCENT
}

export const themeReducer = (previousState = NO_CHOICE, { type, payload }) => {
  if (typeof previousState === 'string') {
    previousState = migrate(previousState)
  }
  if (type === CHANGE_THEME) {
    return { ...previousState, choice: payload }
  }
  if (type === CHANGE_ACCENT) {
    return { ...previousState, accent: payload }
  }
  return previousState
}
