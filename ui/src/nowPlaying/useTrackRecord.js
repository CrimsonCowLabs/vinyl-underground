import { useGetOne } from 'react-admin'

// useTrackRecord is the Track playing as the server has it now, so a Star or
// rating given elsewhere shows. With the server out of reach (a Download
// played offline) it is the Track as it was queued. A Track queued from a
// Playlist carries its position there as its id, so it goes by the trackId.
export const useTrackRecord = (current) => {
  const id = current?.isRadio ? undefined : current?.trackId
  const { data } = useGetOne('song', id, { enabled: !!id })
  return { ...current?.song, mediaFileId: undefined, ...(data || {}), id }
}
