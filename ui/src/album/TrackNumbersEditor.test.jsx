import React from 'react'
import { render, fireEvent, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { TrackNumbersEditor } from './TrackNumbersEditor'
import {
  duplicateNumbers,
  trackNumberChanges,
  trackNumbersValid,
  toRows,
} from './trackNumbers'

const songs = [
  { id: 'mf1', title: 'One', discNumber: 1, trackNumber: 2 },
  { id: 'mf2', title: 'Two', discNumber: 1, trackNumber: 1 },
  { id: 'mf3', title: 'Three', discNumber: 0, trackNumber: 3 },
]

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  useTranslate: () => (key, opts) =>
    opts ? `${key} ${JSON.stringify(opts)}` : key,
}))

const set = (rows, id, disc, track) =>
  rows.map((r) => (r.id === id ? { ...r, disc, track } : r))

describe('track numbers', () => {
  it('lists only the Tracks whose numbers changed, as numbers', () => {
    let rows = toRows(songs)
    expect(trackNumberChanges(rows)).toEqual([])
    rows = set(rows, 'mf1', '1', '1')
    rows = set(rows, 'mf2', ' 1 ', '2')
    expect(trackNumberChanges(rows)).toEqual([
      { id: 'mf1', discNumber: 1, trackNumber: 1 },
      { id: 'mf2', discNumber: 1, trackNumber: 2 },
    ])
  })

  it('shows no disc number as empty, and keeps it so', () => {
    const rows = toRows(songs)
    expect(rows[2].disc).toBe('')
    const changed = set(rows, 'mf3', '', '4')
    expect(trackNumberChanges(changed)).toEqual([
      { id: 'mf3', discNumber: 0, trackNumber: 4 },
    ])
    expect(trackNumbersValid(changed)).toBe(true)
  })

  it('accepts only whole numbers from 1 for a changed number', () => {
    const rows = toRows(songs)
    expect(trackNumbersValid(rows)).toBe(true)
    expect(trackNumbersValid(set(rows, 'mf1', '1', '0'))).toBe(false)
    expect(trackNumbersValid(set(rows, 'mf1', '1', 'x'))).toBe(false)
    expect(trackNumbersValid(set(rows, 'mf1', '1', '2.5'))).toBe(false)
    expect(trackNumbersValid(set(rows, 'mf1', '', '2'))).toBe(false)
    expect(trackNumbersValid(set(rows, 'mf1', '1000', '2'))).toBe(false)
    expect(trackNumbersValid(set(rows, 'mf1', '2', '10'))).toBe(true)
  })

  it('finds the Tracks of a disc that share a number', () => {
    const rows = toRows(songs)
    expect(duplicateNumbers(rows)).toEqual([])
    expect(duplicateNumbers(set(rows, 'mf1', '1', '1'))).toEqual([
      { disc: 1, track: 1, titles: ['One', 'Two'] },
    ])
    expect(duplicateNumbers(set(rows, 'mf1', '2', '1'))).toEqual([])
  })
})

describe('TrackNumbersEditor', () => {
  const Editor = ({ initial }) => {
    const [rows, setRows] = React.useState(initial)
    return <TrackNumbersEditor rows={rows} onChange={setRows} />
  }

  it("shows each Track's disc and track number to correct", () => {
    render(<Editor initial={toRows(songs)} />)
    expect(screen.getByText('One')).toBeTruthy()
    expect(screen.getByTestId('track-number-mf1').value).toBe('2')
    expect(screen.getByTestId('disc-number-mf1').value).toBe('1')
    fireEvent.change(screen.getByTestId('track-number-mf1'), {
      target: { value: '5' },
    })
    expect(screen.getByTestId('track-number-mf1').value).toBe('5')
  })

  it('warns, without blocking, when two Tracks of a disc end up with one number', () => {
    render(<Editor initial={toRows(songs)} />)
    expect(screen.queryByRole('alert')).toBeNull()
    fireEvent.change(screen.getByTestId('track-number-mf1'), {
      target: { value: '1' },
    })
    expect(screen.getByRole('alert').textContent).toContain(
      'resources.album.editor.duplicateNumber',
    )
    expect(screen.getByRole('alert').textContent).toContain('One, Two')
  })
})
