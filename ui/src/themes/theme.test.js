import themes from './index'
import { describe, it, expect } from 'vitest'

describe('the theme list', () => {
  it('offers the Vinyl Underground theme first, then the 27 inherited themes', () => {
    const names = Object.values(themes).map((t) => t.themeName)
    expect(names.slice(0, 2)).toEqual([
      'Vinyl Underground Dark',
      'Vinyl Underground Light',
    ])
    expect(names.slice(2)).toHaveLength(27)
    expect(names).toEqual(
      expect.arrayContaining(['Dark', 'Light', 'Nord', 'Spotify-ish']),
    )
  })

  it.each(Object.entries(themes))(
    '%s has a name and a player theme',
    (id, theme) => {
      expect(theme.themeName).toBeTruthy()
      expect(['dark', 'light']).toContain(theme.player.theme)
    },
  )
})

describe('NDPlaylistDetails styles', () => {
  const themeEntries = Object.entries(themes)

  it.each(themeEntries)(
    '%s should not set minWidth on details',
    (themeName, theme) => {
      const details = theme.overrides?.NDPlaylistDetails?.details
      expect(details?.minWidth).toBeUndefined()
    },
  )
})

describe('NDAlbumGridView styles', () => {
  const themeEntries = Object.entries(themes)

  // The hover overlay is a sibling of the image, so it keeps square corners.
  it.each(themeEntries)(
    '%s should not round the grid cover image on its own',
    (themeName, theme) => {
      const container = theme.overrides?.NDAlbumGridView?.albumContainer
      expect(container?.['& img']?.borderRadius).toBeUndefined()
    },
  )

  it.each(themeEntries)(
    '%s should clip the grid cover link when it is rounded',
    (themeName, theme) => {
      const link = theme.overrides?.NDAlbumGridView?.link
      if (!link?.borderRadius) return
      expect(link.overflow).toBe('hidden')
    },
  )
})
