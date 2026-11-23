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
  TableRow,
  Typography,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import { planCaseMerge, runCaseMerge, stepOps } from './caseMerge'

const useStyles = makeStyles((theme) => ({
  path: { wordBreak: 'break-all' },
  old: { color: theme.palette.text.secondary, wordBreak: 'break-all' },
  section: { marginTop: theme.spacing(2) },
  summary: { display: 'block' },
}))

// The rows of each list shown at a time; the counts cover the whole plan.
const pageRows = 100

const knownErrors = ['editingOff', 'planChanged', 'nothingToMerge']

const errorKey = (err) => {
  const code = err?.body?.error
  return `resources.caseMerge.errors.${knownErrors.includes(code) ? code : 'unknown'}`
}

// One step: what it does, the path now and the one it goes to.
const StepRow = ({ step }) => {
  const classes = useStyles()
  const translate = useTranslate()
  return (
    <TableRow>
      <TableCell>{translate(`resources.caseMerge.op.${step.op}`)}</TableCell>
      <TableCell>
        <span className={classes.old}>{step.path}</span>
        {step.newPath && (
          <>
            <br />
            <span className={classes.path}>→ {step.newPath}</span>
          </>
        )}
      </TableCell>
    </TableRow>
  )
}

StepRow.propTypes = { step: PropTypes.object.isRequired }

const Section = ({ title, children }) => {
  const classes = useStyles()
  return (
    <div className={classes.section}>
      <Typography variant="subtitle2">{title}</Typography>
      <Table size="small">
        <TableBody>{children}</TableBody>
      </Table>
    </div>
  )
}

Section.propTypes = {
  title: PropTypes.string.isRequired,
  children: PropTypes.node,
}

const Plan = ({ plan, onMore, busy }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const more =
    plan.clashes.length < plan.totalClashes ||
    plan.steps.length < plan.totalSteps ||
    plan.decide.length < plan.totalDecide
  return (
    <>
      <DialogContentText>
        <span className={classes.summary}>
          <strong>
            {translate(`resources.caseMerge.phase.${plan.phase || 'none'}`)}
          </strong>
        </span>
        {plan.totalClashes > 0 && (
          <span className={classes.summary}>
            {translate('resources.caseMerge.summary.clashes', {
              smart_count: plan.totalClashes,
            })}
          </span>
        )}
        {stepOps
          .filter((op) => plan.counts?.[op] > 0)
          .map((op) => (
            <span key={op} className={classes.summary}>
              {translate(`resources.caseMerge.summary.${op}`, {
                smart_count: plan.counts[op],
              })}
            </span>
          ))}
        {plan.totalDecide > 0 && (
          <span className={classes.summary}>
            {translate('resources.caseMerge.summary.decide', {
              smart_count: plan.totalDecide,
            })}
          </span>
        )}
      </DialogContentText>
      {plan.totalClashes > 0 && (
        <Section title={translate('resources.caseMerge.clashesTitle')}>
          {plan.clashes.map((c) => (
            <TableRow key={`${c.libraryId}/${c.parent}/${c.names.join('|')}`}>
              <TableCell className={classes.path}>
                {c.names
                  .map((n) => (c.parent ? `${c.parent}/${n}` : n))
                  .join(' · ')}
              </TableCell>
            </TableRow>
          ))}
        </Section>
      )}
      {plan.totalSteps > 0 && (
        <Section title={translate('resources.caseMerge.stepsTitle')}>
          {plan.steps.map((s) => (
            <StepRow key={`${s.libraryId}/${s.op}/${s.path}`} step={s} />
          ))}
        </Section>
      )}
      {plan.totalDecide > 0 && (
        <Section title={translate('resources.caseMerge.decideTitle')}>
          {plan.decide.map((s) => (
            <StepRow key={`${s.libraryId}/${s.path}`} step={s} />
          ))}
        </Section>
      )}
      {more && (
        <Button onClick={onMore} disabled={busy} data-testid="case-merge-more">
          {translate('resources.caseMerge.more')}
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

// The case merge (#169): shows the next phase's plan (the sets of twins, each step, and the
// clashes left for the admin), and runs it as one edit job only once the admin asks. After a run
// the plan is shown again, for the next pass or phase; each run can be undone from the Edit
// history.
const CaseMergeDialog = ({ open, onClose }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const [plan, setPlan] = useState(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    setBusy(true)
    return planCaseMerge(0, pageRows)
      .then(setPlan)
      .catch((err) => {
        notify(errorKey(err), { type: 'warning' })
        onClose()
      })
      .finally(() => setBusy(false))
  }, [notify, onClose])

  useEffect(() => {
    if (!open) {
      setPlan(null)
      return
    }
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const handleMore = () => {
    const start = Math.max(
      plan.clashes.length,
      plan.steps.length,
      plan.decide.length,
    )
    setBusy(true)
    planCaseMerge(start, start + pageRows)
      .then((next) => {
        if (next.token !== plan.token) {
          notify('resources.caseMerge.errors.planChanged', { type: 'warning' })
          return load()
        }
        setPlan({
          ...plan,
          clashes: plan.clashes.concat(next.clashes),
          steps: plan.steps.concat(next.steps),
          decide: plan.decide.concat(next.decide),
        })
      })
      .catch((err) => notify(errorKey(err), { type: 'warning' }))
      .finally(() => setBusy(false))
  }

  const handleRun = () => {
    setBusy(true)
    runCaseMerge(plan.token)
      .then(({ job, background }) => {
        if (background) {
          notify('resources.caseMerge.inBackground', { type: 'info' })
          onClose()
          return
        }
        if (job.skipped > 0) {
          notify('resources.caseMerge.doneSkipped', {
            type: 'warning',
            messageArgs: { smart_count: job.skipped },
          })
        } else {
          notify('resources.caseMerge.done', { type: 'info' })
        }
        // The next pass, or phase, as the Library is now.
        return load()
      })
      .catch((err) => {
        notify(errorKey(err), { type: 'warning' })
        if (err?.body?.error === 'planChanged') return load()
      })
      .finally(() => setBusy(false))
  }

  return (
    <Dialog
      open={open}
      onClose={busy ? undefined : onClose}
      maxWidth="md"
      fullWidth
      aria-labelledby="case-merge-title"
    >
      <DialogTitle id="case-merge-title">
        {translate('resources.caseMerge.title')}
      </DialogTitle>
      <DialogContent>
        <DialogContentText>
          {translate('resources.caseMerge.help')}
        </DialogContentText>
        {plan ? (
          <Plan plan={plan} onMore={handleMore} busy={busy} />
        ) : (
          <CircularProgress size={24} />
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={busy}>
          {translate('ra.action.close')}
        </Button>
        <Button
          color="primary"
          variant="contained"
          onClick={handleRun}
          disabled={busy || !plan || !plan.phase}
          data-testid="case-merge-run"
        >
          {translate('resources.caseMerge.run')}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

CaseMergeDialog.propTypes = {
  open: PropTypes.bool.isRequired,
  onClose: PropTypes.func.isRequired,
}

export default CaseMergeDialog
