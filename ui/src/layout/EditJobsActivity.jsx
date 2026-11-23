import React, { useEffect, useState } from 'react'
import { useSelector } from 'react-redux'
import { useTranslate } from 'react-admin'
import {
  Box,
  CardContent,
  Divider,
  LinearProgress,
  Typography,
  makeStyles,
} from '@material-ui/core'
import { httpClient } from '../dataProvider'
import { REST_URL } from '../consts'

// How many of the latest edit jobs the panel shows.
const shown = 5

const useStyles = makeStyles((theme) => ({
  content: { padding: theme.spacing(2, 3), maxWidth: '28em' },
  job: { marginTop: theme.spacing(1) },
  path: { wordBreak: 'break-all' },
  skipped: { color: theme.palette.warning.main },
}))

// The codes a skipped File's reason is translated from; others show the server's own words.
const reasons = [
  'changedOnDisk',
  'editingOff',
  'invalid',
  'notFound',
  'unsupportedFormat',
  'notVerified',
  'linked',
  'oldCopyLeft',
  'interrupted',
  'alreadyUndone',
  'changedLater',
  'tagsChanged',
  'unfinished',
  'notUndoable',
]

// A File an undo job skipped is explained with the Edit history's messages, a tag edit's with
// the Edit dialog's.
const Reason = ({ step, kind }) => {
  const translate = useTranslate()
  const messages = kind === 'undo' ? 'editHistory' : 'song.tagEditor'
  return reasons.includes(step.code)
    ? translate(`resources.${messages}.errors.${step.code}`)
    : step.error || step.code
}

const EditJob = ({ job }) => {
  const translate = useTranslate()
  const classes = useStyles()
  const kind = translate(`activity.edits.kind.${job.kind}`, {
    _: job.kind,
  })
  return (
    <Box className={classes.job} data-testid={`edit-job-${job.id}`}>
      <Typography variant="body2">
        {kind} · {translate('activity.edits.files', { smart_count: job.total })}
      </Typography>
      {job.state === 'queued' && (
        <Typography variant="caption" color="textSecondary">
          {translate('activity.edits.queued')}
        </Typography>
      )}
      {job.state === 'running' && (
        <>
          <LinearProgress
            variant="determinate"
            value={job.total ? (100 * job.done) / job.total : 0}
          />
          <Typography variant="caption" display="block">
            {translate('activity.edits.progress', {
              done: job.done,
              total: job.total,
            })}
          </Typography>
          {job.current && (
            <Typography
              variant="caption"
              color="textSecondary"
              display="block"
              className={classes.path}
            >
              {translate('activity.edits.current', { path: job.current })}
            </Typography>
          )}
        </>
      )}
      {job.state === 'done' && job.skipped === 0 && (
        <Typography variant="caption" color="textSecondary">
          {translate('activity.edits.done')}
        </Typography>
      )}
      {job.skipped > 0 && (
        <>
          <Typography variant="caption" className={classes.skipped}>
            {translate('activity.edits.skipped', {
              smart_count: job.skipped,
            })}
          </Typography>
          {(job.steps || []).map((s) => (
            <Box key={s.id || s.path} ml={1}>
              <Typography
                variant="caption"
                display="block"
                className={classes.path}
              >
                {s.path}
              </Typography>
              <Typography
                variant="caption"
                display="block"
                color="textSecondary"
              >
                <Reason step={s} kind={job.kind} />
              </Typography>
            </Box>
          ))}
        </>
      )}
    </Box>
  )
}

// The latest edit jobs in the Activity panel (#162): a running job's progress and the File it
// is writing, and the Files a job skipped, with why. It reloads them on each edit job event.
export const EditJobsActivity = () => {
  const translate = useTranslate()
  const classes = useStyles()
  const event = useSelector((state) => state.activity.editJob)
  const [jobs, setJobs] = useState([])

  useEffect(() => {
    let active = true
    httpClient(`${REST_URL}/editJob?_end=${shown}`)
      .then(({ json }) => active && setJobs(Array.isArray(json) ? json : []))
      .catch(() => {})
    return () => {
      active = false
    }
  }, [event])

  if (jobs.length === 0) return null
  return (
    <>
      <Divider />
      <CardContent className={classes.content}>
        <Typography variant="subtitle2">
          {translate('activity.edits.title')}
        </Typography>
        {jobs.map((job) => (
          <EditJob key={job.id} job={job} />
        ))}
      </CardContent>
    </>
  )
}

export default EditJobsActivity
