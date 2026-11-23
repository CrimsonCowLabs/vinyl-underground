import { describe, it, expect } from 'vitest'
import { changedValues, editableValues } from './tagEdit'

const fields = [
  { key: 'title', type: 'text', multi: false },
  { key: 'genre', type: 'text', multi: true },
  { key: 'tracknumber', type: 'number', multi: false },
  { key: 'compilation', type: 'bool', multi: false },
  { key: 'lyrics', type: 'longtext', multi: false },
]

describe('editableValues', () => {
  it('gives every field at least one input, so an empty tag can be filled in', () => {
    expect(
      editableValues(fields, { title: ['Song'], genre: ['Rock', 'Pop'] }),
    ).toEqual({
      title: ['Song'],
      genre: ['Rock', 'Pop'],
      tracknumber: [''],
      compilation: [''],
      lyrics: [''],
    })
  })
})

describe('changedValues', () => {
  const original = {
    title: ['Song'],
    genre: ['Rock'],
    tracknumber: ['2'],
    compilation: [],
    lyrics: [],
  }

  it('is empty when nothing was changed, blank inputs included', () => {
    expect(
      changedValues(fields, original, editableValues(fields, original)),
    ).toEqual({})
  })

  it('holds only the fields that changed, as lists without blanks', () => {
    const edited = {
      ...editableValues(fields, original),
      title: ['Song (Live)'],
      genre: ['Rock', '', 'Pop'],
    }
    expect(changedValues(fields, original, edited)).toEqual({
      title: ['Song (Live)'],
      genre: ['Rock', 'Pop'],
    })
  })

  it('sends a cleared field as no values, which removes the tag', () => {
    const edited = { ...editableValues(fields, original), tracknumber: [''] }
    expect(changedValues(fields, original, edited)).toEqual({ tracknumber: [] })
  })

  it('ignores spaces around a value, as the server trims them', () => {
    const edited = { ...editableValues(fields, original), title: [' Song '] }
    expect(changedValues(fields, original, edited)).toEqual({})
  })
})
