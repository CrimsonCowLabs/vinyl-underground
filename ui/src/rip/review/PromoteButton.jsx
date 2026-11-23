import React, { useCallback, useMemo, useState } from 'react'
import { useRefresh, useTranslate } from 'react-admin'
import { Button, Tooltip } from '@material-ui/core'
import config from '../../config'
import { httpClient } from '../../dataProvider'
import OrganizeDialog from '../../organize/OrganizeDialog'
import { jobUrl } from './reviewApi'

// promoteApi is Promote's plan and run (#171), for the Organize dialog: Organize from the Inbox,
// the job's Files to their place in the Library.
const promoteApi = (jobId) => ({
  plan: (_scope, start = 0, end = 100) =>
    httpClient(`${jobUrl(jobId)}/promote/plan?_start=${start}&_end=${end}`, {
      method: 'POST',
    }).then(({ json }) => json),
  run: (_scope, token) =>
    httpClient(`${jobUrl(jobId)}/promote`, {
      method: 'POST',
      body: JSON.stringify({ token }),
    }).then(({ status, json }) => ({ job: json, background: status === 202 })),
})

// PromoteButton is a ready job's "Promote all" (#171): it shows where each of the job's Files will
// land in the Library's organised tree (Organize's plan, with the Files it can't place and why),
// and once the admin runs it, moves them there as one job, undoable from the Edit history. The
// list then refreshes, so a job whose Files have all moved goes. It isn't there while Promote is
// off (no writable Library), and waits for unsaved edits (dirty) to be saved, as the plan follows
// the tags the Files have.
const PromoteButton = ({ jobId, review, dirty }) => {
  const translate = useTranslate()
  const refresh = useRefresh()
  const [open, setOpen] = useState(false)
  const api = useMemo(() => promoteApi(jobId), [jobId])
  const scope = useMemo(() => ({ jobId }), [jobId])
  const handleClose = useCallback(() => setOpen(false), [])
  const handleDone = useCallback(() => {
    setOpen(false)
    refresh()
  }, [refresh])

  if (!config.enablePromote) return null

  const files = (review?.tracks || []).filter((t) => t.path)
  const button = (
    <Button
      color="primary"
      variant="contained"
      size="small"
      onClick={() => setOpen(true)}
      disabled={dirty || files.length === 0}
    >
      {translate('resources.rip.promote.button')}
    </Button>
  )

  return (
    <>
      {dirty ? (
        <Tooltip title={translate('resources.rip.promote.unsaved')}>
          {/* A disabled button gets no hover events: the span does. */}
          <span>{button}</span>
        </Tooltip>
      ) : (
        button
      )}
      {open && (
        <OrganizeDialog
          open={open}
          scope={scope}
          title={translate('resources.rip.promote.title')}
          helpKey="resources.rip.promote.help"
          api={api}
          onClose={handleClose}
          onDone={handleDone}
        />
      )}
    </>
  )
}

export default PromoteButton
