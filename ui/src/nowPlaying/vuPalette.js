import { fade } from '@material-ui/core/styles'

// vuPalette is the Vinyl Underground theme's extra colours (palette.vu), or,
// under one of the inherited themes, near equivalents from the standard MUI
// palette keys.
export const vuPalette = (theme) => {
  const p = theme.palette
  const vu = p.vu || {}
  return {
    surface2: vu.surface2 || fade(p.text.primary, 0.06),
    surface3: vu.surface3 || fade(p.text.primary, 0.14),
    accentSoft: vu.accentSoft || fade(p.primary.main, 0.16),
    accentGlow: vu.accentGlow || fade(p.primary.main, 0.22),
    grain: vu.grain || null,
  }
}
