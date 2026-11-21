import { describe, it, expect, vi } from 'vitest'
import { HttpError } from 'react-admin'
import { withDownloads } from './offlineDataProvider'

const tracks = [
  { id: '1', mediaFileId: 't1', title: 'Bravo', duration: 200 },
  { id: '2', mediaFileId: 't2', title: 'Alpha', duration: 100 },
  { id: '10', mediaFileId: 't3', title: 'Charlie', duration: 300 },
]
const RECORDS = {
  pl1: { playlist: { id: 'pl1', name: 'Road trip' }, tracks },
  pl2: { playlist: { id: 'pl2', name: 'Autumn' }, tracks: [] },
}

const networkDown = () => Promise.reject(new TypeError('Failed to fetch'))

const setup = ({ online = true, server = {}, loaded = true } = {}) => {
  const dataProvider = {
    getOne: vi.fn(server.getOne || networkDown),
    getList: vi.fn(server.getList || networkDown),
    getManyReference: vi.fn(server.getManyReference || networkDown),
    getMany: vi.fn(networkDown),
  }
  // Downloads are read from the device asynchronously when the app starts
  let records = {}
  let finishLoading
  const ready = new Promise((resolve) => {
    finishLoading = () => {
      records = RECORDS
      resolve()
    }
  })
  const downloads = {
    getRecord: (id) => records[id],
    list: () => Object.keys(records).map((id) => ({ id })),
  }
  const provider = withDownloads(dataProvider, {
    downloads,
    isOnline: () => online,
    ready: () => ready,
  })
  if (loaded) finishLoading()
  return { dataProvider, provider, finishLoading: () => finishLoading() }
}

const tracksOf = (playlist_id, params = {}) => ({
  filter: { playlist_id },
  sort: { field: 'id', order: 'ASC' },
  pagination: { page: 1, perPage: 100 },
  ...params,
})

describe('withDownloads', () => {
  it('uses the server when it answers', async () => {
    const fromServer = { data: { id: 'pl1', name: 'Newer name' } }
    const { provider } = setup({ server: { getOne: async () => fromServer } })

    await expect(provider.getOne('playlist', { id: 'pl1' })).resolves.toBe(
      fromServer,
    )
  })

  it('opens a downloaded Playlist when the network is down', async () => {
    const { provider } = setup()

    const { data } = await provider.getOne('playlist', { id: 'pl1' })

    expect(data.name).toBe('Road trip')
  })

  it('does not wait for the network when the device is offline', async () => {
    const { provider, dataProvider } = setup({ online: false })

    await provider.getOne('playlist', { id: 'pl1' })

    expect(dataProvider.getOne).not.toHaveBeenCalled()
  })

  it('waits for the Downloads to load when the app starts offline', async () => {
    const { provider, finishLoading } = setup({ online: false, loaded: false })

    const answer = provider.getOne('playlist', { id: 'pl1' })
    setTimeout(finishLoading, 0)

    await expect(answer).resolves.toMatchObject({ data: { name: 'Road trip' } })
  })

  it('passes server errors through, rather than hiding them', async () => {
    const error = new HttpError('Unauthorized', 401)
    const { provider } = setup({
      server: { getOne: () => Promise.reject(error) },
    })

    await expect(provider.getOne('playlist', { id: 'pl1' })).rejects.toBe(error)
  })

  it('fails as before for a Playlist that is not downloaded', async () => {
    const { provider } = setup()

    await expect(provider.getOne('playlist', { id: 'nope' })).rejects.toThrow(
      'Failed to fetch',
    )
  })

  it('fails as before for anything other than Playlists', async () => {
    const { provider } = setup({ online: false })

    await expect(provider.getOne('album', { id: 'pl1' })).rejects.toThrow(
      'Failed to fetch',
    )
  })

  it('lists the Tracks of a downloaded Playlist in Playlist order', async () => {
    const { provider } = setup()

    const result = await provider.getManyReference('playlistTrack', {
      target: 'playlist_id',
      id: 'pl1',
      ...tracksOf('pl1'),
    })

    expect(result.total).toBe(3)
    expect(result.data.map((t) => t.id)).toEqual(['1', '2', '10'])
  })

  it('sorts and pages the Tracks like the server would', async () => {
    const { provider } = setup()

    const result = await provider.getList(
      'playlistTrack',
      tracksOf('pl1', {
        sort: { field: 'title', order: 'DESC' },
        pagination: { page: 1, perPage: 2 },
      }),
    )

    expect(result.total).toBe(3)
    expect(result.data.map((t) => t.title)).toEqual(['Charlie', 'Bravo'])
  })

  it('returns every Track when asked for no page size (Play all)', async () => {
    const { provider } = setup()

    const result = await provider.getList(
      'playlistTrack',
      tracksOf('pl1', { pagination: { page: 1, perPage: 0 } }),
    )

    expect(result.data).toHaveLength(3)
  })

  it('lists the downloaded Playlists when the network is down', async () => {
    const { provider } = setup()

    const result = await provider.getList('playlist', {
      filter: {},
      sort: { field: 'name', order: 'ASC' },
      pagination: { page: 1, perPage: 25 },
    })

    expect(result.data.map((p) => p.name)).toEqual(['Autumn', 'Road trip'])
    expect(result.total).toBe(2)
  })
})
