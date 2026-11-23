import { renderHook } from '@testing-library/react-hooks'
import { vi } from 'vitest'

vi.mock('../dataProvider', () => ({
  httpClient: vi.fn(),
}))

import { httpClient } from '../dataProvider'
import { useLibraryEditing } from './useLibraryEditing'

describe('useLibraryEditing', () => {
  beforeEach(() => {
    httpClient.mockReset()
    localStorage.clear()
  })

  it('reports editing on for the admin when a Library is writable', async () => {
    localStorage.setItem('role', 'admin')
    httpClient.mockResolvedValue({
      json: {
        enabled: true,
        libraries: [
          { id: 1, name: 'Music', writable: true },
          { id: 3, name: 'Old', writable: false },
        ],
      },
    })
    const { result, waitForNextUpdate } = renderHook(() => useLibraryEditing())
    expect(result.current.enabled).toBe(false)
    await waitForNextUpdate()
    expect(httpClient).toHaveBeenCalledWith('/api/libraryEditing')
    expect(result.current.loaded).toBe(true)
    expect(result.current.enabled).toBe(true)
    expect(result.current.isWritable(1)).toBe(true)
    expect(result.current.isWritable(3)).toBe(false)
    expect(result.current.isWritable(9)).toBe(false)
  })

  it('reports editing off for the admin with a read-only Library', async () => {
    localStorage.setItem('role', 'admin')
    httpClient.mockResolvedValue({
      json: {
        enabled: false,
        libraries: [{ id: 1, name: 'Music', writable: false }],
      },
    })
    const { result, waitForNextUpdate } = renderHook(() => useLibraryEditing())
    await waitForNextUpdate()
    expect(result.current.enabled).toBe(false)
    expect(result.current.isWritable(1)).toBe(false)
  })

  it('stays off when the server cannot be asked', async () => {
    localStorage.setItem('role', 'admin')
    httpClient.mockRejectedValue(new Error('boom'))
    const { result, waitForNextUpdate } = renderHook(() => useLibraryEditing())
    await waitForNextUpdate()
    expect(result.current.loaded).toBe(true)
    expect(result.current.enabled).toBe(false)
  })

  it('never reports editing on to a Listener, and never asks the server', () => {
    localStorage.setItem('role', 'regular')
    const { result } = renderHook(() => useLibraryEditing())
    expect(result.current.enabled).toBe(false)
    expect(result.current.isWritable(1)).toBe(false)
    expect(httpClient).not.toHaveBeenCalled()
  })
})
