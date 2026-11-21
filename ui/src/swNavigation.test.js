import { describe, it, expect, vi } from 'vitest'
import { createNavigationHandler } from './swNavigation'

const OFFLINE = { offlinePage: true }

const setup = (networkResult) => {
  const networkOnly = {
    handle: vi.fn(() =>
      networkResult instanceof Error
        ? Promise.reject(networkResult)
        : Promise.resolve(networkResult),
    ),
  }
  const offlineFallback = vi.fn(() => Promise.resolve(OFFLINE))
  return {
    networkOnly,
    offlineFallback,
    handler: createNavigationHandler(networkOnly, offlineFallback),
  }
}

describe('createNavigationHandler', () => {
  it('serves the response from the network', async () => {
    const response = { status: 200 }
    const { handler, offlineFallback } = setup(response)

    await expect(handler({})).resolves.toBe(response)
    expect(offlineFallback).not.toHaveBeenCalled()
  })

  it('falls back to the offline page when the network fails', async () => {
    const { handler } = setup(new Error('Failed to fetch'))

    await expect(handler({})).resolves.toBe(OFFLINE)
  })

  it.each([500, 502, 503])(
    'falls back to the offline page on %i',
    async (status) => {
      const { handler } = setup({ status })

      await expect(handler({})).resolves.toBe(OFFLINE)
    },
  )

  it.each([304, 401, 404])('passes %i through untouched', async (status) => {
    const response = { status }
    const { handler, offlineFallback } = setup(response)

    await expect(handler({})).resolves.toBe(response)
    expect(offlineFallback).not.toHaveBeenCalled()
  })

  it('passes the navigation params to the network strategy', async () => {
    const params = { request: { url: '/app/' } }
    const { handler, networkOnly } = setup({ status: 200 })

    await handler(params)

    expect(networkOnly.handle).toHaveBeenCalledWith(params)
  })
})

describe('createNavigationHandler with a stored app shell', () => {
  const SHELL = { shell: true }

  const setupShell = (networkResult, storedShell) => {
    const { networkOnly, offlineFallback } = setup(networkResult)
    const shell = {
      put: vi.fn(() => Promise.resolve()),
      match: vi.fn(() => Promise.resolve(storedShell)),
    }
    return {
      shell,
      offlineFallback,
      handler: createNavigationHandler(networkOnly, offlineFallback, shell),
    }
  }

  it('keeps a copy of the app shell each time it loads', async () => {
    const response = { status: 200, clone: () => ({ copy: true }) }
    const { handler, shell } = setupShell(response)

    await expect(handler({})).resolves.toBe(response)
    expect(shell.put).toHaveBeenCalledWith({ copy: true })
  })

  it.each([304, 401, 404])('does not keep a %i', async (status) => {
    const { handler, shell } = setupShell({ status, clone: () => ({}) })

    await handler({})

    expect(shell.put).not.toHaveBeenCalled()
  })

  it('opens the stored app shell when offline, so Downloads can play', async () => {
    const { handler, offlineFallback } = setupShell(
      new Error('Failed to fetch'),
      SHELL,
    )

    await expect(handler({})).resolves.toBe(SHELL)
    expect(offlineFallback).not.toHaveBeenCalled()
  })

  it('falls back to the offline page when no app shell was stored', async () => {
    const { handler } = setupShell(new Error('Failed to fetch'), undefined)

    await expect(handler({})).resolves.toBe(OFFLINE)
  })

  it('serves the stored app shell when the server is down', async () => {
    const { handler } = setupShell({ status: 502 }, SHELL)

    await expect(handler({})).resolves.toBe(SHELL)
  })
})
