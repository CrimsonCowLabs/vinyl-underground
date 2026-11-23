// The sums behind the lightweight visuals (#96), kept free of canvas and Web
// Audio so they can be checked on fixed numbers.

// logBands splits an analyser's frequency bins into `count` bars spaced
// evenly in octaves from minHz to maxHz, as the ear hears them: each is
// { from, to }, a range of bin indexes (to excluded). The deep bass spans
// less than a bin per bar, so each bar is given at least one bin of its own.
export const logBands = (
  count,
  { sampleRate, binCount, minHz = 40, maxHz = 16000 },
) => {
  const binHz = sampleRate / 2 / binCount
  const top = Math.min(maxHz, sampleRate / 2)
  const edges = []
  for (let i = 0; i <= count; i++) {
    const hz = minHz * (top / minHz) ** (i / count)
    const bin = Math.round(hz / binHz)
    edges.push(i === 0 ? bin : Math.max(bin, edges[i - 1] + 1))
  }
  const bands = []
  for (let i = 0; i < count; i++) {
    bands.push({
      from: Math.min(edges[i], binCount - 1),
      to: Math.min(edges[i + 1], binCount),
    })
  }
  return bands
}

// bandLevels is each bar's level from 0 to 1: the loudest bin in it, from an
// analyser's byte frequency data.
export const bandLevels = (data, bands) =>
  bands.map(({ from, to }) => {
    let loudest = 0
    for (let i = from; i < to; i++) if (data[i] > loudest) loudest = data[i]
    return loudest / 255
  })

// smooth eases `levels` toward `targets` in place: a level rises by `rise` of
// the gap each frame and falls by `fall`, so bars jump up with a beat and
// sink back gently rather than flicker. It takes the targets' length.
export const smooth = (levels, targets, { rise = 0.6, fall = 0.12 } = {}) => {
  levels.length = targets.length
  for (let i = 0; i < targets.length; i++) {
    const level = levels[i] ?? 0
    const target = targets[i]
    levels[i] = level + (target - level) * (target > level ? rise : fall)
  }
  return levels
}

// wavePoints samples `count` points evenly across an analyser's byte
// time-domain data, each from -1 to 1 with 0 for silence.
export const wavePoints = (data, count) => {
  const points = new Array(count)
  for (let i = 0; i < count; i++) {
    points[i] = (data[Math.floor((i * data.length) / count)] - 128) / 128
  }
  return points
}
