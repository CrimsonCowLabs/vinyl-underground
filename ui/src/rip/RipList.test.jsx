import React from 'react'
import { render, screen } from '@testing-library/react'
import { ListContextProvider } from 'react-admin'
import { TestContext } from 'ra-test'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import RipList, { RipItems, RipStatusField } from './RipList.jsx'
import { stubResizeObserver } from '../common/stubResizeObserver'
import { alreadyHaveExtra } from './alreadyHaveExtra'
import RipJobPanel from './review/RipJobPanel.jsx'

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

// The page's width, as useAtLeastWide would measure it
const page = vi.hoisted(() => ({ wide: true }))
vi.mock('./useRipPolling', () => ({ useRipPolling: () => {} }))
// The list stands in with jobs already loaded, so only the table is under test
const jobs = vi.hoisted(() => ({
  j1: {
    id: 'j1',
    kind: 'rip',
    status: 'failed',
    title: 'Great Album',
    source: 'youtube',
    url: 'https://music.youtube.com/playlist?list=OLAK5uy_a_long_id',
    folder: 'Great Album',
    error: 'ERROR: Sign in to confirm you are not a bot',
    createdAt: '2026-11-20T10:00:00Z',
    items: [],
  },
}))
vi.mock('../common', async (importOriginal) => {
  const { ListContextProvider } = await import('react-admin')
  const { cloneElement } = await import('react')
  return {
    ...(await importOriginal()),
    useAtLeastWide: () => [() => {}, page.wide],
    List: ({ children }) => (
      <ListContextProvider
        value={{
          resource: 'rip',
          basePath: '/rip',
          data: jobs,
          ids: Object.keys(jobs),
          currentSort: { field: 'createdAt', order: 'DESC' },
          loaded: true,
          total: 1,
          selectedIds: [],
        }}
      >
        {cloneElement(children, { resource: 'rip', basePath: '/rip' })}
      </ListContextProvider>
    ),
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

  it('still says "already have" on an item of a job still ripping, through trackExtra', () => {
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
    const { getAllByRole } = render(
      <RipJobPanel
        record={{ ...album, items }}
        fallback={<RipItems />}
        trackExtra={alreadyHaveExtra}
      />,
    )
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

// The cut-short cells watch their size; jsdom has no ResizeObserver
let restoreResizeObserver
beforeEach(() => {
  restoreResizeObserver = stubResizeObserver().restore
})
afterEach(() => restoreResizeObserver())

describe('RipList', () => {
  const renderList = () =>
    render(
      <TestContext enableReducers>
        <RipList resource="rip" basePath="/rip" />
      </TestContext>,
    )
  const headers = () =>
    screen.getAllByRole('columnheader').map((th) => th.textContent)

  it('shows every column where there is room', () => {
    page.wide = true
    renderList()
    expect(headers()).toEqual(
      expect.arrayContaining([
        'resources.rip.fields.source',
        'resources.rip.fields.path',
        'resources.rip.fields.url',
        'resources.rip.fields.error',
      ]),
    )
    expect(screen.getByText('youtube')).toBeInTheDocument()
  })

  it('drops the Source and File columns where the page is narrow', () => {
    page.wide = false
    renderList()
    const shown = headers()
    expect(shown).not.toContain('resources.rip.fields.source')
    expect(shown).not.toContain('resources.rip.fields.path')
    expect(shown).toEqual(
      expect.arrayContaining([
        'resources.rip.fields.status',
        'resources.rip.fields.title',
        'resources.rip.fields.url',
        'resources.rip.fields.error',
        'resources.rip.fields.createdAt',
      ]),
    )
    expect(screen.queryByText('youtube')).not.toBeInTheDocument()
    // The URL and error are still there, cut short if need be
    expect(screen.getByText(jobs.j1.url)).toBeInTheDocument()
    expect(screen.getByText(jobs.j1.error)).toBeInTheDocument()
  })
})
