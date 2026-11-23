import { useEffect, useMemo, useState } from 'react'
import { alpha, lighten, useTheme } from '@material-ui/core/styles'
import { readCoverColour } from '../themes/coverColour'
import { readableAccent } from '../themes/artAccent'

// useVisualTheme is the colours a visual draws with, from the player's theme
// (so a cover's accent, with "From album art", flows in). Spectrum and
// waveform draw over a dimmed cover, and the glow is light, so all of them
// want the bright form of a colour, whichever mode the app is in:
//
//   accent   the accent, for bars and the wave; lightened in light mode,
//            where the accent is darkened to read on cream
//   glow     the accent, faint, for the tops of bars and under the wave
//   cover    the colour of `cover`, made vivid, for the glow; the accent for
//            a grey, missing or unreadable cover
//   mode     'dark' or 'light'
export const useVisualTheme = (cover) => {
  const theme = useTheme()
  const mode = theme.palette.type === 'light' ? 'light' : 'dark'
  const primary = theme.palette.primary.main
  const accent = mode === 'light' ? lighten(primary, 0.35) : primary
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
  const coverColour = colour ? readableAccent(colour, 'dark').main : accent
  return useMemo(
    () => ({ accent, glow: alpha(accent, 0.35), cover: coverColour, mode }),
    [accent, coverColour, mode],
  )
}
