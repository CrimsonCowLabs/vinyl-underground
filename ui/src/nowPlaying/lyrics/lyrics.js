// Lyrics come as OpenSubsonic structured lyrics: a list of entries (the main
// lyrics, maybe translations), each with lines and whether they are timed.
// That is both what the server keeps in a Track's `lyrics` (a JSON string)
// and what getLyricsBySongId answers with.

const parse = (structured) => {
  if (Array.isArray(structured)) return structured
  if (!structured) return []
  try {
    const list = JSON.parse(structured)
    return Array.isArray(list) ? list : []
  } catch {
    return []
  }
}

const isMain = (entry) => !entry.kind?.trim() || entry.kind === 'main'
const isTimed = (entry) =>
  !!entry.synced && entry.line.every((l) => Number.isFinite(l.start))

// readLyrics is a Track's lyrics as Now Playing shows them: synced, as
// { synced: true, lines: [{ time (seconds), text }] }; unsynced, as
// { synced: false, lines: [text] }; or null when it has none. It reads the
// main lyrics (a translation only when there are none), timed ones first.
export const readLyrics = (structured) => {
  const entries = parse(structured).filter(
    (e) => Array.isArray(e?.line) && e.line.some((l) => l.value?.trim()),
  )
  const main = entries.filter(isMain)
  const candidates = main.length ? main : entries
  const entry = candidates.find(isTimed) || candidates[0]
  if (!entry) return null
  if (!isTimed(entry)) {
    return { synced: false, lines: entry.line.map((l) => l.value || '') }
  }
  // A positive offset shows the lines sooner
  const offset = entry.offset || 0
  return {
    synced: true,
    lines: entry.line.map((l) => ({
      time: (l.start - offset) / 1000,
      text: l.value || '',
    })),
  }
}

// currentLine is the index of the synced line being sung at the time (in
// seconds): the last one that has started, or -1 before the first.
export const currentLine = (lines, seconds) => {
  let index = -1
  lines.forEach((line, i) => {
    if (line.time <= seconds) index = i
  })
  return index
}
