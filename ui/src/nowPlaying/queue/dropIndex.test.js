import { describe, it, expect } from 'vitest'
import { dropIndex } from './dropIndex'

// Rows 56px high, in a queue of 5
const drop = (from, dy) => dropIndex(from, dy, 56, 5)

describe('dropIndex: where a dragged Track lands', () => {
  it('stays put until it is dragged over half a row', () => {
    expect(drop(2, 27)).toBe(2)
    expect(drop(2, -27)).toBe(2)
  })

  it('moves a row for each row dragged past', () => {
    expect(drop(2, 29)).toBe(3)
    expect(drop(2, 120)).toBe(4)
    expect(drop(2, -60)).toBe(1)
  })

  it('stops at either end of the queue', () => {
    expect(drop(1, -500)).toBe(0)
    expect(drop(3, 500)).toBe(4)
  })
})
