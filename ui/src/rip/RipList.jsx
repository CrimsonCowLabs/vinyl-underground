import React from 'react'
import {
  Datagrid,
  FunctionField,
  TextField,
  UrlField,
  useTranslate,
} from 'react-admin'
import { List, DateField } from '../common'
import RipForm from './RipForm'
import { useRipPolling } from './useRipPolling'

export const RipStatusField = ({ record }) => {
  const translate = useTranslate()
  if (!record) return null
  return translate(`resources.rip.status.${record.status}`, {
    _: record.status,
  })
}

const RipDatagrid = (props) => {
  useRipPolling()
  return <Datagrid {...props} />
}

const RipList = (props) => {
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
        <RipDatagrid>
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
        </RipDatagrid>
      </List>
    </>
  )
}

export default RipList
