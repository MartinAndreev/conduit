import type { Theme } from "@tui/theme.js";

interface HomeFooterProps {
  searching: boolean;
  actionModalOpen: boolean;
  theme: Theme;
}

export function HomeFooter({
  searching,
  actionModalOpen,
  theme,
}: HomeFooterProps) {
  const content = searching
    ? " Search: type to filter  [Enter] Apply  [Esc] Cancel and resume navigation"
    : actionModalOpen
      ? " Actions: [↑↓] Choose  [Enter] Confirm  [Esc/q] Close"
      : " [/] Search  [↑↓] Select  [Enter] Actions  [q] Quit";

  return (
    <box width="100%" height={1} flexDirection="row">
      <text content={content} fg={theme.text.muted} />
    </box>
  );
}
