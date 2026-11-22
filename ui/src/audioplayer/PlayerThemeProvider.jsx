import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useSelector } from 'react-redux'
import { ThemeProvider } from '@material-ui/core/styles'
import { createMuiTheme } from 'react-admin'
import { currentAccentId } from '../reducers/themeReducer'
import { coverColour, readableAccent } from '../themes/artAccent'
import { readCoverPixels } from '../themes/coverPixels'
import { withAccent } from '../themes/vinylUnderground'
import {
  accents,
  ART_ACCENT,
  DEFAULT_ACCENT,
} from '../themes/vinylUndergroundPalette'

const FADE_MS = 700
// Steps of the fade: each one rebuilds the player's theme, so not every frame
const FADE_STEP_MS = 50

// Cover colours already read, by cover URL (null for a grey or failed cover)
const coverColours = new Map()
const MAX_CACHED = 50

const readColour = (cover) => {
  if (coverColours.has(cover)) return Promise.resolve(coverColours.get(cover))
  return (
    readCoverPixels(cover)
      // null pixels: the server's placeholder, for a missing cover
      .then((pixels) => (pixels ? coverColour(pixels) : null))
      .catch(() => null)
      .then((colour) => {
        if (coverColours.size >= MAX_CACHED) {
          coverColours.delete(coverColours.keys().next().value)
        }
        coverColours.set(cover, colour)
        return colour
      })
  )
}

// useCoverColour is the colour taken from the cover, or null for no cover
// or one that is grey or fails to load, and whether it's the first cover read
// since the player appeared. While the next cover is being read, it stays on
// the last one's, so the fade starts once it's known.
const useCoverColour = (cover, enabled) => {
  const [read, setRead] = useState({ colour: null, count: 0 })
  useEffect(() => {
    if (!enabled || !cover) {
      setRead((r) => ({ colour: null, count: r.count }))
      return
    }
    let live = true
    readColour(cover).then(
      (colour) => live && setRead((r) => ({ colour, count: r.count + 1 })),
    )
    return () => {
      live = false
    }
  }, [cover, enabled])
  return { colour: enabled ? read.colour : null, first: read.count <= 1 }
}

const rgb = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16))
const mix = (from, to, t) => {
  const a = rgb(from)
  const b = rgb(to)
  return (
    '#' +
    a
      .map((v, i) =>
        Math.round(v + (b[i] - v) * t)
          .toString(16)
          .padStart(2, '0'),
      )
      .join('')
      .toUpperCase()
  )
}

const reducedMotion = () =>
  !!window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches

// useFade follows `target` (a #RRGGBB colour), fading from the colour shown
// over FADE_MS, unless `instant` or motion is reduced.
const useFade = (target, instant) => {
  const [shown, setShown] = useState(target)
  const shownRef = useRef(target)
  const instantRef = useRef(instant)
  instantRef.current = instant
  useEffect(() => {
    const from = shownRef.current
    if (from === target) return
    const show = (hex) => {
      shownRef.current = hex
      setShown(hex)
    }
    if (instantRef.current || reducedMotion()) {
      show(target)
      return
    }
    let frame
    let start
    let last = 0
    const tick = (now) => {
      if (start === undefined) start = now
      const t = Math.min(1, (now - start) / FADE_MS)
      if (t === 1 || now - last >= FADE_STEP_MS) {
        last = now
        // ease in and out
        show(mix(from, target, t < 0.5 ? 2 * t * t : 1 - (-2 * t + 2) ** 2 / 2))
      }
      if (t < 1) frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame)
  }, [target])
  return shown
}

// useArtAccentTheme is `theme` with its accent taken from `cover`, when the
// Listener chose "From album art" and the theme is Vinyl Underground; any
// other theme or accent as it is. A grey, missing or failed cover gets the
// default accent.
const useArtAccentTheme = (theme, cover) => {
  const mode = theme.palette?.vu ? theme.palette.type : null
  const enabled = useSelector(currentAccentId) === ART_ACCENT && !!mode
  const { colour, first } = useCoverColour(cover, enabled)
  const fallback = mode ? accents[DEFAULT_ACCENT][mode] : null
  const target = colour ? readableAccent(colour, mode) : fallback
  // The first cover read after a load shows at once, rather than fading in
  const main = useFade(target?.main, first)
  return useMemo(
    () =>
      !enabled || main === fallback.main
        ? theme
        : withAccent(theme, { main, contrastText: target.contrastText }),
    [theme, enabled, main, fallback, target?.contrastText],
  )
}

// PlayerThemeProvider is the theme of the player's subtree: the mini player,
// the player bar and Now Playing (and the desktop side panel, #91). With
// "From album art" its accent is the current cover's, so any component in it
// that styles with the theme's palette keys (palette.primary, palette.vu's
// accentSoft and accentGlow) follows the cover, and the player package's
// stylesheet is redrawn with it. The rest of the app keeps the default accent.
export const PlayerThemeProvider = ({ theme, cover, children }) => {
  const playerTheme = useArtAccentTheme(theme, cover)
  const muiTheme = useMemo(() => createMuiTheme(playerTheme), [playerTheme])
  return (
    <ThemeProvider theme={muiTheme}>
      {playerTheme !== theme && playerTheme.player?.stylesheet && (
        // After the app's own player stylesheet in <head>, so it wins
        <style data-vu-player-accent="">{playerTheme.player.stylesheet}</style>
      )}
      {children}
    </ThemeProvider>
  )
}
