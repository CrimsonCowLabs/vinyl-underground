import React from 'react'
import { render, screen, waitFor, act } from '@testing-library/react'
import { Provider } from 'react-redux'
import { createStore, combineReducers } from 'redux'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { EditJobsActivity } from './EditJobsActivity'
import { activityReducer } from '../reducers'
import { EVENT_EDIT_JOB } from '../actions'

const { mockHttpClient } = vi.hoisted(() => ({ mockHttpClient: vi.fn() }))
vi.mock('../dataProvider', () => ({ httpClient: mockHttpClient }))

vi.mock('react-admin', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    useTranslate: () => (key, opts) =>
      opts ? `${key} ${JSON.stringify(opts)}` : key,
  }
})

const running = {
  id: 'job-2',
  kind: 'tags',
  state: 'running',
  total: 3,
  done: 1,
  skipped: 0,
  current: 'Artist/Album/02 Two.mp3',
  steps: [],
}
const finished = {
  id: 'job-1',
  kind: 'tags',
  state: 'done',
  total: 2,
  done: 2,
  skipped: 1,
  current: '',
  steps: [
    {
      path: 'Artist/Album/01 One.mp3',
      state: 'skipped',
      code: 'changedOnDisk',
      error: 'the File changed on disk, rescan first',
    },
  ],
}

describe('<EditJobsActivity />', () => {
  let store
  const show = () =>
    render(
      <Provider store={store}>
        <EditJobsActivity />
      </Provider>,
    )

  beforeEach(() => {
    vi.clearAllMocks()
    store = createStore(combineReducers({ activity: activityReducer }))
    mockHttpClient.mockResolvedValue({ json: [running, finished] })
  })

  it("shows a running job's progress and the File being written", async () => {
    show()
    await waitFor(() =>
      screen.getByText('activity.edits.progress {"done":1,"total":3}'),
    )
    expect(mockHttpClient).toHaveBeenCalledWith(
      expect.stringContaining('/editJob'),
    )
    expect(
      screen.getByText(
        'activity.edits.current {"path":"Artist/Album/02 Two.mp3"}',
      ),
    ).toBeTruthy()
  })

  it('lists the Files a finished job skipped, and why', async () => {
    show()
    await waitFor(() => screen.getByText('Artist/Album/01 One.mp3'))
    expect(
      screen.getByText('activity.edits.skipped {"smart_count":1}'),
    ).toBeTruthy()
    expect(
      screen.getByText('resources.song.tagEditor.errors.changedOnDisk'),
    ).toBeTruthy()
  })

  it("adds the server's detail to the reason, when it says more", async () => {
    mockHttpClient.mockResolvedValue({
      json: [
        {
          ...finished,
          steps: [
            {
              path: 'Artist/Album/01 One.mp3',
              state: 'skipped',
              code: 'notVerified',
              error:
                "the edited copy didn't check out, so the File was left as it was: COMMENT changed too, to []",
            },
            {
              path: 'Artist/Album/02 Two.mp3',
              state: 'skipped',
              code: 'linked',
              error:
                "the File is a link (a symlink, or one of several hard links), so it can't be replaced: /music/Artist/Album/02 Two.mp3",
            },
          ],
        },
      ],
    })
    show()
    await waitFor(() =>
      screen.getByText(
        'resources.song.tagEditor.errors.notVerified (COMMENT changed too, to [])',
      ),
    )
    // A detail that only repeats the File's path adds nothing.
    expect(
      screen.getByText('resources.song.tagEditor.errors.linked'),
    ).toBeTruthy()
  })

  it("explains a File an undo job skipped with the Edit history's message", async () => {
    mockHttpClient.mockResolvedValue({
      json: [
        {
          ...finished,
          kind: 'undo',
          steps: [{ ...finished.steps[0], code: 'interrupted' }],
        },
      ],
    })
    show()
    await waitFor(() =>
      screen.getByText('resources.editHistory.errors.interrupted'),
    )
  })

  it('shows nothing when there are no edit jobs', async () => {
    mockHttpClient.mockResolvedValue({ json: [] })
    const { container } = show()
    await waitFor(() => expect(mockHttpClient).toHaveBeenCalled())
    expect(container.textContent).toBe('')
  })

  it('follows the progress live, as edit job events come', async () => {
    show()
    await waitFor(() =>
      screen.getByText('activity.edits.progress {"done":1,"total":3}'),
    )
    mockHttpClient.mockResolvedValue({
      json: [{ ...running, done: 2, current: 'Artist/Album/03 Three.mp3' }],
    })
    act(() => {
      store.dispatch({
        type: EVENT_EDIT_JOB,
        data: { id: 'job-2', state: 'running', total: 3, done: 2 },
      })
    })
    await waitFor(() =>
      screen.getByText('activity.edits.progress {"done":2,"total":3}'),
    )
    expect(mockHttpClient).toHaveBeenCalledTimes(2)
  })
})
