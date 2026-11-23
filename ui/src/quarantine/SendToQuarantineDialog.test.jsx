import React from 'react'
import { render, fireEvent, waitFor, screen } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { SendToQuarantineDialog } from './SendToQuarantineDialog'

const { mockSend, mockNotify } = vi.hoisted(() => ({
  mockSend: vi.fn(),
  mockNotify: vi.fn(),
}))

vi.mock('./quarantine', async (importOriginal) => ({
  ...(await importOriginal()),
  sendToQuarantine: mockSend,
}))

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  useNotify: () => mockNotify,
  useTranslate: () => (key) => key,
}))

const renderDialog = (selection = { albumId: 'al-1' }) => {
  const onDone = vi.fn()
  render(
    <SendToQuarantineDialog
      open
      selection={selection}
      count={2}
      onClose={vi.fn()}
      onDone={onDone}
    />,
  )
  return { onDone }
}

describe('<SendToQuarantineDialog />', () => {
  beforeEach(() => vi.clearAllMocks())

  it('says the Files move, and nothing is deleted, before it sends the selection as one job', async () => {
    mockSend.mockResolvedValue({ job: { skipped: 0 }, background: false })
    const { onDone } = renderDialog()
    expect(screen.getByText('resources.quarantine.send.content')).toBeTruthy()
    expect(mockSend).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText('resources.quarantine.send.confirm'))
    await waitFor(() => expect(onDone).toHaveBeenCalled())
    expect(mockSend).toHaveBeenCalledWith({ albumId: 'al-1' })
    expect(mockNotify).toHaveBeenCalledWith(
      'resources.quarantine.notifications.sent',
      { type: 'info', messageArgs: { smart_count: 2 } },
    )
  })

  it('says how many Files were skipped', async () => {
    mockSend.mockResolvedValue({ job: { skipped: 1 }, background: false })
    renderDialog({ ids: ['t1', 't2'] })
    fireEvent.click(screen.getByText('resources.quarantine.send.confirm'))
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.quarantine.notifications.sentSkipped',
        { type: 'warning', messageArgs: { smart_count: 1 } },
      ),
    )
  })

  it('explains a refusal', async () => {
    mockSend.mockRejectedValue({ body: { error: 'inQuarantine' } })
    const { onDone } = renderDialog()
    fireEvent.click(screen.getByText('resources.quarantine.send.confirm'))
    await waitFor(() =>
      expect(mockNotify).toHaveBeenCalledWith(
        'resources.quarantine.errors.inQuarantine',
        { type: 'warning' },
      ),
    )
    expect(onDone).not.toHaveBeenCalled()
  })
})
