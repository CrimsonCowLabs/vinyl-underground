import {
  Edit,
  FormDataConsumer,
  SimpleForm,
  TextInput,
  TextField,
  BooleanInput,
  required,
  useTranslate,
  usePermissions,
  ReferenceInput,
  SelectInput,
} from 'react-admin'
import { Redirect } from 'react-router-dom'
import { isWritable, Title } from '../common'

const SyncFragment = ({ formData, variant, ...rest }) => {
  return (
    <>
      {formData.path && <BooleanInput source="sync" {...rest} />}
      {formData.path && <TextField source="path" {...rest} />}
    </>
  )
}

const PlaylistTitle = ({ record }) => {
  const translate = useTranslate()
  const resourceName = translate('resources.playlist.name', { smart_count: 1 })
  return <Title subTitle={`${resourceName} "${record ? record.name : ''}"`} />
}

export const PlaylistEditForm = (props) => {
  const { record } = props
  const { permissions } = usePermissions()
  // A Playlist shared by another Listener is read-only, even for admins.
  if (record?.id && !isWritable(record.ownerId)) {
    return <Redirect to={`/playlist/${record.id}/show`} />
  }
  return (
    <SimpleForm redirect="list" variant={'outlined'} {...props}>
      <TextInput source="name" validate={required()} />
      <TextInput
        multiline
        minRows={3}
        source="comment"
        fullWidth
        inputProps={{
          style: { resize: 'vertical' },
        }}
      />
      {permissions === 'admin' ? (
        <ReferenceInput
          source="ownerId"
          reference="user"
          perPage={0}
          sort={{ field: 'name', order: 'ASC' }}
        >
          <SelectInput
            label={'resources.playlist.fields.ownerName'}
            optionText="userName"
          />
        </ReferenceInput>
      ) : (
        <TextField source="ownerName" />
      )}
      <BooleanInput source="public" disabled={!isWritable(record.ownerId)} />
      <FormDataConsumer>
        {(formDataProps) => <SyncFragment {...formDataProps} />}
      </FormDataConsumer>
    </SimpleForm>
  )
}

const PlaylistEdit = (props) => (
  <Edit title={<PlaylistTitle />} actions={false} {...props}>
    <PlaylistEditForm {...props} />
  </Edit>
)

export default PlaylistEdit
