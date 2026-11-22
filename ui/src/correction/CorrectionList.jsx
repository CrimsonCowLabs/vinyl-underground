import React, { useCallback, useState } from 'react'
import PropTypes from 'prop-types'
import {
  Button,
  Confirm,
  Datagrid,
  DateField,
  FunctionField,
  List,
  TextField,
  useDataProvider,
  useNotify,
  useRefresh,
  useTranslate,
} from 'react-admin'
import { Chip, Typography } from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import UndoIcon from '@material-ui/icons/Undo'

const useStyles = makeStyles((theme) => ({
  arrow: { margin: theme.spacing(0, 1), color: theme.palette.text.secondary },
}))

// "Greatful Dead → Grateful Dead"
export const CorrectionChangeField = ({ record }) => {
  const classes = useStyles()
  if (!record) return null
  return (
    <Typography variant="body2" component="span">
      <span>{record.from}</span>
      <span className={classes.arrow} aria-hidden>
        →
      </span>
      <span>{record.to}</span>
    </Typography>
  )
}

CorrectionChangeField.propTypes = { record: PropTypes.object }

// How many Tracks the Correction applies to now; none means it is orphaned.
export const CorrectionStatusField = ({ record }) => {
  const translate = useTranslate()
  if (!record) return null
  if (record.undoing) {
    return (
      <Chip
        size="small"
        label={translate('resources.correction.status.undoing')}
      />
    )
  }
  if (!record.trackCount) {
    return (
      <Chip
        size="small"
        color="secondary"
        label={translate('resources.correction.status.orphaned')}
      />
    )
  }
  return (
    <Typography variant="body2" component="span">
      {translate('resources.correction.status.tracks', {
        smart_count: record.trackCount,
      })}
    </Typography>
  )
}

CorrectionStatusField.propTypes = { record: PropTypes.object }

// What undoing says, by kind
const undoContent = {
  albumCover: 'resources.correction.message.undoContentAlbumCover',
  albumCoverBlock: 'resources.correction.message.undoContentAlbumCoverBlock',
  default: 'resources.correction.message.undoContent',
}

// Undo deletes the Correction and rescans the folders it applied to, restoring the tags' values.
export const UndoCorrectionButton = ({ record }) => {
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const notify = useNotify()
  const refresh = useRefresh()
  const [open, setOpen] = useState(false)
  const [running, setRunning] = useState(false)

  const handleConfirm = useCallback(() => {
    setOpen(false)
    setRunning(true)
    dataProvider
      .undoCorrection(record.id)
      .then(({ data }) =>
        notify(
          data.done
            ? 'resources.correction.notifications.undone'
            : 'resources.correction.notifications.undoing',
          { type: 'info' },
        ),
      )
      .catch((err) =>
        notify(
          err?.body?.error === 'undoLaterFirst'
            ? 'resources.correction.errors.undoLaterFirst'
            : 'ra.page.error',
          { type: 'warning' },
        ),
      )
      .finally(() => {
        setRunning(false)
        refresh()
      })
  }, [dataProvider, record, notify, refresh])

  if (!record) return null
  return (
    <>
      <Button
        label={translate('resources.correction.actions.undo')}
        onClick={(e) => {
          e.stopPropagation()
          setOpen(true)
        }}
        disabled={running}
      >
        <UndoIcon />
      </Button>
      <Confirm
        isOpen={open}
        title="resources.correction.message.undoTitle"
        content={undoContent[record.kind] ?? undoContent.default}
        translateOptions={{ from: record.from, to: record.to }}
        onConfirm={handleConfirm}
        onClose={() => setOpen(false)}
      />
    </>
  )
}

UndoCorrectionButton.propTypes = { record: PropTypes.object }

// Every Correction, newest first. The server returns them all, unpaged and pre-sorted.
const CorrectionList = (props) => {
  const translate = useTranslate()
  return (
    <List
      {...props}
      exporter={false}
      bulkActionButtons={false}
      pagination={null}
      perPage={1000}
      sort={{ field: 'createdAt', order: 'DESC' }}
    >
      <Datagrid>
        <FunctionField
          source="kind"
          sortable={false}
          render={(r) => translate(`resources.correction.kinds.${r.kind}`)}
        />
        <FunctionField
          source="change"
          sortable={false}
          render={(r) => <CorrectionChangeField record={r} />}
        />
        <TextField source="createdBy" sortable={false} />
        <DateField source="createdAt" showTime sortable={false} />
        <FunctionField
          source="status"
          sortable={false}
          render={(r) => <CorrectionStatusField record={r} />}
        />
        <UndoCorrectionButton />
      </Datagrid>
    </List>
  )
}

export default CorrectionList
