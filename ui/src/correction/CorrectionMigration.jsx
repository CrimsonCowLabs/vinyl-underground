import React, { useCallback, useEffect, useState } from 'react'
import PropTypes from 'prop-types'
import {
  Button,
  Confirm,
  useDataProvider,
  useNotify,
  useRefresh,
  useTranslate,
} from 'react-admin'
import {
  Card,
  CardContent,
  Chip,
  List,
  ListItem,
  ListItemText,
  Typography,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import SaveAltIcon from '@material-ui/icons/SaveAlt'

const useStyles = makeStyles((theme) => ({
  card: { marginBottom: theme.spacing(2) },
  flag: { marginRight: theme.spacing(1), marginTop: theme.spacing(0.5) },
  change: { display: 'block' },
}))

// A flag's code, as the migration page explains it; an unknown code shows as it is.
const codeKeys = [
  'missing',
  'changedOnDisk',
  'unsupportedFormat',
  'editingOff',
  'imageMissing',
  'linked',
  'notVerified',
  'noAlbumFolder',
  'sharedFolder',
  'invalidImage',
  'interrupted',
]

export const FileFlags = ({ flags, label }) => {
  const classes = useStyles()
  const translate = useTranslate()
  if (!flags?.length) return null
  return (
    <div>
      <Typography variant="caption" color="textSecondary">
        {translate(label, { smart_count: flags.length })}
      </Typography>
      <div>
        {flags.map((f, i) => (
          <Chip
            key={`${f.path}-${i}`}
            className={classes.flag}
            size="small"
            color="secondary"
            variant="outlined"
            title={f.error}
            label={`${f.path ? `${f.path}: ` : ''}${
              codeKeys.includes(f.code)
                ? translate(`resources.correction.migration.codes.${f.code}`)
                : f.code
            }`}
          />
        ))}
      </div>
    </div>
  )
}

FileFlags.propTypes = { flags: PropTypes.array, label: PropTypes.string }

// One Correction, and what writing it changes in its Files.
export const MigrationItem = ({ item }) => {
  const classes = useStyles()
  const translate = useTranslate()
  const { correction, changes, files, flags, keepsRelease, lastRun } = item
  return (
    <ListItem divider alignItems="flex-start">
      <ListItemText
        disableTypography
        primary={
          <Typography variant="subtitle2">
            {`${translate(`resources.correction.kinds.${correction.kind}`)}: ${correction.from} → ${correction.to}`}
          </Typography>
        }
        secondary={
          <div>
            {changes.map((c, i) => (
              <Typography
                key={`${c.field}-${i}`}
                variant="body2"
                className={classes.change}
              >
                {translate(`resources.correction.migration.fields.${c.field}`)}
                {': '}
                {c.from ? `${c.from} → ${c.to}` : c.to}
              </Typography>
            ))}
            <Typography variant="body2" color="textSecondary">
              {files > 0
                ? translate('resources.correction.migration.files', {
                    smart_count: files,
                  })
                : translate('resources.correction.migration.noFiles')}
            </Typography>
            {keepsRelease && (
              <Typography variant="body2" color="textSecondary">
                {translate('resources.correction.migration.keepsRelease')}
              </Typography>
            )}
            <FileFlags
              flags={flags}
              label="resources.correction.migration.flagged"
            />
            <FileFlags
              flags={lastRun}
              label="resources.correction.migration.lastRun"
            />
          </div>
        }
      />
    </ListItem>
  )
}

MigrationItem.propTypes = { item: PropTypes.object.isRequired }

// The Correction migration (#165): every Correction kept before ADR-0011, applied whenever a
// File's tags were read, listed with what writing it into its Files changes and the Files it
// can't write. Approving it writes them all as one edit job, which the Edit history can undo;
// each Correction goes once its Files are written. Shown only while there are some.
const CorrectionMigration = () => {
  const classes = useStyles()
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const notify = useNotify()
  const refresh = useRefresh()
  const [plan, setPlan] = useState(null)
  const [open, setOpen] = useState(false)
  const [running, setRunning] = useState(false)

  const load = useCallback(
    () =>
      dataProvider
        .getCorrectionMigration()
        .then(({ data }) => setPlan(data))
        .catch(() => setPlan(null)),
    [dataProvider],
  )
  useEffect(() => {
    load()
  }, [load])

  const handleConfirm = useCallback(() => {
    setOpen(false)
    setRunning(true)
    dataProvider
      .migrateCorrections()
      .then(({ data }) => {
        if (data.background) {
          notify('resources.correction.migration.notifications.background', {
            type: 'info',
          })
        } else if (data.job?.skipped > 0) {
          notify('resources.correction.migration.notifications.skipped', {
            type: 'warning',
            messageArgs: { smart_count: data.job.skipped },
          })
        } else {
          notify('resources.correction.migration.notifications.done', {
            type: 'info',
          })
        }
      })
      .catch((err) =>
        notify(
          err?.body?.error === 'editingOff'
            ? 'resources.correction.migration.notifications.editingOff'
            : 'ra.page.error',
          { type: 'warning' },
        ),
      )
      .finally(() => {
        setRunning(false)
        load()
        refresh()
      })
  }, [dataProvider, notify, refresh, load])

  if (!plan?.items?.length) return null
  return (
    <Card className={classes.card}>
      <CardContent>
        <Typography variant="h6">
          {translate('resources.correction.migration.title')}
        </Typography>
        <Typography variant="body2" color="textSecondary">
          {translate('resources.correction.migration.help')}
        </Typography>
        <List dense>
          {plan.items.map((item) => (
            <MigrationItem key={item.correction.id} item={item} />
          ))}
        </List>
        <Button
          variant="contained"
          label={translate('resources.correction.migration.approve', {
            smart_count: plan.files,
          })}
          onClick={() => setOpen(true)}
          disabled={running}
        >
          <SaveAltIcon />
        </Button>
        <Confirm
          isOpen={open}
          title="resources.correction.migration.confirmTitle"
          content="resources.correction.migration.confirmContent"
          translateOptions={{ smart_count: plan.files }}
          onConfirm={handleConfirm}
          onClose={() => setOpen(false)}
        />
      </CardContent>
    </Card>
  )
}

export default CorrectionMigration
