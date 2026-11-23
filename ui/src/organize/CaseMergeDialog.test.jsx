import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import CaseMergeDialog from './CaseMergeDialog'

const { mockPlan, mockRun, mockNotify } = vi.hoisted(() => ({
  mockPlan: vi.fn(),
  mockRun: vi.fn(),
  mockNotify: vi.fn(),
}))

vi.mock('./caseMerge', async (importOriginal) => ({
  ...(await importOriginal()),
  planCaseMerge: mockPlan,
  runCaseMerge: mockRun,
}))

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  useNotify: () => mockNotify,
  useTranslate: () => (key, opts) =>
    opts && Object.keys(opts).length ? `${key} ${JSON.stringify(opts)}` : key,
}))

const renamePlan = {
  token: 'tok-1',
  phase: 'rename',
  counts: { rename: 1 },
  clashes: [
    { libraryId: 1, parent: 'Music', names: ['DIRE STRAITS', 'Dire Straits'] },
  ],
  steps: [
    {
      libraryId: 1,
      op: 'rename',
      path: 'Music/DIRE STRAITS',
      newPath: 'Music/DIRE STRAITS [case-merge]',
    },
  ],
  decide: [],
  totalClashes: 1,
  totalSteps: 1,
  totalDecide: 0,
}

const mergePlan = {
  token: 'tok-2',
  phase: 'merge',
  counts: { move: 1, rmdir: 1 },
  clashes: [],
  steps: [
    {
      libraryId: 1,
      op: 'move',
      path: 'Music/DIRE STRAITS [case-merge]/b.mp3',
      newPath: 'Music/Dire Straits/b.mp3',
    },
    { libraryId: 1, op: 'rmdir', path: 'Music/DIRE STRAITS [case-merge]' },
  ],
  decide: [
    {
      libraryId: 1,
      op: 'decide',
      path: 'Music/DIRE STRAITS [case-merge]/a.mp3',
      newPath: 'Music/Dire Straits/a.mp3',
    },
  ],
  totalClashes: 0,
  totalSteps: 2,
  totalDecide: 1,
}

const nothing = {
  token: 'tok-3',
  phase: '',
  counts: {},
  clashes: [],
  steps: [],
  decide: [],
  totalClashes: 0,
  totalSteps: 0,
  totalDecide: 0,
}

describe('CaseMergeDialog', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lists the sets of twins and the renames before anything is renamed', async () => {
    mockPlan.mockResolvedValue(renamePlan)
    render(<CaseMergeDialog open onClose={vi.fn()} />)
    await screen.findByText('Music/DIRE STRAITS · Music/Dire Straits')
    expect(
      screen.getByText('resources.caseMerge.phase.rename'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('→ Music/DIRE STRAITS [case-merge]'),
    ).toBeInTheDocument()
    expect(mockRun).not.toHaveBeenCalled()
  })

  it('runs the plan reviewed, then shows the next phase, with the clashes left for the admin', async () => {
    mockPlan.mockResolvedValueOnce(renamePlan).mockResolvedValueOnce(mergePlan)
    mockRun.mockResolvedValue({ job: { skipped: 0 }, background: false })
    render(<CaseMergeDialog open onClose={vi.fn()} />)
    await screen.findByText('Music/DIRE STRAITS · Music/Dire Straits')
    fireEvent.click(screen.getByTestId('case-merge-run'))
    await waitFor(() => expect(mockRun).toHaveBeenCalledWith('tok-1'))
    await screen.findByText('resources.caseMerge.phase.merge')
    expect(mockNotify).toHaveBeenCalledWith('resources.caseMerge.done', {
      type: 'info',
    })
    expect(
      screen.getByText('resources.caseMerge.decideTitle'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('Music/DIRE STRAITS [case-merge]/a.mp3'),
    ).toBeInTheDocument()
  })

  it('has nothing to run once no twins are left', async () => {
    mockPlan.mockResolvedValue(nothing)
    render(<CaseMergeDialog open onClose={vi.fn()} />)
    await screen.findByText('resources.caseMerge.phase.none')
    expect(screen.getByTestId('case-merge-run')).toBeDisabled()
  })

  it('shows the plan again when the Library changed since it was made', async () => {
    mockPlan.mockResolvedValueOnce(renamePlan).mockResolvedValueOnce(nothing)
    mockRun.mockRejectedValue({ body: { error: 'planChanged' } })
    render(<CaseMergeDialog open onClose={vi.fn()} />)
    await screen.findByText('Music/DIRE STRAITS · Music/Dire Straits')
    fireEvent.click(screen.getByTestId('case-merge-run'))
    await screen.findByText('resources.caseMerge.phase.none')
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.caseMerge.errors.planChanged',
      { type: 'warning' },
    )
  })
})
