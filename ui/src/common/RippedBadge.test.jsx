import * as React from 'react'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { describe, afterEach, it, expect } from 'vitest'
import { TranslationProvider } from 'react-admin'
import polyglotI18nProvider from 'ra-i18n-polyglot'
import en from '../i18n/en.json'
import { RippedBadge } from './RippedBadge'

const i18nProvider = polyglotI18nProvider(() => en, 'en')

const renderBadge = (record) =>
  render(
    <TranslationProvider i18nProvider={i18nProvider}>
      <RippedBadge record={record} />
    </TranslationProvider>,
  )

const url = 'https://www.youtube.com/watch?v=dQw4w9WgXcQ'

describe('<RippedBadge />', () => {
  afterEach(cleanup)

  it('labels a Rip as "Ripped", naming the site for screen readers', () => {
    renderBadge({ title: 'Song', source: 'YouTube', sourceUrl: url })

    expect(screen.getByText('Ripped')).toBeInTheDocument()
    expect(screen.getByLabelText('Ripped from YouTube')).toBeInTheDocument()
  })

  it('shows the site and source URL in a tooltip', async () => {
    renderBadge({ title: 'Song', source: 'YouTube', sourceUrl: url })

    fireEvent.mouseOver(screen.getByText('Ripped'))

    const tooltip = await screen.findByRole('tooltip')
    expect(tooltip).toHaveTextContent('Ripped from YouTube')
    expect(tooltip).toHaveTextContent(url)
  })

  it('renders nothing for a Track that is not a Rip', () => {
    const { container } = renderBadge({ title: 'Bought on CD' })

    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing without a record', () => {
    const { container } = renderBadge(undefined)

    expect(container).toBeEmptyDOMElement()
  })
})
