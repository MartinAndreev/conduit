import type { RefinementDatabase } from "../../../domains/refinement/interfaces/database-schema.js";

export interface ImportLedgerTable {
  source_path: string;
  source_checksum: string;
  imported_at: string;
  record_count: number;
  status: string;
  diagnostic: string | null;
}

export interface ImportLedgerDatabase {
  import_ledger: ImportLedgerTable;
}

export type LegacyImportDatabase = ImportLedgerDatabase & RefinementDatabase;
