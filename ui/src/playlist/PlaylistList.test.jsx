import React from 'react'
import { act, render, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import en from '../i18n/en.json'

vi.mock('../config', () => ({
  default: { enableFavourites: true },
}))

vi.mock('../common', () => ({
  LoveButton: ({ record, resource }) => (
    <button data-testid="love" data-resource={resource}>
      {record?.starred ? 'starred' : 'not-starred'}
    </button>
  ),
}))

// The real Downloads, on an in-memory stand-in for the device's storage whose
// Tracks finish storing only when the test says so
const device = vi.hoisted(() => {
  const state = { records: new Map(), trackIds: new Set(), pending: [] }
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
        new Promise((resolve, reject) => {
          state.pending.push({
            resolve: () => {
              state.trackIds.add(t.mediaFileId)
              resolve()
            },
            reject,
          })
        }),
      deleteTrack: async (id) => {
        state.trackIds.delete(id)
      },
    },
  }
})

// The server's Playlists, which tests change as another device would
const server = vi.hoisted(() => ({ playlists: {}, tracks: {} }))
const setTracks = (id, ...ns) => {
  const before = server.playlists[id]
  server.playlists[id] = {
    id,
    name: `Playlist ${id}`,
    updatedAt: (before?.updatedAt || 0) + 1,
  }
  server.tracks[id] = ns.map((n, i) => ({
    id: String(i + 1),
    mediaFileId: `t${n}`,
  }))
}

vi.mock('../download/browserStorage', () => ({
  browserStorage: device.storage,
}))

vi.mock('../dataProvider/wrapperDataProvider', () => ({
  default: {
    getOne: async (resource, { id }) => ({ data: { ...server.playlists[id] } }),
    getList: async (resource, { filter }) => ({
      data: [...server.tracks[filter.playlist_id]],
    }),
  },
}))

// Translates with the English strings, so the tests read as the Listener does
const english = (key, options = {}) => {
  const text = key.split('.').reduce((node, part) => node?.[part], en)
  if (typeof text !== 'string') throw new Error(`No English for ${key}`)
  return text.replace(/%\{(\w+)\}/g, (_, name) => options[name])
}
vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  useTranslate: () => english,
}))

const { PlaylistLove, PlaylistName } = await import('./PlaylistList')
const { downloads } = await import('../download')

// Lets the Downloads' promises settle and the list re-render
const settle = () => act(() => new Promise((r) => setTimeout(r, 0)))

const finishNextTrack = async () => {
  device.state.pending.shift().resolve()
  await settle()
}

const failNextTrack = async () => {
  device.state.pending.shift().reject(new TypeError('Failed to fetch'))
  await settle()
}

const finishAll = async () => {
  while (device.state.pending.length) await finishNextTrack()
}

const download = async (id) => {
  act(() => {
    downloads.start(id)
  })
  await settle()
}

const badge = () => screen.queryByRole('img')

const renderName = (id) =>
  render(<PlaylistName record={{ id, name: `Playlist ${id}` }} />)

describe('<PlaylistLove />', () => {
  it('renders a LoveButton bound to the playlist resource', () => {
    render(<PlaylistLove record={{ id: 'pl-1', starred: true }} />)
    const btn = screen.getByTestId('love')
    expect(btn.getAttribute('data-resource')).toBe('playlist')
    expect(btn.textContent).toBe('starred')
  })

  it('exposes datagrid header props so the column renders unsorted', () => {
    // The Datagrid reads these off the element; the wrapper body must not
    // forward them to the button (which would leak onto the DOM).
    expect(PlaylistLove.defaultProps).toEqual({
      source: 'starred',
      sortable: false,
    })
  })
})

describe('<PlaylistName />', () => {
  beforeEach(() => {
    vi.stubGlobal('caches', {})
    localStorage.setItem('username', 'ana')
    setTracks('pl1', 1, 2, 3)
  })

  afterEach(async () => {
    await finishAll()
    await act(() => downloads.remove('pl1'))
    vi.unstubAllGlobals()
    localStorage.setItem('username', 'admin')
  })

  it('shows the name of a Playlist without a Download, and no badge', async () => {
    renderName('pl1')
    await settle()

    expect(screen.getByText('Playlist pl1')).toBeInTheDocument()
    expect(badge()).not.toBeInTheDocument()
  })

  it('shows a Download of the Playlist as it goes, then that it is downloaded', async () => {
    renderName('pl1')
    await download('pl1')

    expect(
      screen.getByRole('img', { name: 'Downloading 0/3' }),
    ).toBeInTheDocument()

    await finishNextTrack()
    expect(
      screen.getByRole('img', { name: 'Downloading 1/3' }),
    ).toBeInTheDocument()

    await finishAll()
    expect(
      screen.getByRole('img', { name: 'Downloaded: plays offline' }),
    ).toBeInTheDocument()
    expect(screen.getByText('Playlist pl1')).toBeInTheDocument()
  })

  it('warns that the Download of the Playlist was interrupted', async () => {
    renderName('pl1')
    await download('pl1')
    await finishNextTrack()
    await failNextTrack()
    await failNextTrack()

    expect(
      screen.getByRole('img', {
        name: 'Download interrupted at 1/3. Open the Playlist to resume it.',
      }),
    ).toBeInTheDocument()
  })

  it('shows a downloaded Playlist changed on another device as downloading until it is up to date', async () => {
    renderName('pl1')
    await download('pl1')
    await finishAll()

    setTracks('pl1', 1, 2, 3, 4)
    act(() => {
      downloads.refresh()
    })
    await settle()

    expect(
      screen.getByRole('img', { name: 'Downloading 3/4' }),
    ).toBeInTheDocument()

    await finishAll()
    expect(
      screen.getByRole('img', { name: 'Downloaded: plays offline' }),
    ).toBeInTheDocument()
  })

  it("shows no badge in a browser that can't store Downloads", async () => {
    await download('pl1')
    await finishAll()
    vi.unstubAllGlobals()

    renderName('pl1')
    await settle()

    expect(screen.getByText('Playlist pl1')).toBeInTheDocument()
    expect(badge()).not.toBeInTheDocument()
  })
})
