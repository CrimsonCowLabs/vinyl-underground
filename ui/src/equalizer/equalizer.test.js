import { describe, it, expect, beforeEach } from 'vitest'
import { createAudioChain } from '../audioplayer/audioChain'
import { createEqualizer } from './equalizer'

// A fake Web Audio graph, as in audioChain.test.js: each node records what it
// is connected to, so a test can walk the path from the element to the
// speakers and read each filter's settings.
class FakeNode {
  constructor(kind) {
    this.kind = kind
    this.outputs = []
  }
  connect(node) {
    this.outputs.push(node)
    return node
  }
  disconnect() {
    this.outputs = []
  }
}

class FakeParam {
  constructor(value) {
    this.value = value
  }
  setValueAtTime(value) {
    this.value = value
  }
  setTargetAtTime(value) {
    this.value = value
  }
}

const contexts = []

class FakeAudioContext {
  constructor() {
    this.state = 'suspended'
    this.currentTime = 0
    this.sampleRate = 48000
    this.destination = new FakeNode('destination')
    this.sources = []
    contexts.push(this)
  }
  createMediaElementSource() {
    const source = new FakeNode('source')
    this.sources.push(source)
    return source
  }
  createGain() {
    const node = new FakeNode('gain')
    node.gain = new FakeParam(1)
    return node
  }
  createAnalyser() {
    return new FakeNode('analyser')
  }
  createBiquadFilter() {
    const node = new FakeNode('filter')
    node.type = 'lowpass'
    node.frequency = new FakeParam(350)
    node.Q = new FakeParam(1)
    node.gain = new FakeParam(0)
    return node
  }
  resume() {
    this.state = 'running'
    return Promise.resolve()
  }
  close() {
    return Promise.resolve()
  }
}

const newElement = () => ({ paused: true, crossOrigin: null })

// The nodes from the element's source to the speakers, in order.
const pathNodes = (context) => {
  const nodes = []
  let node = context.sources[0]
  while (node) {
    nodes.push(node)
    node = node.outputs[0]
  }
  return nodes
}
const path = (context) => pathNodes(context).map((node) => node.kind)
const filters = (context) =>
  pathNodes(context).filter((node) => node.kind === 'filter')
// The equalizer's preamp: the first gain on the path (ReplayGain's is last)
const preamp = (context) => pathNodes(context)[1]

const FLAT = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
const BASS = [6, 5, 4, 2, 0, 0, 0, 0, 0, 0]

const settings = (overrides = {}) => ({
  on: true,
  bands: FLAT,
  preamp: 0,
  ...overrides,
})

describe('the equalizer', () => {
  let chain
  let equalizer

  beforeEach(() => {
    contexts.length = 0
    chain = createAudioChain({ AudioContext: FakeAudioContext })
    chain.attach(newElement())
    equalizer = createEqualizer(chain)
  })

  const apply = (eq, { available = true, isRadio = false } = {}) =>
    equalizer.apply(eq, { available, isRadio })

  it('builds no chain while it is off', () => {
    apply(settings({ on: false, bands: BASS }))
    expect(contexts).toHaveLength(0)
  })

  it('puts a preamp and ten filters, 31 Hz to 16 kHz, into the chain', () => {
    apply(settings({ bands: BASS }))

    expect(contexts).toHaveLength(1)
    expect(path(contexts[0])).toEqual([
      'source',
      'gain',
      ...Array(10).fill('filter'),
      'analyser',
      'gain',
      'destination',
    ])
    expect(
      filters(contexts[0]).map((f) => [
        f.type,
        f.frequency.value,
        f.type === 'peaking' ? f.Q.value : 'no Q',
        f.gain.value,
      ]),
    ).toEqual([
      ['lowshelf', 31, 'no Q', 6],
      ['peaking', 62, 1.41, 5],
      ['peaking', 125, 1.41, 4],
      ['peaking', 250, 1.41, 2],
      ['peaking', 500, 1.41, 0],
      ['peaking', 1000, 1.41, 0],
      ['peaking', 2000, 1.41, 0],
      ['peaking', 4000, 1.41, 0],
      ['peaking', 8000, 1.41, 0],
      ['highshelf', 16000, 'no Q', 0],
    ])
  })

  it('builds no chain for a flat curve', () => {
    apply(settings({ bands: FLAT, preamp: 0 }))
    expect(contexts).toHaveLength(0)
  })

  it('builds the chain for a flat curve with the preamp lowered', () => {
    apply(settings({ bands: FLAT, preamp: -6 }))
    expect(contexts).toHaveLength(1)
    expect(preamp(contexts[0]).gain.value).toBeCloseTo(0.501, 3)
  })

  describe('preamp', () => {
    const preampFor = (bands, listenersPreamp = 0) => {
      apply(settings({ bands, preamp: listenersPreamp }))
      return preamp(contexts[0]).gain.value
    }
    const only = (index, gain) => FLAT.map((_, i) => (i === index ? gain : 0))

    it('lowers the level by as much as one band is boosted', () => {
      expect(preampFor(only(5, 6))).toBeCloseTo(0.501, 2) // -6 dB
    })

    it('lowers the level for a boosted shelf', () => {
      expect(preampFor(only(0, 6))).toBeCloseTo(0.501, 2)
      expect(preampFor(only(9, 12))).toBeCloseTo(0.251, 2) // -12 dB
    })

    it('lowers it further where neighbouring boosts add up', () => {
      const gain = preampFor([0, 6, 6, 0, 0, 0, 0, 0, 0, 0])
      expect(gain).toBeLessThan(0.45)
      expect(gain).toBeGreaterThan(0.25)
    })

    it('leaves the level alone when bands are only cut', () => {
      expect(preampFor([-6, 0, -3, 0, 0, -12, 0, 0, 0, -1])).toBe(1)
    })

    it("adds the Listener's own preamp on top", () => {
      expect(preampFor(only(5, 6), -3)).toBeCloseTo(0.355, 2) // -9 dB
    })
  })

  it("shapes a ReplayGain'd Track, before ReplayGain's gain", () => {
    chain.setReplayGain(0.5)
    apply(settings({ bands: BASS }))

    const nodes = pathNodes(contexts[0])
    expect(nodes.map((n) => n.kind)).toEqual([
      'source',
      'gain',
      ...Array(10).fill('filter'),
      'analyser',
      'gain',
      'destination',
    ])
    expect(nodes[13].gain.value).toBe(0.5)
  })

  it('builds no chain while audio effects are unavailable (iOS, switch off)', () => {
    apply(settings({ bands: BASS }), { available: false })
    expect(contexts).toHaveLength(0)

    apply(settings({ bands: BASS }), { available: true })
    expect(filters(contexts[0])).toHaveLength(10)

    apply(settings({ bands: BASS }), { available: false })
    expect(path(contexts[0])).toEqual([
      'source',
      'analyser',
      'gain',
      'destination',
    ])
  })

  describe('on Internet Radio', () => {
    it('waits for a Library Track before building the chain', () => {
      apply(settings({ bands: BASS }), { isRadio: true })
      expect(contexts).toHaveLength(0)

      apply(settings({ bands: BASS }), { isRadio: false })
      expect(filters(contexts[0])).toHaveLength(10)
    })

    it('filters the radio once the chain is already built', () => {
      chain.setReplayGain(1)
      apply(settings({ bands: BASS }), { isRadio: true })
      expect(filters(contexts[0])).toHaveLength(10)
    })

    it('stays in the chain when a radio stream follows a Track', () => {
      apply(settings({ bands: BASS }))
      apply(settings({ bands: FLAT.map(() => -3) }), { isRadio: true })
      expect(filters(contexts[0]).map((f) => f.gain.value)).toEqual(
        FLAT.map(() => -3),
      )
    })
  })

  it('applies the current curve to the new chain when the player remounts', () => {
    apply(settings({ bands: BASS }))
    chain.attach(newElement()) // a fresh player element: a new chain

    expect(contexts).toHaveLength(2)
    expect(filters(contexts[1]).map((f) => f.gain.value)).toEqual(BASS)

    // and later changes reach the new chain's filters
    apply(settings({ bands: [0, 0, 0, 0, 0, 0, 0, 0, 0, 4] }))
    expect(filters(contexts[1]).map((f) => f.gain.value)).toEqual([
      0, 0, 0, 0, 0, 0, 0, 0, 0, 4,
    ])
    expect(preamp(contexts[1]).gain.value).toBeLessThan(1)
  })

  it('adjusts the same filters live as the curve changes', () => {
    apply(settings({ bands: BASS }))
    const before = filters(contexts[0])

    apply(settings({ bands: [0, 0, 0, 0, 0, 0, 0, 0, 0, -9] }))

    expect(contexts).toHaveLength(1)
    expect(filters(contexts[0])).toEqual(before)
    expect(filters(contexts[0]).map((f) => f.gain.value)).toEqual([
      0, 0, 0, 0, 0, 0, 0, 0, 0, -9,
    ])
  })

  it('takes itself out of the chain when turned off, or set flat', () => {
    apply(settings({ bands: BASS }))
    apply(settings({ on: false, bands: BASS }))
    expect(path(contexts[0])).toEqual([
      'source',
      'analyser',
      'gain',
      'destination',
    ])

    apply(settings({ bands: BASS }))
    apply(settings({ bands: FLAT }))
    expect(path(contexts[0])).toEqual([
      'source',
      'analyser',
      'gain',
      'destination',
    ])
  })
})
