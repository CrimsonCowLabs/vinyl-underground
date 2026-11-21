import { describe, it, expect, vi } from 'vitest'
import { createDownloads, trackIdOf } from './downloads'

const track = (n) => ({ id: String(n), mediaFileId: `t${n}`, title: `T${n}` })

const PLAYLISTS = {
  pl1: {
    playlist: { id: 'pl1', name: 'Road trip' },
    tracks: [1, 2, 3].map(track),
  },
  pl2: {
    playlist: { id: 'pl2', name: 'Rainy day' },
    tracks: [3, 4].map(track),
  },
}

// Album Tracks are songs: their id is the Track id
const song = (n) => ({ id: `t${n}`, title: `T${n}` })

const ALBUMS = {
  al1: {
    album: { id: 'al1', name: 'Blue', albumArtist: 'Joni Mitchell' },
    tracks: [4, 5].map(song),
  },
}

// An in-memory stand-in for the device's storage, with switches to make
// storing a Track fail (as it does when the network drops mid-Download).
const fakeStorage = (initial = {}) => {
  const records = new Map(Object.entries(initial.records || {}))
  const trackIds = new Set(initial.trackIds || [])
  const failing = new Set()
  return {
    records,
    trackIds,
    failing,
    saved: [],
    listDownloads: async () => [...records.values()],
    saveDownload: async (record) => {
      const kind = record.kind || 'playlist'
      records.set(`${kind}:${record[kind].id}`, record)
    },
    deleteDownload: async (id, kind = 'playlist') => {
      records.delete(`${kind}:${id}`)
    },
    listTrackIds: async () => [...trackIds],
    // Every stored Track takes 10 bytes per digit of its number: t3 is 30
    listTrackSizes: async () =>
      Object.fromEntries([...trackIds].map((id) => [id, 10 * +id.slice(1)])),
    saveTrack: async function (t) {
      const id = t.mediaFileId || t.id
      if (failing.has(id)) throw new TypeError('Failed to fetch')
      this.saved.push(id)
      trackIds.add(id)
    },
    deleteTrack: async (id) => {
      trackIds.delete(id)
    },
  }
}

const setup = ({ storage = fakeStorage(), listener = 'ana' } = {}) => {
  const server = {
    getPlaylist: vi.fn(async (id) => structuredClone(PLAYLISTS[id])),
    getAlbum: vi.fn(async (id) => structuredClone(ALBUMS[id])),
  }
  const requestPersistence = vi.fn(async () => true)
  let currentListener = listener
  const downloads = createDownloads({
    server,
    storage,
    requestPersistence,
    currentListener: () => currentListener,
  })
  return {
    downloads,
    storage,
    server,
    requestPersistence,
    switchListener: (l) => (currentListener = l),
  }
}

describe('downloads', () => {
  it('stores every Track of a Playlist on the device', async () => {
    const { downloads, storage } = setup()

    await downloads.start('pl1')

    expect([...storage.trackIds].sort()).toEqual(['t1', 't2', 't3'])
    expect(downloads.get('pl1')).toMatchObject({
      id: 'pl1',
      name: 'Road trip',
      state: 'downloaded',
      done: 3,
      total: 3,
    })
    expect(downloads.isTrackDownloaded('t2')).toBe(true)
    expect(downloads.isTrackDownloaded('t9')).toBe(false)
  })

  it('asks for persistent storage when a Download starts', async () => {
    const { downloads, requestPersistence } = setup()

    await downloads.start('pl1')

    expect(requestPersistence).toHaveBeenCalled()
  })

  it('reports progress while Tracks are being stored', async () => {
    const { downloads } = setup()
    const seen = []
    downloads.subscribe(() => {
      const d = downloads.get('pl1')
      d && seen.push(`${d.state} ${d.done}/${d.total}`)
    })

    await downloads.start('pl1')

    expect(seen).toContain('downloading 0/3')
    expect(seen).toContain('downloading 2/3')
    expect(seen[seen.length - 1]).toBe('downloaded 3/3')
  })

  it('does not fetch a Track again when another Download already has it', async () => {
    const { downloads, storage } = setup()
    await downloads.start('pl1')

    await downloads.start('pl2')

    expect(storage.saved).toEqual(['t1', 't2', 't3', 't4'])
    expect(downloads.get('pl2')).toMatchObject({ done: 2, total: 2 })
  })

  it('is interrupted when Tracks fail to store, and resumes where it stopped', async () => {
    const { downloads, storage } = setup()
    storage.failing.add('t2')

    await downloads.start('pl1')

    expect(downloads.get('pl1')).toMatchObject({
      state: 'interrupted',
      done: 2,
      total: 3,
    })

    storage.failing.clear()
    await downloads.resume()

    expect(storage.saved).toEqual(['t1', 't3', 't2'])
    expect(downloads.get('pl1')).toMatchObject({ state: 'downloaded', done: 3 })
  })

  it('resumes an unfinished Download after the app restarts', async () => {
    const storage = fakeStorage({
      records: { 'playlist:pl1': { listener: 'ana', ...PLAYLISTS.pl1 } },
      trackIds: ['t1'],
    })
    const { downloads } = setup({ storage })

    await downloads.load()
    expect(downloads.get('pl1')).toMatchObject({
      state: 'interrupted',
      done: 1,
      total: 3,
    })

    await downloads.resume()

    expect(storage.saved).toEqual(['t2', 't3'])
    expect(downloads.get('pl1').state).toBe('downloaded')
  })

  it('keeps the Playlist and its Tracks for playing offline', async () => {
    const { downloads } = setup()
    await downloads.start('pl1')

    expect(downloads.getRecord('pl1').playlist.name).toBe('Road trip')
    expect(downloads.getRecord('pl1').tracks.map((t) => t.title)).toEqual([
      'T1',
      'T2',
      'T3',
    ])
  })

  it('removing a Download frees Tracks no other Download uses', async () => {
    const { downloads, storage } = setup()
    await downloads.start('pl1')
    await downloads.start('pl2')

    await downloads.remove('pl1')

    expect(downloads.get('pl1')).toBeUndefined()
    expect(storage.records.has('playlist:pl1')).toBe(false)
    expect([...storage.trackIds].sort()).toEqual(['t3', 't4'])
    expect(downloads.isTrackDownloaded('t1')).toBe(false)
  })

  it('removing a Download while it is downloading stops it', async () => {
    const { downloads, storage } = setup()
    let removal
    downloads.subscribe(() => {
      if (!removal && downloads.get('pl1')?.done === 1) {
        removal = downloads.remove('pl1')
      }
    })

    await downloads.start('pl1')
    await removal

    expect(storage.trackIds.size).toBe(0)
    expect(storage.records.size).toBe(0)
  })

  it("lists only the current Listener's Downloads", async () => {
    const { downloads, switchListener } = setup()
    await downloads.start('pl1')

    switchListener('ben')

    expect(downloads.get('pl1')).toBeUndefined()
    expect(downloads.list()).toEqual([])
    expect(downloads.getRecord('pl1')).toBeUndefined()
  })

  it('downloads an album like a Playlist', async () => {
    const { downloads, storage } = setup()

    await downloads.start('al1', 'album')

    expect([...storage.trackIds].sort()).toEqual(['t4', 't5'])
    expect(downloads.get('al1', 'album')).toMatchObject({
      id: 'al1',
      kind: 'album',
      name: 'Blue',
      state: 'downloaded',
      total: 2,
    })
    expect(downloads.getRecord('al1', 'album').album.name).toBe('Blue')
    expect(downloads.get('al1')).toBeUndefined()
  })

  it('lists albums and Playlists together, each with its kind', async () => {
    const { downloads } = setup()
    await downloads.start('pl2')
    await downloads.start('al1', 'album')

    expect(downloads.list().map((d) => `${d.kind} ${d.name}`)).toEqual([
      'playlist Rainy day',
      'album Blue',
    ])
  })

  it('removing an album keeps Tracks a Playlist still uses', async () => {
    const { downloads, storage } = setup()
    await downloads.start('pl2')
    await downloads.start('al1', 'album')

    await downloads.remove('al1', 'album')

    expect(downloads.get('al1', 'album')).toBeUndefined()
    expect(storage.records.has('album:al1')).toBe(false)
    expect([...storage.trackIds].sort()).toEqual(['t3', 't4'])
  })

  it('opens Playlist Downloads stored before albums could be downloaded', async () => {
    const storage = fakeStorage({
      records: { old: { listener: 'ana', ...PLAYLISTS.pl1 } },
      trackIds: ['t1', 't2', 't3'],
    })
    const { downloads } = setup({ storage })

    await downloads.load()

    expect(downloads.get('pl1')).toMatchObject({
      kind: 'playlist',
      state: 'downloaded',
    })
  })

  it('measures the space each Download uses, and the total', async () => {
    const { downloads } = setup()
    await downloads.start('pl1') // t1 t2 t3: 10 + 20 + 30
    await downloads.start('pl2') // t3 t4: 30 + 40
    await downloads.start('al1', 'album') // t4 t5: 40 + 50

    const usage = await downloads.usage()

    // A Track shared by several Downloads counts in each of them, but only
    // once in the total, which is the space actually used on the device
    expect(usage.downloads.map((d) => `${d.name} ${d.size}`)).toEqual([
      'Road trip 60',
      'Rainy day 70',
      'Blue 90',
    ])
    expect(usage.total).toBe(150)
  })

  it('measures only the Tracks stored so far', async () => {
    const { downloads, storage } = setup()
    storage.failing.add('t2')
    await downloads.start('pl1')

    const usage = await downloads.usage()

    expect(usage.downloads[0]).toMatchObject({ name: 'Road trip', size: 40 })
    expect(usage.total).toBe(40)
  })

  it("measures only the current Listener's Downloads", async () => {
    const { downloads, switchListener } = setup()
    await downloads.start('pl1')
    switchListener('ben')
    await downloads.start('pl2')

    const usage = await downloads.usage()

    expect(usage.downloads.map((d) => d.name)).toEqual(['Rainy day'])
    expect(usage.total).toBe(70)
  })

  it('gathers every downloaded Track once, for shuffling offline', async () => {
    const { downloads, storage } = setup()
    storage.failing.add('t5')
    await downloads.start('pl1')
    await downloads.start('pl2')
    await downloads.start('al1', 'album')

    const tracks = downloads.downloadedTracks()

    // t3 and t4 are in two Downloads each; t5 never finished storing
    expect(tracks.map(trackIdOf)).toEqual(['t1', 't2', 't3', 't4'])
    expect(tracks.map((t) => t.title)).toEqual(['T1', 'T2', 'T3', 'T4'])
  })

  it("gathers only the current Listener's downloaded Tracks", async () => {
    const { downloads, switchListener } = setup()
    await downloads.start('pl1')
    switchListener('ben')
    await downloads.start('al1', 'album')

    expect(downloads.downloadedTracks().map(trackIdOf)).toEqual(['t4', 't5'])
  })
})
