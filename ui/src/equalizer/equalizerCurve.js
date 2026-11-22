// The equalizer's ten bands, an octave apart. The ends are shelves, so the
// lowest band lifts or cuts everything below it and the highest everything
// above it; the eight between are peaking filters about an octave
// wide (Q 1.41), so neighbouring bands blend into a smooth curve.
export const BANDS = [31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000]
export const PEAKING_Q = 1.41

const LAST = BANDS.length - 1
export const bandType = (index) =>
  index === 0 ? 'lowshelf' : index === LAST ? 'highshelf' : 'peaking'

const dbToGain = (db) => 10 ** (db / 20)

// isFlat: a curve that leaves the sound as it is, so needs no filters at all.
export const isFlat = ({ bands, preamp }) =>
  preamp === 0 && bands.every((gain) => gain === 0)

// A band's biquad coefficients [b0, b1, b2, a0, a1, a2], from the Audio EQ
// Cookbook formulas Web Audio's BiquadFilterNode is specified with (its
// shelves have a slope of 1).
const coefficients = (index, gain, sampleRate) => {
  const A = 10 ** (gain / 40)
  const w0 = (2 * Math.PI * BANDS[index]) / sampleRate
  const cos = Math.cos(w0)
  const type = bandType(index)
  if (type === 'peaking') {
    const alpha = Math.sin(w0) / (2 * PEAKING_Q)
    return [
      1 + alpha * A,
      -2 * cos,
      1 - alpha * A,
      1 + alpha / A,
      -2 * cos,
      1 - alpha / A,
    ]
  }
  const s = 2 * Math.sqrt(A) * (Math.sin(w0) / 2) * Math.SQRT2
  const sign = type === 'lowshelf' ? 1 : -1
  return [
    A * (A + 1 - sign * (A - 1) * cos + s),
    sign * 2 * A * (A - 1 - sign * (A + 1) * cos),
    A * (A + 1 - sign * (A - 1) * cos - s),
    A + 1 + sign * (A - 1) * cos + s,
    -sign * 2 * (A - 1 + sign * (A + 1) * cos),
    A + 1 + sign * (A - 1) * cos - s,
  ]
}

// |H| of a biquad at angular frequency w, in dB
const responseDb = ([b0, b1, b2, a0, a1, a2], w) => {
  const c1 = Math.cos(w)
  const s1 = Math.sin(w)
  const c2 = Math.cos(2 * w)
  const s2 = Math.sin(2 * w)
  const numerator = (b0 + b1 * c1 + b2 * c2) ** 2 + (b1 * s1 + b2 * s2) ** 2
  const denominator = (a0 + a1 * c1 + a2 * c2) ** 2 + (a1 * s1 + a2 * s2) ** 2
  return 10 * Math.log10(numerator / denominator)
}

// Where the curve is measured: log-spaced from 10 Hz up to the Nyquist
// frequency, plus each band's own centre.
const POINTS = 240
const measuredAt = (sampleRate) => {
  const nyquist = sampleRate / 2
  const spaced = Array.from(
    { length: POINTS + 1 },
    (_, i) => 10 * (nyquist / 10) ** (i / POINTS),
  )
  return [...spaced, ...BANDS.filter((f) => f < nyquist)]
}

// peakDb is the most the ten bands together lift any frequency, in dB:
// overlapping boosts add up to more than either band alone.
export const peakDb = (bands, sampleRate = 48000) => {
  const boosted = bands
    .map((gain, index) => [gain, index])
    .filter(([gain]) => gain !== 0)
  if (!boosted.some(([gain]) => gain > 0)) return 0
  const filters = boosted.map(([gain, index]) =>
    coefficients(index, gain, sampleRate),
  )
  return Math.max(
    ...measuredAt(sampleRate).map((frequency) => {
      const w = (2 * Math.PI * frequency) / sampleRate
      return filters.reduce((sum, filter) => sum + responseDb(filter, w), 0)
    }),
  )
}

// preampGain is the linear gain the preamp applies: the Listener's preamp,
// lowered by however much the bands lift the loudest frequency, so a boost
// doesn't push full-scale audio past full scale and clip.
export const preampGain = ({ bands, preamp }, sampleRate) =>
  dbToGain(preamp - Math.max(0, peakDb(bands, sampleRate)))
