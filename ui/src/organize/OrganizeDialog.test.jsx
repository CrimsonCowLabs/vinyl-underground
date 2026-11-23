import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import OrganizeDialog from './OrganizeDialog'
import { organizeScope } from './organize'

const { mockPlan, mockRun, mockNotify } = vi.hoisted(() => ({
  mockPlan: vi.fn(),
  mockRun: vi.fn(),
  mockNotify: vi.fn(),
}))

vi.mock('./organize', async (importOriginal) => ({
  ...(await importOriginal()),
  planOrganize: mockPlan,
  runOrganize: mockRun,
}))

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  useNotify: () => mockNotify,
  useTranslate: () => (key, opts) =>
    opts && Object.keys(opts).length ? `${key} ${JSON.stringify(opts)}` : key,
}))

const plan = {
  token: 'tok-1',
  tracks: 1,
  files: 1,
  inPlace: 2,
  unplaced: { noAlbumArtist: 1 },
  moves: [
    {
      trackId: 't1',
      path: 'iTunes/x/a.mp3',
      newPath: 'Band/Record (1999)/01 A.mp3',
      status: 'move',
    },
    {
      path: 'iTunes/x/cover.jpg',
      newPath: 'Band/Record (1999)/cover.jpg',
      status: 'file',
    },
  ],
  left: [{ trackId: 't2', path: 'Mix/b.mp3', status: 'noAlbumArtist' }],
  totalMoves: 2,
  totalLeft: 1,
}

const scope = { albumId: 'al-1' }

const renderDialog = (props = {}) => {
  const onDone = vi.fn()
  const onClose = vi.fn()
  render(
    <OrganizeDialog
      open
      scope={scope}
      title="Organize"
      onClose={onClose}
      onDone={onDone}
      {...props}
    />,
  )
  return { onDone, onClose }
}

describe('OrganizeDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockPlan.mockResolvedValue(plan)
  })

  it('shows the plan before anything moves: every move, old → new, and each File left, with why', async () => {
    renderDialog()
    await screen.findByText('iTunes/x/a.mp3')
    expect(mockPlan).toHaveBeenCalledWith(scope, 0, 100)
    expect(
      screen.getByText('→ Band/Record (1999)/01 A.mp3'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('→ Band/Record (1999)/cover.jpg'),
    ).toBeInTheDocument()
    expect(screen.getByText('Mix/b.mp3')).toBeInTheDocument()
    expect(
      screen.getByText('resources.organize.reasons.noAlbumArtist'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('resources.organize.summary.inPlace {"smart_count":2}'),
    ).toBeInTheDocument()
    expect(mockRun).not.toHaveBeenCalled()
  })

  it('runs the plan reviewed, by its token, and says so', async () => {
    mockRun.mockResolvedValue({ job: { skipped: 0 }, background: false })
    const { onDone } = renderDialog()
    await screen.findByText('iTunes/x/a.mp3')
    fireEvent.click(screen.getByTestId('organize-run'))
    await waitFor(() => expect(onDone).toHaveBeenCalled())
    expect(mockRun).toHaveBeenCalledWith(scope, 'tok-1')
    expect(mockNotify).toHaveBeenCalledWith('resources.organize.done', {
      type: 'info',
    })
  })

  it('says when the job runs on in the background, and when Files were skipped', async () => {
    mockRun.mockResolvedValueOnce({ job: {}, background: true })
    renderDialog()
    await screen.findByText('iTunes/x/a.mp3')
    fireEvent.click(screen.getByTestId('organize-run'))
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.organize.inBackground',
        { type: 'info' },
      ),
    )
  })

  it('shows the plan again when the Library changed since it was made', async () => {
    mockRun.mockRejectedValue({ body: { error: 'planChanged' } })
    const { onDone } = renderDialog()
    await screen.findByText('iTunes/x/a.mp3')
    fireEvent.click(screen.getByTestId('organize-run'))
    await waitFor(() => expect(mockPlan).toHaveBeenCalledTimes(2))
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.organize.errors.planChanged',
      { type: 'warning' },
    )
    expect(onDone).not.toHaveBeenCalled()
  })

  it("can't run a plan that moves no Track", async () => {
    mockPlan.mockResolvedValue({
      ...plan,
      tracks: 0,
      files: 0,
      moves: [],
      totalMoves: 0,
    })
    renderDialog()
    await screen.findByText('Mix/b.mp3')
    expect(screen.getByTestId('organize-run')).toBeDisabled()
  })

  it('loads more of the plan', async () => {
    mockPlan.mockResolvedValue({ ...plan, totalMoves: 250 })
    renderDialog()
    await screen.findByText('iTunes/x/a.mp3')
    fireEvent.click(screen.getByTestId('organize-more'))
    await waitFor(() =>
      expect(mockPlan).toHaveBeenLastCalledWith(scope, 0, 200),
    )
  })
})

describe('organizeScope', () => {
  it('sends an album, a filter, or the selected Tracks', () => {
    expect(organizeScope({ albumId: 'a' })).toEqual({ albumId: 'a' })
    expect(organizeScope({ filter: {} })).toEqual({ filter: {} })
    expect(organizeScope({ ids: ['1'] })).toEqual({ ids: ['1'] })
  })
})
