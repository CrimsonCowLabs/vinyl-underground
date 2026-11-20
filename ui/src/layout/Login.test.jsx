import * as React from 'react'
import { cleanup, render, screen } from '@testing-library/react'
import { describe, it, expect, vi, afterEach } from 'vitest'
import config from '../config'
import { InsightsNotice } from './Login'

vi.mock('react-admin', async (importOriginal) => ({
  ...(await importOriginal()),
  useTranslate: () => () => 'Usage data is collected. See [details] for more.',
}))

describe('<InsightsNotice />', () => {
  const original = config.enableInsightsCollector

  afterEach(() => {
    config.enableInsightsCollector = original
    cleanup()
  })

  it('is shown when the insights collector is enabled', () => {
    config.enableInsightsCollector = true
    render(<InsightsNotice url="https://example.com/insights" />)

    expect(screen.queryByText(/Usage data is collected/)).not.toBeNull()
  })

  it('is hidden when the insights collector is disabled, the default', () => {
    config.enableInsightsCollector = false
    render(<InsightsNotice url="https://example.com/insights" />)

    expect(screen.queryByText(/Usage data is collected/)).toBeNull()
  })
})
