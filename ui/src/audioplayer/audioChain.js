// The player's one Web Audio chain (ADR-0010):
//
//   element → source → equalizer slot → analyser → ReplayGain gain → output
//
// A browser lets an audio element be routed through Web Audio only once, so
// ReplayGain, the equalizer and visuals all share this chain instead of each
// building their own. It is built the first time a feature asks for it, at any
// point during playback, and never before: once built, the element's sound only
// comes out through the AudioContext. A slot no feature is using passes audio
// through unchanged (no filters, unity gain; an analyser never alters audio).
//
// Whether the equalizer and visuals may ask at all is decided by
// useAudioEffectsAvailable, not here.
export const createAudioChain = (options = {}) => {
  const getAudioContextClass = () =>
    'AudioContext' in options ? options.AudioContext : window.AudioContext

  let element = null
  let graph = null // { context, source, analyser, gain, filters }

  // What features have asked for, kept so the chain can be built late (or
  // rebuilt for a new element) with everything already in place.
  let replayGain = null
  let equalizer = null
  let analyserWanted = false

  // Told when the chain is built or torn down, so a feature holding nodes
  // from it (visuals' analyser) can ask again
  const listeners = new Set()
  const changed = () => listeners.forEach((listener) => listener())

  const wanted = () =>
    replayGain !== null || equalizer !== null || analyserWanted

  const applyReplayGain = () => {
    const { gain, context } = graph
    gain.gain.setValueAtTime(replayGain ?? 1, context.currentTime)
  }

  const connectEqualizer = () => {
    const { context, source, analyser } = graph
    graph.filters.forEach((filter) => filter.disconnect())
    source.disconnect()
    graph.filters = equalizer ? equalizer(context) : []
    const last = graph.filters.reduce(
      (previous, filter) => previous.connect(filter),
      source,
    )
    last.connect(analyser)
  }

  const build = () => {
    if (graph) return true
    const AudioContextClass = getAudioContextClass()
    if (!element || !AudioContextClass) return false

    const context = new AudioContextClass()
    // We need this to support radios in Firefox. A radio stream already
    // playing from a host without CORS stays silent until it is reloaded.
    element.crossOrigin = 'anonymous'
    const source = context.createMediaElementSource(element)
    const analyser = context.createAnalyser()
    const gain = context.createGain()
    analyser.connect(gain)
    gain.connect(context.destination)
    graph = { context, source, analyser, gain, filters: [] }

    connectEqualizer()
    applyReplayGain()
    // Built mid-playback: the element now plays only through the context.
    if (!element.paused) resume()
    changed()
    return true
  }

  const resume = () => {
    if (graph && graph.context.state !== 'running') {
      // Outside a user gesture the browser may refuse; the next play retries.
      graph.context.resume().catch(() => {})
    }
  }

  return {
    // Hands the chain the player's audio element. Builds nothing unless a
    // feature has already asked for the chain. A different element (the
    // player remounted) gets a fresh chain with what was asked for: features
    // holding filters or an analyser from the old one must ask again.
    attach(newElement) {
      if (newElement === element) return
      const wasBuilt = graph !== null
      if (graph) {
        graph.context.close()
        graph = null
      }
      element = newElement
      if (wasBuilt) changed()
      if (wanted()) build()
    },

    // ReplayGain's linear gain for the current Track, or null when ReplayGain
    // is off. A number builds the chain; null leaves the slot at unity gain.
    setReplayGain(value) {
      replayGain = value
      if (graph) applyReplayGain()
      else if (replayGain !== null) build()
    },

    // Puts a filter bank into the equalizer slot. `createFilters(context)`
    // returns the AudioNodes to chain in series; it is called once per build,
    // so the caller keeps the returned nodes to adjust them live. null empties
    // the slot. Returns the nodes in the slot, or null if there is no chain.
    setEqualizer(createFilters) {
      equalizer = createFilters
      if (graph) connectEqualizer()
      else if (equalizer) build()
      return graph && equalizer ? graph.filters : null
    },

    // The AnalyserNode visuals read from, building the chain if needed. null
    // before the player has an element, or without Web Audio.
    getAnalyser() {
      analyserWanted = true
      return build() ? graph.analyser : null
    },

    // Visuals no longer need the analyser (turned off, unavailable, or
    // waiting on Internet Radio). A chain already built stays, but a new
    // element (a player remount) isn't routed through Web Audio for them.
    releaseAnalyser() {
      analyserWanted = false
    },

    // Call when playback starts: a context may start suspended, or be
    // suspended by the browser. Does nothing until the chain is built.
    resume,

    // Whether the chain is built. A feature that shouldn't be the one to
    // build it (the equalizer on Internet Radio) checks this first.
    isBuilt: () => graph !== null,

    // Calls listener whenever the chain is built or torn down (the player
    // remounted with a new element). Returns a function that stops it.
    onChange(listener) {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
  }
}

export const audioChain = createAudioChain()
