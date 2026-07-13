export type SelectionBehavior = "bounded" | "cyclic";

export interface SelectableListOptions {
  readonly itemCount: number;
  readonly selectedIndex: number;
  readonly onSelect: (index: number) => void;
  readonly behavior: SelectionBehavior;
}
