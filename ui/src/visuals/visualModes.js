// The visuals a Listener can choose in Now Playing, in the order the Visuals
// tool lists them, after "Off". Each is a visual for runVisual: an id, its
// label's translation key, and create(canvas, { analyser, theme }).
//
// `surround` is how the visual sits with the album art: 'over' draws on the
// cover, dimmed (spectrum, waveform); 'around' draws behind it, past its
// edges, by GLOW_SPREAD (glow).
//
// Milkdrop (#97) adds itself here: { id: 'milkdrop', surround: 'over',
// create: async (canvas, options) => (await import('./milkdrop')).create(...) },
// so its code loads only when chosen, and Now Playing, Full screen and the
// warning need nothing more.
import { RENDERERS } from './renderers'

export const VISUAL_MODES = [
  { id: 'spectrum', surround: 'over', ...RENDERERS.spectrum },
  { id: 'waveform', surround: 'over', ...RENDERERS.waveform },
  { id: 'glow', surround: 'around', ...RENDERERS.glow },
]

export const OFF = 'off'

export const visualMode = (id) => VISUAL_MODES.find((mode) => mode.id === id)
