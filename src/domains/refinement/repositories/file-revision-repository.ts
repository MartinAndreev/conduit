import { mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import path from "node:path";
import type { Feature } from "@domains/features/types/feature.js";
import type { RefinementRevisionRepository } from "@domains/refinement/interfaces/revision-repository.js";
import type {
  ClarificationQuestion,
  RefinementRevision,
  RevisionStatus,
} from "@domains/refinement/types/revision.js";

const metadataFile = "revision.json";

function revisionDirectory(feature: Feature, id: string): string {
  return path.join(feature.directory, "revisions", id);
}

function parseQuestions(source: string): readonly ClarificationQuestion[] {
  const sections = source.split(/^##\s+/m).slice(1);
  const questions = sections.map((section, index) => {
    const [heading = `Q-${String(index + 1).padStart(3, "0")}`, ...body] =
      section.trim().split("\n");
    const text = body.join("\n").trim();
    const options = [...text.matchAll(/^\s*[-*]\s+(.+)$/gm)].map(
      (match) => match[1],
    );
    const context = text
      .replace(/^###\s*(why it matters|context)\s*$/gim, "")
      .replace(/^###\s*options\s*$/gim, "")
      .replace(/^\s*[-*]\s+.+$/gm, "")
      .trim();
    return {
      id: heading.trim(),
      question: context || text || heading.trim(),
      context,
      options,
    };
  });
  return questions.length
    ? questions
    : source.trim()
      ? [{ id: "Q-001", question: source.trim(), options: [] }]
      : [];
}

export class FileRefinementRevisionRepository implements RefinementRevisionRepository {
  async create(
    feature: Feature,
    feedback?: string,
  ): Promise<RefinementRevision> {
    const root = path.join(feature.directory, "revisions");
    const entries = await readdir(root, { withFileTypes: true }).catch(
      () => [],
    );
    const next =
      Math.max(
        0,
        ...entries
          .filter((entry) => entry.isDirectory())
          .map((entry) => Number(entry.name))
          .filter(Number.isFinite),
      ) + 1;
    const now = new Date().toISOString();
    const revision: RefinementRevision = {
      id: String(next).padStart(3, "0"),
      status: "running",
      directory: revisionDirectory(feature, String(next).padStart(3, "0")),
      createdAt: now,
      updatedAt: now,
      ...(feedback?.trim() ? { feedback: feedback.trim() } : {}),
    };
    await mkdir(revision.directory, { recursive: true });
    await this.writeMetadata(revision);
    if (revision.feedback)
      await writeFile(
        path.join(revision.directory, "feedback.md"),
        `# Review feedback\n\n${revision.feedback}\n`,
      );
    return revision;
  }

  async getLatest(feature: Feature): Promise<RefinementRevision | null> {
    const root = path.join(feature.directory, "revisions");
    const entries = await readdir(root, { withFileTypes: true }).catch(
      () => [],
    );
    const ids = entries
      .filter((entry) => entry.isDirectory())
      .map((entry) => entry.name)
      .sort();
    if (!ids.length) return null;
    const directory = revisionDirectory(feature, ids.at(-1)!);
    return JSON.parse(
      await readFile(path.join(directory, metadataFile), "utf8"),
    ) as RefinementRevision;
  }

  async updateStatus(
    revision: RefinementRevision,
    status: RevisionStatus,
  ): Promise<RefinementRevision> {
    const updated = {
      ...revision,
      status,
      updatedAt: new Date().toISOString(),
    };
    await this.writeMetadata(updated);
    return updated;
  }

  async saveQuestions(
    revision: RefinementRevision,
    source: string,
  ): Promise<readonly ClarificationQuestion[]> {
    await writeFile(
      path.join(revision.directory, "questions.md"),
      source.trim() + "\n",
    );
    return parseQuestions(source);
  }

  async readQuestions(
    revision: RefinementRevision,
  ): Promise<readonly ClarificationQuestion[]> {
    const source = await readFile(
      path.join(revision.directory, "questions.md"),
      "utf8",
    ).catch(() => "");
    return parseQuestions(source);
  }

  async saveAnswers(
    revision: RefinementRevision,
    answers: string,
  ): Promise<void> {
    await writeFile(
      path.join(revision.directory, "answers.md"),
      `# User answers\n\n${answers.trim()}\n`,
    );
  }

  async recordReview(
    revision: RefinementRevision,
    decision: "approved" | "changes_requested",
    feedback?: string,
  ): Promise<void> {
    const body = feedback?.trim()
      ? `\n\n## Feedback\n\n${feedback.trim()}`
      : "";
    await writeFile(
      path.join(revision.directory, "review.md"),
      `# Packet review\n\nDecision: ${decision}${body}\n`,
    );
  }

  async recordRun(
    revision: RefinementRevision,
    transcript: string,
  ): Promise<void> {
    await writeFile(
      path.join(revision.directory, "architect-run.md"),
      `# Architect run\n\n\`\`\`text\n${transcript.trim()}\n\`\`\`\n`,
    );
  }

  private async writeMetadata(revision: RefinementRevision): Promise<void> {
    await writeFile(
      path.join(revision.directory, metadataFile),
      JSON.stringify(revision, null, 2) + "\n",
    );
  }
}
