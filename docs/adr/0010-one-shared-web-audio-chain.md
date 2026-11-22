# ReplayGain, the equalizer and visuals share one Web Audio chain

ReplayGain already routes the player's audio through Web Audio: in `ui/src/audioplayer/Player.jsx` it builds AudioContext → MediaElementSource → GainNode, lazily, and only when ReplayGain is on. The equalizer needs filters in the same path, and visuals need an analyser on it. A browser lets `createMediaElementSource` be called only once per audio element, and the player has one element (ADR-0009), so each feature cannot build its own chain. We chose one chain, owned by one module, that every feature plugs into: source → equalizer → analyser for visuals → ReplayGain gain → output. A slot that isn't in use passes audio through unchanged, and the chain is built the first time any feature needs it, at any point during playback.

Once an element is routed through Web Audio, its sound only comes out through the AudioContext. On an iPhone, an installed web app's AudioContext can be suspended when the screen locks, which stops playback. So on an iPhone (and an iPad, which runs the same WebKit) the equalizer and visuals sit behind an "Audio effects" switch in Settings, off by default, with a short warning that playback may stop when the screen is locked. While it is off, they are unavailable and the chain is built only if ReplayGain asks for it, as today. So an iPhone with ReplayGain on already carries the locked-screen risk; the switch keeps the new features from adding it for everyone else. Android and desktop browsers always have them and don't show the switch.

## Consequences

- All Library audio is same-origin, including Downloads, which the service worker answers from Cache Storage, so it can be read by the analyser and filtered without CORS issues. Internet Radio streams come from other hosts. Building the chain sets `crossOrigin='anonymous'` on the one element, whatever it plays (it was added for radio in Firefox), so a radio stream plays through the chain only when its host allows CORS.
- ReplayGain keeps its track and album modes and behaves exactly as before.
- The switch is stored per device, like the other new player settings.
- To be checked on a real iPhone in the installed app (#94): whether playback continues on a locked screen with the chain active. If it does, the switch can be relaxed and this ADR revised.
