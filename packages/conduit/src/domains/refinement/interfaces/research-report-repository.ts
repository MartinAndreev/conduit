import type { ResearchReport } from "../types/research-report.js";

export interface ResearchReportRepository {
  save(featureId: string, report: string): Promise<ResearchReport>;
  load(featureId: string): Promise<ResearchReport | undefined>;
}
