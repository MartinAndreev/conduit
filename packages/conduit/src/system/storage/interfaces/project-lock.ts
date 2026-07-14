export interface ProjectLock {
  readonly lockPath: string;
  release(): Promise<void>;
}

export interface ProjectLockFactory {
  acquire(projectRoot: string, stateDirectory?: string): Promise<ProjectLock>;
}
