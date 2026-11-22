import { useSelector } from 'react-redux'
import useMediaQuery from '@material-ui/core/useMediaQuery'
import themes from './index'
import { AUTO_THEME_ID } from '../consts'
import { currentAccentId, currentThemeId } from '../reducers/themeReducer'
import { useEffect, useMemo } from 'react'
import { withAccent } from './vinylUnderground'
import { accents, ART_ACCENT } from './vinylUndergroundPalette'

const useCurrentTheme = () => {
  // Runs above the ThemeProvider carrying the prop below, so it needs its own noSsr or the
  // auto theme renders dark first and flips.
  const prefersLightMode = useMediaQuery('(prefers-color-scheme: light)', {
    noSsr: true,
  })
  const themeId = useSelector(currentThemeId)
  const accentId = useSelector(currentAccentId)
  const base =
    themeId === AUTO_THEME_ID
      ? prefersLightMode
        ? themes.VinylUndergroundLightTheme
        : themes.VinylUndergroundDarkTheme
      : themes[themeId]
  // "From album art" colours only the player (useArtAccentTheme): the rest
  // of the app keeps the default accent
  const theme = useMemo(
    () =>
      accentId === ART_ACCENT
        ? base
        : withAccent(base, accents[accentId][base.palette?.type]),
    [base, accentId],
  )

  useEffect(() => {
    const styles = document.getElementsByTagName('style')
    let style
    for (let i = 0; i < styles.length; i++) {
      if (styles[i].id === 'nd-player-style-override') {
        style = styles[i]
      }
    }
    if (theme.player.stylesheet) {
      if (style === undefined) {
        style = document.createElement('style')
        style.id = 'nd-player-style-override'
        style.innerHTML = theme.player.stylesheet
        document.head.appendChild(style)
      } else {
        style.innerHTML = theme.player.stylesheet
      }
    } else {
      if (style !== undefined) {
        document.head.removeChild(style)
      }
    }

    // Set body background color to match theme (fixes white background on pull-to-refresh)
    const isDark = theme.palette?.type === 'dark'
    const bgColor =
      theme.palette?.background?.default || (isDark ? '#303030' : '#fafafa')
    document.body.style.backgroundColor = bgColor
  }, [theme])

  // We never server-render, so let media queries resolve on the first render: the default
  // defers them to an effect, which makes every mount paint the wrong breakpoint and reflow.
  return useMemo(
    () => ({
      ...theme,
      props: {
        ...theme.props,
        MuiUseMediaQuery: { noSsr: true },
        MuiPopover: { disableScrollLock: true },
      },
    }),
    [theme],
  )
}

export default useCurrentTheme
