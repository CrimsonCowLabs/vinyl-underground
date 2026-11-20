import * as React from 'react'
import { TestContext } from 'ra-test'
import { DataProviderContext, SimpleForm } from 'react-admin'
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import { describe, afterEach, it, expect, vi } from 'vitest'
import { SelectStreamingQuality } from './SelectStreamingQuality'
import { decisionService } from '../transcode'

const renderSelect = (dataProvider) =>
  render(
    <DataProviderContext.Provider value={dataProvider}>
      <TestContext initialState={{ admin: { ui: { optimistic: false } } }}>
        <SimpleForm toolbar={null}>
          <SelectStreamingQuality />
        </SimpleForm>
      </TestContext>
    </DataProviderContext.Provider>,
  )

const choose = async (label) => {
  fireEvent.mouseDown(screen.getByRole('button'))
  const option = await screen.findByRole('option', { name: label })
  fireEvent.click(option)
}

describe('SelectStreamingQuality', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it("shows the Listener's saved setting", async () => {
    const dataProvider = {
      getStreamingQuality: vi
        .fn()
        .mockResolvedValue({ data: { preference: 'original' } }),
      setStreamingQuality: vi.fn(),
    }
    renderSelect(dataProvider)

    expect(
      await screen.findByText(
        'menu.personal.options.streamingQuality.original',
      ),
    ).toBeInTheDocument()
  })

  it('saves a new choice and drops cached stream decisions', async () => {
    const invalidateAll = vi.spyOn(decisionService, 'invalidateAll')
    const dataProvider = {
      getStreamingQuality: vi
        .fn()
        .mockResolvedValue({ data: { preference: 'auto' } }),
      setStreamingQuality: vi
        .fn()
        .mockResolvedValue({ data: { preference: 'dataSaver' } }),
    }
    renderSelect(dataProvider)
    await screen.findByText('menu.personal.options.streamingQuality.auto')

    await choose('menu.personal.options.streamingQuality.dataSaver')

    await waitFor(() =>
      expect(dataProvider.setStreamingQuality.mock.calls[0]?.[0]).toBe(
        'dataSaver',
      ),
    )
    await waitFor(() => expect(invalidateAll).toHaveBeenCalled())
  })
})
