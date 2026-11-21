import { describe, it, expect, vi } from 'vitest'
import { createDownloads } from './downloads'

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
      records.set(record.playlist.id, record)
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

const setup = ({ storage = fakeStorage(), listener = 'ana' } = {}) => {
  const server = {
    getPlaylist: vi.fn(async (id) => structuredClone(PLAYLISTS[id])),
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
      records: { pl1: { listener: 'ana', ...PLAYLISTS.pl1 } },
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
    expect(storage.records.has('pl1')).toBe(false)
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
})
