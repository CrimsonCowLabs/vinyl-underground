import React from 'react'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { ThemeProvider, createMuiTheme } from '@material-ui/core/styles'
import { TestContext } from 'ra-test'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { settingsReducer } from '../reducers/settingsReducer'
import { ToggleFieldsMenu } from '../common/ToggleFieldsMenu'
import AlbumTableView from './AlbumTableView'

// The row's other controls need the player, artwork and a server; only the
// Download column is under test here
vi.mock('../common', async (importOriginal) => ({
  ...(await importOriginal()),
  AlbumContextMenu: () => null,
  ArtworkAvatar: () => null,
  ArtistLinkField: () => null,
  RatingField: () => null,
}))
vi.mock('react-dnd', () => ({ useDrag: () => [{}, () => {}] }))

// The album Downloads on this device, which the table follows as they change
const downloads = vi.hoisted(() => ({
  supported: true,
  of: {},
  listeners: new Set(),
}))
const setDownload = (id, download) => {
  downloads.of = { ...downloads.of, [id]: download }
  downloads.listeners.forEach((l) => l())
}
vi.mock('../download/index', async () => {
  const { useEffect, useState } = await import('react')
  return {
    downloadsSupported: () => downloads.supported,
    useDownload: (id, kind) => {
      const read = () => (kind === 'album' ? downloads.of[id] : undefined)
      const [download, setValue] = useState(read)
      useEffect(() => {
        const update = () => setValue(read())
        downloads.listeners.add(update)
        return () => downloads.listeners.delete(update)
      })
      return download
    },
  }
})

const albums = {
  al1: { id: 'al1', name: 'Blue' },
  al2: { id: 'al2', name: 'Hejira' },
}

// The table with the menu that shows and hides its optional columns, as the
// album list's toolbar has it, with the album columns a Listener chose earlier
const renderTable = (savedColumns) =>
  render(
    <ThemeProvider theme={createMuiTheme()}>
      <TestContext
        enableReducers
        customReducers={{ settings: settingsReducer }}
        initialState={
          savedColumns && {
            settings: {
              toggleableFields: { album: savedColumns },
              omittedFields: { album: [] },
            },
          }
        }
      >
        <ToggleFieldsMenu resource="album" />
        <AlbumTableView
          resource="album"
          basePath="/album"
          data={albums}
          ids={['al1', 'al2']}
          currentSort={{ field: 'name', order: 'ASC' }}
          loaded
          total={2}
        />
      </TestContext>
    </ThemeProvider>,
  )

const DOWNLOAD = 'resources.album.fields.download'

const downloadHeader = () =>
  screen.queryByRole('columnheader', { name: DOWNLOAD })

// The Download column's cell in an album's row
const downloadCell = async (name) => {
  const header = await screen.findByRole('columnheader', { name: DOWNLOAD })
  const column = [...header.parentElement.children].indexOf(header)
  return screen.getByText(name).closest('tr').children[column]
}

const openColumnsMenu = () =>
  fireEvent.click(screen.getByRole('button', { name: 'more' }))

// Shows or hides a column, then closes the menu (which hides the table from
// queries while it's open)
const toggleColumn = (name) => {
  openColumnsMenu()
  const item = screen.getByRole('menuitem', { name })
  fireEvent.click(item)
  fireEvent.keyDown(item, { key: 'Escape' })
}

describe('AlbumTableView Download column', () => {
  beforeEach(() => {
    downloads.supported = true
    downloads.of = {}
    downloads.listeners.clear()
  })

  it("shows the Download badge in the row of an album that has one, and an empty cell in the others'", async () => {
    downloads.of.al1 = { id: 'al1', kind: 'album', state: 'downloaded' }

    renderTable()

    const cell = await downloadCell('Blue')
    expect(
      within(cell).getByRole('img', { name: 'download.badge.downloaded' }),
    ).toBeInTheDocument()
    expect(await downloadCell('Hejira')).toBeEmptyDOMElement()
  })

  it('can be hidden and shown again from the columns menu', async () => {
    renderTable()
    await downloadCell('Blue')

    toggleColumn(DOWNLOAD)
    expect(downloadHeader()).not.toBeInTheDocument()

    toggleColumn(DOWNLOAD)
    expect(downloadHeader()).toBeInTheDocument()
  })

  it('appears without undoing the columns a Listener chose before it existed', async () => {
    // Saved before the Download column was added: year hidden, createdAt shown
    renderTable({
      artist: true,
      songCount: true,
      playCount: true,
      year: false,
      mood: false,
      duration: true,
      size: false,
      rating: true,
      createdAt: true,
    })

    await downloadCell('Blue')
    expect(
      screen.queryByRole('columnheader', {
        name: 'resources.album.fields.year',
      }),
    ).not.toBeInTheDocument()
  })

  it("is neither shown nor offered in browsers that can't store Downloads", async () => {
    downloads.supported = false
    downloads.of.al1 = { id: 'al1', kind: 'album', state: 'downloaded' }

    renderTable()
    await screen.findByText('Blue')
    expect(downloadHeader()).not.toBeInTheDocument()
    openColumnsMenu()

    expect(
      screen.getByRole('menuitem', { name: 'resources.album.fields.year' }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('menuitem', { name: DOWNLOAD }),
    ).not.toBeInTheDocument()
  })

  it.each([
    [
      'downloading',
      { state: 'downloading', done: 3, total: 10 },
      'download.message.downloading',
    ],
    [
      'interrupted',
      { state: 'interrupted' },
      'download.badge.interrupted.album',
    ],
  ])('shows an album whose Download is %s', async (_, download, label) => {
    downloads.of.al1 = { id: 'al1', kind: 'album', ...download }

    renderTable()

    const cell = await downloadCell('Blue')
    expect(within(cell).getByRole('img', { name: label })).toBeInTheDocument()
  })

  it("updates an album's cell as its Download changes", async () => {
    renderTable()
    expect(await downloadCell('Blue')).toBeEmptyDOMElement()

    act(() =>
      setDownload('al1', {
        id: 'al1',
        kind: 'album',
        state: 'downloading',
        done: 1,
        total: 10,
      }),
    )
    expect(
      within(await downloadCell('Blue')).getByRole('img', {
        name: 'download.message.downloading',
      }),
    ).toBeInTheDocument()

    act(() =>
      setDownload('al1', { id: 'al1', kind: 'album', state: 'downloaded' }),
    )
    expect(
      within(await downloadCell('Blue')).getByRole('img', {
        name: 'download.badge.downloaded',
      }),
    ).toBeInTheDocument()

    act(() => setDownload('al1', undefined))
    expect(await downloadCell('Blue')).toBeEmptyDOMElement()
  })
})
