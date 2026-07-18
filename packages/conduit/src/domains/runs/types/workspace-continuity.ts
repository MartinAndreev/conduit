export type WorkspaceContinuity =
  | Readonly<{
      state: "no-retained";
      roles: readonly string[];
    }>
  | Readonly<{
      state: "compatible-continue";
      runId: string;
      roles: readonly string[];
      preservedRoles: readonly string[];
      retryRoles: readonly string[];
    }>
  | Readonly<{
      state: "incompatible-retained";
      runId?: string;
      runIds: readonly string[];
      roles: readonly string[];
      reason: string;
    }>
  | Readonly<{
      state: "lease-conflict";
      runId: string;
      roles: readonly string[];
      reason: string;
    }>;
