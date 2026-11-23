import React, { useCallback, useState } from 'react'
import { Button, useTranslate } from 'react-admin'
import { MdMergeType } from 'react-icons/md'
import CaseMergeDialog from './CaseMergeDialog'

// The Library manager's case merge tool (#169): opens the next phase's plan; nothing is renamed
// or moved until the admin runs it.
const CaseMergeButton = () => {
  const translate = useTranslate()
  const [open, setOpen] = useState(false)
  const handleClose = useCallback(() => setOpen(false), [])
  return (
    <>
      <Button
        label={translate('resources.libraryTrack.actions.caseMerge')}
        onClick={() => setOpen(true)}
        data-testid="case-merge-button"
      >
        <MdMergeType />
      </Button>
      <CaseMergeDialog open={open} onClose={handleClose} />
    </>
  )
}

export default CaseMergeButton
