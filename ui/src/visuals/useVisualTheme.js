import { useEffect, useMemo, useState } from 'react'
import { useTheme } from '@material-ui/core/styles'
import { readCoverColour } from '../themes/coverColour'
import { readableAccent } from '../themes/artAccent'
import { vuPalette } from '../nowPlaying/vuPalette'

// useVisualTheme is the colours a visual draws with, from the player's theme
// (so a cover's accent, with "From album art", flows in):
//
//   accent   the accent, for bars and the wave
//   glow     the accent's soft glow (palette.vu.accentGlow)
//   cover    the colour of `cover`, kept vivid, for the glow; the accent for
//            a grey, missing or unreadable cover
//   mode     'dark' or 'light'
export const useVisualTheme = (cover) => {
  const theme = useTheme()
  const mode = theme.palette.type === 'light' ? 'light' : 'dark'
  const accent = theme.palette.primary.main
  const { accentGlow } = vuPalette(theme)
  const [read, setRead] = useState({ cover: null, colour: null })

  useEffect(() => {
    if (!cover) return
    let live = true
    readCoverColour(cover).then((colour) => live && setRead({ cover, colour }))
    return () => {
      live = false
    }
  }, [cover])

  const colour = read.cover === cover && read.colour
  const coverColour = colour ? readableAccent(colour, mode).main : accent
  return useMemo(
    () => ({ accent, glow: accentGlow, cover: coverColour, mode }),
    [accent, accentGlow, coverColour, mode],
  )
}
