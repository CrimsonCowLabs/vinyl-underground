// The Vinyl Underground colour theme's palette, as settled by the UI refresh
// prototype's review (#88). docs/design.md ("UI refresh", "Palette") is the
// source of truth; prototypes/ui-refresh/README.md has the contrast ratios.

export const surfaces = {
  // Slate: a cool blue-grey
  dark: {
    background: '#0F1216', // palette.background.default, theme-color
    paper: '#161A1F', // palette.background.paper: player bar, sheets
    surface2: '#1F242B', // chips, inputs, cards
    surface3: '#2A3038', // selected pill, slider track
    divider: '#2E343C',
    text: '#E8ECF1',
    text2: '#9AA4B0',
    text3: '#6B7480', // large text and marks only (3:1)
  },
  // Warm cream
  light: {
    background: '#F3EDE4',
    paper: '#FBF8F3',
    surface2: '#E9E1D5',
    surface3: '#FFFFFF',
    divider: '#D6CBBC',
    text: '#1E1915',
    text2: '#5E5247',
    text3: '#7E7163',
  },
}

// How each variant layers things: the accent's tints, the hover wash over
// text colour, the shadow under sheets, and the fill of raised buttons and
// tracks (so they still stand out from paper on cream).
export const effects = {
  dark: {
    accentSoft: 0.16,
    accentGlow: 0.3,
    hover: 0.06,
    shadow: '0 18px 40px rgba(0, 0, 0, 0.55)',
    raised: surfaces.dark.surface3,
    raisedHover: surfaces.dark.surface2,
  },
  light: {
    accentSoft: 0.14,
    accentGlow: 0.22,
    hover: 0.05,
    shadow: '0 16px 34px rgba(60, 40, 20, 0.18)',
    raised: surfaces.light.surface2,
    raisedHover: surfaces.light.divider,
  },
}

// Each accent has a value per variant: the light one is darker so it still
// reads on cream. Text on an accent is Slate in dark and white in light. The
// order is the picker's. "From album art" is in artAccent.js.
export const ON_ACCENT = { dark: '#0F1216', light: '#FFFFFF' }

const accent = (dark, light) => ({
  dark: { main: dark, contrastText: ON_ACCENT.dark },
  light: { main: light, contrastText: ON_ACCENT.light },
})

export const accents = {
  teal: accent('#3EC1B0', '#086A5F'),
  vermilion: accent('#FF5B2E', '#B0300F'),
  amber: accent('#FF9A1F', '#934E00'),
  mustard: accent('#E2B53A', '#765A00'),
  olive: accent('#9DBB4E', '#4A6510'),
  denim: accent('#6FA3F0', '#2F5DB0'),
  plum: accent('#C08BF0', '#7A3FB0'),
  rose: accent('#FF6F91', '#B42851'),
}

export const DEFAULT_ACCENT = 'teal'
// The accent that colours Now Playing and the mini player from the cover
export const ART_ACCENT = 'art'

// A 180px tile of SVG noise, as a data URI: cool-white grain at 7% on dark,
// brown grain at 8% on light.
const noise = (rgb, alpha) =>
  `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='180' height='180'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='.85' numOctaves='2' stitchTiles='stitch'/%3E%3CfeColorMatrix values='0 0 0 0 ${rgb[0]} 0 0 0 0 ${rgb[1]} 0 0 0 0 ${rgb[2]} 0 0 0 ${alpha} 0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`

export const grain = {
  dark: noise(['.9', '.95', '1'], '.07'),
  light: noise(['.2', '.12', '.05'], '.08'),
}
