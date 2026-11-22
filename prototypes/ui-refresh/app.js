// PROTOTYPE, THROWAWAY (issue #88). Fake data, fake playback, no audio.
// Everything lives in one in-memory `state`; the URL query mirrors it so a
// particular combination can be shared (e.g. ?theme=light&accent=art&frame=desktop).
;(function () {
  'use strict'

  // ---------- Icons (Material Icons paths, as used by MUI v4) ----------
  const P = {
    play: 'M8 5v14l11-7z',
    pause: 'M6 19h4V5H6v14zm8-14v14h4V5h-4z',
    next: 'M6 18l8.5-6L6 6v12zM16 6v12h2V6h-2z',
    prev: 'M6 6h2v12H6zm3.5 6l8.5 6V6z',
    shuffle:
      'M10.59 9.17L5.41 4 4 5.41l5.17 5.17 1.42-1.41zM14.5 4l2.04 2.04L4 18.59 5.41 20 17.96 7.46 20 9.5V4h-5.5zm.33 9.41l-1.41 1.41 3.13 3.13L14.5 20H20v-5.5l-2.04 2.04-3.13-3.13z',
    repeat: 'M7 7h10v3l4-4-4-4v3H5v6h2V7zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2v4z',
    heart:
      'M12 21.35l-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z',
    heartOutline:
      'M16.5 3c-1.74 0-3.41.81-4.5 2.09C10.91 3.81 9.24 3 7.5 3 4.42 3 2 5.42 2 8.5c0 3.78 3.4 6.86 8.55 11.54L12 21.35l1.45-1.32C18.6 15.36 22 12.28 22 8.5 22 5.42 19.58 3 16.5 3zm-4.4 15.55l-.1.1-.1-.1C7.14 14.24 4 11.39 4 8.5 4 6.5 5.5 5 7.5 5c1.54 0 3.04.99 3.57 2.36h1.87C13.46 5.99 14.96 5 16.5 5c2 0 3.5 1.5 3.5 3.5 0 2.89-3.14 5.74-7.9 10.05z',
    star: 'M12 17.27L18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2 9.19 8.63 2 9.24l5.46 4.73L5.82 21z',
    down: 'M16.59 8.59L12 13.17 7.41 8.59 6 10l6 6 6-6z',
    close:
      'M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z',
    more: 'M12 8c1.1 0 2-.9 2-2s-.9-2-2-2-2 .9-2 2 .9 2 2 2zm0 2c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2zm0 6c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2z',
    moreH:
      'M6 10c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2zm12 0c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2zm-6 0c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2z',
    lyrics: 'M14 17H4v2h10v-2zm6-8H4v2h16V9zM4 15h16v-2H4v2zM4 5v2h16V5H4z',
    queue:
      'M15 6H3v2h12V6zm0 4H3v2h12v-2zM3 16h8v-2H3v2zM17 6v8.18c-.31-.11-.65-.18-1-.18-1.66 0-3 1.34-3 3s1.34 3 3 3 3-1.34 3-3V8h3V6h-5z',
    eq: 'M10 20h4V4h-4v16zm-6 0h4v-8H4v8zM16 9v11h4V9h-4z',
    visuals:
      'M7 18h2V6H7v12zm4 4h2V2h-2v20zm-8-8h2v-4H3v4zm12 4h2V6h-2v12zm4-8v4h2v-4h-2z',
    sleep:
      'M15 1H9v2h6V1zm-4 13h2V8h-2v6zm8.03-6.61l1.42-1.42c-.43-.51-.9-.99-1.41-1.41l-1.42 1.42C16.07 4.74 14.12 4 12 4c-4.97 0-9 4.03-9 9s4.02 9 9 9 9-4.03 9-9c0-2.12-.74-4.07-1.97-5.61zM12 20c-3.87 0-7-3.13-7-7s3.13-7 7-7 7 3.13 7 7-3.13 7-7 7z',
    speed:
      'M20.38 8.57l-1.23 1.85a8 8 0 0 1-.22 7.58H5.07A8 8 0 0 1 15.58 6.85l1.85-1.23A10 10 0 0 0 3.35 19a2 2 0 0 0 1.72 1h13.85a2 2 0 0 0 1.74-1 10 10 0 0 0-.27-10.44zm-9.79 6.84a2 2 0 0 0 2.83 0l5.66-8.49-8.49 5.66a2 2 0 0 0 0 2.83z',
    library:
      'M20 2H8c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm-2 5h-3v5.5c0 1.38-1.12 2.5-2.5 2.5S10 13.88 10 12.5s1.12-2.5 2.5-2.5c.57 0 1.08.19 1.5.51V5h4v2zM4 6H2v14c0 1.1.9 2 2 2h14v-2H4V6z',
    radio:
      'M3.24 6.15C2.51 6.43 2 7.17 2 8v12c0 1.1.89 2 2 2h16c1.11 0 2-.9 2-2V8c0-1.11-.89-2-2-2H8.3l8.26-3.34L15.88 1 3.24 6.15zM7 20c-1.66 0-3-1.34-3-3s1.34-3 3-3 3 1.34 3 3-1.34 3-3 3zm13-8h-2v-2h-2v2H4V8h16v4z',
    download: 'M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z',
    search:
      'M15.5 14h-.79l-.28-.27A6.471 6.471 0 0 0 16 9.5 6.5 6.5 0 1 0 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0C7.01 14 5 11.99 5 9.5S7.01 5 9.5 5 14 7.01 14 9.5 11.99 14 9.5 14z',
    drag: 'M20 9H4v2h16V9zM4 15h16v-2H4v2z',
    album:
      'M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 14.5c-2.49 0-4.5-2.01-4.5-4.5S9.51 7.5 12 7.5s4.5 2.01 4.5 4.5-2.01 4.5-4.5 4.5zm0-5.5c-.55 0-1 .45-1 1s.45 1 1 1 1-.45 1-1-.45-1-1-1z',
    person:
      'M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z',
    note: 'M12 3v10.55c-.59-.34-1.27-.55-2-.55-2.21 0-4 1.79-4 4s1.79 4 4 4 4-1.79 4-4V7h4V3h-6z',
    auto: 'M7.5 5.6L10 7 8.6 4.5 10 2 7.5 3.4 5 2l1.4 2.5L5 7zm12 9.8L17 14l1.4 2.5L17 19l2.5-1.4L22 19l-1.4-2.5L22 14zM22 2l-2.5 1.4L17 2l1.4 2.5L17 7l2.5-1.4L22 7l-1.4-2.5zm-7.63 5.29a.996.996 0 0 0-1.41 0L1.29 18.96a.996.996 0 0 0 0 1.41l2.34 2.34c.39.39 1.02.39 1.41 0L16.7 11.05a.996.996 0 0 0 0-1.41l-2.33-2.35zm-1.03 5.49l-2.12-2.12 2.44-2.44 2.12 2.12-2.44 2.44z',
    inbox:
      'M19 3H4.99c-1.11 0-1.98.89-1.98 2L3 19c0 1.1.88 2 1.99 2H19c1.1 0 2-.9 2-2V5c0-1.11-.9-2-2-2zm0 12h-4c0 1.66-1.35 3-3 3s-3-1.34-3-3H4.99V5H19v10z',
    volume:
      'M3 9v6h4l5 5V4L7 9H3zm13.5 3c0-1.77-1.02-3.29-2.5-4.03v8.05c1.48-.73 2.5-2.25 2.5-4.02zM14 3.23v2.06c2.89.86 5 3.54 5 6.71s-2.11 5.85-5 6.71v2.06c4.01-.91 7-4.49 7-8.77s-2.99-7.86-7-8.77z',
    openPanel:
      'M21 11V3h-8l3.29 3.29-10 10L3 13v8h8l-3.29-3.29 10-10z',
    closePanel:
      'M22 3.41L16.71 8.7 20 12h-8V4l3.29 3.29L20.59 2 22 3.41zM3.41 22l5.29-5.29L12 20v-8H4l3.29 3.29L2 20.59 3.41 22z',
  }
  const icon = (n) =>
    `<svg class="i" viewBox="0 0 24 24" aria-hidden="true"><path d="${P[n]}"/></svg>`

  // ---------- Palette ----------
  // Fixed accents: [dark-variant, light-variant]. Each value passes WCAG AA
  // (4.5:1) against its variant's background, and against its on-accent text.
  const ACCENTS = [
    { id: 'vermilion', name: 'Vermilion', dark: '#FF5B2E', light: '#B0300F' },
    { id: 'amber', name: 'Amber', dark: '#FF9A1F', light: '#934E00' },
    { id: 'mustard', name: 'Mustard', dark: '#E2B53A', light: '#765A00' },
    { id: 'olive', name: 'Olive', dark: '#9DBB4E', light: '#4A6510' },
    { id: 'teal', name: 'Teal', dark: '#3EC1B0', light: '#086A5F' },
    { id: 'denim', name: 'Denim', dark: '#6FA3F0', light: '#2F5DB0' },
    { id: 'plum', name: 'Plum', dark: '#C08BF0', light: '#7A3FB0' },
    { id: 'rose', name: 'Rose', dark: '#FF6F91', light: '#B42851' },
  ]
  // Dark bases. Slate is the chosen dark palette; the others stay for comparison.
  const DARK_BASES = {
    slate: { name: 'Slate', bg: '#0F1216', s2: '#1F242B' },
    warm: { name: 'Warm', bg: '#14110F', s2: '#28221E' },
    graphite: { name: 'Graphite', bg: '#121212', s2: '#242424' },
    ink: { name: 'Ink', bg: '#000000', s2: '#1A1A1A' },
  }
  const LIGHT = { bg: '#F3EDE4', s2: '#E9E1D5' }
  const BG = new Proxy({}, { get: (_, t) => (t === 'dark' ? DARK_BASES[state.base].bg : LIGHT.bg) })
  const SURFACE2 = new Proxy({}, { get: (_, t) => (t === 'dark' ? DARK_BASES[state.base].s2 : LIGHT.s2) })

  // ---------- Sample data (all fictional) ----------
  const ALBUMS = [
    { title: 'Small Hours', artist: 'The Lantern Pilots', style: 'sun', c: ['#0E3B47', '#1F6F78', '#F2B33D', '#E8577A'], ink: '#F6E7C8' },
    { title: 'Copper Static', artist: 'Moth Radio Collective', style: 'bands', c: ['#2B1E3F', '#C84B31', '#ECDBBA', '#E09F3E'], ink: '#ECDBBA' },
    { title: 'Marigold Sleep', artist: 'Juniper Okafor', style: 'dot', c: ['#3B5249', '#F4A259', '#F4E285'], ink: '#F4E285' },
    { title: 'Tin Planet', artist: 'Velour Ghosts', style: 'grid', c: ['#1F3A93', '#F5D547', '#E94F37', '#F1EDE4'], ink: '#1F3A93' },
    { title: 'Night Ferry', artist: 'Mabel & the Lowlights', style: 'waves', c: ['#0B1D3A', '#5DA9E9', '#C9E4F5'], ink: '#C9E4F5' },
    { title: 'Okra Season', artist: 'Dust Choir', style: 'type', c: ['#6B8F3A', '#F1E9DA', '#2E3B1F'], ink: '#F1E9DA' },
    { title: 'Halcyon Moth', artist: 'Paper Satellites', style: 'sun', c: ['#2A1A3F', '#7B3F8C', '#F7C59F', '#FF6F59'], ink: '#F7C59F' },
    { title: 'Basement Weather', artist: 'The Quiet Engines', style: 'grid', c: ['#1B1B1B', '#D7263D', '#F4D35E', '#E9E3D5'], ink: '#1B1B1B' },
    { title: 'Slow Orbit', artist: 'Orbit Garden', style: 'dot', c: ['#E7DCC8', '#C0392B', '#1B4965'], ink: '#1B4965' },
    { title: 'Corduroy Heart', artist: 'Penny Vale', style: 'waves', c: ['#5C2A1E', '#E08E45', '#F8E5C8'], ink: '#F8E5C8' },
    { title: 'Room Tone', artist: 'Static Fern', style: 'type', c: ['#14213D', '#FCA311', '#E5E5E5'], ink: '#E5E5E5' },
    { title: 'Allotment Neon', artist: 'Kestrel Arcade', style: 'bands', c: ['#0D3B2E', '#2EC4B6', '#FF9F1C', '#CBF3F0'], ink: '#CBF3F0' },
  ]

  const SYNCED = [
    [0, 'Headlights low on the ring road'],
    [12, 'counting the cats-eyes home'],
    [24, 'you had the window down'],
    [33, 'singing to the dial tone'],
    [46, 'Lamplight freeway'],
    [53, 'nobody else awake'],
    [61, 'just the hum of the engine'],
    [69, 'and the long way round we take'],
    [84, 'Service station coffee'],
    [95, 'tastes like a rainy June'],
    [107, 'the radio found a station'],
    [116, 'playing our old tune'],
    [130, 'Lamplight freeway'],
    [138, 'nobody else awake'],
    [146, 'just the hum of the engine'],
    [154, 'and the long way round we take'],
    [176, '(instrumental)'],
    [204, 'Pull in by the gasworks'],
    [214, 'sun coming up like a rumour'],
    [226, 'lamplight freeway'],
    [236, 'take the long way, take the long way'],
  ]
  const UNSYNCED = [
    'Copper wire in the attic',
    'humming a song it heard in 1979',
    'nobody wired it in',
    'nobody turned it on',
    '',
    'Static, static',
    'singing on the line',
    'static, static',
    'somebody else’s Saturday night',
    '',
    'We left the window open',
    'so the weather could come in',
    'and every time it thunders',
    'the radio starts again',
  ]

  const ALL_TRACKS = [
    { title: 'Lamplight Freeway', album: 0, dur: 252, lyrics: 'synced' },
    { title: 'Copper Static', album: 1, dur: 218, lyrics: 'unsynced' },
    { title: 'Marigold Sleep', album: 2, dur: 301 },
    { title: 'Satellite Kitchen', album: 3, dur: 197 },
    { title: 'Ferry Lights at Four', album: 4, dur: 265 },
    { title: 'Long Grass', album: 5, dur: 183 },
    { title: 'Paper Moon Motel', album: 6, dur: 240 },
    { title: 'Damp Basement Blues', album: 7, dur: 229 },
  ]

  const DJ = { name: 'Night Owl', initials: 'NO' }
  const STATION = 'After Hours Crate' // a Station shaped by the Night Owl DJ
  const BLURBS = [
    'Past midnight, so we’re easing off the gas. The Lantern Pilots cut this in a church hall with the heating off; listen for the breath before the last chorus.',
    'Something with a bit of rust on it now. Moth Radio Collective recorded straight to tape in a garage that floods every spring.',
    'You skipped the loud ones twice tonight, so here’s Juniper Okafor keeping it hushed.',
    'Velour Ghosts: three chords, one borrowed organ and a drummer who refused to use cymbals.',
    'For anyone still up, a song about the last boat home. Mabel & the Lowlights, from the Night Ferry LP.',
    'Dust Choir next. Recorded in one take, in a field, at dawn. The birds weren’t invited.',
    'Paper Satellites, because it’s that hour where everything sounds like a motel sign.',
    'Ending this run somewhere damp and low. The Quiet Engines, from the basement up.',
  ]

  // ---------- State ----------
  const q = new URLSearchParams(location.search)
  const state = {
    theme: q.get('theme') === 'light' ? 'light' : 'dark',
    base: Object.prototype.hasOwnProperty.call(DARK_BASES, q.get('base')) ? q.get('base') : 'slate',
    accent: ACCENTS.some((a) => a.id === q.get('accent')) || q.get('accent') === 'art' ? q.get('accent') : 'vermilion',
    frame: q.get('frame') === 'desktop' ? 'desktop' : 'phone',
    station: q.get('station') === '1',
    npOpen: q.get('np') !== '0',
    tab: ['art', 'lyrics', 'queue'].includes(q.get('tab')) ? q.get('tab') : 'art',
    queue: ALL_TRACKS.map((t, i) => i),
    cur: 0,
    pos: 37,
    playing: q.get('playing') !== '0',
    starred: false,
    rating: 4,
    shuffle: false,
    repeat: false,
    visuals: false,
    eqOpen: false,
    eqPreset: 'Flat',
    sleep: 0,
    speed: 0,
  }
  const SLEEP = ['Off', '15m', '30m', '1h', 'End']
  const SPEED = [1, 1.25, 1.5, 0.75]

  const track = () => ALL_TRACKS[state.queue[state.cur]]
  const album = (t = track()) => ALBUMS[t.album]
  const $ = (s, el = document) => el.querySelector(s)
  const $$ = (s, el = document) => Array.from(el.querySelectorAll(s))
  const esc = (s) =>
    String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c])
  const fmt = (s) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, '0')}`

  // ---------- Sample covers, drawn on canvas ----------
  // Drawn locally so the canvas is never tainted and the "From album art"
  // extraction can read the pixels, even when opened from file://.
  function drawCover(a) {
    const S = 300
    const cv = document.createElement('canvas')
    cv.width = cv.height = S
    const g = cv.getContext('2d')
    const c = a.c
    g.fillStyle = c[0]
    g.fillRect(0, 0, S, S)
    if (a.style === 'sun') {
      const lg = g.createLinearGradient(0, 0, 0, S)
      lg.addColorStop(0, c[0])
      lg.addColorStop(1, c[1])
      g.fillStyle = lg
      g.fillRect(0, 0, S, S)
      g.fillStyle = c[2]
      g.beginPath()
      g.arc(150, 150, 82, 0, Math.PI * 2)
      g.fill()
      g.fillStyle = c[1]
      for (let i = 0; i < 6; i++) g.fillRect(60, 158 + i * 13, 180, 2 + i * 1.6)
      g.fillStyle = c[3]
      g.beginPath()
      g.moveTo(0, S)
      g.lineTo(0, 232)
      g.lineTo(90, 196)
      g.lineTo(170, 244)
      g.lineTo(240, 208)
      g.lineTo(S, 236)
      g.lineTo(S, S)
      g.fill()
    } else if (a.style === 'bands') {
      g.save()
      g.translate(150, 150)
      g.rotate(-Math.PI / 4)
      const seq = [c[0], c[1], c[0], c[2], c[0], c[3]]
      for (let i = -12; i < 12; i++) {
        g.fillStyle = seq[(i + 24) % seq.length]
        g.fillRect(i * 30, -260, 30, 520)
      }
      g.restore()
    } else if (a.style === 'dot') {
      g.fillStyle = c[1]
      g.beginPath()
      g.arc(182, 128, 96, 0, Math.PI * 2)
      g.fill()
      g.fillStyle = c[2]
      g.beginPath()
      g.arc(92, 214, 34, 0, Math.PI * 2)
      g.fill()
    } else if (a.style === 'grid') {
      g.fillStyle = c[3]
      g.fillRect(0, 0, 150, 150)
      g.fillStyle = c[1]
      g.beginPath()
      g.moveTo(0, 150)
      g.arc(0, 150, 150, -Math.PI / 2, 0)
      g.fill()
      g.fillStyle = c[0]
      g.fillRect(150, 0, 150, 150)
      g.fillStyle = c[3]
      g.beginPath()
      g.arc(225, 75, 48, 0, Math.PI * 2)
      g.fill()
      g.fillStyle = c[2]
      g.fillRect(0, 150, 150, 150)
      g.fillStyle = c[3]
      g.beginPath()
      g.moveTo(20, 280)
      g.lineTo(75, 170)
      g.lineTo(130, 280)
      g.fill()
      g.fillStyle = c[1]
      g.fillRect(150, 150, 150, 150)
      g.fillStyle = c[0]
      g.beginPath()
      g.moveTo(300, 300)
      g.arc(300, 300, 150, Math.PI, Math.PI * 1.5)
      g.fill()
    } else if (a.style === 'waves') {
      g.lineWidth = 3
      for (let k = 0; k < 11; k++) {
        g.strokeStyle = k % 3 === 2 ? c[2] : c[1]
        g.globalAlpha = 0.35 + (k / 11) * 0.65
        g.beginPath()
        for (let x = 0; x <= S; x += 4) {
          const y = 40 + k * 20 + 12 * Math.sin(x / 26 + k * 0.6)
          x ? g.lineTo(x, y) : g.moveTo(x, y)
        }
        g.stroke()
      }
      g.globalAlpha = 1
    } else if (a.style === 'type') {
      g.fillStyle = c[1]
      g.font = '900 210px system-ui, sans-serif'
      g.fillText(a.title[0], 14, 214)
      g.fillStyle = c[2]
      g.fillRect(0, 236, S, 8)
    }
    // Sleeve text, outlined in the opposite tone so it reads over any part of the art
    const lightInk = parseInt(a.ink.slice(1, 3), 16) > 128
    g.strokeStyle = lightInk ? 'rgba(0, 0, 0, 0.55)' : 'rgba(255, 255, 255, 0.6)'
    g.lineWidth = 3
    g.lineJoin = 'round'
    g.fillStyle = a.ink
    // shrink long titles so they fit the sleeve
    const fit = (text, weight, px, y) => {
      do g.font = `${weight} ${px--}px system-ui, sans-serif`
      while (g.measureText(text).width > S - 32 && px > 8)
      g.strokeText(text, 16, y)
      g.fillText(text, 16, y)
    }
    fit(a.title.toUpperCase(), 800, 17, S - 34)
    fit(a.artist, 600, 13, S - 15)
    return { canvas: cv, url: cv.toDataURL('image/png') }
  }
  ALBUMS.forEach((a) => Object.assign(a, drawCover(a)))

  // ---------- Colour maths ----------
  const hexToRgb = (h) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16))
  const rgbToHex = (r) =>
    '#' + r.map((v) => Math.round(v).toString(16).padStart(2, '0')).join('').toUpperCase()
  function lum(rgb) {
    const [r, g, b] = rgb.map((v) => {
      v /= 255
      return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4
    })
    return 0.2126 * r + 0.7152 * g + 0.0722 * b
  }
  function contrast(a, b) {
    const x = lum(typeof a === 'string' ? hexToRgb(a) : a)
    const y = lum(typeof b === 'string' ? hexToRgb(b) : b)
    return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05)
  }
  function rgbToHsl([r, g, b]) {
    r /= 255
    g /= 255
    b /= 255
    const mx = Math.max(r, g, b)
    const mn = Math.min(r, g, b)
    const l = (mx + mn) / 2
    if (mx === mn) return [0, 0, l]
    const d = mx - mn
    const s = l > 0.5 ? d / (2 - mx - mn) : d / (mx + mn)
    let h
    if (mx === r) h = (g - b) / d + (g < b ? 6 : 0)
    else if (mx === g) h = (b - r) / d + 2
    else h = (r - g) / d + 4
    return [h / 6, s, l]
  }
  function hslToRgb([h, s, l]) {
    if (!s) return [l * 255, l * 255, l * 255]
    const f = (p, q, t) => {
      if (t < 0) t += 1
      if (t > 1) t -= 1
      if (t < 1 / 6) return p + (q - p) * 6 * t
      if (t < 1 / 2) return q
      if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6
      return p
    }
    const q2 = l < 0.5 ? l * (1 + s) : l + s - l * s
    const p = 2 * l - q2
    return [f(p, q2, h + 1 / 3), f(p, q2, h), f(p, q2, h - 1 / 3)].map((v) => v * 255)
  }

  // The most vivid colour that covers a fair part of the cover:
  // pixels are bucketed by hue, weighted by saturation and mid-lightness.
  const artCache = new Map()
  function extractColour(a) {
    if (artCache.has(a)) return artCache.get(a)
    const N = 32
    const cv = document.createElement('canvas')
    cv.width = cv.height = N
    const g = cv.getContext('2d')
    g.drawImage(a.canvas, 0, 0, N, N)
    const px = g.getImageData(0, 0, N, N).data
    const bins = Array.from({ length: 12 }, () => ({ w: 0, r: 0, g: 0, b: 0 }))
    for (let i = 0; i < px.length; i += 4) {
      const rgb = [px[i], px[i + 1], px[i + 2]]
      const [h, s, l] = rgbToHsl(rgb)
      const w = s * s * Math.max(0, 1 - Math.abs(l - 0.5) * 1.8)
      if (w < 0.05) continue
      const bin = bins[Math.floor(h * 12) % 12]
      bin.w += w
      bin.r += rgb[0] * w
      bin.g += rgb[1] * w
      bin.b += rgb[2] * w
    }
    const best = bins.reduce((m, b) => (b.w > m.w ? b : m))
    const out = best.w > 2 ? rgbToHex([best.r / best.w, best.g / best.w, best.b / best.w]) : null
    artCache.set(a, out)
    return out
  }

  // Keep the hue, nudge lightness until it reads at AA (4.5:1) on both the
  // background and the raised surfaces, then pick black-ish or white text for it.
  function readableAccent(hex, theme) {
    let [h, s, l] = rgbToHsl(hexToRgb(hex))
    s = Math.max(s, 0.45)
    const step = theme === 'dark' ? 0.01 : -0.01
    let rgb = hslToRgb([h, s, l])
    for (let i = 0; i < 100; i++) {
      if (contrast(rgb, BG[theme]) >= 4.5 && contrast(rgb, SURFACE2[theme]) >= 4.5) break
      l = Math.min(1, Math.max(0, l + step))
      rgb = hslToRgb([h, s, l])
    }
    return rgbToHex(rgb)
  }
  const onAccent = (hex) => (contrast(hex, '#0F1216') >= contrast(hex, '#FFFFFF') ? '#0F1216' : '#FFFFFF')
  const alpha = (hex, a) => `rgba(${hexToRgb(hex).join(', ')}, ${a})`

  let accentInfo = {}
  function applyAccent() {
    const root = document.documentElement
    root.dataset.theme = state.theme
    root.dataset.base = state.base
    let hex
    let raw = null
    let fallback = false
    if (state.accent === 'art') {
      raw = extractColour(album())
      if (raw) hex = readableAccent(raw, state.theme)
      else {
        fallback = true
        hex = ACCENTS[0][state.theme]
      }
    } else {
      const a = ACCENTS.find((x) => x.id === state.accent) || ACCENTS[0]
      hex = a[state.theme]
    }
    const on = onAccent(hex)
    root.style.setProperty('--vu-accent', hex)
    root.style.setProperty('--vu-on-accent', on)
    root.style.setProperty('--vu-accent-soft', alpha(hex, state.theme === 'dark' ? 0.16 : 0.14))
    root.style.setProperty('--vu-accent-glow', alpha(hex, state.theme === 'dark' ? 0.3 : 0.22))
    $('meta[name=theme-color]').content = BG[state.theme]
    accentInfo = { hex, on, raw, fallback }
  }

  // ---------- Rendering ----------
  function albumCard(a) {
    return `<div class="album"><img src="${a.url}" alt="Cover of ${esc(a.title)}"><div class="t">${esc(a.title)}</div><div class="a">${esc(a.artist)}</div></div>`
  }
  function renderStatic() {
    const grid = ALBUMS.concat(ALBUMS.slice(0, 6)).map(albumCard).join('')
    $('#phone-grid').innerHTML = grid
    $('#desk-grid').innerHTML = ALBUMS.concat(ALBUMS).map(albumCard).join('')
    $$('[data-icon]').forEach((el) => (el.innerHTML = icon(el.dataset.icon)))
  }

  function renderTabbar() {
    const tabs = [
      ['Library', 'library'],
      ['Playlists', 'queue'],
      ['Stations', 'radio'],
      ['Downloads', 'download'],
      ['More', 'moreH'],
    ]
    const active = state.station ? 'Stations' : 'Library'
    $('#tabbar').innerHTML = tabs
      .map(
        ([n, i]) =>
          `<button class="tab ${n === active ? 'active' : ''}" data-nav="${n}" ${n === active ? 'aria-current="page"' : ''}><span class="pill">${icon(i)}</span>${n}</button>`,
      )
      .join('')
  }

  function renderSide() {
    const item = (n, i, act) =>
      `<button class="side-item ${act ? 'active' : ''}" data-nav="${n}">${icon(i)}${n}</button>`
    $('#desk-side').innerHTML =
      `<div class="overline">Library</div>` +
      item('Albums', 'album', true) +
      item('Artists', 'person') +
      item('Tracks', 'note') +
      `<div class="overline">Listen</div>` +
      item('Playlists', 'queue') +
      item('Stations', 'radio') +
      item('Generated Playlists', 'auto') +
      item('Downloads', 'download') +
      `<div class="overline">Admin</div>` +
      item('Inbox', 'inbox') +
      item('Rips', 'album')
  }

  function playIcon() {
    return icon(state.playing ? 'pause' : 'play')
  }
  function subline(t) {
    const a = album(t)
    return (state.station ? `<span class="dj-hint">${DJ.name}</span> · ` : '') + esc(a.artist)
  }

  function renderMini() {
    const t = track()
    $('#phone-mini').innerHTML = `
      <div class="mini" data-act="open" role="button" tabindex="0" aria-label="Open Now Playing">
        <img src="${album().url}" alt="">
        <div style="min-width:0"><div class="t">${esc(t.title)}</div><div class="a">${subline(t)}</div></div>
        <button class="icon-btn" data-act="toggle" aria-label="${state.playing ? 'Pause' : 'Play'}">${playIcon()}</button>
        <button class="icon-btn" data-act="next" aria-label="Next">${icon('next')}</button>
        <div class="mini-progress"><span></span></div>
      </div>`
  }

  function renderBar() {
    const t = track()
    $('#desk-bar').innerHTML = `
      <div class="bar-now">
        <button class="bar-cover" data-act="panel" aria-label="${state.npOpen ? 'Close' : 'Open'} Now Playing"><img src="${album().url}" alt=""><span class="exp">${icon(state.npOpen ? 'closePanel' : 'openPanel')}</span></button>
        <div style="min-width:0"><div class="t">${esc(t.title)}</div><div class="a">${subline(t)}</div></div>
      </div>
      <div class="bar-mid">
        <div class="bar-ctl">
          <button class="icon-btn small ${state.shuffle ? 'on' : ''}" data-act="shuffle" aria-label="Shuffle" aria-pressed="${state.shuffle}">${icon('shuffle')}</button>
          <button class="icon-btn" data-act="prev" aria-label="Previous">${icon('prev')}</button>
          <button class="play" data-act="toggle" aria-label="${state.playing ? 'Pause' : 'Play'}">${playIcon()}</button>
          <button class="icon-btn" data-act="next" aria-label="Next">${icon('next')}</button>
          <button class="icon-btn small ${state.repeat ? 'on' : ''}" data-act="repeat" aria-label="Repeat" aria-pressed="${state.repeat}">${icon('repeat')}</button>
        </div>
        <div class="desk-seek"><span class="el"></span><input class="seekbar" type="range" min="0" max="${t.dur}" step="1" aria-label="Seek"><span class="rem"></span></div>
      </div>
      <div class="bar-right">
        <button class="icon-btn ${state.starred ? 'on' : ''}" data-act="star" aria-label="Star" aria-pressed="${state.starred}">${icon(state.starred ? 'heart' : 'heartOutline')}</button>
        <button class="icon-btn" data-act="tab-queue" aria-label="Queue">${icon('queue')}</button>
        <span class="icon-btn" aria-hidden="true">${icon('volume')}</span>
        <span class="desk-seek vol" style="display:block"><input type="range" min="0" max="100" value="70" style="--p:70%" aria-label="Volume" data-vol></span>
        <button class="icon-btn ${state.npOpen ? 'on' : ''}" data-act="panel" aria-label="${state.npOpen ? 'Close' : 'Open'} Now Playing" aria-pressed="${state.npOpen}">${icon(state.npOpen ? 'closePanel' : 'openPanel')}</button>
      </div>`
  }

  function stageHTML() {
    const t = track()
    const a = album()
    if (state.tab === 'lyrics') {
      if (t.lyrics === 'synced')
        return `<div class="lyrics" id="lyrics">${SYNCED.map(([s, l]) => `<p data-seek="${s}">${esc(l)}</p>`).join('')}</div>`
      if (t.lyrics === 'unsynced')
        return `<div class="lyrics unsynced"><div class="note">Lyrics aren’t synced for this track</div>${UNSYNCED.map((l) => (l ? `<p>${esc(l)}</p>` : '<p>&nbsp;</p>')).join('')}</div>`
      return `<div class="empty">No lyrics for this track</div>`
    }
    if (state.tab === 'queue') {
      const rows = state.queue
        .map((ti, i) => {
          const qt = ALL_TRACKS[ti]
          const qa = ALBUMS[qt.album]
          const cur = i === state.cur
          return `<div class="qrow ${cur ? 'current' : ''}" data-q="${i}" ${cur ? 'aria-current="true"' : ''}>
            <span class="grip" aria-label="Drag to reorder">${icon('drag')}</span>
            <img src="${qa.url}" alt="">
            <div style="min-width:0"><div class="t">${esc(qt.title)}</div><div class="a">${esc(qa.artist)}</div></div>
            <span class="d">${fmt(qt.dur)}</span>
            <button class="rm" data-rm="${i}" aria-label="Remove ${esc(qt.title)} from queue">${icon('close')}</button>
          </div>`
        })
        .join('')
      return `<div class="queue"><div class="queue-head"><span class="overline">${state.station ? `Up next, picked by ${DJ.name}` : 'Up next'}</span>${state.station ? '' : '<button data-act="clear">Clear</button>'}</div>${rows}</div>`
    }
    const bars = state.visuals
      ? `<div class="visuals">${[0.1, 0.5, 0.2, 0.7, 0.35, 0.05, 0.6, 0.3]
          .map((d) => `<span style="animation-delay:-${d}s"></span>`)
          .join('')}</div>`
      : ''
    return `<div class="art"><div class="record" aria-hidden="true"></div><img src="${a.url}" alt="Cover of ${esc(a.title)}">${bars}</div>`
  }

  function npHTML() {
    const t = track()
    const a = album()
    const tabBtn = (id, label) =>
      `<button role="tab" aria-selected="${state.tab === id}" data-tab="${id}">${label}</button>`
    const tool = (act, ic, label, on, value) =>
      `<button data-act="${act}" class="${on ? 'on' : ''}" aria-pressed="${!!on}" aria-label="${label}${value ? ': ' + value : ''}">${icon(ic)}<span>${value || label}</span></button>`
    const dj = state.station
      ? `<div class="dj" aria-live="polite">
          <div class="dj-avatar" aria-hidden="true">${DJ.initials}</div>
          <div style="min-width:0"><div class="dj-name">${DJ.name}<span class="tag">DJ</span></div>
          <div class="dj-blurb">${esc(BLURBS[state.queue[state.cur] % BLURBS.length])}</div></div>
        </div>`
      : ''
    const eqOn = state.eqOpen || state.eqPreset !== 'Flat'
    return `
      <div class="np ${state.playing ? 'playing' : 'paused'}" role="dialog" aria-label="Now Playing">
        <div class="np-head">
          <button class="icon-btn btn-down" data-act="close" aria-label="Close Now Playing">${icon('down')}</button>
          <button class="icon-btn btn-close" data-act="close" aria-label="Close Now Playing">${icon('close')}</button>
          <div style="min-width:0"><div class="overline">${state.station ? 'Station' : 'Playing from album'}</div>
            <div class="src">${state.station ? esc(STATION) : esc(a.title)}</div></div>
          <button class="icon-btn" data-nav="More options" aria-label="More options">${icon('more')}</button>
        </div>
        <div class="np-tabs" role="tablist" aria-label="Now Playing view">
          ${tabBtn('art', state.visuals ? 'Visuals' : 'Art')}${tabBtn('lyrics', 'Lyrics')}${tabBtn('queue', 'Queue')}
        </div>
        <div class="np-stage" role="tabpanel">${stageHTML()}</div>
        <div class="meta">
          <div class="title"><a href="#" data-nav="Track: ${esc(t.title)}">${esc(t.title)}</a></div>
          <div class="side"><button class="icon-btn ${state.starred ? 'on' : ''}" data-act="star" aria-label="Star" aria-pressed="${state.starred}">${icon(state.starred ? 'heart' : 'heartOutline')}</button></div>
          <div class="sub"><a href="#" data-nav="Artist: ${esc(a.artist)}">${esc(a.artist)}</a> · <a href="#" data-nav="Album: ${esc(a.title)}">${esc(a.title)}</a></div>
          <div class="rating" role="radiogroup" aria-label="Rating">${[1, 2, 3, 4, 5]
            .map(
              (n) =>
                `<button role="radio" aria-checked="${state.rating === n}" aria-label="${n} star${n > 1 ? 's' : ''}" data-rate="${n}" class="${n <= state.rating ? 'on' : ''}">${icon('star')}</button>`,
            )
            .join('')}</div>
        </div>
        ${dj}
        <div class="seek">
          <input class="seekbar" type="range" min="0" max="${t.dur}" step="1" aria-label="Seek">
          <div class="times"><span class="el"></span><span class="rem"></span></div>
        </div>
        <div class="controls">
          <button class="icon-btn small ${state.shuffle ? 'on' : ''}" data-act="shuffle" aria-label="Shuffle" aria-pressed="${state.shuffle}">${icon('shuffle')}</button>
          <button class="icon-btn" data-act="prev" aria-label="Previous">${icon('prev')}</button>
          <button class="play" data-act="toggle" aria-label="${state.playing ? 'Pause' : 'Play'}">${playIcon()}</button>
          <button class="icon-btn" data-act="next" aria-label="Next">${icon('next')}</button>
          <button class="icon-btn small ${state.repeat ? 'on' : ''}" data-act="repeat" aria-label="Repeat" aria-pressed="${state.repeat}">${icon('repeat')}</button>
        </div>
        <div class="toolbar" role="toolbar" aria-label="Now Playing tools">
          ${tool('tab-lyrics', 'lyrics', 'Lyrics', state.tab === 'lyrics')}
          ${tool('tab-queue', 'queue', 'Queue', state.tab === 'queue')}
          ${tool('eq', 'eq', 'EQ', eqOn, state.eqPreset !== 'Flat' ? state.eqPreset : '')}
          ${tool('visuals', 'visuals', 'Visuals', state.visuals)}
          ${tool('sleep', 'sleep', 'Sleep', state.sleep > 0, state.sleep ? SLEEP[state.sleep] : '')}
          ${tool('speed', 'speed', 'Speed', state.speed > 0, state.speed ? SPEED[state.speed] + '×' : '')}
        </div>
      </div>
      ${state.eqOpen ? eqHTML() : ''}`
  }

  const EQ_PRESETS = {
    Flat: [0, 0, 0, 0, 0],
    Warm: [3, 2, 0, -1, -2],
    Bass: [6, 4, 0, 0, 0],
    Vocal: [-2, 0, 3, 4, 1],
  }
  function eqHTML() {
    const bands = ['60 Hz', '230 Hz', '910 Hz', '3.6 kHz', '14 kHz']
    const vals = EQ_PRESETS[state.eqPreset]
    return `<div class="eq" role="dialog" aria-label="Equaliser">
      <div class="eq-head"><b>Equaliser</b><button class="icon-btn" data-act="eq" aria-label="Close equaliser">${icon('close')}</button></div>
      <div class="chips">${Object.keys(EQ_PRESETS)
        .map((p) => `<button class="chip ${p === state.eqPreset ? 'active' : ''}" data-eq="${p}">${p}</button>`)
        .join('')}</div>
      ${bands
        .map(
          (b, i) =>
            `<label class="eq-row"><span>${b}</span><input type="range" min="-12" max="12" value="${vals[i]}" style="--p:${((vals[i] + 12) / 24) * 100}%" aria-label="${b}"></label>`,
        )
        .join('')}
    </div>`
  }

  function renderNP() {
    const phone = state.frame === 'phone'
    const host = phone ? $('#phone-np') : $('#desk-np')
    const other = phone ? $('#desk-np') : $('#phone-np')
    other.innerHTML = ''
    host.innerHTML = npHTML()
    $('#phone-np').classList.toggle('open', phone && state.npOpen)
    $('.desk').classList.toggle('panel-open', !phone && state.npOpen)
    $('#phone-np').setAttribute('aria-hidden', String(!(phone && state.npOpen)))
    lastLyric = -1
    tick(true)
  }

  // Wraps the whole render so every change shows its effect everywhere.
  function render() {
    document.body.dataset.frame = state.frame
    applyAccent()
    renderTabbar()
    renderMini()
    renderBar()
    renderNP()
    renderProto()
    writeUrl()
  }

  // ---------- Fake playback ----------
  let lastLyric = -1
  function tick(force) {
    const t = track()
    const p = (state.pos / t.dur) * 100
    $$('.seekbar').forEach((el) => {
      if (document.activeElement !== el || force) el.value = state.pos
      el.style.setProperty('--p', p + '%')
    })
    $$('.el').forEach((el) => (el.textContent = fmt(state.pos)))
    $$('.rem').forEach((el) => (el.textContent = '-' + fmt(t.dur - state.pos)))
    $$('.mini-progress span').forEach((el) => (el.style.width = p + '%'))
    const ly = $('#lyrics')
    if (ly) {
      let idx = 0
      SYNCED.forEach(([s], i) => {
        if (s <= state.pos) idx = i
      })
      if (idx !== lastLyric) {
        lastLyric = idx
        $$('p', ly).forEach((el, i) => {
          el.className = i < idx ? 'past' : i === idx ? 'now' : ''
        })
        const now = ly.children[idx]
        ly.scrollTop = now.offsetTop - ly.clientHeight * 0.38
      }
    }
  }
  setInterval(() => {
    if (!state.playing) return
    state.pos += SPEED[state.speed]
    if (state.pos >= track().dur) {
      if (!state.repeat) return go(1)
      state.pos = 0
    }
    tick()
  }, 1000)

  function go(d) {
    if (d < 0 && state.pos > 3) {
      state.pos = 0
      return tick(true)
    }
    state.cur = (state.cur + d + state.queue.length) % state.queue.length
    state.pos = 0
    render()
  }

  // ---------- Toast for faked navigation ----------
  let toastTimer
  function toast(msg) {
    let el = $('#toast')
    if (!el) {
      el = document.createElement('div')
      el.id = 'toast'
      el.style.cssText =
        'position:fixed;left:50%;bottom:24px;transform:translateX(-50%);z-index:999;padding:8px 14px;border-radius:8px;font:12px ui-monospace,monospace;background:#1a1a1a;color:#fffbd6;border:1.5px dashed #fffbd6;max-width:90vw;transition:opacity .2s'
      document.body.appendChild(el)
    }
    el.textContent = 'Prototype: ' + msg
    el.style.opacity = 1
    clearTimeout(toastTimer)
    toastTimer = setTimeout(() => (el.style.opacity = 0), 1800)
  }

  // ---------- Events ----------
  document.addEventListener('click', (e) => {
    const t = e.target
    const set = t.closest('[data-set]')
    if (set) {
      const [k, v] = set.dataset.set.split(':')
      if (k === 'station' || k === 'npOpen') state[k] = v === '1'
      else state[k] = v
      if (k === 'station') {
        state.cur = 0
        state.pos = 12
      }
      return render()
    }
    if (t.closest('.proto-toggle')) return $('#proto').classList.toggle('collapsed')
    const rm = t.closest('[data-rm]')
    if (rm) {
      e.stopPropagation()
      const i = +rm.dataset.rm
      if (state.queue.length <= 1) return
      state.queue.splice(i, 1)
      if (i < state.cur) state.cur--
      else if (i === state.cur) {
        state.cur = state.cur % state.queue.length
        state.pos = 0
      }
      return render()
    }
    const qr = t.closest('[data-q]')
    if (qr) {
      state.cur = +qr.dataset.q
      state.pos = 0
      state.playing = true
      return render()
    }
    const seek = t.closest('[data-seek]')
    if (seek) {
      state.pos = +seek.dataset.seek
      return tick(true)
    }
    const tab = t.closest('[data-tab]')
    if (tab) {
      state.tab = tab.dataset.tab
      return render()
    }
    const rate = t.closest('[data-rate]')
    if (rate) {
      const n = +rate.dataset.rate
      state.rating = state.rating === n ? 0 : n
      return render()
    }
    const eq = t.closest('[data-eq]')
    if (eq) {
      state.eqPreset = eq.dataset.eq
      return render()
    }
    const nav = t.closest('[data-nav]')
    if (nav) {
      e.preventDefault()
      const inNp = nav.closest('.np-host')
      if (inNp && nav.tagName === 'A') {
        // title/artist/album navigate and close the full-screen view
        if (state.frame === 'phone' || window.innerWidth < 1000) state.npOpen = false
        render()
      }
      return toast('would open ' + nav.dataset.nav)
    }
    const actEl = t.closest('[data-act]')
    if (!actEl) return
    const act = actEl.dataset.act
    switch (act) {
      case 'open':
        state.npOpen = true
        break
      case 'close':
        state.npOpen = false
        break
      case 'panel':
        state.npOpen = !state.npOpen
        break
      case 'toggle':
        state.playing = !state.playing
        break
      case 'next':
        return go(1)
      case 'prev':
        return go(-1)
      case 'shuffle':
      case 'repeat':
      case 'visuals':
        state[act] = !state[act]
        if (act === 'visuals') state.tab = 'art'
        break
      case 'star':
        state.starred = !state.starred
        break
      case 'tab-lyrics':
      case 'tab-queue': {
        const want = act.slice(4)
        if (state.frame === 'desktop' && !state.npOpen) {
          state.npOpen = true
          state.tab = want
        } else state.tab = state.tab === want ? 'art' : want
        break
      }
      case 'eq':
        state.eqOpen = !state.eqOpen
        break
      case 'sleep':
        state.sleep = (state.sleep + 1) % SLEEP.length
        toast('sleep timer ' + SLEEP[state.sleep])
        break
      case 'speed':
        state.speed = (state.speed + 1) % SPEED.length
        break
      case 'clear':
        state.queue = [state.queue[state.cur]]
        state.cur = 0
        break
      default:
        return
    }
    render()
  })

  document.addEventListener('input', (e) => {
    const el = e.target
    if (el.classList.contains('seekbar')) {
      state.pos = +el.value
      tick()
    } else if (el.type === 'range') {
      el.style.setProperty('--p', ((el.value - el.min) / (el.max - el.min)) * 100 + '%')
    }
  })

  document.addEventListener('keydown', (e) => {
    if (e.target.closest('input, textarea, [contenteditable]')) return
    if (e.key === 'Escape' && state.npOpen) {
      state.npOpen = false
      render()
    } else if ((e.key === 'Enter' || e.key === ' ') && e.target.classList.contains('mini')) {
      e.preventDefault()
      state.npOpen = true
      render()
    } else if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
      // cycle the accent, like a variant switcher
      const ids = ACCENTS.map((a) => a.id).concat('art')
      const i = ids.indexOf(state.accent)
      state.accent = ids[(i + (e.key === 'ArrowRight' ? 1 : -1) + ids.length) % ids.length]
      render()
    }
  })

  window.addEventListener('resize', renderProto)

  // ---------- Prototype control panel ----------
  function renderProto() {
    const seg = (key, opts) =>
      `<div class="seg">${opts
        .map(([v, l]) => `<button class="${String(state[key] === true ? '1' : state[key] === false ? '0' : state[key]) === v ? 'sel' : ''}" data-set="${key}:${v}">${l}</button>`)
        .join('')}</div>`
    const sw =
      ACCENTS.map(
        (a) =>
          `<button class="swatch ${state.accent === a.id ? 'sel' : ''}" style="background:${a[state.theme]}" data-set="accent:${a.id}" title="${a.name}" aria-label="${a.name}"></button>`,
      ).join('') +
      `<button class="swatch art ${state.accent === 'art' ? 'sel' : ''}" data-set="accent:art" title="From album art" aria-label="From album art"></button>`
    const name =
      state.accent === 'art' ? 'From album art' : (ACCENTS.find((a) => a.id === state.accent) || ACCENTS[0]).name
    const w = window.innerWidth
    const mode =
      state.frame === 'phone' ? 'full screen' : w >= 1000 ? 'side panel (400px)' : 'full screen (<1000px)'
    const ai = accentInfo
    const lines = [
      `accent   ${name}`,
      ai.raw ? `from art <i style="background:${ai.raw}"></i> ${ai.raw}` : null,
      ai.fallback ? 'from art: no colour, default used' : null,
      `in use   <i style="background:${ai.hex}"></i> ${ai.hex}`,
      `vs bg    ${contrast(ai.hex, BG[state.theme]).toFixed(2)}:1`,
      `on it    <i style="background:${ai.on}"></i> ${ai.on} ${contrast(ai.hex, ai.on).toFixed(2)}:1`,
      `NP mode  ${mode}`,
      `viewport ${w}px`,
      `track    ${esc(track().title)}`,
    ].filter(Boolean)
    $('#proto-body').innerHTML = `
      <h4>Theme</h4>${seg('theme', [['dark', 'Dark'], ['light', 'Light']])}
      <h4>Dark base</h4>${seg('base', Object.entries(DARK_BASES).map(([k, b]) => [k, b.name]))}
      <h4>Accent <span style="font-weight:400;text-transform:none">(← →)</span></h4><div class="swatches">${sw}</div>
      <h4>Framing</h4>${seg('frame', [['phone', 'Phone'], ['desktop', 'Desktop']])}
      <h4>Station</h4>${seg('station', [['0', 'Off'], ['1', 'On']])}
      <h4>Now Playing</h4>${seg('npOpen', [['0', 'Closed'], ['1', 'Open']])}
      <div class="readout">${lines.join('\n')}</div>`
  }

  function writeUrl() {
    const p = new URLSearchParams({
      theme: state.theme,
      base: state.base,
      accent: state.accent,
      frame: state.frame,
      station: state.station ? '1' : '0',
      np: state.npOpen ? '1' : '0',
      tab: state.tab,
      playing: state.playing ? '1' : '0',
    })
    try {
      history.replaceState(null, '', '?' + p)
    } catch (_) {
      // some browsers refuse to rewrite file:// URLs; the prototype still works
    }
  }

  renderStatic()
  renderSide()
  render()
})()
