import { describe, it, expect } from 'vitest'
import { contrastRatio } from './contrast'
import { surfaces, accents, DEFAULT_ACCENT } from './vinylUndergroundPalette'

describe('contrastRatio', () => {
  it('is 21:1 for black on white', () => {
    expect(contrastRatio('#000000', '#FFFFFF')).toBeCloseTo(21, 5)
  })
  it('is 1:1 for a colour on itself, whatever its case', () => {
    expect(contrastRatio('#3EC1B0', '#3ec1b0')).toBeCloseTo(1, 5)
  })
  it('matches the prototype README for body text on Slate', () => {
    expect(contrastRatio('#E8ECF1', '#0F1216')).toBeCloseTo(15.8, 1)
  })
})

describe('Vinyl Underground palette', () => {
  it('records the reviewed Slate dark and cream light values', () => {
    expect(surfaces.dark).toEqual({
      background: '#0F1216',
      paper: '#161A1F',
      surface2: '#1F242B',
      surface3: '#2A3038',
      divider: '#2E343C',
      text: '#E8ECF1',
      text2: '#9AA4B0',
      text3: '#6B7480',
    })
    expect(surfaces.light).toEqual({
      background: '#F3EDE4',
      paper: '#FBF8F3',
      surface2: '#E9E1D5',
      surface3: '#FFFFFF',
      divider: '#D6CBBC',
      text: '#1E1915',
      text2: '#5E5247',
      text3: '#7E7163',
    })
  })

  it('defaults to Teal', () => {
    expect(accents[DEFAULT_ACCENT]).toEqual({
      dark: { main: '#3EC1B0', contrastText: '#0F1216' },
      light: { main: '#086A5F', contrastText: '#FFFFFF' },
    })
  })

  it('offers the eight reviewed accents, Teal first', () => {
    const values = Object.entries(accents).map(([id, a]) => [
      id,
      a.dark.main,
      a.light.main,
    ])
    expect(values).toEqual([
      ['teal', '#3EC1B0', '#086A5F'],
      ['vermilion', '#FF5B2E', '#B0300F'],
      ['amber', '#FF9A1F', '#934E00'],
      ['mustard', '#E2B53A', '#765A00'],
      ['olive', '#9DBB4E', '#4A6510'],
      ['denim', '#6FA3F0', '#2F5DB0'],
      ['plum', '#C08BF0', '#7A3FB0'],
      ['rose', '#FF6F91', '#B42851'],
    ])
  })

  it.each(Object.keys(accents))(
    'puts Slate text on %s in dark and white text in light',
    (name) => {
      expect(accents[name].dark.contrastText).toBe('#0F1216')
      expect(accents[name].light.contrastText).toBe('#FFFFFF')
    },
  )

  describe.each(['dark', 'light'])('%s', (mode) => {
    const s = surfaces[mode]

    it.each(['background', 'paper', 'surface2'])(
      'body and secondary text pass AA (4.5:1) on %s',
      (ground) => {
        expect(contrastRatio(s.text, s[ground])).toBeGreaterThanOrEqual(4.5)
        expect(contrastRatio(s.text2, s[ground])).toBeGreaterThanOrEqual(4.5)
      },
    )

    it('disabled text passes AA for large text and marks (3:1)', () => {
      expect(contrastRatio(s.text3, s.background)).toBeGreaterThanOrEqual(3)
      expect(contrastRatio(s.text3, s.paper)).toBeGreaterThanOrEqual(3)
    })

    it.each(Object.keys(accents))(
      'the %s accent passes AA on the background and paper, and under its own text',
      (name) => {
        const a = accents[name][mode]
        expect(contrastRatio(a.main, s.background)).toBeGreaterThanOrEqual(4.5)
        expect(contrastRatio(a.main, s.paper)).toBeGreaterThanOrEqual(4.5)
        expect(contrastRatio(a.contrastText, a.main)).toBeGreaterThanOrEqual(
          4.5,
        )
      },
    )
  })
})
