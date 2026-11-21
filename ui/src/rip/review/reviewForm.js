// The review card's form state: the album-level fields, and each Track's own, keyed by item.
// Proposed tags (#34) can be dropped in with the same setters the inputs use.

export const ALBUM_FIELDS = ['album', 'albumArtist', 'year']
export const TRACK_FIELDS = ['title', 'artist', 'trackNumber']

// Tracks with a File: only those have tags to edit.
export const editableTracks = (review) =>
  (review?.tracks || []).filter((t) => t.status === 'ready' && t.path)

export const initialValues = (review) => {
  const values = { tracks: {} }
  ALBUM_FIELDS.forEach((f) => {
    values[f] = review?.[f] ?? ''
  })
  editableTracks(review).forEach((t) => {
    values.tracks[t.itemId] = {}
    TRACK_FIELDS.forEach((f) => {
      values.tracks[t.itemId][f] = t[f] ?? ''
    })
  })
  return values
}

export const setAlbumField = (values, field, value) => ({
  ...values,
  [field]: value,
})

export const setTrackField = (values, itemId, field, value) => ({
  ...values,
  tracks: {
    ...values.tracks,
    [itemId]: { ...values.tracks[itemId], [field]: value },
  },
})

// editsFrom is the PUT /api/rip/{jobId}/tags body: only what the admin changed, so tags left
// alone are never rewritten (and Tracks of different albums keep theirs). Returns null when
// nothing changed.
export const editsFrom = (review, values) => {
  const initial = initialValues(review)
  const edits = {}
  ALBUM_FIELDS.forEach((f) => {
    if (values[f].trim() !== initial[f]) edits[f] = values[f].trim()
  })
  const tracks = []
  Object.keys(initial.tracks).forEach((itemId) => {
    const edit = {}
    TRACK_FIELDS.forEach((f) => {
      const v = (values.tracks[itemId]?.[f] ?? '').trim()
      if (v !== initial.tracks[itemId][f]) edit[f] = v
    })
    if (Object.keys(edit).length > 0) tracks.push({ itemId, ...edit })
  })
  if (tracks.length > 0) edits.tracks = tracks
  return Object.keys(edits).length > 0 ? edits : null
}

const numberRe = /^\d{0,4}$/

// invalidFields names the fields the server would refuse: a year or track number that isn't a
// number.
export const invalidFields = (values) => {
  const bad = {}
  if (!numberRe.test(values.year.trim())) bad.year = true
  Object.entries(values.tracks).forEach(([itemId, t]) => {
    if (!numberRe.test((t.trackNumber ?? '').trim())) bad[itemId] = true
  })
  return bad
}
