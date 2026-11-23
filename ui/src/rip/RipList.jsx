import React from 'react'
import { Datagrid, FunctionField, TextField, useTranslate } from 'react-admin'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from '@material-ui/core'
import { makeStyles } from '@material-ui/core/styles'
import { List, DateField, OverflowTooltip } from '../common'
import RipForm from './RipForm'
import { alreadyHaveExtra } from './alreadyHaveExtra'
import YtDlpControl from './YtDlpControl'
import { useRipPolling } from './useRipPolling'
import { ripProgress } from './ripProgress'
import RipJobPanel from './review/RipJobPanel'
import PromoteButton from './review/PromoteButton'
import UploadForm from './upload/UploadForm'
import JobOrigin from './JobOrigin'
import config from '../config'
import { useWiderThan } from './useWiderThan'

const ACTIVE = ['queued', 'downloading']

// RipStatusField shows a job's status. A playlist's shows how many Tracks are done while it rips,
// and a ready job with failed Tracks says how many failed.
export const RipStatusField = ({ record }) => {
  const translate = useTranslate()
  if (!record) return null
  const status = translate(`resources.rip.status.${record.status}`, {
    _: record.status,
  })
  const { total, done, failed } = ripProgress(record)
  if (ACTIVE.includes(record.status) && total > 1) {
    return translate('resources.rip.messages.progress', { status, done, total })
  }
  if (record.status === 'ready' && failed > 0) {
    return translate('resources.rip.messages.readyWithFailures', {
      smart_count: failed,
    })
  }
  return status
}

// RipItems lists a job's Tracks, each with its own status, error and File. trackExtra(item)
// renders next to an item's title, as on the review card.
export const RipItems = ({ record, trackExtra }) => {
  const translate = useTranslate()
  const items = record?.items || []
  return (
    <Table size="small">
      <TableHead>
        <TableRow>
          <TableCell>#</TableCell>
          <TableCell>{translate('resources.rip.fields.title')}</TableCell>
          <TableCell>{translate('resources.rip.fields.status')}</TableCell>
          <TableCell>{translate('resources.rip.fields.path')}</TableCell>
        </TableRow>
      </TableHead>
      <TableBody>
        {items.map((it) => (
          <TableRow key={it.id}>
            <TableCell>{it.position}</TableCell>
            <TableCell>
              {it.artist ? `${it.artist} - ${it.title}` : it.title}
              {trackExtra && trackExtra(it)}
            </TableCell>
            <TableCell>
              {translate(`resources.rip.status.${it.status}`, {
                _: it.status,
              })}
              {it.error && (
                <Typography variant="caption" color="error" display="block">
                  {it.error}
                </Typography>
              )}
            </TableCell>
            <TableCell>{it.path}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

// The File column: a single video's File, or the folder a playlist's Files go into.
const filePath = (record) =>
  record.folder || (record.items?.length === 1 ? record.items[0].path : '')

// Narrower than this, the job table drops its Source and File columns, so the
// rest still fit beside the Now Playing panel. A job's Files are on its
// expanded row anyway.
export const ALL_COLUMNS_WIDTH = 1100

const useStyles = makeStyles(
  {
    table: {
      // Narrower than this the cut-short columns would be too thin to read,
      // so the table scrolls sideways in its card instead (see Layout)
      minWidth: 560,
      // max-width: 0 lets the URL, File and error columns shrink below their
      // text; they share the room the other columns leave
      '& td.column-url, & td.column-path, & td.column-error': {
        maxWidth: 0,
        width: '18%',
      },
      // Otherwise the cut-short columns squeeze a title to a word a line
      '& td.column-title': {
        minWidth: '8em',
      },
    },
    clipped: {
      display: 'block',
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      whiteSpace: 'nowrap',
    },
  },
  { name: 'NDRipList' },
)

// Clipped shows its text (or children) on one line, cut short with an
// ellipsis if it doesn't fit, with the full text on hover when it is cut.
export const Clipped = ({ title, children }) => {
  const classes = useStyles()
  if (!title && !children) return null
  return (
    <OverflowTooltip title={title || ''}>
      <span className={classes.clipped}>{children || title}</span>
    </OverflowTooltip>
  )
}

// The URL column: a Rip's link, or where an upload or drop came from
const originField = (record) => (
  <Clipped title={record.url}>
    <JobOrigin record={record} />
  </Clipped>
)

// promoteActions puts Promote all in the review card's actions slot.
const promoteActions = (props) => <PromoteButton {...props} />

const RipDatagrid = (props) => {
  useRipPolling()
  return <Datagrid {...props} />
}

const RipList = (props) => {
  const classes = useStyles()
  // Measures the page itself, not the window, so the Now Playing panel
  // opening or closing re-flows the table straight away
  const [pageRef, allColumns] = useWiderThan(ALL_COLUMNS_WIDTH)
  return (
    <div ref={pageRef}>
      {config.enableRipping && <YtDlpControl />}
      {config.enableRipping && <RipForm />}
      {config.enableInbox && <UploadForm />}
      <List
        {...props}
        sort={{ field: 'createdAt', order: 'DESC' }}
        exporter={false}
        bulkActionButtons={false}
        actions={false}
        pagination={null}
      >
        <RipDatagrid
          className={classes.table}
          expand={
            <RipJobPanel
              fallback={<RipItems />}
              trackExtra={alreadyHaveExtra}
              actions={promoteActions}
            />
          }
        >
          <FunctionField
            source="status"
            render={(record) => <RipStatusField record={record} />}
            sortable={false}
          />
          <TextField source="title" sortable={false} />
          {allColumns && <TextField source="source" sortable={false} />}
          <FunctionField source="url" render={originField} sortable={false} />
          {allColumns && (
            <FunctionField
              source="path"
              render={(record) => <Clipped title={filePath(record)} />}
              sortable={false}
            />
          )}
          <FunctionField
            source="error"
            render={(record) => <Clipped title={record.error} />}
            sortable={false}
          />
          <DateField source="createdAt" showTime sortable={false} />
        </RipDatagrid>
      </List>
    </div>
  )
}

export default RipList
