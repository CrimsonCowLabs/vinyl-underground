import { describe, it, expect } from 'vitest'
import { currentLine, readLyrics } from './lyrics'

// What the server stores in a Track's lyrics, and what getLyricsBySongId
// answers with: OpenSubsonic structured lyrics, times in milliseconds
const synced = {
  lang: 'eng',
  synced: true,
  line: [
    { start: 1000, value: 'Headlights low on the ring road' },
    { start: 5500, value: 'counting the cats-eyes home' },
    { start: 9000, value: 'you had the window down' },
  ],
}
const unsynced = {
  lang: 'eng',
  synced: false,
  line: [
    { value: 'Copper static on the line' },
    { value: '' },
    { value: 'nobody else awake' },
  ],
}

describe('readLyrics', () => {
  it('reads synced lyrics as timed lines, in seconds', () => {
    expect(readLyrics(JSON.stringify([synced]))).toEqual({
      synced: true,
      lines: [
        { time: 1, text: 'Headlights low on the ring road' },
        { time: 5.5, text: 'counting the cats-eyes home' },
        { time: 9, text: 'you had the window down' },
      ],
    })
  })

  it('keeps unsynced lyrics as plain lines, blank ones included', () => {
    expect(readLyrics([unsynced])).toEqual({
      synced: false,
      lines: ['Copper static on the line', '', 'nobody else awake'],
    })
  })

  it('applies the offset: a positive one shows the lines sooner', () => {
    const lyrics = readLyrics([{ ...synced, offset: 500 }])
    expect(lyrics.lines.map((l) => l.time)).toEqual([0.5, 5, 8.5])
  })

  it('takes the main lyrics over a translation', () => {
    const translation = {
      ...synced,
      kind: 'translation',
      line: [{ start: 1000, value: 'Phares bas sur le périphérique' }],
    }
    const lyrics = readLyrics([translation, { ...unsynced, kind: 'main' }])
    expect(lyrics.synced).toBe(false)
    expect(lyrics.lines[0]).toBe('Copper static on the line')
  })

  it('prefers synced lyrics when a Track has both', () => {
    expect(readLyrics([unsynced, synced]).synced).toBe(true)
  })

  it('reads lines marked synced but without times as unsynced', () => {
    const lyrics = readLyrics([{ synced: true, line: [{ value: 'la la' }] }])
    expect(lyrics).toEqual({ synced: false, lines: ['la la'] })
  })

  it('finds no lyrics in nothing, an empty list, blank lines or bad JSON', () => {
    expect(readLyrics(undefined)).toBeNull()
    expect(readLyrics('')).toBeNull()
    expect(readLyrics('[]')).toBeNull()
    expect(readLyrics([{ synced: false, line: [{ value: ' ' }] }])).toBeNull()
    expect(readLyrics('not json')).toBeNull()
  })
})

describe('currentLine', () => {
  const { lines } = readLyrics([synced])

  it('is none before the first line starts', () => {
    expect(currentLine(lines, 0.5)).toBe(-1)
  })

  it('is the last line that has started', () => {
    expect(currentLine(lines, 1)).toBe(0)
    expect(currentLine(lines, 7)).toBe(1)
    expect(currentLine(lines, 200)).toBe(2)
  })
})
