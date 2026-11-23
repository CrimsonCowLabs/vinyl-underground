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
import CorrectionMigration from './CorrectionMigration'

const useStyles = makeStyles((theme) => ({
  arrow: { margin: theme.spacing(0, 1), color: theme.palette.text.secondary },
}))

// What stays app data once the Corrections kept before ADR-0011 are written into the Files
// (#165): an image marked wrong for an album, and a MusicBrainz release picked for one (an album
// Correction holding only the release, until #170 writes it). Only these can be undone; the
// others are written into the Files by the migration.
const isAppData = (record) =>
  !record.undoing &&
  (record.kind === 'albumCoverBlock' ||
    (record.kind === 'album' &&
      !record.albumName &&
      !record.albumArtist &&
      !record.year))

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

// How many Tracks app data applies to now (none means it is orphaned); a Correction still to be
// written into the Files says so.
export const CorrectionStatusField = ({ record }) => {
  const translate = useTranslate()
  if (!record) return null
  if (!isAppData(record)) {
    return (
      <Chip
        size="small"
        color="primary"
        label={translate('resources.correction.status.toMigrate')}
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
  albumCoverBlock: 'resources.correction.message.undoContentAlbumCoverBlock',
  album: 'resources.correction.message.undoContentRelease',
}

// Undo deletes a cover block or a picked release; the album's cover is resolved again.
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
      .then(() =>
        notify('resources.correction.notifications.undone', { type: 'info' }),
      )
      .catch((err) =>
        notify(
          err?.body?.error === 'migrateFirst'
            ? 'resources.correction.errors.migrateFirst'
            : 'ra.page.error',
          { type: 'warning' },
        ),
      )
      .finally(() => {
        setRunning(false)
        refresh()
      })
  }, [dataProvider, record, notify, refresh])

  if (!record || !isAppData(record)) return null
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
        content={undoContent[record.kind]}
        translateOptions={{ from: record.from, to: record.to }}
        onConfirm={handleConfirm}
        onClose={() => setOpen(false)}
      />
    </>
  )
}

UndoCorrectionButton.propTypes = { record: PropTypes.object }

// Every Correction, newest first (the server returns them all, unpaged and pre-sorted), below the
// migration that writes those kept before ADR-0011 into the Files.
const CorrectionList = (props) => {
  const translate = useTranslate()
  return (
    <>
      <CorrectionMigration />
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
    </>
  )
}

export default CorrectionList
