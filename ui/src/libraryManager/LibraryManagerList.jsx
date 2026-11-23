import React, { cloneElement, useCallback, useState } from 'react'
import PropTypes from 'prop-types'
import { Link } from 'react-router-dom'
import {
  AutocompleteInput,
  Button,
  Datagrid,
  Filter,
  FunctionField,
  List,
  NumberField,
  Pagination,
  ReferenceInput,
  SearchInput,
  SelectArrayInput,
  SelectInput,
  TextField,
  TextInput,
  TopToolbar,
  useListContext,
  useRefresh,
  useTranslate,
  useUnselectAll,
} from 'react-admin'
import { Card, CardContent, Typography } from '@material-ui/core'
import EditIcon from '@material-ui/icons/Edit'
import { MdDeleteSweep, MdHistory } from 'react-icons/md'
import { RippedBadge } from '../common/RippedBadge'
import { useLibraryEditing } from '../library/useLibraryEditing'
import BulkEditDialog from './BulkEditDialog'
import { SendToQuarantineDialog } from '../quarantine/SendToQuarantineDialog'
import { matchingFilter } from './bulkEdit'
import OrganizeButton from '../organize/OrganizeButton'
import CaseMergeButton from '../organize/CaseMergeButton'

const resource = 'libraryTrack'

// The formats the Library holds (a File's suffix).
const formats = ['mp3', 'flac', 'm4a', 'aiff', 'wav', 'ogg', 'opus', 'wma']
const missingTags = ['album', 'album_artist', 'year', 'track_number']

const LibraryManagerFilter = (props) => {
  const translate = useTranslate()
  const choice = (group) => (id) => ({
    id,
    name: translate(`resources.libraryTrack.${group}.${id}`),
  })
  return (
    <Filter {...props} variant="outlined">
      <SearchInput source="title" alwaysOn />
      <TextInput source="folder" alwaysOn resettable />
      <ReferenceInput
        source="artists_id"
        reference="artist"
        sort={{ field: 'name', order: 'ASC' }}
        filterToQuery={(searchText) => ({ name: [searchText] })}
      >
        <AutocompleteInput emptyText="-- None --" />
      </ReferenceInput>
      <ReferenceInput
        source="album_id"
        reference="album"
        sort={{ field: 'name', order: 'ASC' }}
        filterToQuery={(searchText) => ({ name: [searchText] })}
      >
        <AutocompleteInput emptyText="-- None --" />
      </ReferenceInput>
      <SelectInput
        source="format"
        choices={formats.map((id) => ({ id, name: id }))}
      />
      <SelectInput
        source="rip"
        choices={['true', 'false'].map(choice('rip'))}
      />
      <SelectArrayInput
        source="missing_tag"
        choices={missingTags.map(choice('missingTag'))}
      />
    </Filter>
  )
}

// The bulk edit, of the Tracks selected (bulk action) or of all matching the filter (toolbar).
const BulkEditButton = ({ selection, count, label, disabled }) => {
  const translate = useTranslate()
  const refresh = useRefresh()
  const unselectAll = useUnselectAll()
  const [open, setOpen] = useState(false)
  const handleDone = useCallback(() => {
    setOpen(false)
    unselectAll(resource)
    refresh()
  }, [unselectAll, refresh])
  return (
    <>
      <Button
        label={translate(label, { smart_count: count })}
        onClick={() => setOpen(true)}
        disabled={disabled}
      >
        <EditIcon />
      </Button>
      <BulkEditDialog
        open={open}
        selection={selection}
        count={count}
        onClose={() => setOpen(false)}
        onDone={handleDone}
      />
    </>
  )
}

BulkEditButton.propTypes = {
  selection: PropTypes.object.isRequired,
  count: PropTypes.number,
  label: PropTypes.string.isRequired,
  disabled: PropTypes.bool,
}

// Sends the selected Tracks to Quarantine, as one job (#168).
const SendSelectedToQuarantine = ({ selectedIds }) => {
  const translate = useTranslate()
  const refresh = useRefresh()
  const unselectAll = useUnselectAll()
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button
        label={translate('resources.quarantine.actions.sendSelected', {
          smart_count: selectedIds.length,
        })}
        onClick={() => setOpen(true)}
      >
        <MdDeleteSweep />
      </Button>
      {open && (
        <SendToQuarantineDialog
          open
          selection={{ ids: selectedIds }}
          count={selectedIds.length}
          onClose={() => setOpen(false)}
          onDone={() => {
            setOpen(false)
            unselectAll(resource)
            refresh()
          }}
        />
      )}
    </>
  )
}

SendSelectedToQuarantine.propTypes = { selectedIds: PropTypes.array }

const SelectedBulkEdit = ({ selectedIds }) => {
  const refresh = useRefresh()
  const unselectAll = useUnselectAll()
  return (
    <>
      <BulkEditButton
        selection={{ ids: selectedIds }}
        count={selectedIds.length}
        label="resources.libraryTrack.actions.editSelected"
      />
      <OrganizeButton
        scope={{ ids: selectedIds }}
        count={selectedIds.length}
        label="resources.libraryTrack.actions.organizeSelected"
        onDone={() => {
          unselectAll(resource)
          refresh()
        }}
      />
      <SendSelectedToQuarantine selectedIds={selectedIds} />
    </>
  )
}

SelectedBulkEdit.propTypes = { selectedIds: PropTypes.array }

// OrganizeMatching organizes all the Tracks matching the filter (with none, the whole Library).
const OrganizeMatching = ({ filterValues, total }) => {
  const refresh = useRefresh()
  return (
    <OrganizeButton
      scope={{ filter: matchingFilter(filterValues) }}
      count={total}
      label="resources.libraryTrack.actions.organizeMatching"
      disabled={!total}
      onDone={refresh}
    />
  )
}

OrganizeMatching.propTypes = {
  filterValues: PropTypes.object,
  total: PropTypes.number,
}

// The Library manager's tools: a link ({ to, label, icon }), or a component given the list's
// filterValues and total: Organize (#167), the case merge (#169), the Edit history and Quarantine
// (#168).
const tools = [
  { key: 'organize', component: OrganizeMatching },
  { key: 'caseMerge', component: CaseMergeButton },
  {
    key: 'editHistory',
    to: '/editHistory',
    label: 'resources.libraryTrack.actions.editHistory',
    icon: MdHistory,
  },
  {
    key: 'quarantine',
    to: '/quarantine',
    label: 'resources.libraryTrack.actions.quarantine',
    icon: MdDeleteSweep,
  },
]

export const LibraryManagerActions = ({
  filters,
  resource,
  showFilter,
  displayedFilters,
}) => {
  const translate = useTranslate()
  const { filterValues, total } = useListContext()
  return (
    <TopToolbar>
      {filters &&
        cloneElement(filters, {
          resource,
          showFilter,
          displayedFilters,
          filterValues,
          context: 'button',
        })}
      <BulkEditButton
        selection={{ filter: matchingFilter(filterValues) }}
        count={total}
        label="resources.libraryTrack.actions.editMatching"
        disabled={!total}
      />
      {tools.map(({ key, to, label, icon: Icon, component: Tool }) =>
        Tool ? (
          <Tool key={key} filterValues={filterValues} total={total} />
        ) : (
          <Button key={key} component={Link} to={to} label={translate(label)}>
            <Icon />
          </Button>
        ),
      )}
    </TopToolbar>
  )
}

LibraryManagerActions.propTypes = {
  filters: PropTypes.element,
  resource: PropTypes.string,
  showFilter: PropTypes.func,
  displayedFilters: PropTypes.object,
}

const LibraryManagerPagination = (props) => (
  <Pagination rowsPerPageOptions={[25, 50, 100, 250]} {...props} />
)

const EditingOff = () => {
  const translate = useTranslate()
  return (
    <Card>
      <CardContent>
        <Typography>
          {translate('resources.libraryTrack.message.editingOff')}
        </Typography>
      </CardContent>
    </Card>
  )
}

// The Library manager (#166, ADR-0011): the admin's page for cleaning up the Library in bulk.
// One row per Track, with its main tags and its File's path, format and bitrate, paged, sorted
// and filtered on the server, so it stays responsive over the whole Library. Tracks whose File
// is missing aren't listed: they can't be edited.
const LibraryManagerList = (props) => {
  const editing = useLibraryEditing()
  if (!editing.loaded) return null
  if (!editing.enabled) return <EditingOff />
  return (
    <List
      {...props}
      resource={resource}
      filter={{ missing: false }}
      filters={<LibraryManagerFilter />}
      actions={<LibraryManagerActions />}
      bulkActionButtons={<SelectedBulkEdit />}
      sort={{ field: 'path', order: 'ASC' }}
      exporter={false}
      perPage={50}
      pagination={<LibraryManagerPagination />}
    >
      <Datagrid>
        <FunctionField
          source="title"
          render={(r) => (
            <>
              {r.title} <RippedBadge record={r} />
            </>
          )}
        />
        <TextField source="artist" />
        <TextField source="album" />
        <TextField source="albumArtist" sortable={false} />
        <NumberField source="year" />
        <NumberField source="trackNumber" sortable={false} />
        <TextField source="path" />
        <TextField source="suffix" sortable={false} />
        <NumberField source="bitRate" sortable={false} />
      </Datagrid>
    </List>
  )
}

export default LibraryManagerList
