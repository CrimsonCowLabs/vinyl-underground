import {
  isWritable,
  isReadOnly,
  isSmartPlaylist,
  canChangeTracks,
} from './playlistUtils'

describe('playlistUtils', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  describe('isWritable', () => {
    it('returns true if user is the owner', () => {
      localStorage.setItem('userId', 'user1')
      expect(isWritable('user1')).toBe(true)
    })

    it('returns false for an admin who is not the owner', () => {
      localStorage.setItem('userId', 'admin1')
      localStorage.setItem('role', 'admin')
      expect(isWritable('user1')).toBe(false)
    })

    it('returns false if user is not the owner and not an admin', () => {
      localStorage.setItem('userId', 'user2')
      expect(isWritable('user1')).toBe(false)
    })
  })

  describe('isReadOnly', () => {
    it('returns true if user is not the owner and not an admin', () => {
      localStorage.setItem('userId', 'user2')
      expect(isReadOnly('user1')).toBe(true)
    })

    it('returns false if user is the owner', () => {
      localStorage.setItem('userId', 'user1')
      expect(isReadOnly('user1')).toBe(false)
    })

    it('returns true for an admin who is not the owner', () => {
      localStorage.setItem('userId', 'admin1')
      localStorage.setItem('role', 'admin')
      expect(isReadOnly('user1')).toBe(true)
    })
  })

  describe('isSmartPlaylist', () => {
    it('returns true if playlist has rules', () => {
      const playlist = { rules: [] }
      expect(isSmartPlaylist(playlist)).toBe(true)
    })

    it('returns false if playlist does not have rules', () => {
      const playlist = {}
      expect(isSmartPlaylist(playlist)).toBe(false)
    })
  })

  describe('canChangeTracks', () => {
    it('returns true if user is the owner and playlist is not smart', () => {
      localStorage.setItem('userId', 'user1')
      const playlist = { ownerId: 'user1' }
      expect(canChangeTracks(playlist)).toBe(true)
    })

    it('returns false if user is not the owner', () => {
      localStorage.setItem('userId', 'user2')
      const playlist = { ownerId: 'user1' }
      expect(canChangeTracks(playlist)).toBe(false)
    })

    it('returns false for an admin on a playlist shared by someone else', () => {
      localStorage.setItem('userId', 'admin1')
      localStorage.setItem('role', 'admin')
      const playlist = { ownerId: 'user1', public: true }
      expect(canChangeTracks(playlist)).toBe(false)
    })

    it('returns false if playlist is smart', () => {
      localStorage.setItem('userId', 'user1')
      const playlist = { ownerId: 'user1', rules: [] }
      expect(canChangeTracks(playlist)).toBe(false)
    })

    it('returns false if playlist is synced', () => {
      localStorage.setItem('userId', 'user1')
      const playlist = { ownerId: 'user1', sync: true }
      expect(canChangeTracks(playlist)).toBe(false)
    })
  })
})
