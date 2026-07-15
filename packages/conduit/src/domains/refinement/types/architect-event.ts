export type ArchitectEventType =
  | "thought"
  | "activity"
  | "tool-call"
  | "tool-output"
  | "file-change"
  | "patch"
  | "error"
  | "lifecycle";

export interface ArchitectEvent {
  readonly type: ArchitectEventType;
  readonly timestamp: string;
  readonly content: string;
  readonly expanded?: boolean;
  readonly files?: readonly string[];
  readonly diff?: string;
}

export interface ParsedArchitectLine {
  readonly event: ArchitectEvent;
  readonly nextIndex: number;
}
