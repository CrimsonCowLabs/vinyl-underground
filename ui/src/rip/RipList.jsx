import React from 'react'
import {
  Datagrid,
  FunctionField,
  TextField,
  UrlField,
  useTranslate,
} from 'react-admin'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from '@material-ui/core'
import { List, DateField } from '../common'
import RipForm from './RipForm'
import { alreadyHaveExtra } from './alreadyHaveExtra'
import YtDlpControl from './YtDlpControl'
import { useRipPolling } from './useRipPolling'
import { ripProgress } from './ripProgress'
import RipJobPanel from './review/RipJobPanel'

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

const RipDatagrid = (props) => {
  useRipPolling()
  return <Datagrid {...props} />
}

const RipList = (props) => {
  return (
    <>
      <YtDlpControl />
      <RipForm />
      <List
        {...props}
        sort={{ field: 'createdAt', order: 'DESC' }}
        exporter={false}
        bulkActionButtons={false}
        actions={false}
        pagination={null}
      >
        <RipDatagrid
          expand={
            <RipJobPanel
              fallback={<RipItems />}
              trackExtra={alreadyHaveExtra}
            />
          }
        >
          <FunctionField
            source="status"
            render={(record) => <RipStatusField record={record} />}
            sortable={false}
          />
          <TextField source="title" sortable={false} />
          <TextField source="source" sortable={false} />
          <UrlField
            source="url"
            target="_blank"
            rel="noopener noreferrer"
            sortable={false}
          />
          <FunctionField source="path" render={filePath} sortable={false} />
          <TextField source="error" sortable={false} />
          <DateField source="createdAt" showTime sortable={false} />
        </RipDatagrid>
      </List>
    </>
  )
}

export default RipList
