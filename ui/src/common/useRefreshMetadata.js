import { useCallback } from 'react'
import { useDataProvider, useNotify } from 'react-admin'

// Rescans the record's folders, then refreshes its artwork and external info. The request
// answers once the scan is done (the scan's own refresh event reloads the page); the returned
// promise always resolves, after notifying.
export const useRefreshMetadata = () => {
  const dataProvider = useDataProvider()
  const notify = useNotify()

  return useCallback(
    (resource, id) =>
      dataProvider
        .refreshMetadata(resource, id)
        .then(() => notify('message.metadataRefreshed'))
        .catch((error) =>
          notify(
            error?.status === 409
              ? 'message.metadataRefreshBusy'
              : 'ra.page.error',
            'warning',
          ),
        ),
    [dataProvider, notify],
  )
}
