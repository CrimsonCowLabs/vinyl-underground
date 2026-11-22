import { fade } from '@material-ui/core/styles'
import createStylesheet from './vinylUnderground.css.js'
import {
  accents,
  DEFAULT_ACCENT,
  grain,
  surfaces,
} from './vinylUndergroundPalette'

const fontFamily = [
  'system-ui',
  '-apple-system',
  '"Segoe UI"',
  'Roboto',
  '"Helvetica Neue"',
  'Arial',
  'sans-serif',
].join(',')

const ROUND = 999

// The Vinyl Underground colour theme (not a Station's Theme) in its dark
// (Slate) or light (cream) variant. The accent is its one input, so #99 can
// offer the other accents: pass an accent's { main, contrastText } for the
// variant.
export const createVinylUndergroundTheme = ({
  mode,
  accent = accents[DEFAULT_ACCENT][mode],
}) => {
  const s = surfaces[mode]
  const dark = mode === 'dark'
  const accentSoft = fade(accent.main, dark ? 0.16 : 0.14)
  const accentGlow = fade(accent.main, dark ? 0.3 : 0.22)
  const hover = fade(s.text, dark ? 0.06 : 0.05)
  const shadow = dark
    ? '0 18px 40px rgba(0, 0, 0, 0.55)'
    : '0 16px 34px rgba(60, 40, 20, 0.18)'

  return {
    themeName: dark ? 'Vinyl Underground Dark' : 'Vinyl Underground Light',
    // The status bar and the installed app's title bar
    themeColor: s.background,
    palette: {
      type: mode,
      primary: { main: accent.main, contrastText: accent.contrastText },
      secondary: { main: accent.main, contrastText: accent.contrastText },
      background: { default: s.background, paper: s.paper },
      divider: s.divider,
      text: { primary: s.text, secondary: s.text2, disabled: s.text3 },
      action: {
        hover,
        selected: accentSoft,
        active: s.text2,
        disabled: s.text3,
      },
      // Values with no MUI key, for the mini player (#100), the tab bar
      // (#101) and Now Playing (#90)
      vu: {
        surface2: s.surface2,
        surface3: s.surface3,
        accentSoft,
        accentGlow,
        grain: grain[mode],
      },
    },
    shape: { borderRadius: 12 },
    typography: {
      fontFamily,
      h5: { fontWeight: 800, letterSpacing: '-0.02em' },
      h6: { fontWeight: 700, letterSpacing: '-0.01em' },
      overline: { fontSize: 11, fontWeight: 700, letterSpacing: '0.14em' },
    },
    overrides: {
      MuiCssBaseline: {
        '@global': {
          body: {
            backgroundColor: s.background,
            backgroundImage: grain[mode],
          },
        },
      },
      MuiAppBar: {
        colorSecondary: {
          backgroundColor: s.paper,
          color: s.text,
        },
        root: {
          borderBottom: `1px solid ${s.divider}`,
          boxShadow: 'none',
        },
      },
      MuiPaper: {
        root: { backgroundImage: 'none' },
        elevation1: { boxShadow: 'none', border: `1px solid ${s.divider}` },
      },
      MuiCard: {
        root: { borderRadius: 12 },
      },
      MuiDialog: {
        paper: {
          backgroundColor: s.paper,
          border: `1px solid ${s.divider}`,
          boxShadow: shadow,
        },
      },
      MuiDrawer: {
        paper: { backgroundColor: s.paper },
        paperAnchorDockedLeft: { borderRight: `1px solid ${s.divider}` },
      },
      MuiPopover: {
        paper: { border: `1px solid ${s.divider}`, boxShadow: shadow },
      },
      MuiMenu: {
        paper: { backgroundColor: s.paper },
      },
      MuiListItem: {
        root: {
          '&$selected, &$selected:hover': { backgroundColor: accentSoft },
        },
      },
      MuiMenuItem: {
        root: {
          '&$selected, &$selected:hover': { backgroundColor: accentSoft },
        },
      },
      MuiChip: {
        root: {
          backgroundColor: s.surface2,
          color: s.text,
          borderRadius: ROUND,
        },
        outlined: { borderColor: s.divider },
        clickable: {
          '&:hover, &:focus': { backgroundColor: s.surface3 },
        },
      },
      MuiButton: {
        root: { textTransform: 'none', fontWeight: 600 },
        contained: {
          boxShadow: 'none',
          backgroundColor: dark ? s.surface3 : s.surface2,
          color: s.text,
          '&:hover': { backgroundColor: dark ? s.surface2 : s.divider },
        },
        containedPrimary: { '&:hover': { boxShadow: 'none' } },
      },
      MuiFab: {
        root: { boxShadow: 'none' },
      },
      MuiToggleButton: {
        root: { borderColor: s.divider },
      },
      MuiTab: {
        root: { textTransform: 'none', fontWeight: 600 },
      },
      MuiTabs: {
        indicator: { backgroundColor: accent.main },
      },
      MuiFilledInput: {
        root: {
          backgroundColor: s.surface2,
          borderTopLeftRadius: 12,
          borderTopRightRadius: 12,
          '&:hover': { backgroundColor: s.surface3 },
          '&$focused': { backgroundColor: s.surface2 },
          '&$disabled': { backgroundColor: s.surface2 },
        },
        underline: {
          '&:before': { borderBottomColor: s.divider },
        },
      },
      MuiOutlinedInput: {
        notchedOutline: { borderColor: s.divider },
      },
      MuiInput: {
        underline: {
          '&:before': { borderBottomColor: s.divider },
        },
      },
      MuiTableCell: {
        root: { borderBottom: `1px solid ${s.divider}` },
        head: { color: s.text2, fontWeight: 600 },
      },
      MuiTableRow: {
        hover: { '&:hover': { backgroundColor: `${hover} !important` } },
      },
      MuiTooltip: {
        tooltip: {
          backgroundColor: s.surface3,
          color: s.text,
          border: `1px solid ${s.divider}`,
          fontSize: '0.75rem',
        },
      },
      MuiSnackbarContent: {
        root: { backgroundColor: s.surface3, color: s.text },
      },
      MuiLinearProgress: {
        colorPrimary: { backgroundColor: dark ? s.surface3 : s.surface2 },
      },
      MuiSlider: {
        rail: { backgroundColor: s.surface3, opacity: 1 },
      },
      MuiSwitch: {
        // So the off track reads on cream as well as on Slate
        track: { backgroundColor: s.text3, opacity: 0.6 },
      },
      MuiDivider: {
        root: { backgroundColor: s.divider },
      },
      MuiLink: {
        root: { color: accent.main },
      },
      RaLayout: {
        root: {
          backgroundColor: s.background,
          backgroundImage: grain[mode],
        },
        content: { backgroundColor: 'transparent' },
      },
      RaSidebar: {
        fixed: { backgroundColor: 'transparent' },
      },
      RaMenuItemLink: {
        active: { color: `${s.text} !important` },
      },
      RaLink: {
        link: { color: accent.main },
      },
      RaListToolbar: {
        toolbar: { backgroundColor: 'transparent' },
      },
      RaToolbar: {
        regular: { backgroundColor: s.paper },
      },
      NDAlbumGridView: {
        // The hover overlay is a sibling of the image, so the link clips both
        link: { borderRadius: 6, overflow: 'hidden' },
        albumName: { fontWeight: 600, color: s.text },
        albumSubtitle: { color: s.text2 },
        albumPlayButton: {
          backgroundColor: accent.main,
          color: accent.contrastText,
          borderRadius: '50%',
          '&:hover': { backgroundColor: `${accent.main} !important` },
        },
      },
      NDAlbumDetails: {
        cover: { borderRadius: 6 },
        recordName: { fontWeight: 800, letterSpacing: '-0.02em' },
      },
      NDPlaylistDetails: {
        title: { fontWeight: 800, letterSpacing: '-0.02em' },
      },
      NDAudioPlayer: {
        audioTitle: { color: s.text },
        songInfo: { color: s.text2 },
      },
      NDLogin: {
        main: {
          // Darkens the photo behind the card so the card reads on any image
          boxShadow: `inset 0 0 0 2000px ${fade(s.background, 0.6)}`,
        },
        card: {
          minWidth: 300,
          backgroundColor: s.paper,
          border: `1px solid ${s.divider}`,
          boxShadow: shadow,
        },
        systemNameLink: { color: accent.main },
        welcome: { color: s.text2 },
      },
      NDMobileArtistDetails: {
        bgContainer: {
          background: `linear-gradient(to bottom, ${fade(s.background, 0.72)}, ${s.background}) !important`,
        },
      },
    },
    player: {
      theme: mode,
      stylesheet: createStylesheet({ mode, s, accent, accentSoft }),
    },
  }
}

export const VinylUndergroundDarkTheme = createVinylUndergroundTheme({
  mode: 'dark',
})
export const VinylUndergroundLightTheme = createVinylUndergroundTheme({
  mode: 'light',
})
