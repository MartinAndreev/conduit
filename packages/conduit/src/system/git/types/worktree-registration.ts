export interface GitWorktreeRegistration {
  readonly workspacePath: string;
  readonly head?: string;
  readonly branch?: string;
  readonly prunable: boolean;
  readonly locked: boolean;
}
