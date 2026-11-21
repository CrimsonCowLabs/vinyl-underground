import { describe, it, expect } from 'vitest'
import {
  editsFrom,
  initialValues,
  invalidFields,
  removeTrack,
  setAlbumField,
  setTrackField,
  applyProposals,
  fieldHint,
  proposedValues,
  withProposalsFrom,
} from './reviewForm'

const review = {
  jobId: 'j1',
  album: 'Rip Album',
  albumArtist: '',
  year: '',
  tracks: [
    {
      itemId: 'a',
      status: 'ready',
      path: 'Album/a.opus',
      title: 'First',
      artist: 'Band',
      trackNumber: '',
    },
    { itemId: 'b', status: 'failed', error: 'gone' },
    {
      itemId: 'c',
      status: 'ready',
      path: 'Album/c.opus',
      title: 'Third',
      artist: 'Band',
      trackNumber: '3',
    },
  ],
}

describe('reviewForm', () => {
  it('starts from the tags in the Files, for the Tracks that have one', () => {
    const values = initialValues(review)
    expect(values.album).toBe('Rip Album')
    expect(Object.keys(values.tracks)).toEqual(['a', 'c'])
    expect(values.tracks.c).toEqual({
      title: 'Third',
      artist: 'Band',
      trackNumber: '3',
    })
  })

  it('sends nothing when nothing changed', () => {
    expect(editsFrom(review, initialValues(review))).toBeNull()
  })

  it('sends only the fields the admin changed, trimmed', () => {
    let values = initialValues(review)
    values = setAlbumField(values, 'year', ' 1999 ')
    values = setTrackField(values, 'c', 'title', 'Third Song')
    values = setTrackField(values, 'a', 'trackNumber', '1')
    expect(editsFrom(review, values)).toEqual({
      year: '1999',
      tracks: [
        { itemId: 'a', trackNumber: '1' },
        { itemId: 'c', title: 'Third Song' },
      ],
    })
  })

  it("doesn't count whitespace around a tag as an edit", () => {
    const padded = {
      ...review,
      tracks: [{ ...review.tracks[0], title: 'First ' }],
    }
    expect(editsFrom(padded, initialValues(padded))).toBeNull()
  })

  it('keeps the edits to the other Tracks when one is discarded', () => {
    let values = setTrackField(initialValues(review), 'a', 'title', 'One')
    values = setTrackField(values, 'c', 'title', 'Three')
    values = removeTrack(values, 'c')
    const left = { ...review, tracks: review.tracks.slice(0, 2) }
    expect(editsFrom(left, values)).toEqual({
      tracks: [{ itemId: 'a', title: 'One' }],
    })
  })

  it('flags a year or track number that is not a number', () => {
    let values = initialValues(review)
    expect(invalidFields(values)).toEqual({})
    values = setAlbumField(values, 'year', 'soon')
    values = setTrackField(values, 'c', 'trackNumber', 'x')
    expect(invalidFields(values)).toEqual({ year: true, c: true })
  })

  describe('proposals (#34)', () => {
    const proposed = {
      ...review,
      proposal: { album: 'Real Album', albumArtist: 'Band', year: '1999' },
      proposalsPending: false,
      tracks: [
        {
          ...review.tracks[0],
          proposal: { status: 'ready', title: 'First Song', artist: 'Band' },
        },
        review.tracks[1],
        {
          ...review.tracks[2],
          proposal: { status: 'pending', title: 'Old Guess', artist: 'Band' },
        },
      ],
    }

    it('lists the proposed values, for the Tracks with a File', () => {
      expect(proposedValues(proposed)).toEqual({
        album: 'Real Album',
        albumArtist: 'Band',
        year: '1999',
        tracks: {
          a: { title: 'First Song', artist: 'Band' },
          c: { title: 'Old Guess', artist: 'Band' },
        },
      })
    })

    it('has none for a card without proposals', () => {
      expect(proposedValues(review)).toEqual({ tracks: {} })
    })

    it('fills every proposed value into the form, which then saves only those that change a tag', () => {
      const values = applyProposals(
        initialValues(proposed),
        proposedValues(proposed),
      )
      expect(values.album).toBe('Real Album')
      expect(values.tracks.a.title).toBe('First Song')
      expect(values.tracks.a.trackNumber).toBe('')
      expect(editsFrom(proposed, values)).toEqual({
        album: 'Real Album',
        albumArtist: 'Band',
        year: '1999',
        tracks: [
          { itemId: 'a', title: 'First Song' },
          { itemId: 'c', title: 'Old Guess' },
        ],
      })
    })

    it('leaves a Track discarded meanwhile out', () => {
      const values = applyProposals(
        removeTrack(initialValues(proposed), 'c'),
        proposedValues(proposed),
      )
      expect(values.tracks.c).toBeUndefined()
    })

    it("offers a proposal that isn't in the field yet, and marks one taken but not saved", () => {
      expect(fieldHint('Rip Album', 'Rip Album', 'Real Album')).toEqual({
        offer: 'Real Album',
      })
      expect(fieldHint('Rip Album', 'Typed', 'Real Album')).toEqual({
        offer: 'Real Album',
      })
      expect(fieldHint('Rip Album', ' Real Album ', 'Real Album')).toEqual({
        accepted: true,
        file: 'Rip Album',
      })
      expect(fieldHint('Real Album', 'Real Album', 'Real Album')).toBeNull()
      expect(fieldHint('Rip Album', 'Rip Album', undefined)).toBeNull()
    })

    it('takes fresh proposals in without touching the tags the form started from', () => {
      const fresh = {
        ...proposed,
        album: 'Changed On Disk',
        tracks: proposed.tracks.map((t) =>
          t.itemId === 'c'
            ? {
                ...t,
                title: 'X',
                proposal: { status: 'ready', title: 'Third Song' },
              }
            : t,
        ),
      }
      const merged = withProposalsFrom(proposed, fresh)
      expect(merged.album).toBe('Rip Album')
      expect(merged.proposalsPending).toBe(false)
      expect(merged.tracks[2].proposal.title).toBe('Third Song')
      expect(merged.tracks[2].title).toBe('Third')
    })
  })
})
