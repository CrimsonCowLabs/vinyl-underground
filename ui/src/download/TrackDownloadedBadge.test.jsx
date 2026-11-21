import React from 'react'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import en from '../i18n/en.json'

// The real Downloads, on an in-memory stand-in for the device's storage whose
// Tracks finish storing only when the test says so
const device = vi.hoisted(() => {
  const state = {
    records: new Map(),
    trackIds: new Set(),
    pending: [], // { id, resolve }
  }
  const key = (kind, id) => `${kind}:${id}`
  return {
    state,
    storage: {
      listDownloads: async () => [...state.records.values()],
      saveDownload: async (r) => {
        state.records.set(key(r.kind, r[r.kind].id), r)
      },
      deleteDownload: async (id, kind) => {
        state.records.delete(key(kind, id))
      },
      listTrackIds: async () => [...state.trackIds],
      listTrackSizes: async () => ({}),
      saveTrack: (t) =>
        new Promise((resolve) => {
          const id = t.mediaFileId || t.id
          state.pending.push({
            id,
            resolve: () => {
              state.trackIds.add(id)
              resolve()
            },
          })
        }),
      deleteTrack: async (id) => {
        state.trackIds.delete(id)
      },
    },
  }
})

const ALBUMS = { al1: { id: 'al1', name: 'Blue' } }
const PLAYLISTS = { pl1: { id: 'pl1', name: 'Road trip' } }
const SONGS = { al1: [{ id: 't1' }, { id: 't2' }] }
// Playlist Tracks carry the Track id in mediaFileId
const PLAYLIST_TRACKS = { pl1: [{ id: '1', mediaFileId: 't2' }] }

vi.mock('./browserStorage', () => ({ browserStorage: device.storage }))

vi.mock('../dataProvider/wrapperDataProvider', () => ({
  default: {
    getOne: async (resource, { id }) => ({
      data: resource === 'album' ? ALBUMS[id] : PLAYLISTS[id],
    }),
    getList: async (resource, { filter }) => ({
      data:
        resource === 'song'
          ? SONGS[filter.album_id]
          : PLAYLIST_TRACKS[filter.playlist_id],
    }),
  },
}))

// Translates with the English strings, so the tests read as the Listener does
const english = (key) => {
  const text = key.split('.').reduce((node, part) => node?.[part], en)
  if (typeof text !== 'string') throw new Error(`No English for ${key}`)
  return text
}
vi.mock('react-admin', () => ({ useTranslate: () => english }))

const { TrackDownloadedBadge } = await import('./TrackDownloadedBadge')
const { downloads } = await import('./index')

// Lets the Downloads' promises settle and the badge re-render
const settle = () => act(() => new Promise((r) => setTimeout(r, 0)))

const finishAll = async () => {
  while (device.state.pending.length) {
    device.state.pending.shift().resolve()
    await settle()
  }
}

const startDownload = async (id, kind) => {
  act(() => {
    downloads.start(id, kind)
  })
  await settle()
}

const badge = () => screen.queryByRole('img', { name: 'Downloaded' })

describe('<TrackDownloadedBadge />', () => {
  beforeEach(() => {
    vi.stubGlobal('caches', {})
    localStorage.setItem('username', 'ana')
  })

  afterEach(async () => {
    await finishAll()
    await act(() => downloads.remove('al1', 'album'))
    await act(() => downloads.remove('pl1', 'playlist'))
    vi.unstubAllGlobals()
    localStorage.setItem('username', 'admin')
  })

  it('shows nothing for a song that is not stored on this device', async () => {
    const { container } = render(<TrackDownloadedBadge record={{ id: 't9' }} />)
    await settle()

    expect(container).toBeEmptyDOMElement()
  })

  it('appears on a song once its Track from an album Download has finished storing', async () => {
    render(<TrackDownloadedBadge record={{ id: 't1' }} />)
    await startDownload('al1', 'album')
    expect(badge()).not.toBeInTheDocument()

    await finishAll()

    expect(badge()).toBeInTheDocument()
  })

  it('shows on a Playlist Track from a Playlist Download', async () => {
    render(<TrackDownloadedBadge record={{ id: '1', mediaFileId: 't2' }} />)
    await startDownload('pl1', 'playlist')
    await finishAll()

    expect(badge()).toBeInTheDocument()
  })

  it('stays until the last Download holding the song is removed', async () => {
    render(<TrackDownloadedBadge record={{ id: 't2' }} />)
    await startDownload('al1', 'album')
    await startDownload('pl1', 'playlist')
    await finishAll()

    await act(() => downloads.remove('al1', 'album'))
    expect(badge()).toBeInTheDocument()

    await act(() => downloads.remove('pl1', 'playlist'))
    expect(badge()).not.toBeInTheDocument()
  })

  it('explains itself in a tooltip', async () => {
    render(<TrackDownloadedBadge record={{ id: 't1' }} />)
    await startDownload('al1', 'album')
    await finishAll()

    fireEvent.mouseOver(badge())

    expect(await screen.findByRole('tooltip')).toHaveTextContent(
      'Stored on this device for offline play',
    )
  })

  it('is hidden where the browser cannot store Downloads', async () => {
    render(<TrackDownloadedBadge record={{ id: 't1' }} />)
    await startDownload('al1', 'album')
    await finishAll()

    vi.stubGlobal('caches', undefined)
    const { container } = render(<TrackDownloadedBadge record={{ id: 't1' }} />)
    await settle()

    expect(container).toBeEmptyDOMElement()
  })
})
