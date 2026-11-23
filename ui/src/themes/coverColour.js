import { coverColour } from './artAccent'
import { readCoverPixels } from './coverPixels'

// Cover colours already read, by cover URL (null for a grey or failed cover)
const coverColours = new Map()
const MAX_CACHED = 50

// readCoverColour is the colour taken from a cover, as #rrggbb, or null for a
// grey, missing or failed one. The player's accent (PlayerThemeProvider) and the visuals'
// glow both use it.
export const readCoverColour = (cover) => {
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
