import React, { useCallback, useState } from 'react'
import PropTypes from 'prop-types'
import { Button, useTranslate } from 'react-admin'
import { MdDriveFileMove } from 'react-icons/md'
import OrganizeDialog from './OrganizeDialog'
import { organizeScope } from './organize'

// The Organize button (#167): opens the plan for its scope (an album, selected Tracks, or all
// matching the Library manager's filter); nothing moves until the admin runs it.
const OrganizeButton = ({ scope, label, count, disabled, onDone, size }) => {
  const translate = useTranslate()
  const [open, setOpen] = useState(false)
  const handleClose = useCallback(() => setOpen(false), [])
  const handleDone = useCallback(() => {
    setOpen(false)
    onDone && onDone()
  }, [onDone])
  const text = translate(label, { smart_count: count || 0 })
  return (
    <>
      <Button
        label={text}
        onClick={(e) => {
          e.stopPropagation()
          setOpen(true)
        }}
        disabled={disabled}
        size={size}
        data-testid="organize-button"
      >
        <MdDriveFileMove />
      </Button>
      <OrganizeDialog
        open={open}
        scope={organizeScope(scope)}
        title={text}
        onClose={handleClose}
        onDone={handleDone}
      />
    </>
  )
}

OrganizeButton.propTypes = {
  scope: PropTypes.object.isRequired,
  label: PropTypes.string.isRequired,
  count: PropTypes.number,
  disabled: PropTypes.bool,
  onDone: PropTypes.func,
  size: PropTypes.string,
}

export default OrganizeButton
