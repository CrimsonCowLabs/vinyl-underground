import React from 'react'
import { render } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { RipItems, RipStatusField } from './RipList.jsx'

vi.mock('react-admin', async () => {
  const actual = await vi.importActual('react-admin')
  return {
    ...actual,
    // Shows the key and its options, so the test sees what would be translated.
    useTranslate: () => (key, opts) =>
      opts && Object.keys(opts).some((k) => k !== '_')
        ? `${key} ${JSON.stringify(opts)}`
        : key,
  }
})

const album = {
  id: 'j1',
  status: 'downloading',
  folder: 'Great Album',
  items: [
    {
      id: 'a',
      position: 1,
      title: 'First',
      artist: 'Band',
      status: 'ready',
      path: 'Great Album/Band - First [a].opus',
      error: '',
    },
    {
      id: 'b',
      position: 2,
      title: 'Second',
      artist: 'Band',
      status: 'failed',
      path: '',
      error: 'ERROR: Video unavailable',
    },
    {
      id: 'c',
      position: 3,
      title: 'Third',
      artist: '',
      status: 'downloading',
      path: '',
      error: '',
    },
  ],
}

describe('RipItems', () => {
  it("lists each Track with its own status, and a failed Track's error", () => {
    const { getAllByRole, getByText } = render(<RipItems record={album} />)
    const rows = getAllByRole('row').slice(1)
    expect(rows).toHaveLength(3)
    expect(rows[0].textContent).toContain('Band - First')
    expect(rows[0].textContent).toContain('resources.rip.status.ready')
    expect(rows[0].textContent).toContain('Great Album/Band - First [a].opus')
    expect(rows[1].textContent).toContain('resources.rip.status.failed')
    expect(getByText('ERROR: Video unavailable')).toBeInTheDocument()
    expect(rows[2].textContent).toContain('Third')
    expect(rows[2].textContent).toContain('resources.rip.status.downloading')
  })

  it('still says "already have" on an item after it was queued', () => {
    const items = album.items.map((it, i) =>
      i === 1
        ? {
            ...it,
            alreadyHave: [
              {
                id: 'mf-1',
                title: 'Second',
                artist: 'Band',
                album: 'Debut',
                libraryName: 'Music Library',
              },
            ],
          }
        : { ...it, alreadyHave: [] },
    )
    const { getAllByRole } = render(<RipItems record={{ ...album, items }} />)
    const rows = getAllByRole('row').slice(1)
    expect(rows[1].textContent).toContain('resources.rip.messages.alreadyHave')
    expect(rows[1].textContent).toContain(
      'Band - Second · Debut · Music Library',
    )
    expect(rows[0].textContent).not.toContain('alreadyHave')
  })
})

describe('RipStatusField', () => {
  it('shows how far a playlist has got while it rips', () => {
    const { container } = render(<RipStatusField record={album} />)
    expect(container.textContent).toBe(
      'resources.rip.messages.progress {"status":"resources.rip.status.downloading","done":2,"total":3}',
    )
  })

  it('says how many Tracks failed in a job that is ready', () => {
    const { container } = render(
      <RipStatusField record={{ ...album, status: 'ready' }} />,
    )
    expect(container.textContent).toBe(
      'resources.rip.messages.readyWithFailures {"smart_count":1}',
    )
  })

  it('shows a single video the same as before', () => {
    const { container } = render(
      <RipStatusField
        record={{ status: 'ready', items: [album.items[0]], folder: '' }}
      />,
    )
    expect(container.textContent).toBe('resources.rip.status.ready')
  })
})
