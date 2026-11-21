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

  it('opens the tooltip for keyboard users too', async () => {
    renderBadge({ title: 'Song', source: 'YouTube', sourceUrl: url })

    const badge = screen.getByLabelText('Ripped from YouTube')
    badge.focus()
    expect(badge).toHaveFocus()

    expect(await screen.findByRole('tooltip')).toHaveTextContent(url)
  })

  it('renders nothing for a Track that is not a Rip', () => {
    const { container } = renderBadge({ title: 'Bought on CD' })

    expect(container).toBeEmptyDOMElement()
  })

  // Only the app's ripper (yt-dlp, #28) writes a web SOURCEURL. CD rippers and other
  // taggers may write SOURCE (e.g. "CD") but are not Rips.
  it('renders nothing for a File another tool tagged SOURCE=CD, with no URL', () => {
    const { container } = renderBadge({ title: 'From CD', source: 'CD' })

    expect(container).toBeEmptyDOMElement()
  })

  it.each([
    ['a file path', 'file:///music/cd/track01.flac'],
    ['a disc ID', 'cddb:7a0b4c09'],
    ['a script URL', 'javascript:alert(1)'],
    ['plain text', 'my CD collection'],
    ['an http URL with no host', 'http://'],
  ])('renders nothing when SOURCEURL is %s', (_, sourceUrl) => {
    const { container } = renderBadge({
      title: 'Song',
      source: 'CD',
      sourceUrl,
    })

    expect(container).toBeEmptyDOMElement()
  })

  it('accepts a plain http URL', () => {
    renderBadge({
      title: 'Song',
      source: 'Bandcamp',
      sourceUrl: 'http://artist.bandcamp.com/track/song',
    })

    expect(screen.getByLabelText('Ripped from Bandcamp')).toBeInTheDocument()
  })

  it('names the URL host when the File has a URL but no SOURCE', () => {
    renderBadge({ title: 'Song', sourceUrl: url })

    expect(
      screen.getByLabelText('Ripped from www.youtube.com'),
    ).toBeInTheDocument()
  })

  it('renders nothing without a record', () => {
    const { container } = renderBadge(undefined)

    expect(container).toBeEmptyDOMElement()
  })
})
