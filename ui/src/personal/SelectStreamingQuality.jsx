import { useEffect, useState } from 'react'
import {
  SelectInput,
  useDataProvider,
  useNotify,
  useTranslate,
} from 'react-admin'
import { decisionService } from '../transcode'

// Must match the Preference values in core/streamquality.
const PREFERENCES = ['auto', 'original', 'dataSaver']

export const SelectStreamingQuality = (props) => {
  const translate = useTranslate()
  const dataProvider = useDataProvider()
  const notify = useNotify()
  const [current, setCurrent] = useState(null)

  useEffect(() => {
    dataProvider
      .getStreamingQuality()
      .then(({ data }) => setCurrent(data.preference))
      .catch(() => setCurrent('auto'))
  }, [dataProvider])

  if (current === null) return null

  const choices = PREFERENCES.map((id) => ({
    id,
    name: translate(`menu.personal.options.streamingQuality.${id}`),
  }))

  return (
    <SelectInput
      {...props}
      source="streamingQuality"
      label={translate('menu.personal.options.streamingQuality.name')}
      helperText={translate('menu.personal.options.streamingQuality.help')}
      defaultValue={current}
      choices={choices}
      translateChoice={false}
      onChange={(event) => {
        const preference = event.target.value
        dataProvider
          .setStreamingQuality(preference)
          .then(() => {
            setCurrent(preference)
            // Decisions already fetched were made with the old setting.
            decisionService.invalidateAll()
          })
          .catch(() => notify('ra.page.error', 'warning'))
      }}
    />
  )
}
