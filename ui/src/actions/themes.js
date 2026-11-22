export const CHANGE_THEME = 'CHANGE_THEME'

export const changeTheme = (theme) => ({
  type: CHANGE_THEME,
  payload: theme,
})

export const CHANGE_ACCENT = 'CHANGE_ACCENT'

// The Vinyl Underground theme's accent: a key of its accents, or "From album
// art"
export const changeAccent = (accent) => ({
  type: CHANGE_ACCENT,
  payload: accent,
})
