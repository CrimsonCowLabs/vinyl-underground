import React from 'react'
import { render, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import * as RA from 'react-admin'
import RipForm from './RipForm.jsx'
import en from '../i18n/en.json'

const create = vi.fn()
const notify = vi.fn()
const refresh = vi.fn()

vi.mock('react-admin', async () => {
  const actual = await vi.importActual('react-admin')
  return {
    ...actual,
    useTranslate: () => (key) => key,
    useDataProvider: vi.fn(),
    useNotify: vi.fn(),
    useRefresh: vi.fn(),
  }
})

describe('RipForm', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    RA.useDataProvider.mockReturnValue({ create })
    RA.useNotify.mockReturnValue(notify)
    RA.useRefresh.mockReturnValue(refresh)
  })

  const submit = (url) => {
    const utils = render(<RipForm />)
    fireEvent.change(utils.getByLabelText('resources.rip.fields.url'), {
      target: { value: url },
    })
    fireEvent.click(
      utils.getByRole('button', { name: /resources.rip.actions.rip/ }),
    )
    return utils
  }

  it('queues a Rip of the pasted URL and refreshes the job list', async () => {
    create.mockResolvedValue({ data: { id: 'j1', status: 'queued' } })
    const { getByLabelText } = submit('  https://www.youtube.com/watch?v=abc ')

    await waitFor(() => expect(refresh).toHaveBeenCalled())
    expect(create).toHaveBeenCalledWith('rip', {
      data: { url: 'https://www.youtube.com/watch?v=abc' },
    })
    expect(notify).toHaveBeenCalledWith('resources.rip.messages.queued', 'info')
    expect(getByLabelText('resources.rip.fields.url').value).toBe('')
  })

  it("shows the server's reason when the URL is refused", async () => {
    create.mockRejectedValue({
      message: 'Bad Request',
      body: { error: 'enter the http(s) address of a web page with a video' },
    })
    const { getByLabelText } = submit('file:///etc/passwd')

    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        'enter the http(s) address of a web page with a video',
        'warning',
      ),
    )
    expect(refresh).not.toHaveBeenCalled()
    expect(getByLabelText('resources.rip.fields.url').value).toBe(
      'file:///etc/passwd',
    )
  })

  it("says why when the page couldn't be read, rather than that the Rip is queued", async () => {
    create.mockResolvedValue({
      data: {
        id: 'j1',
        status: 'failed',
        error: 'ERROR: [youtube] abc: Video unavailable',
      },
    })
    submit('https://www.youtube.com/watch?v=abc')

    await waitFor(() => expect(refresh).toHaveBeenCalled())
    expect(notify).toHaveBeenCalledWith(
      'ERROR: [youtube] abc: Video unavailable',
      'warning',
    )
    expect(notify).not.toHaveBeenCalledWith(
      'resources.rip.messages.queued',
      'info',
    )
  })

  describe('a playlist of more than 50 items', () => {
    const url = 'https://www.youtube.com/playlist?list=PL1'
    const needsConfirmation = {
      status: 409,
      message: 'Conflict',
      body: {
        error: 'this playlist has 51 items; confirm to rip them all',
        needsConfirmation: true,
        url,
        title: 'Big Playlist',
        count: 51,
        titles: ['Band - Song 1', 'Band - Song 2'],
      },
    }

    it('asks first, and rips them all once confirmed', async () => {
      create.mockRejectedValueOnce(needsConfirmation)
      create.mockResolvedValueOnce({ data: { id: 'j1', status: 'queued' } })
      const { findByRole, getByText, getByLabelText } = submit(url)

      const dialog = await findByRole('dialog')
      expect(dialog.textContent).toContain('Big Playlist')
      expect(getByText('Band - Song 1')).toBeInTheDocument()
      expect(refresh).not.toHaveBeenCalled()
      expect(notify).not.toHaveBeenCalled()

      fireEvent.click(
        getByText('resources.rip.actions.ripAll').closest('button'),
      )
      await waitFor(() => expect(refresh).toHaveBeenCalled())
      expect(create).toHaveBeenLastCalledWith('rip', {
        data: { url, confirm: true },
      })
      expect(getByLabelText('resources.rip.fields.url').value).toBe('')
    })

    it('queues nothing when the admin cancels', async () => {
      create.mockRejectedValueOnce(needsConfirmation)
      const { findByRole, getByText, queryByRole, getByLabelText } = submit(url)

      await findByRole('dialog')
      fireEvent.click(getByText('ra.action.cancel').closest('button'))
      await waitFor(() => expect(queryByRole('dialog')).toBeNull())
      expect(create).toHaveBeenCalledTimes(1)
      expect(getByLabelText('resources.rip.fields.url').value).toBe(url)
    })
  })

  it('shows why a playlist of more than 300 items is refused', async () => {
    create.mockRejectedValue({
      status: 422,
      body: {
        error:
          'this playlist has 1200 items, and at most 300 can be ripped at once',
      },
    })
    submit('https://www.youtube.com/playlist?list=PL1')
    await waitFor(() =>
      expect(notify).toHaveBeenCalledWith(
        'this playlist has 1200 items, and at most 300 can be ripped at once',
        'warning',
      ),
    )
  })

  it('suggests YouTube Music album links for the best metadata', () => {
    const { getByText } = render(<RipForm />)
    expect(getByText('resources.rip.messages.urlHelp')).toBeInTheDocument()
    expect(en.resources.rip.messages.urlHelp).toContain('music.youtube.com')
    expect(en.resources.rip.messages.urlHelp).toMatch(/album/)
  })

  it("doesn't send an empty URL", () => {
    const { getByRole } = render(<RipForm />)
    expect(
      getByRole('button', { name: /resources.rip.actions.rip/ }),
    ).toBeDisabled()
  })
})
