import React from 'react'
import PropTypes from 'prop-types'
import { useTranslate } from 'react-admin'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Typography,
} from '@material-ui/core'
import { Alert } from '@material-ui/lab'
import { makeStyles } from '@material-ui/core/styles'
import {
  MAX_DISC,
  MAX_TRACK,
  duplicateNumbers,
  numberValid,
} from './trackNumbers'

const useStyles = makeStyles((theme) => ({
  number: { width: '4.5em', paddingRight: theme.spacing(1) },
  title: { overflowWrap: 'anywhere' },
  input: { textAlign: 'right' },
}))

// The album's Tracks with their disc and track numbers, to correct in the Edit dialog. A warning
// shows when two Tracks of a disc share a number.
export const TrackNumbersEditor = ({ rows, onChange, disabled }) => {
  const translate = useTranslate()
  const classes = useStyles()
  if (!rows?.length) return null
  const set = (id, field, value) =>
    onChange(rows.map((r) => (r.id === id ? { ...r, [field]: value } : r)))
  const duplicates = duplicateNumbers(rows)

  const numberField = (r, field, label, max) => (
    <TextField
      value={r[field]}
      onChange={(e) => set(r.id, field, e.target.value)}
      disabled={disabled}
      error={
        !numberValid(r[field], field === 'disc' ? r.fromDisc : r.fromTrack, max)
      }
      inputProps={{
        inputMode: 'numeric',
        className: classes.input,
        'aria-label': `${label}: ${r.title}`,
        'data-testid': `${field === 'disc' ? 'disc' : 'track'}-number-${r.id}`,
      }}
    />
  )
  const discLabel = translate('resources.album.editor.discNumber')
  const trackLabel = translate('resources.album.editor.trackNumber')

  return (
    <>
      <Typography variant="subtitle2" style={{ marginTop: 16 }}>
        {translate('resources.album.editor.trackNumbers')}
      </Typography>
      <Table size="small">
        <TableHead>
          <TableRow>
            <TableCell className={classes.number}>{discLabel}</TableCell>
            <TableCell className={classes.number}>{trackLabel}</TableCell>
            <TableCell>{translate('resources.song.fields.title')}</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map((r) => (
            <TableRow key={r.id}>
              <TableCell className={classes.number}>
                {numberField(r, 'disc', discLabel, MAX_DISC)}
              </TableCell>
              <TableCell className={classes.number}>
                {numberField(r, 'track', trackLabel, MAX_TRACK)}
              </TableCell>
              <TableCell className={classes.title}>{r.title}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {duplicates.length > 0 && (
        <Alert severity="warning" style={{ marginTop: 8 }}>
          {duplicates.map((d) => (
            <div key={`${d.disc}-${d.track}`}>
              {translate('resources.album.editor.duplicateNumber', {
                disc: d.disc || '–',
                track: d.track,
                titles: d.titles.join(', '),
              })}
            </div>
          ))}
        </Alert>
      )}
    </>
  )
}

TrackNumbersEditor.propTypes = {
  rows: PropTypes.arrayOf(PropTypes.object),
  onChange: PropTypes.func.isRequired,
  disabled: PropTypes.bool,
}

export default TrackNumbersEditor
