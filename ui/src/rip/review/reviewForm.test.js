import { describe, it, expect } from 'vitest'
import {
  editsFrom,
  initialValues,
  invalidFields,
  removeTrack,
  setAlbumField,
  setTrackField,
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
})
