import { useEffect, useState } from 'react'
import { useDataProvider } from 'react-admin'

// How the server matches Artist names (model.ArtistNameKey): regardless of capitals, extra
// spaces, and typographic quotes and dashes.
export const nameKey = (name) =>
  name
    .toLowerCase()
    .replace(/[‘’‛′]/g, "'")
    .replace(/[＂〃ˮײ᳓″‶˶ʺ“”˝‟]/g, '"')
    .replace(/[‐–—−―]/g, '-')
    .split(/\s+/)
    .filter(Boolean)
    .join(' ')

// useArtistSuggestions offers the existing Artists whose name matches what is typed, but the
// Artist excludeId.
export const useArtistSuggestions = (name, excludeId) => {
  const dataProvider = useDataProvider()
  const [artists, setArtists] = useState([])
  useEffect(() => {
    const q = name.trim()
    if (!q) {
      setArtists([])
      return
    }
    let stale = false
    const timer = setTimeout(() => {
      dataProvider
        .getList('artist', {
          pagination: { page: 1, perPage: 10 },
          sort: { field: 'name', order: 'ASC' },
          filter: { name: q },
        })
        .then(({ data }) => {
          if (!stale) setArtists(data.filter((a) => a.id !== excludeId))
        })
        .catch(() => {
          if (!stale) setArtists([])
        })
    }, 250)
    return () => {
      stale = true
      clearTimeout(timer)
    }
  }, [dataProvider, name, excludeId])
  return artists
}
