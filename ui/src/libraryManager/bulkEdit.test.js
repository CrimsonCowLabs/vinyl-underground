import { describe, it, expect } from 'vitest'
import { bulkEditRequest, matchingFilter } from './bulkEdit'

describe('bulkEditRequest', () => {
  it('names the selected Tracks', () => {
    expect(
      bulkEditRequest({
        selection: { ids: ['t1', 't2'] },
        field: 'album',
        values: [' New '],
      }),
    ).toEqual({ ids: ['t1', 't2'], field: 'album', values: ['New'] })
  })

  it('sends the filter for all matching, not the Tracks', () => {
    expect(
      bulkEditRequest({
        selection: { filter: { folder: 'Artist', format: 'mp3' } },
        field: 'genre',
        values: ['Rock', '', 'Pop'],
      }),
    ).toEqual({
      filter: { folder: 'Artist', format: 'mp3' },
      field: 'genre',
      values: ['Rock', 'Pop'],
    })
  })

  it('clears the field with no values', () => {
    expect(
      bulkEditRequest({
        selection: { ids: ['t1'] },
        field: 'comment',
        values: ['ignored'],
        clear: true,
      }).values,
    ).toEqual([])
  })
})

describe('matchingFilter', () => {
  it("is the list's filter, without empty values, and an empty filter for everything", () => {
    expect(matchingFilter({ folder: 'A', format: '', rip: undefined })).toEqual(
      { folder: 'A' },
    )
    expect(matchingFilter(undefined)).toEqual({})
  })
})
