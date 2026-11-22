import { useEffect } from 'react'
import useCurrentTheme from './themes/useCurrentTheme'

const useChangeThemeColor = () => {
  const theme = useCurrentTheme()
  // A theme can name its own colour; the Vinyl Underground theme uses its background
  const color =
    theme.themeColor ||
    theme.palette?.primary?.light ||
    theme.palette?.primary?.main ||
    '#ffffff'
  useEffect(() => {
    const themeColor = document.querySelector("meta[name='theme-color']")
    themeColor.setAttribute('content', color)
  }, [color])
}

export default useChangeThemeColor
