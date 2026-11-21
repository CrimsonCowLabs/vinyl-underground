import { describe, it, expect } from 'vitest'
import {
  editsFrom,
  initialValues,
  invalidFields,
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

  it('flags a year or track number that is not a number', () => {
    let values = initialValues(review)
    expect(invalidFields(values)).toEqual({})
    values = setAlbumField(values, 'year', 'soon')
    values = setTrackField(values, 'c', 'trackNumber', 'x')
    expect(invalidFields(values)).toEqual({ year: true, c: true })
  })
})
