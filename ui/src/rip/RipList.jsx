import React from 'react'
import {
  Datagrid,
  FunctionField,
  TextField,
  UrlField,
  useRefresh,
  useTranslate,
} from 'react-admin'
import { List, DateField } from '../common'
import { useInterval } from '../common/useInterval'
import RipForm from './RipForm'

// Jobs run on the server whether or not this page is open; while it is, it polls for their status.
const POLL_INTERVAL_MS = 3000

export const RipStatusField = ({ record }) => {
  const translate = useTranslate()
  if (!record) return null
  return translate(`resources.rip.status.${record.status}`, {
    _: record.status,
  })
}

const RipList = (props) => {
  const refresh = useRefresh()
  useInterval(refresh, POLL_INTERVAL_MS)

  return (
    <>
      <RipForm />
      <List
        {...props}
        sort={{ field: 'createdAt', order: 'DESC' }}
        exporter={false}
        bulkActionButtons={false}
        actions={false}
        pagination={null}
      >
        <Datagrid>
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
          <TextField source="path" sortable={false} />
          <TextField source="error" sortable={false} />
          <DateField source="createdAt" showTime sortable={false} />
        </Datagrid>
      </List>
    </>
  )
}

export default RipList
