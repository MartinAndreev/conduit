export interface ExecutableReplacer {
  replace(stagedExecutable: string, destination: string): Promise<void>;
}
