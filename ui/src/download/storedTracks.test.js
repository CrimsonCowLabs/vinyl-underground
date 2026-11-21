import { describe, it, expect } from 'vitest'
import { createDownloads } from './downloads'
import { createStoredTracks } from './storedTracks'

// Playlist Tracks carry the Track id in mediaFileId; album Tracks are songs,
// whose id is the Track id
const playlistTrack = (n) => ({ id: String(n), mediaFileId: `t${n}` })
const song = (n) => ({ id: `t${n}` })

const SERVER = {
  getPlaylist: async (id) => ({
    playlist: { id, name: 'Road trip' },
    tracks: [1, 2].map(playlistTrack),
  }),
  getAlbum: async (id) => ({
    album: { id, name: 'Blue' },
    tracks: [2, 3].map(song),
  }),
}

// An in-memory stand-in for the device's storage, counting how often the
// stored Tracks are listed
const fakeStorage = () => {
  const records = new Map()
  const trackIds = new Set()
  const key = (r) => `${r.kind}:${r[r.kind].id}`
  return {
    listed: 0,
    listDownloads: async () => [...records.values()],
    saveDownload: async (r) => {
      records.set(key(r), r)
    },
    deleteDownload: async (id, kind) => {
      records.delete(`${kind}:${id}`)
    },
    listTrackIds: async function () {
      this.listed++
      return [...trackIds]
    },
    listTrackSizes: async () => ({}),
    saveTrack: async (t) => {
      trackIds.add(t.mediaFileId || t.id)
    },
    deleteTrack: async (id) => {
      trackIds.delete(id)
    },
  }
}

const setup = () => {
  const storage = fakeStorage()
  let listener = 'ana'
  const downloads = createDownloads({
    server: SERVER,
    storage,
    requestPersistence: async () => true,
    currentListener: () => listener,
  })
  return {
    downloads,
    storage,
    storedTracks: createStoredTracks(downloads),
    switchListener: (l) => (listener = l),
  }
}

describe('storedTracks', () => {
  it('knows a song from an album Download is stored on this device', async () => {
    const { downloads, storedTracks } = setup()

    await downloads.start('al1', 'album')

    expect(storedTracks.has('t3')).toBe(true)
  })

  it('knows a song from a Playlist Download is stored on this device', async () => {
    const { downloads, storedTracks } = setup()

    await downloads.start('pl1')

    expect(storedTracks.has('t1')).toBe(true)
  })

  it('knows a song in no Download is not stored', async () => {
    const { downloads, storedTracks } = setup()

    await downloads.start('pl1')

    expect(storedTracks.has('t3')).toBe(false)
    expect(storedTracks.has('t9')).toBe(false)
  })

  it('keeps a song in two Downloads until the last of them is removed', async () => {
    const { downloads, storedTracks } = setup()
    await downloads.start('pl1')
    await downloads.start('al1', 'album')

    await downloads.remove('pl1')
    expect(storedTracks.has('t2')).toBe(true)
    expect(storedTracks.has('t1')).toBe(false)

    await downloads.remove('al1', 'album')
    expect(storedTracks.has('t2')).toBe(false)
  })

  it('tells a song as soon as its Track has finished storing, and when it goes', async () => {
    const { downloads, storedTracks } = setup()
    const seen = []
    storedTracks.subscribe('t2', () => seen.push(storedTracks.has('t2')))

    await downloads.start('pl1')
    await downloads.remove('pl1')

    expect(seen).toEqual([true, false])
  })

  it('leaves songs whose answer did not change alone', async () => {
    const { downloads, storedTracks } = setup()
    const told = []
    storedTracks.subscribe('t1', () => told.push('t1'))
    storedTracks.subscribe('t3', () => told.push('t3'))

    await downloads.start('pl1')

    expect(told).toEqual(['t1'])
  })

  it('stops telling a song once it unsubscribes', async () => {
    const { downloads, storedTracks } = setup()
    const told = []
    const unsubscribe = storedTracks.subscribe('t1', () => told.push('t1'))

    unsubscribe()
    await downloads.start('pl1')

    expect(told).toEqual([])
  })

  it('answers for a long list without reading the browser storage per song', async () => {
    const { downloads, storage, storedTracks } = setup()
    await downloads.load()
    await downloads.start('pl1')
    const ids = Array.from({ length: 1000 }, (_, n) => `t${n}`)
    ids.forEach((id) => storedTracks.subscribe(id, () => {}))

    const answers = ids.filter((id) => storedTracks.has(id))

    expect(answers).toEqual(['t1', 't2'])
    expect(storage.listed).toBe(1)
  })

  it("does not count another Listener's Downloads", async () => {
    const { downloads, storedTracks, switchListener } = setup()
    await downloads.start('pl1')

    switchListener('bo')

    expect(storedTracks.has('t1')).toBe(false)
  })
})
