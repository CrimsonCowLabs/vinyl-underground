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

// Each accent has a value per variant: the light one is darker so it still
// reads on cream. #99 adds the other fixed accents and "From album art".
export const accents = {
  teal: {
    dark: { main: '#3EC1B0', contrastText: '#0F1216' },
    light: { main: '#086A5F', contrastText: '#FFFFFF' },
  },
}

export const DEFAULT_ACCENT = 'teal'

// A 180px tile of SVG noise, as a data URI: cool-white grain at 7% on dark,
// brown grain at 8% on light.
const noise = (rgb, alpha) =>
  `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='180' height='180'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='.85' numOctaves='2' stitchTiles='stitch'/%3E%3CfeColorMatrix values='0 0 0 0 ${rgb[0]} 0 0 0 0 ${rgb[1]} 0 0 0 0 ${rgb[2]} 0 0 0 ${alpha} 0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`

export const grain = {
  dark: noise(['.9', '.95', '1'], '.07'),
  light: noise(['.2', '.12', '.05'], '.08'),
}
