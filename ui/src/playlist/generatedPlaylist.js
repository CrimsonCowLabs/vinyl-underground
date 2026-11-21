import { useCallback, useEffect, useRef, useState } from 'react'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

// Generated Playlists (ADR-0004): the server's LLM picks real Tracks from the
// Library for a Prompt. Shared by the Generate and Regenerate dialogs.
// Keep these in sync with core/generatedplaylist.
export const DEFAULT_LENGTH = 25
export const MIN_LENGTH = 10
export const MAX_LENGTH = 100
export const MAX_PROMPT_LENGTH = 500

export const GENERATED_PLAYLIST_URL = `${REST_URL}/generatedPlaylist`

const errorMessages = {
  aiUnavailable: 'resources.playlist.message.aiUnavailable',
  aiInvalidReply: 'resources.playlist.message.aiInvalidReply',
  noTracks: 'resources.playlist.message.noTracks',
  invalidPrompt: 'resources.playlist.message.invalidPrompt',
  invalidLength: 'resources.playlist.message.invalidLength',
  notGenerated: 'resources.playlist.message.notGenerated',
  notFound: 'resources.playlist.message.generatedNotFound',
  notAuthorized: 'resources.playlist.message.notAuthorized',
}

export const errorMessage = (err) =>
  errorMessages[err?.body?.error] || 'resources.playlist.message.generateFailed'

// usePreview asks the server for a preview. Only the latest request counts,
// and unmounting (closing the dialog) cancels it, so the server stops the LLM
// work nobody is waiting for.
export const usePreview = () => {
  const [generating, setGenerating] = useState(false)
  const [error, setError] = useState(null)
  const [preview, setPreview] = useState(null)
  const request = useRef(0)
  const inFlight = useRef(null)
  useEffect(
    () => () => {
      request.current++
      inFlight.current?.abort()
    },
    [],
  )

  const generate = useCallback((url, body) => {
    const id = ++request.current
    inFlight.current?.abort()
    const controller = new AbortController()
    inFlight.current = controller
    setGenerating(true)
    setError(null)
    setPreview(null)
    return httpClient(url, {
      method: 'POST',
      body: body && JSON.stringify(body),
      signal: controller.signal,
    })
      .then(({ json }) => {
        if (id !== request.current) return null
        setPreview(json)
        return json
      })
      .catch((err) => {
        if (id !== request.current) return null
        setError(errorMessage(err))
        return null
      })
      .finally(() => {
        if (id === request.current) setGenerating(false)
      })
  }, [])

  return { generating, error, setError, preview, generate }
}

// saveGeneratedPlaylist saves a preview as a new Playlist and returns its id.
// The server names it after its Prompt when name is empty.
export const saveGeneratedPlaylist = (preview, name = '') =>
  httpClient(GENERATED_PLAYLIST_URL, {
    method: 'POST',
    body: JSON.stringify({
      name: name.trim(),
      prompt: preview.prompt,
      length: preview.length,
      trackIds: preview.tracks.map((t) => t.id),
    }),
  }).then(({ json }) => json.id)

// replaceGeneratedPlaylistTracks swaps the Generated Playlist's Tracks for the
// preview's.
export const replaceGeneratedPlaylistTracks = (playlistId, preview) =>
  httpClient(`${GENERATED_PLAYLIST_URL}/${playlistId}/tracks`, {
    method: 'PUT',
    body: JSON.stringify({ trackIds: preview.tracks.map((t) => t.id) }),
  })
