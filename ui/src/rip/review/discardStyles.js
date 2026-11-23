import { alpha, makeStyles } from '@material-ui/core/styles'

// useDiscardStyles marks Discard as destructive, in the theme's error colour, as the Delete
// buttons for a Library or a user are: outlined on the card, filled in the dialog that confirms
// it, and a red bin for one Track.
export const useDiscardStyles = makeStyles(
  (theme) => ({
    outlined: {
      color: theme.palette.error.main,
      borderColor: alpha(theme.palette.error.main, 0.5),
      '&:hover': {
        borderColor: theme.palette.error.main,
        backgroundColor: alpha(theme.palette.error.main, 0.08),
      },
    },
    contained: {
      color: theme.palette.error.contrastText,
      backgroundColor: theme.palette.error.main,
      '&:hover': {
        backgroundColor: theme.palette.error.dark,
        // On a touch screen a tapped button doesn't stay dark
        '@media (hover: none)': {
          backgroundColor: theme.palette.error.main,
        },
      },
    },
    icon: {
      color: theme.palette.error.main,
      '&:hover': {
        backgroundColor: alpha(theme.palette.error.main, 0.08),
      },
    },
  }),
  { name: 'NDRipDiscard' },
)
