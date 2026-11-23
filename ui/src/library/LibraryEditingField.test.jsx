import React from 'react'
import { render } from '@testing-library/react'
import { TestContext } from 'ra-test'
import LibraryEditingField from './LibraryEditingField'

const renderField = (isWritable) =>
  render(
    <TestContext>
      <LibraryEditingField
        source="editing"
        record={{ id: 1, name: 'Music' }}
        isWritable={isWritable}
      />
    </TestContext>,
  )

describe('LibraryEditingField', () => {
  it('shows a writable library as editable', () => {
    const { container } = renderField(() => true)
    expect(container.querySelector('[data-testid="true"]')).not.toBeNull()
  })

  it('shows a read-only library as not editable', () => {
    const { container } = renderField(() => false)
    expect(container.querySelector('[data-testid="false"]')).not.toBeNull()
  })

  it('is blank for a library that was not checked', () => {
    const { container } = renderField(() => undefined)
    expect(container).toBeEmptyDOMElement()
  })
})
