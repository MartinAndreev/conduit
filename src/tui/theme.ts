export const theme = {
  surface: {
    base: "#20251F",
    raised: "#2B332A",
  },
  action: {
    primary: "#8FB6A0",
    attention: "#D8C28B",
  },
  text: {
    default: "#D8D5C8",
    strong: "#E5E1D4",
    muted: "#8B8B8B",
  },
  status: {
    error: "#E06060",
  },
} as const;

export type Theme = typeof theme;
export type SurfaceToken = keyof typeof theme.surface;
export type ActionToken = keyof typeof theme.action;
export type TextToken = keyof typeof theme.text;
export type StatusToken = keyof typeof theme.status;
