import { useEffect, useState } from 'react'
import { useDataProvider } from 'react-admin'

// The album's Tracks' disc and track numbers as the Edit dialog corrects them (see
// TrackNumbersEditor).

// The highest numbers the server accepts (core/corrections/track_numbers.go)
export const MAX_DISC = 999
export const MAX_TRACK = 9999

// A Track's numbers as edited: strings, as typed. No disc number shows as empty. Its composers
// come along for the album's Composer field (see sharedComposers).
export const toRows = (tracks) =>
  tracks.map((t) => ({
    id: t.id,
    title: t.title,
    composers: (t.participants?.composer || []).map((c) => c.name),
    fromDisc: t.discNumber || 0,
    fromTrack: t.trackNumber || 0,
    disc: t.discNumber ? String(t.discNumber) : '',
    track: t.trackNumber ? String(t.trackNumber) : '',
  }))

// An edited number, or NaN when it is no whole number. Empty is 0 (no number).
const parse = (value) => {
  const v = value.trim()
  if (v === '') return 0
  return /^\d+$/.test(v) ? Number(v) : NaN
}

// The album's Composer field: several composers are typed apart with ";".
export const splitComposers = (value) =>
  value
    .split(';')
    .map((c) => c.split(/\s+/).filter(Boolean).join(' '))
    .filter(Boolean)
export const joinComposers = (composers) => composers.join('; ')

// The composers every Track has, as the Composer field shows them; '' when the Tracks' composers
// differ (or none has any), and null for an album with no Tracks.
export const sharedComposers = (rows) => {
  if (!rows?.length) return null
  const first = joinComposers(rows[0].composers || [])
  return rows.every((r) => joinComposers(r.composers || []) === first)
    ? first
    : ''
}

// The Tracks whose numbers changed, as the server takes them.
export const trackNumberChanges = (rows) =>
  rows
    .map((r) => ({
      id: r.id,
      discNumber: parse(r.disc),
      trackNumber: parse(r.track),
      row: r,
    }))
    .filter(
      ({ discNumber, trackNumber, row }) =>
        discNumber !== row.fromDisc || trackNumber !== row.fromTrack,
    )
    .map(({ row, ...change }) => change)

// The rows once their changes are saved: the saved numbers are the Tracks' numbers now, so
// saving again (e.g. retrying the album after it was refused) doesn't send them again.
export const markSaved = (rows) =>
  rows.map((r) => ({
    ...r,
    fromDisc: parse(r.disc),
    fromTrack: parse(r.track),
  }))

// A changed number must be a whole number from 1; one left as it was may stay empty.
export const numberValid = (value, from, max) => {
  const n = parse(value)
  return n === from || (Number.isInteger(n) && n >= 1 && n <= max)
}

export const trackNumbersValid = (rows) =>
  rows.every(
    (r) =>
      numberValid(r.disc, r.fromDisc, MAX_DISC) &&
      numberValid(r.track, r.fromTrack, MAX_TRACK),
  )

// The numbers that more than one Track of a disc has, with those Tracks' titles. Only a warning:
// the admin may mean it (e.g. while numbering the album through).
export const duplicateNumbers = (rows) => {
  const byNumber = new Map()
  for (const r of rows) {
    const disc = parse(r.disc)
    const track = parse(r.track)
    if (!Number.isInteger(disc) || !track) continue
    const key = `${disc}-${track}`
    if (!byNumber.has(key)) byNumber.set(key, { disc, track, titles: [] })
    byNumber.get(key).titles.push(r.title)
  }
  return [...byNumber.values()].filter((d) => d.titles.length > 1)
}

// The album's Tracks in the Library, in album order, as rows to edit; null while loading. A
// missing Track (e.g. one in Quarantine) is left out, so it neither clashes with a Track in the
// Library nor counts toward the album's track count.
export const useTrackNumberRows = (albumId) => {
  const dataProvider = useDataProvider()
  const [rows, setRows] = useState(null)
  useEffect(() => {
    let current = true
    dataProvider
      .getList('song', {
        pagination: { page: 1, perPage: 1000 },
        sort: { field: 'album', order: 'ASC' },
        filter: { album_id: albumId, missing: false },
      })
      .then(({ data }) => current && setRows(toRows(data || [])))
      .catch(() => current && setRows([]))
    return () => {
      current = false
    }
  }, [dataProvider, albumId])
  return [rows, setRows]
}
