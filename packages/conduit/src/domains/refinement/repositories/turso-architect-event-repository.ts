import { createTursoKysely } from "@system/storage/adapters/kysely-turso-dialect.js";
import type { DatabaseConnection } from "@system/storage/interfaces/database.js";
import type { ArchitectEventRepository } from "../interfaces/architect-event-repository.js";
import type { RefinementDatabase } from "../interfaces/database-schema.js";
import type { ArchitectEvent } from "../types/architect-event.js";
import {
  extractArchitectEvents,
  normalizeArchitectEvents,
} from "../helpers/architect-event-parser.js";

export class TursoArchitectEventRepository implements ArchitectEventRepository {
  private readonly database;

  constructor(connection: DatabaseConnection) {
    this.database = createTursoKysely<RefinementDatabase>(connection);
  }

  async loadEvents(featureId: string): Promise<readonly ArchitectEvent[]> {
    const rows = await this.database
      .selectFrom("refinement_events")
      .select(["event_type", "timestamp", "content", "files_json", "diff"])
      .where("feature_id", "=", featureId)
      .orderBy("sequence")
      .execute();
    const events = rows.flatMap((row) => {
      const event: ArchitectEvent = {
        type: row.event_type as ArchitectEvent["type"],
        timestamp: row.timestamp,
        content: row.content,
        ...(row.files_json
          ? { files: JSON.parse(row.files_json) as string[] }
          : {}),
        ...(row.diff ? { diff: row.diff } : {}),
      };
      if (event.type === "activity" && event.content.includes("\n")) {
        const parsed = extractArchitectEvents(event.content, event.timestamp);
        if (parsed.length > 0) {
          return parsed;
        }
      }
      return [event];
    });
    return normalizeArchitectEvents(events);
  }
}
