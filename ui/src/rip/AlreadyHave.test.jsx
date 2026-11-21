import React from 'react'
import { render } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { AlreadyHave } from './AlreadyHave.jsx'

vi.mock('react-admin', async () => {
  const actual = await vi.importActual('react-admin')
  return { ...actual, useTranslate: () => (key) => key }
})

const library = {
  id: 'mf-1',
  title: 'Halo',
  artist: 'Beyoncé',
  album: 'I Am... Sasha Fierce',
  libraryName: 'Music Library',
}
const rip = {
  id: 'mf-2',
  title: 'Halo (Official Video)',
  artist: 'Beyoncé',
  album: '',
  libraryName: 'Inbox',
  source: 'YouTube',
  sourceUrl: 'https://www.youtube.com/watch?v=bnVUHWCynig',
}

describe('AlreadyHave', () => {
  it('lists the Tracks the Library already seems to have: artist, title, album and library', () => {
    const { container, getByText } = render(<AlreadyHave tracks={[library]} />)
    expect(getByText('resources.rip.messages.alreadyHave')).toBeInTheDocument()
    expect(container.textContent).toContain('Beyoncé - Halo')
    expect(container.textContent).toContain('I Am... Sasha Fierce')
    expect(container.textContent).toContain('Music Library')
  })

  it('shows the "Ripped" badge for a Track that is a Rip', () => {
    const { getAllByText } = render(<AlreadyHave tracks={[library, rip]} />)
    expect(getAllByText('resources.song.ripped.label')).toHaveLength(1)
  })

  it('renders nothing without matches', () => {
    const { container } = render(<AlreadyHave tracks={[]} />)
    expect(container).toBeEmptyDOMElement()
    expect(render(<AlreadyHave />).container).toBeEmptyDOMElement()
  })
})
