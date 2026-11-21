import { describe, it, expect, vi } from 'vitest'
import { createDownloads } from './downloads'

// Bringing Downloads up to date when their Playlists change on the server

// A playlistTrack row: `id` is the Track's position in the Playlist
const tracksOf = (...ns) =>
  ns.map((n, i) => ({
    id: String(i + 1),
    mediaFileId: `t${n}`,
    title: `T${n}`,
  }))

// The server's Playlists, which tests change as another device would
const fakeServer = () => {
  const playlists = {}
  const unreachable = new Set() // Playlist ids whose requests fail
  let clock = 0
  const reach = (id) => {
    if (unreachable.has(id)) throw new TypeError('Failed to fetch')
  }
  const server = {
    playlists,
    unreachable,
    // Changes a Playlist's Tracks, as adding, removing or reordering does
    setTracks: (id, ...ns) => {
      const current = playlists[id]?.playlist || { id, name: `Playlist ${id}` }
      playlists[id] = {
        playlist: { ...current, updatedAt: `2026-01-01T00:00:${++clock}Z` },
        tracks: tracksOf(...ns),
      }
    },
    getPlaylist: vi.fn(async (id) => {
      reach(id)
      if (!playlists[id])
        throw Object.assign(new Error('Not found'), { status: 404 })
      return structuredClone(playlists[id])
    }),
    getPlaylistInfo: vi.fn(async (id) => {
      reach(id)
      return playlists[id] ? structuredClone(playlists[id].playlist) : null
    }),
  }
  return server
}

const fakeStorage = () => {
  const records = new Map()
  const trackIds = new Set()
  const failing = new Set()
  return {
    records,
    trackIds,
    failing,
    saved: [],
    listDownloads: async () => [...records.values()],
    saveDownload: async (record) => {
      records.set(record.playlist.id, structuredClone(record))
    },
    deleteDownload: async (id) => {
      records.delete(id)
    },
    listTrackIds: async () => [...trackIds],
    saveTrack: async function (t) {
      if (failing.has(t.mediaFileId)) throw new TypeError('Failed to fetch')
      this.saved.push(t.mediaFileId)
      trackIds.add(t.mediaFileId)
    },
    deleteTrack: async (id) => {
      trackIds.delete(id)
    },
  }
}

const setup = () => {
  const server = fakeServer()
  const storage = fakeStorage()
  let listener = 'ana'
  const downloads = createDownloads({
    server,
    storage,
    requestPersistence: async () => true,
    currentListener: () => listener,
  })
  return { downloads, server, storage, switchListener: (l) => (listener = l) }
}

const positions = (record) => record.tracks.map((t) => t.mediaFileId)

describe('refreshing Downloads', () => {
  it('downloads Tracks added to the Playlist on another device', async () => {
    const { downloads, server, storage } = setup()
    server.setTracks('pl1', 1, 2)
    await downloads.start('pl1')

    server.setTracks('pl1', 1, 2, 3)
    await downloads.refresh()

    expect([...storage.trackIds].sort()).toEqual(['t1', 't2', 't3'])
    expect(downloads.get('pl1')).toMatchObject({
      state: 'downloaded',
      done: 3,
      total: 3,
    })
    expect(positions(storage.records.get('pl1'))).toEqual(['t1', 't2', 't3'])
  })

  it('deletes Tracks removed from the Playlist, unless another Download has them', async () => {
    const { downloads, server, storage } = setup()
    server.setTracks('pl1', 1, 2, 3)
    server.setTracks('pl2', 3, 4)
    await downloads.start('pl1')
    await downloads.start('pl2')

    server.setTracks('pl1', 1)
    await downloads.refresh()

    expect([...storage.trackIds].sort()).toEqual(['t1', 't3', 't4'])
    expect(downloads.isTrackDownloaded('t2')).toBe(false)
    expect(downloads.isTrackDownloaded('t3')).toBe(true)
    expect(downloads.get('pl1')).toMatchObject({ done: 1, total: 1 })
  })

  it('keeps the Playlist in its new order, without fetching Tracks again', async () => {
    const { downloads, server, storage } = setup()
    server.setTracks('pl1', 1, 2, 3)
    await downloads.start('pl1')

    server.setTracks('pl1', 3, 1, 2)
    await downloads.refresh()

    expect(positions(downloads.getRecord('pl1'))).toEqual(['t3', 't1', 't2'])
    expect(positions(storage.records.get('pl1'))).toEqual(['t3', 't1', 't2'])
    expect(storage.saved).toEqual(['t1', 't2', 't3'])
  })

  it('does not fetch the Tracks of a Playlist that has not changed', async () => {
    const { downloads, server } = setup()
    server.setTracks('pl1', 1, 2)
    await downloads.start('pl1')
    server.getPlaylist.mockClear()

    await downloads.refresh()

    expect(server.getPlaylistInfo).toHaveBeenCalledWith('pl1')
    expect(server.getPlaylist).not.toHaveBeenCalled()
  })

  it('always fetches the Tracks of a Smart Playlist, whose rules pick new ones', async () => {
    const { downloads, server, storage } = setup()
    server.setTracks('pl1', 1)
    server.playlists.pl1.playlist.rules = { all: [] }
    await downloads.start('pl1')

    // The server re-evaluates the rules when its Tracks are read
    const { updatedAt } = server.playlists.pl1.playlist
    server.setTracks('pl1', 1, 2)
    server.playlists.pl1.playlist.updatedAt = updatedAt
    await downloads.refresh()

    expect([...storage.trackIds].sort()).toEqual(['t1', 't2'])
  })

  it('removes the Download of a Playlist deleted on the server', async () => {
    const { downloads, server, storage } = setup()
    server.setTracks('pl1', 1, 2)
    server.setTracks('pl2', 2, 3)
    await downloads.start('pl1')
    await downloads.start('pl2')

    delete server.playlists.pl1
    await downloads.refresh()

    expect(downloads.get('pl1')).toBeUndefined()
    expect(storage.records.has('pl1')).toBe(false)
    expect([...storage.trackIds].sort()).toEqual(['t2', 't3'])
  })

  it('leaves a Download as it is when its Playlist cannot be reached, and refreshes the others', async () => {
    const { downloads, server, storage } = setup()
    server.setTracks('pl1', 1)
    server.setTracks('pl2', 2)
    await downloads.start('pl1')
    await downloads.start('pl2')

    server.setTracks('pl1', 3)
    server.setTracks('pl2', 4)
    server.unreachable.add('pl1')
    await downloads.refresh()

    expect(positions(downloads.getRecord('pl1'))).toEqual(['t1'])
    expect(downloads.get('pl1').state).toBe('downloaded')
    expect([...storage.trackIds].sort()).toEqual(['t1', 't4'])
  })

  it('finishes a Download interrupted during a refresh on the next one', async () => {
    const { downloads, server, storage } = setup()
    server.setTracks('pl1', 1)
    await downloads.start('pl1')

    server.setTracks('pl1', 1, 2, 3)
    storage.failing.add('t3')
    await downloads.refresh()
    expect(downloads.get('pl1')).toMatchObject({
      state: 'interrupted',
      done: 2,
      total: 3,
    })

    storage.failing.clear()
    await downloads.refresh()

    expect(downloads.get('pl1')).toMatchObject({ state: 'downloaded', done: 3 })
  })

  it('does not bring back a Download removed while it was being refreshed', async () => {
    const { downloads, server, storage } = setup()
    server.setTracks('pl1', 1)
    await downloads.start('pl1')

    server.setTracks('pl1', 1, 2)
    const fetchPlaylist = server.getPlaylist.getMockImplementation()
    server.getPlaylist.mockImplementationOnce(async (id) => {
      const answer = await fetchPlaylist(id)
      await downloads.remove('pl1')
      return answer
    })
    await downloads.refresh()

    expect(downloads.get('pl1')).toBeUndefined()
    expect(storage.records.size).toBe(0)
    expect(storage.trackIds.size).toBe(0)
  })

  it('keeps a Download started again while its Playlist was being checked', async () => {
    const { downloads, server, storage } = setup()
    server.setTracks('pl1', 1)
    await downloads.start('pl1')

    // The check answers "deleted", but by then the Listener has removed the
    // Download and started it again, so that newer Download must stay
    server.getPlaylistInfo.mockImplementationOnce(async () => {
      await downloads.remove('pl1')
      await downloads.start('pl1')
      return null
    })
    await downloads.refresh()

    expect(downloads.get('pl1')).toMatchObject({ state: 'downloaded' })
    expect(storage.records.has('pl1')).toBe(true)
    expect(storage.trackIds.has('t1')).toBe(true)
  })

  it("leaves another Listener's Downloads, and the Tracks they use, alone", async () => {
    const { downloads, server, storage, switchListener } = setup()
    server.setTracks('pl1', 1, 2)
    await downloads.start('pl1')
    switchListener('ben')
    server.setTracks('pl2', 2)
    await downloads.start('pl2')

    // Ben can't see Ana's Playlist
    delete server.playlists.pl1
    server.setTracks('pl2', 3)
    await downloads.refresh()

    expect(server.getPlaylistInfo).not.toHaveBeenCalledWith('pl1')
    expect(storage.records.has('pl1')).toBe(true)
    expect([...storage.trackIds].sort()).toEqual(['t1', 't2', 't3'])
  })

  it('checks each Playlist once when refreshes are asked for at the same time', async () => {
    const { downloads, server, storage } = setup()
    server.setTracks('pl1', 1)
    await downloads.start('pl1')

    server.setTracks('pl1', 1, 2)
    await Promise.all([downloads.refresh(), downloads.refresh()])

    expect(server.getPlaylistInfo).toHaveBeenCalledTimes(1)
    expect(storage.saved).toEqual(['t1', 't2'])
  })
})
