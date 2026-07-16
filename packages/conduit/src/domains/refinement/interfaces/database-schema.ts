import type { Generated } from "kysely";

export interface RefinementDraftsTable {
  feature_id: string;
  story: string;
  test_cases: string;
  created_at: string;
  updated_at: string;
  version: number;
}

export interface RefinementRevisionsTable {
  feature_id: string;
  revision_id: string;
  status: string;
  directory: string;
  feedback: string | null;
  questions_source: string | null;
  answers: string | null;
  review_decision: string | null;
  review_feedback: string | null;
  transcript: string | null;
  created_at: string;
  updated_at: string;
  version: number;
}

export interface RefinementEventsTable {
  id: Generated<number>;
  feature_id: string;
  sequence: number;
  event_type: string;
  timestamp: string;
  content: string;
  files_json: string | null;
  diff: string | null;
}

export interface ResearchReportsTable {
  feature_id: string;
  report: string;
  updated_at: string;
  version: number;
}

export interface ClarificationQuestionsTable {
  question_id: string;
  feature_id: string;
  revision_id: string;
  fingerprint: string;
  question_json: string;
  answer: string | null;
  repeat_count: number;
  created_at: string;
  answered_at: string | null;
}

export interface RefinementDatabase {
  refinement_drafts: RefinementDraftsTable;
  refinement_revisions: RefinementRevisionsTable;
  refinement_events: RefinementEventsTable;
  research_reports: ResearchReportsTable;
  clarification_questions: ClarificationQuestionsTable;
}
