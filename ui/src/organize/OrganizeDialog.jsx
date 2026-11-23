import React, { useCallback, useEffect, useState } from 'react'
import PropTypes from 'prop-types'
import { useNotify, useTranslate } from 'react-admin'
import {
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import { planOrganize, reasons, runOrganize } from './organize'

const organizeApi = { plan: planOrganize, run: runOrganize }

const useStyles = makeStyles((theme) => ({
  path: { wordBreak: 'break-all' },
  old: { color: theme.palette.text.secondary, wordBreak: 'break-all' },
  section: { marginTop: theme.spacing(2) },
  summary: { display: 'block' },
}))

// The rows of each list shown at a time; the counts cover the whole plan.
const pageRows = 100

// The refusals the server answers with, each explained; anything else is a generic error.
const knownErrors = [
  'editingOff',
  'noSelection',
  'tooMany',
  'planChanged',
  'nothingToMove',
  // Promote's (#171)
  'notReady',
  'nothingToPromote',
]

const errorKey = (err) => {
  const code = err?.body?.error
  return `resources.organize.errors.${knownErrors.includes(code) ? code : 'unknown'}`
}

const reasonKey = (status) =>
  `resources.organize.reasons.${reasons.includes(status) ? status : 'unknown'}`

// One move of the plan: the File's path now, and the one it goes to.
const MoveRow = ({ row }) => {
  const classes = useStyles()
  return (
    <TableRow>
      <TableCell>
        <span className={classes.old}>{row.path}</span>
        <br />
        <span className={classes.path}>→ {row.newPath}</span>
      </TableCell>
    </TableRow>
  )
}

MoveRow.propTypes = { row: PropTypes.object.isRequired }

// A File the plan leaves where it is, and why.
const LeftRow = ({ row }) => {
  const classes = useStyles()
  const translate = useTranslate()
  return (
    <TableRow>
      <TableCell className={classes.path}>{row.path}</TableCell>
      <TableCell>{translate(reasonKey(row.status))}</TableCell>
    </TableRow>
  )
}

LeftRow.propTypes = { row: PropTypes.object.isRequired }

const Plan = ({ plan, onMore, busy }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const more =
    plan.moves.length < plan.totalMoves || plan.left.length < plan.totalLeft
  return (
    <>
      <DialogContentText>
        <span className={classes.summary}>
          {translate('resources.organize.summary.tracks', {
            smart_count: plan.tracks,
          })}
        </span>
        {plan.files > 0 && (
          <span className={classes.summary}>
            {translate('resources.organize.summary.files', {
              smart_count: plan.files,
            })}
          </span>
        )}
        {plan.inPlace > 0 && (
          <span className={classes.summary}>
            {translate('resources.organize.summary.inPlace', {
              smart_count: plan.inPlace,
            })}
          </span>
        )}
        {plan.totalLeft > 0 && (
          <span className={classes.summary}>
            {translate('resources.organize.summary.left', {
              smart_count: plan.totalLeft,
            })}
          </span>
        )}
      </DialogContentText>
      {plan.totalLeft > 0 && (
        <div className={classes.section}>
          <Typography variant="subtitle2">
            {translate('resources.organize.leftTitle')}
          </Typography>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>
                  {translate('resources.organize.fields.path')}
                </TableCell>
                <TableCell>
                  {translate('resources.organize.fields.reason')}
                </TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {plan.left.map((r) => (
                <LeftRow key={r.path} row={r} />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      {plan.totalMoves > 0 && (
        <div className={classes.section}>
          <Typography variant="subtitle2">
            {translate('resources.organize.movesTitle')}
          </Typography>
          <Table size="small">
            <TableBody>
              {plan.moves.map((r) => (
                <MoveRow key={r.path} row={r} />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      {more && (
        <Button onClick={onMore} disabled={busy} data-testid="organize-more">
          {translate('resources.organize.more')}
        </Button>
      )}
    </>
  )
}

Plan.propTypes = {
  plan: PropTypes.object.isRequired,
  onMore: PropTypes.func.isRequired,
  busy: PropTypes.bool,
}

// Organize (#167): shows the plan for the scope (every move, old → new path, and each File it
// can't place, with why), and runs it, as one edit job, only once the admin asks. The job can be
// undone as a whole from the Edit history. Promote (#171) is Organize from the Inbox: it shows its
// plan here too, with its own plan and run (api) and help text (helpKey).
const OrganizeDialog = ({
  open,
  scope,
  title,
  onClose,
  onDone,
  api = organizeApi,
  helpKey = 'resources.organize.help',
}) => {
  const translate = useTranslate()
  const notify = useNotify()
  const [plan, setPlan] = useState(null)
  const [busy, setBusy] = useState(false)

  // load shows the plan from its first rows.
  const load = useCallback(
    () => {
      setBusy(true)
      return api
        .plan(scope, 0, pageRows)
        .then(setPlan)
        .catch((err) => {
          notify(errorKey(err), { type: 'warning' })
          onClose()
        })
        .finally(() => setBusy(false))
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [JSON.stringify(scope), notify, onClose],
  )

  useEffect(() => {
    if (!open) {
      setPlan(null)
      return
    }
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  // handleMore adds the next rows of both lists. Should the plan have changed meanwhile, it is
  // shown again from the start.
  const handleMore = () => {
    const start = Math.max(plan.moves.length, plan.left.length)
    setBusy(true)
    api
      .plan(scope, start, start + pageRows)
      .then((next) => {
        if (next.token !== plan.token) {
          notify('resources.organize.errors.planChanged', { type: 'warning' })
          return load()
        }
        setPlan({
          ...plan,
          moves: plan.moves.concat(next.moves),
          left: plan.left.concat(next.left),
        })
      })
      .catch((err) => notify(errorKey(err), { type: 'warning' }))
      .finally(() => setBusy(false))
  }

  const handleRun = useCallback(() => {
    setBusy(true)
    api
      .run(scope, plan.token)
      .then(({ job, background }) => {
        if (background) {
          notify('resources.organize.inBackground', { type: 'info' })
        } else if (job.skipped > 0) {
          notify('resources.organize.doneSkipped', {
            type: 'warning',
            messageArgs: { smart_count: job.skipped },
          })
        } else {
          notify('resources.organize.done', { type: 'info' })
        }
        setBusy(false)
        onDone()
      })
      .catch((err) => {
        notify(errorKey(err), { type: 'warning' })
        setBusy(false)
        // The Library changed since: the plan is shown again, as it is now.
        if (err?.body?.error === 'planChanged') load()
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(scope), plan, notify, onDone])

  return (
    <Dialog
      open={open}
      onClose={busy ? undefined : onClose}
      maxWidth="md"
      fullWidth
      aria-labelledby="organize-title"
    >
      <DialogTitle id="organize-title">{title}</DialogTitle>
      <DialogContent>
        <DialogContentText>{translate(helpKey)}</DialogContentText>
        {plan ? (
          <Plan plan={plan} onMore={handleMore} busy={busy} />
        ) : (
          <CircularProgress size={24} />
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={busy}>
          {translate('ra.action.cancel')}
        </Button>
        <Button
          color="primary"
          variant="contained"
          onClick={handleRun}
          disabled={busy || !plan || plan.tracks === 0}
          data-testid="organize-run"
        >
          {translate('resources.organize.run', {
            smart_count: plan?.tracks || 0,
          })}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

OrganizeDialog.propTypes = {
  open: PropTypes.bool.isRequired,
  // { albumId }, { ids } or { filter } (see organizeScope).
  scope: PropTypes.object.isRequired,
  title: PropTypes.string.isRequired,
  onClose: PropTypes.func.isRequired,
  onDone: PropTypes.func.isRequired,
  // { plan(scope, start, end), run(scope, token) }: Organize's API unless given (Promote's).
  api: PropTypes.shape({ plan: PropTypes.func, run: PropTypes.func }),
  helpKey: PropTypes.string,
}

export default OrganizeDialog
