import type {
  GlobalProfile,
  SaveGlobalProfileInput,
} from "../types/global-profile.js";

export interface GlobalProfileRepository {
  load(name: string): Promise<GlobalProfile | undefined>;
  list(): Promise<readonly GlobalProfile[]>;
  save(profile: SaveGlobalProfileInput): Promise<GlobalProfile>;
  delete(name: string): Promise<boolean>;
}
