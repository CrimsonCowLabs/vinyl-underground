import { describe, it, expect, vi, beforeEach } from 'vitest'
import wrapperDataProvider from './wrapperDataProvider'

const { mockProvider, mockHttpClient, mockFetchImage } = vi.hoisted(() => ({
  mockProvider: {
    update: vi.fn(),
    create: vi.fn(),
    getOne: vi.fn(),
  },
  mockHttpClient: vi.fn(),
  mockFetchImage: vi.fn(),
}))

vi.mock('ra-data-json-server', () => ({ default: () => mockProvider }))
vi.mock('./httpClient', () => ({
  default: mockHttpClient,
  fetchImage: mockFetchImage,
}))

describe('wrapperDataProvider', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    mockProvider.update.mockResolvedValue({ data: { id: 'u1' } })
    mockProvider.create.mockResolvedValue({ data: { id: 'u1' } })
    mockHttpClient.mockResolvedValue({ json: [] })
  })

  describe('update user', () => {
    it('sets library associations when an admin edits a non-admin user', async () => {
      localStorage.setItem('role', 'admin')

      await wrapperDataProvider.update('user', {
        id: 'u1',
        data: { name: 'Sam', isAdmin: false, libraryIds: [1] },
      })

      expect(mockProvider.update).toHaveBeenCalledWith(
        'user',
        expect.objectContaining({ id: 'u1' }),
      )
      expect(mockHttpClient).toHaveBeenCalledWith('/api/user/u1/library', {
        method: 'PUT',
        body: JSON.stringify({ libraryIds: [1] }),
      })
    })

    it('does not call the admin-only library endpoint when a non-admin edits their own profile', async () => {
      localStorage.setItem('role', 'regular')

      await wrapperDataProvider.update('user', {
        id: 'u1',
        data: {
          name: 'Sam',
          isAdmin: false,
          libraryIds: [1],
          currentPassword: 'old',
          password: 'new',
        },
      })

      expect(mockProvider.update).toHaveBeenCalled()
      expect(mockHttpClient).not.toHaveBeenCalled()
    })

    it('does not set library associations when the edited user is an admin', async () => {
      localStorage.setItem('role', 'admin')

      await wrapperDataProvider.update('user', {
        id: 'u1',
        data: { name: 'Sam', isAdmin: true, libraryIds: [1] },
      })

      expect(mockProvider.update).toHaveBeenCalled()
      expect(mockHttpClient).not.toHaveBeenCalled()
    })

    it('strips libraryIds from the user update payload', async () => {
      localStorage.setItem('role', 'admin')

      await wrapperDataProvider.update('user', {
        id: 'u1',
        data: { name: 'Sam', isAdmin: false, libraryIds: [1] },
      })

      expect(mockProvider.update).toHaveBeenCalledWith(
        'user',
        expect.objectContaining({
          data: { name: 'Sam', isAdmin: false },
        }),
      )
    })
  })

  describe('refreshMetadata', () => {
    it('posts to the album metadata refresh endpoint', () => {
      mockHttpClient.mockResolvedValue({ json: {} })
      wrapperDataProvider.refreshMetadata('album', 'al-1')
      expect(mockHttpClient).toHaveBeenCalledWith(
        expect.stringContaining('/metadata/al/al-1/refresh'),
        { method: 'POST' },
      )
    })

    it('posts to the artist metadata refresh endpoint', () => {
      mockHttpClient.mockResolvedValue({ json: {} })
      wrapperDataProvider.refreshMetadata('artist', 'ar-1')
      expect(mockHttpClient).toHaveBeenCalledWith(
        expect.stringContaining('/metadata/ar/ar-1/refresh'),
        { method: 'POST' },
      )
    })

    // react-admin rejects a custom method whose response has no `data` key, and the
    // endpoint answers 204 with no body.
    it('resolves to a react-admin shaped response', async () => {
      mockHttpClient.mockResolvedValue({
        status: 204,
        body: '',
        json: undefined,
      })
      await expect(
        wrapperDataProvider.refreshMetadata('album', 'al-1'),
      ).resolves.toEqual({ data: { id: 'al-1' } })
    })
  })

  describe('getCoverPreview', () => {
    it('fetches the preview as an image, with nothing but its path in the URL', async () => {
      const blob = new Blob(['img'], { type: 'image/png' })
      mockFetchImage.mockResolvedValue(blob)
      const signal = new AbortController().signal
      // As react-admin's useDataProvider calls it: (resource, payload), answering { data }
      await expect(
        wrapperDataProvider.getCoverPreview('al-1', {
          candidateId: 'c-1',
          signal,
        }),
      ).resolves.toEqual({ data: blob })
      expect(mockFetchImage).toHaveBeenCalledWith(
        '/api/albumCover/al-1/candidates/c-1/preview',
        { signal },
      )
    })
  })

  describe('Find on MusicBrainz', () => {
    it('searches by album artist and title, encoded in the URL', async () => {
      mockHttpClient.mockResolvedValue({ json: [{ id: 'rel-1' }] })
      await expect(
        wrapperDataProvider.findMusicBrainzReleases(
          'AC/DC & Co',
          'Back in Black?',
        ),
      ).resolves.toEqual({ data: [{ id: 'rel-1' }] })
      expect(mockHttpClient).toHaveBeenCalledWith(
        '/api/musicbrainz/release?artist=AC%2FDC%20%26%20Co&title=Back%20in%20Black%3F',
      )
    })

    it('sends the picked release, with what the admin ticked, to write into the Files', async () => {
      mockHttpClient.mockResolvedValue({ json: { jobId: 'job-1', done: true } })
      const canonical = {
        album: true,
        albumArtist: false,
        date: false,
        titles: true,
      }
      await wrapperDataProvider.correctAlbum('al-1', {
        name: 'Record',
        albumArtist: 'Band',
        year: 1999,
        mbzAlbumId: 'rel-1',
        canonical,
        replaceTrackIds: true,
        cover: false,
      })
      expect(mockHttpClient).toHaveBeenCalledWith('/api/correction/album', {
        method: 'POST',
        body: JSON.stringify({
          albumId: 'al-1',
          name: 'Record',
          albumArtist: 'Band',
          year: 1999,
          mbzAlbumId: 'rel-1',
          canonical,
          replaceTrackIds: true,
          cover: false,
        }),
      })
    })

    it("asks how the album's Tracks pair with a release", async () => {
      mockHttpClient.mockResolvedValue({ json: { holds: true, tracks: [] } })
      await expect(
        wrapperDataProvider.matchMusicBrainzRelease('rel/1', 'al 1'),
      ).resolves.toEqual({ data: { holds: true, tracks: [] } })
      expect(mockHttpClient).toHaveBeenCalledWith(
        '/api/musicbrainz/release/rel%2F1/match?albumId=al%201',
      )
    })
  })

  describe('saves that write to the Files', () => {
    it("sends the album's values with its Tracks' changed numbers, answering the edit job", async () => {
      const answer = {
        jobId: 'job-1',
        done: true,
        albumId: 'al-2',
        job: { skipped: 0 },
      }
      mockHttpClient.mockResolvedValue({ json: answer })
      const tracks = [{ id: 'mf-1', discNumber: 1, trackNumber: 2 }]
      await expect(
        wrapperDataProvider.correctAlbum('al-1', {
          name: 'Record',
          albumArtist: 'Band',
          year: 1999,
          tracks,
        }),
      ).resolves.toEqual({ data: { id: 'job-1', ...answer } })
      expect(mockHttpClient).toHaveBeenCalledWith('/api/correction/album', {
        method: 'POST',
        body: JSON.stringify({
          albumId: 'al-1',
          name: 'Record',
          albumArtist: 'Band',
          year: 1999,
          tracks,
        }),
      })
    })

    it('answers a rename still running in the background', async () => {
      const answer = { jobId: 'job-2', background: true, done: false }
      mockHttpClient.mockResolvedValue({ json: answer })
      await expect(
        wrapperDataProvider.renameArtist('ar-1', 'Band'),
      ).resolves.toEqual({ data: { id: 'job-2', ...answer } })
    })
  })
})
