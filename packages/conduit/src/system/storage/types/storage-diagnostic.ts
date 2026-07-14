export type StorageDiagnostic = Readonly<{
  binding: string;
  projectDatabase: string;
  globalDatabase: string;
  projectMigrationCount: number;
  globalMigrationCount: number;
  interruptedMigrationRecovered: boolean;
}>;
