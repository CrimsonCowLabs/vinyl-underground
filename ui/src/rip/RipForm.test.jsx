import React from 'react'
import { render, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import * as RA from 'react-admin'
import RipForm from './RipForm.jsx'

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

  it("doesn't send an empty URL", () => {
    const { getByRole } = render(<RipForm />)
    expect(
      getByRole('button', { name: /resources.rip.actions.rip/ }),
    ).toBeDisabled()
  })
})
