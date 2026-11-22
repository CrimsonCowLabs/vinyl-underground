import { CHANGE_THEME } from '../actions'
import { AUTO_THEME_ID, AUTO_THEME_CONFIG_VALUE } from '../consts'
import config from '../config'
import themes from '../themes'

// The colour theme a Listener chose on this device, or null if they never
// chose one, so the default (the Vinyl Underground theme, through "Auto")
// can still change under them.
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

export const themeReducer = (previousState = NO_CHOICE, { type, payload }) => {
  if (type === CHANGE_THEME) {
    return { choice: payload }
  }
  if (typeof previousState === 'string') {
    return migrate(previousState)
  }
  return previousState
}
