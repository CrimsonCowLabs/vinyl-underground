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
    pending: [], // { id, resolve, reject }
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
        new Promise((resolve, reject) => {
          state.pending.push({
            id: t.id,
            resolve: () => {
              state.trackIds.add(t.id)
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

const ALBUMS = {
  al1: { id: 'al1', name: 'Blue' },
  al2: { id: 'al2', name: 'Court and Spark' },
}
const SONGS = {
  al1: [{ id: 't1' }, { id: 't2' }, { id: 't3' }],
  al2: [{ id: 't4' }],
}

vi.mock('./browserStorage', () => ({ browserStorage: device.storage }))

vi.mock('../dataProvider/wrapperDataProvider', () => ({
  default: {
    getOne: async (resource, { id }) => ({ data: ALBUMS[id] }),
    getList: async (resource, { filter }) => ({
      data: SONGS[filter.album_id],
    }),
  },
}))

// Translates with the English strings, so the tests read as the Listener does
const english = (key, options = {}) => {
  const text = key.split('.').reduce((node, part) => node?.[part], en)
  if (typeof text !== 'string') throw new Error(`No English for ${key}`)
  return text.replace(/%\{(\w+)\}/g, (_, name) => options[name])
}
vi.mock('react-admin', () => ({ useTranslate: () => english }))

const { DownloadBadge } = await import('./DownloadBadge')
const { downloads } = await import('./index')

// Lets the Downloads' promises settle and the badge re-render
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

const badge = () => screen.queryByRole('img')

describe('<DownloadBadge />', () => {
  beforeEach(() => {
    vi.stubGlobal('caches', {})
    localStorage.setItem('username', 'ana')
  })

  afterEach(async () => {
    await finishAll()
    await act(() => downloads.remove('al1', 'album'))
    await act(() => downloads.remove('al2', 'album'))
    vi.unstubAllGlobals()
    localStorage.setItem('username', 'admin')
  })

  it('shows nothing for an album without a Download on this device', async () => {
    const { container } = render(<DownloadBadge id="al1" kind="album" />)
    await settle()

    expect(container).toBeEmptyDOMElement()
  })

  it('shows a downloaded album with a check and a label for screen readers', async () => {
    render(<DownloadBadge id="al1" kind="album" />)
    act(() => {
      downloads.start('al1', 'album')
    })
    await settle()
    await finishAll()

    expect(
      screen.getByRole('img', { name: 'Downloaded: plays offline' }),
    ).toBeInTheDocument()
    expect(screen.getByTestId('download-badge-downloaded')).toBeInTheDocument()
  })

  it('explains itself in a tooltip', async () => {
    render(<DownloadBadge id="al1" kind="album" />)
    act(() => {
      downloads.start('al1', 'album')
    })
    await settle()
    await finishNextTrack()

    fireEvent.mouseOver(badge())

    expect(await screen.findByRole('tooltip')).toHaveTextContent(
      'Downloading 1/3',
    )
  })

  it('shows the progress of a Download as it goes, then that it is downloaded', async () => {
    render(<DownloadBadge id="al1" kind="album" />)
    act(() => {
      downloads.start('al1', 'album')
    })
    await settle()

    expect(
      screen.getByRole('img', { name: 'Downloading 0/3' }),
    ).toBeInTheDocument()
    expect(screen.getByTestId('download-badge-downloading')).toBeInTheDocument()

    await finishNextTrack()
    expect(
      screen.getByRole('img', { name: 'Downloading 1/3' }),
    ).toBeInTheDocument()

    await finishAll()
    expect(
      screen.getByRole('img', { name: 'Downloaded: plays offline' }),
    ).toBeInTheDocument()
  })

  it('warns that a Download was interrupted and says how to resume it', async () => {
    render(<DownloadBadge id="al1" kind="album" />)
    act(() => {
      downloads.start('al1', 'album')
    })
    await settle()
    await finishNextTrack()
    await failNextTrack()
    await failNextTrack()

    expect(
      screen.getByRole('img', {
        name: 'Download interrupted at 1/3. Open the album to resume it.',
      }),
    ).toBeInTheDocument()
    expect(screen.getByTestId('download-badge-interrupted')).toBeInTheDocument()
  })

  it('disappears when the Download is removed', async () => {
    render(<DownloadBadge id="al2" kind="album" />)
    act(() => {
      downloads.start('al2', 'album')
    })
    await settle()
    await finishAll()
    expect(badge()).toBeInTheDocument()

    await act(() => downloads.remove('al2', 'album'))

    expect(badge()).not.toBeInTheDocument()
  })

  it("shows nothing for another Listener's Download on this device", async () => {
    act(() => {
      downloads.start('al2', 'album')
    })
    await settle()
    await finishAll()
    localStorage.setItem('username', 'ben')

    render(<DownloadBadge id="al2" kind="album" />)
    await settle()

    expect(badge()).not.toBeInTheDocument()
    localStorage.setItem('username', 'ana')
  })

  it("shows nothing in a browser that can't store Downloads", async () => {
    act(() => {
      downloads.start('al2', 'album')
    })
    await settle()
    await finishAll()
    vi.unstubAllGlobals()

    render(<DownloadBadge id="al2" kind="album" />)
    await settle()

    expect(badge()).not.toBeInTheDocument()
  })
})
