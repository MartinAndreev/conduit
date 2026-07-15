import type { Theme } from "@tui/theme.js";

interface HomeFooterProps {
  searching: boolean;
  actionModalOpen: boolean;
  creating: boolean;
  updateConfirmationOpen: boolean;
  updateAvailable: boolean;
  theme: Theme;
}

export function HomeFooter({
  searching,
  actionModalOpen,
  creating,
  updateConfirmationOpen,
  updateAvailable,
  theme,
}: HomeFooterProps) {
  const content = updateConfirmationOpen
    ? " Update: [←→] Choose  [Enter] Confirm  [Esc/q] Cancel"
    : searching
      ? " Search: type to filter  [Enter] Apply  [Esc] Cancel and resume navigation"
      : creating
        ? " New feature: type a title  [Esc] Cancel"
        : actionModalOpen
          ? " Actions: [↑↓] Choose  [Enter] Confirm  [Esc/q] Close"
          : ` [/] Search  [↑↓] Select  [Enter] Actions${updateAvailable ? "  [u] Update" : ""}  [q] Quit`;

  return (
    <box width="100%" height={1} flexDirection="row">
      <text content={content} fg={theme.text.muted} />
    </box>
  );
}
