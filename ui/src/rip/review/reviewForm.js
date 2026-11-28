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
    values[f] = (review?.[f] ?? '').trim()
  })
  editableTracks(review).forEach((t) => {
    values.tracks[t.itemId] = {}
    TRACK_FIELDS.forEach((f) => {
      values.tracks[t.itemId][f] = (t[f] ?? '').trim()
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

// removeTrack drops a discarded Track, keeping the edits to the others.
export const removeTrack = (values, itemId) => {
  const tracks = { ...values.tracks }
  delete tracks[itemId]
  return { ...values, tracks }
}

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
  Object.keys(initial.tracks)
    .filter((itemId) => values.tracks[itemId])
    .forEach((itemId) => {
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

// Proposed tags (#34). The card gets them from the server (review.proposal for the album-level
// fields, each Track's proposal for its own); they are only ever offered, never saved unless the
// admin takes them and presses Save.

// proposedValues is what is proposed for each field, for the Tracks with a File. A field with no
// proposal is left out. A pending proposal (being worked out again) still shows its old values.
export const proposedValues = (review) => {
  const res = { tracks: {} }
  ALBUM_FIELDS.forEach((f) => {
    const v = (review?.proposal?.[f] ?? '').trim()
    if (v) res[f] = v
  })
  editableTracks(review).forEach((t) => {
    const p = {}
    PROPOSED_TRACK_FIELDS.forEach((f) => {
      const v = (t.proposal?.[f] ?? '').trim()
      if (v) p[f] = v
    })
    if (Object.keys(p).length > 0) res.tracks[t.itemId] = p
  })
  return res
}

// PROPOSED_TRACK_FIELDS are the Track fields a proposal can have.
export const PROPOSED_TRACK_FIELDS = ['title', 'artist']

// applyProposals puts every proposed value into the form ("Use proposals"), for the Tracks still
// on the card.
export const applyProposals = (values, proposed) => {
  let res = values
  ALBUM_FIELDS.forEach((f) => {
    if (proposed[f]) res = setAlbumField(res, f, proposed[f])
  })
  Object.entries(proposed.tracks).forEach(([itemId, p]) => {
    if (!res.tracks[itemId]) return
    Object.entries(p).forEach(([f, v]) => {
      res = setTrackField(res, itemId, f, v)
    })
  })
  return res
}

// fieldHint says what to show under a field with a proposal: {offer} when the field doesn't hold
// the proposed value (click to take it), {accepted, file} when it does but the File doesn't yet
// (so it is clearly a proposal, not saved), and null otherwise.
export const fieldHint = (fileValue, value, proposedValue) => {
  if (!proposedValue) return null
  const file = (fileValue ?? '').trim()
  if ((value ?? '').trim() !== proposedValue) return { offer: proposedValue }
  if (file !== proposedValue) return { accepted: true, file }
  return null
}

// withProposalsFrom takes the proposals of a fresh copy of the card into the one the form started
// from, leaving its tags (and so what counts as an edit) as they were. What the Inbox scan found
// since (each Track's mediaFileId and albumId, and the album's cover, #218) comes along too, as
// those aren't tags.
export const withProposalsFrom = (review, fresh) => {
  const byItem = {}
  ;(fresh?.tracks || []).forEach((t) => {
    byItem[t.itemId] = t
  })
  return {
    ...review,
    proposal: fresh?.proposal,
    proposalsPending: !!fresh?.proposalsPending,
    cover: fresh?.cover,
    tracks: review.tracks.map((t) => {
      const f = byItem[t.itemId]
      if (!f) return t
      return {
        ...t,
        proposal: f.proposal,
        mediaFileId: f.mediaFileId,
        albumId: f.albumId,
      }
    }),
  }
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
