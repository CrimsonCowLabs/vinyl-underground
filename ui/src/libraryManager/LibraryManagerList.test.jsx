import React from 'react'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { createTheme, ThemeProvider } from '@material-ui/core/styles'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import LibraryManagerList, { LibraryManagerActions } from './LibraryManagerList'

let editing
let listContext

vi.mock('../library/useLibraryEditing', () => ({
  useLibraryEditing: () => editing,
}))

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  List: () => <div>track list</div>,
  useListContext: () => listContext,
  useRefresh: () => vi.fn(),
  useUnselectAll: () => vi.fn(),
  useNotify: () => vi.fn(),
  useTranslate: () => (key, opts) =>
    opts?.smart_count !== undefined ? `${key} ${opts.smart_count}` : key,
}))

describe('<LibraryManagerList />', () => {
  beforeEach(() => {
    editing = { loaded: true, enabled: true }
    listContext = { filterValues: { folder: 'A', format: '' }, total: 42 }
  })

  it('lists the Tracks while editing is on', () => {
    render(<LibraryManagerList />)
    expect(screen.getByText('track list')).toBeInTheDocument()
  })

  it('says editing is off, and lists nothing, while the Library is read-only', () => {
    editing = { loaded: true, enabled: false }
    render(<LibraryManagerList />)
    expect(
      screen.getByText('resources.libraryTrack.message.editingOff'),
    ).toBeInTheDocument()
    expect(screen.queryByText('track list')).not.toBeInTheDocument()
  })

  it('offers to edit all matching, and links to the Edit history', () => {
    render(
      <ThemeProvider theme={createTheme()}>
        <MemoryRouter>
          <LibraryManagerActions />
        </MemoryRouter>
      </ThemeProvider>,
    )
    expect(
      screen.getByText('resources.libraryTrack.actions.editMatching 42'),
    ).toBeInTheDocument()
    expect(
      screen
        .getByText('resources.libraryTrack.actions.editHistory')
        .closest('a'),
    ).toHaveAttribute('href', '/editHistory')
  })

  it("can't edit all matching when nothing matches", () => {
    listContext = { filterValues: {}, total: 0 }
    render(
      <ThemeProvider theme={createTheme()}>
        <MemoryRouter>
          <LibraryManagerActions />
        </MemoryRouter>
      </ThemeProvider>,
    )
    expect(
      screen
        .getByText('resources.libraryTrack.actions.editMatching 0')
        .closest('button'),
    ).toBeDisabled()
  })
})
