import { describe, it, expect, beforeEach } from 'vitest'
import { createAudioChain } from './audioChain'

// A fake Web Audio graph: each node records what it is connected to, so a
// test can walk the path from the element to the speakers.
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
}

const contexts = []

class FakeAudioContext {
  constructor() {
    this.state = 'suspended'
    this.currentTime = 0
    this.destination = new FakeNode('destination')
    this.sources = []
    this.resumed = 0
    this.closed = false
    contexts.push(this)
  }
  createMediaElementSource(element) {
    if (element.routed) {
      throw new Error('element already routed through a source')
    }
    element.routed = true
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
    node.gain = new FakeParam(0)
    return node
  }
  resume() {
    this.resumed++
    this.state = 'running'
    return Promise.resolve()
  }
  close() {
    this.closed = true
    return Promise.resolve()
  }
}

const newElement = ({ paused = true } = {}) => ({ paused, crossOrigin: null })

// The kinds of node from the element's source to the speakers, in order.
const path = (context) => {
  const kinds = []
  let node = context.sources[0]
  while (node) {
    kinds.push(node.kind)
    expect(node.outputs.length).toBeLessThanOrEqual(1)
    node = node.outputs[0]
  }
  return kinds
}

const nodeOfKind = (context, kind) => {
  let node = context.sources[0]
  while (node && node.kind !== kind) node = node.outputs[0]
  return node
}

describe('createAudioChain', () => {
  let chain

  beforeEach(() => {
    contexts.length = 0
    chain = createAudioChain({ AudioContext: FakeAudioContext })
  })

  it('does not build the chain when an element is attached', () => {
    chain.attach(newElement())
    expect(contexts).toHaveLength(0)
  })

  it('leaves ReplayGain unrequested without building the chain', () => {
    chain.attach(newElement())
    chain.setReplayGain(null)
    expect(contexts).toHaveLength(0)
  })

  it('builds the chain when ReplayGain asks for it, and applies the gain', () => {
    const element = newElement()
    chain.attach(element)
    chain.setReplayGain(0.5)

    expect(contexts).toHaveLength(1)
    expect(path(contexts[0])).toEqual([
      'source',
      'analyser',
      'gain',
      'destination',
    ])
    expect(nodeOfKind(contexts[0], 'gain').gain.value).toBe(0.5)
    // Kept from the ReplayGain-only chain, for Internet Radio in Firefox.
    expect(element.crossOrigin).toBe('anonymous')
  })

  it('updates the gain on the same chain as Tracks change', () => {
    chain.attach(newElement())
    chain.setReplayGain(0.5)
    chain.setReplayGain(0.8)
    expect(contexts).toHaveLength(1)
    expect(nodeOfKind(contexts[0], 'gain').gain.value).toBe(0.8)
  })

  it('passes audio through at unity gain once ReplayGain is turned off', () => {
    chain.attach(newElement())
    chain.setReplayGain(0.5)
    chain.setReplayGain(null)
    expect(nodeOfKind(contexts[0], 'gain').gain.value).toBe(1)
  })

  it('builds when ReplayGain asks before the element is attached', () => {
    chain.setReplayGain(0.25)
    expect(contexts).toHaveLength(0)
    chain.attach(newElement())
    expect(contexts).toHaveLength(1)
    expect(nodeOfKind(contexts[0], 'gain').gain.value).toBe(0.25)
  })

  it('hands out an analyser on demand, building the chain once', () => {
    const element = newElement()
    chain.attach(element)
    const analyser = chain.getAnalyser()

    expect(analyser.kind).toBe('analyser')
    expect(path(contexts[0])).toEqual([
      'source',
      'analyser',
      'gain',
      'destination',
    ])
    // ReplayGain unrequested, so its slot passes audio through.
    expect(nodeOfKind(contexts[0], 'gain').gain.value).toBe(1)

    chain.setReplayGain(0.5)
    expect(chain.getAnalyser()).toBe(analyser)
    expect(contexts).toHaveLength(1)
  })

  it('returns no analyser before an element is attached', () => {
    expect(chain.getAnalyser()).toBeNull()
  })

  it('returns no analyser where the browser has no Web Audio', () => {
    const noWebAudio = createAudioChain({ AudioContext: undefined })
    noWebAudio.attach(newElement())
    expect(noWebAudio.getAnalyser()).toBeNull()
    expect(() => noWebAudio.setReplayGain(0.5)).not.toThrow()
  })

  it('puts an equalizer filter bank between the source and the analyser', () => {
    chain.attach(newElement())
    chain.setReplayGain(0.5)
    const filters = chain.setEqualizer((context) => [
      context.createBiquadFilter(),
      context.createBiquadFilter(),
    ])

    expect(filters).toHaveLength(2)
    expect(path(contexts[0])).toEqual([
      'source',
      'filter',
      'filter',
      'analyser',
      'gain',
      'destination',
    ])
    expect(nodeOfKind(contexts[0], 'gain').gain.value).toBe(0.5)
  })

  it('builds the chain when the equalizer asks for it', () => {
    chain.attach(newElement())
    chain.setEqualizer((context) => [context.createBiquadFilter()])
    expect(contexts).toHaveLength(1)
    expect(path(contexts[0])).toEqual([
      'source',
      'filter',
      'analyser',
      'gain',
      'destination',
    ])
  })

  it('passes audio straight through once the equalizer is removed', () => {
    chain.attach(newElement())
    chain.setEqualizer((context) => [context.createBiquadFilter()])
    chain.setEqualizer(null)
    expect(path(contexts[0])).toEqual([
      'source',
      'analyser',
      'gain',
      'destination',
    ])
  })

  it('replaces one filter bank with another', () => {
    chain.attach(newElement())
    chain.setEqualizer((context) => [context.createBiquadFilter()])
    chain.setEqualizer((context) => [
      context.createBiquadFilter(),
      context.createBiquadFilter(),
      context.createBiquadFilter(),
    ])
    expect(path(contexts[0])).toEqual([
      'source',
      'filter',
      'filter',
      'filter',
      'analyser',
      'gain',
      'destination',
    ])
  })

  it('does not build the chain to remove an equalizer that was never set', () => {
    chain.attach(newElement())
    expect(chain.setEqualizer(null)).toBeNull()
    expect(contexts).toHaveLength(0)
  })

  it('creates only one media element source for the element', () => {
    const element = newElement()
    chain.attach(element)
    chain.setReplayGain(0.5)
    chain.getAnalyser()
    chain.setEqualizer((context) => [context.createBiquadFilter()])
    chain.setEqualizer(null)
    chain.attach(element)
    chain.setReplayGain(0.7)

    expect(contexts).toHaveLength(1)
    expect(contexts[0].sources).toHaveLength(1)
  })

  it('resumes a suspended context when built during playback', () => {
    chain.attach(newElement({ paused: false }))
    chain.getAnalyser()
    expect(contexts[0].state).toBe('running')
  })

  it('leaves the context alone when built while paused', () => {
    chain.attach(newElement({ paused: true }))
    chain.getAnalyser()
    expect(contexts[0].resumed).toBe(0)
  })

  it('resumes the context when playback starts', () => {
    chain.attach(newElement())
    chain.setReplayGain(0.5)
    chain.resume()
    expect(contexts[0].state).toBe('running')
    chain.resume()
    expect(contexts[0].resumed).toBe(1)
  })

  it('does not build the chain to resume it', () => {
    chain.attach(newElement())
    chain.resume()
    expect(contexts).toHaveLength(0)
  })

  it('rebuilds what was asked for on a new element', () => {
    chain.attach(newElement())
    chain.setReplayGain(0.5)
    chain.setEqualizer((context) => [context.createBiquadFilter()])

    chain.attach(newElement())

    expect(contexts).toHaveLength(2)
    expect(contexts[0].closed).toBe(true)
    expect(path(contexts[1])).toEqual([
      'source',
      'filter',
      'analyser',
      'gain',
      'destination',
    ])
    expect(nodeOfKind(contexts[1], 'gain').gain.value).toBe(0.5)
  })

  it('tells whether it is built, without building it', () => {
    chain.attach(newElement())
    expect(chain.isBuilt()).toBe(false)
    expect(contexts).toHaveLength(0)

    chain.setReplayGain(0.5)
    expect(chain.isBuilt()).toBe(true)
  })
})
