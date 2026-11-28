import { useCallback, useEffect, useRef, useState } from 'react'
import { httpClient } from '../../dataProvider'
import { REST_URL } from '../../consts'
import { clearWriting, getWriting, setWriting } from './reviewDrafts'

// WRITE_POLL_MS is how often a card looks again at a release being written into its Files.
export const WRITE_POLL_MS = 2000

// useReleaseWrite follows a MusicBrainz release still being written into the Files of the job
// jobId after its apply came back (the server stops waiting after a while, and the edit job
// carries on in the background). While it is written, writing holds Promote off the card (its
// moves would be refused); once the edit job is done, or gone, onWritten reloads the card, with
// the tags and cover the release wrote. start(editJobId) begins following one. What is followed
// is kept outside the card (see reviewDrafts.js), so a remounted card still waits.
export const useReleaseWrite = (jobId, onWritten) => {
  const [editJobId, setEditJobId] = useState(() => getWriting(jobId))
  const written = useRef(onWritten)
  useEffect(() => {
    written.current = onWritten
  }, [onWritten])

  const start = useCallback(
    (id) => {
      setWriting(jobId, id)
      setEditJobId(id)
    },
    [jobId],
  )

  useEffect(() => {
    if (!editJobId) return undefined
    let active = true
    let timer
    const finish = () => {
      clearWriting(jobId)
      setEditJobId(null)
      written.current?.()
    }
    const look = () => {
      httpClient(`${REST_URL}/editJob/${editJobId}`)
        .then(({ json }) => {
          if (!active) return
          if (json?.state === 'done') finish()
          else timer = setTimeout(look, WRITE_POLL_MS)
        })
        .catch((err) => {
          if (!active) return
          // A job the server no longer has can't hold the card; anything else, look again.
          if (err?.status === 404) finish()
          else timer = setTimeout(look, WRITE_POLL_MS)
        })
    }
    timer = setTimeout(look, WRITE_POLL_MS)
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [jobId, editJobId])

  return { writing: !!editJobId, start }
}
