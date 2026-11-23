import { useEffect, useMemo, useState } from 'react'
import subsonic from '../../subsonic'
import { readLyrics } from './lyrics'

const askServer = (id) =>
  subsonic
    .getLyricsBySongId(id)
    .then(
      ({ json }) =>
        json?.['subsonic-response']?.lyricsList?.structuredLyrics || [],
    )

// useLyrics is the Track's lyrics, as readLyrics gives them, and whether they
// are still being looked up. A queued Track carries the lyrics embedded in its
// File, so they show offline too (a Download); the server is asked as well,
// as it also finds lyrics in a file beside the Track's (an .lrc or .txt), in
// the order its LyricsPriority sets. Nothing is asked for Internet Radio.
export const useLyrics = (current) => {
  const id = current?.isRadio ? null : current?.trackId
  const stored = current?.song?.lyrics
  const embedded = useMemo(() => readLyrics(stored), [stored])
  const [found, setFound] = useState({ id: null, lyrics: null })

  useEffect(() => {
    if (!id) return
    let live = true
    askServer(id)
      .then((list) => readLyrics(list))
      // Offline, or the server can't say: the embedded lyrics will do
      .catch(() => null)
      .then((lyrics) => live && setFound({ id, lyrics }))
    return () => {
      live = false
    }
  }, [id])

  if (!id) return { lyrics: embedded, loading: false }
  if (found.id === id)
    return { lyrics: found.lyrics || embedded, loading: false }
  // Shows the embedded lyrics while the server answers
  return { lyrics: embedded, loading: !embedded }
}
