import { useState, useEffect, useCallback } from "react";
import { useKeyboard } from "@opentui/react";
import type { CommandBus } from "@system/bus/command-bus.js";
import type { QueryBus } from "@system/bus/query-bus.js";
import type { FeatureReadModel } from "@domains/features/types/feature.js";
import type {
  RefinementDraft,
  DraftField,
} from "@domains/refinement/types/draft.js";
import { parseRefinementBrief } from "@helpers/formatting/refinement-brief.js";
import type {
  ClarificationQuestion,
  RefinementRevision,
} from "@domains/refinement/types/revision.js";

export const REFINEMENT_FIELDS: readonly DraftField[] = [
  {
    name: "problem",
    label: "Problem / user story",
    guidance: "Describe the problem or user story that this feature addresses.",
    required: true,
    multiline: true,
  },
  {
    name: "audience",
    label: "User or audience",
    guidance: "Who will use this feature? Describe the target users.",
    required: true,
    multiline: false,
  },
  {
    name: "outcome",
    label: "Desired outcome and acceptance criteria",
    guidance: "What should be achieved? Include acceptance criteria.",
    required: true,
    multiline: true,
  },
  {
    name: "constraints",
    label: "Constraints and non-goals (optional)",
    guidance: "Any constraints or things that are explicitly out of scope.",
    required: false,
    multiline: true,
  },
  {
    name: "testCases",
    label: "QA test cases and regression scenarios",
    guidance: "Describe test cases and regression scenarios.",
    required: true,
    multiline: true,
  },
] as const;

export type RefinementView =
  "form" | "packet" | "preview" | "architect" | "clarifications" | "review";

export interface RefinementControllerState {
  feature: FeatureReadModel | null;
  draft: RefinementDraft | null;
  view: RefinementView;
  values: Record<string, string>;
  loading: boolean;
  error: string | null;
  architectEnabled: boolean;
  architectRunning: boolean;
  packetContent: {
    spec: string;
    plan: string;
    tasks: string;
    testCases: string;
  } | null;
  revision: RefinementRevision | null;
  questions: readonly ClarificationQuestion[];
}

export interface RefinementControllerActions {
  setView: (view: RefinementView) => void;
  setValues: (values: Record<string, string>) => void;
  saveDraft: () => Promise<void>;
  submitForm: (values: Record<string, string>) => void;
  approvePreview: () => Promise<void>;
  rejectPreview: () => void;
  quitPreview: () => void;
  toggleArchitect: () => void;
  editPacketBrief: () => void;
  cancelArchitect: () => Promise<void>;
  submitAnswers: (answers: string) => Promise<void>;
  approvePacket: () => Promise<void>;
  requestPacketChanges: (feedback: string) => Promise<void>;
}

export function useRefinementController(
  commandBus: CommandBus,
  queryBus: QueryBus,
  featureId: string,
  onExit: () => void,
): [RefinementControllerState, RefinementControllerActions] {
  const [feature, setFeature] = useState<FeatureReadModel | null>(null);
  const [draft, setDraft] = useState<RefinementDraft | null>(null);
  const [view, setView] = useState<RefinementView>("form");
  const [values, setValues] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [architectEnabled, setArchitectEnabled] = useState(false);
  const [packetContent, setPacketContent] =
    useState<RefinementControllerState["packetContent"]>(null);
  const [architectRunning, setArchitectRunning] = useState(false);
  const [revision, setRevision] = useState<RefinementRevision | null>(null);
  const [questions, setQuestions] = useState<readonly ClarificationQuestion[]>(
    [],
  );

  const loadData = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);

      const featureResult = await queryBus.execute({
        type: "getFeature",
        featureId,
      });

      if (featureResult.success) {
        const data = featureResult.data as { feature: FeatureReadModel };
        setFeature(data.feature);
      }

      const contentResult = await queryBus.execute({
        type: "getFeatureContent",
        featureId,
      });
      if (contentResult.success) {
        const content = contentResult.data as {
          story: string;
          testCases: string;
          spec: string;
          plan: string;
          tasks: string;
        };
        setPacketContent(content);
        if (content.spec.trim() || content.plan.trim() || content.tasks.trim())
          setView("packet");
        setValues((current) =>
          Object.keys(current).length
            ? current
            : {
                ...parseRefinementBrief(content.story || content.spec),
                testCases: content.testCases
                  .replace(/^# QA test cases\s*/i, "")
                  .trim(),
              },
        );
      }

      const draftResult = await queryBus.execute({
        type: "getDraft",
        featureId,
      });

      if (draftResult.success) {
        const data = draftResult.data as { draft: RefinementDraft | null };
        if (data.draft) {
          setDraft(data.draft);
          setValues({
            problem: data.draft.story,
            testCases: data.draft.testCases,
          });
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [queryBus, featureId]);

  const refreshRevision = useCallback(async () => {
    const result = await queryBus.execute({
      type: "getRefinementRevision",
      featureId,
    });
    if (!result.success) return;
    const data = result.data as {
      revision: RefinementRevision | null;
      questions: readonly ClarificationQuestion[];
    };
    setRevision(data.revision);
    setQuestions(data.questions);
  }, [queryBus, featureId]);
  const refreshPacketContent = useCallback(async () => {
    const result = await queryBus.execute({
      type: "getFeatureContent",
      featureId,
    });
    if (!result.success) return;
    setPacketContent(
      result.data as {
        spec: string;
        plan: string;
        tasks: string;
        testCases: string;
      },
    );
  }, [queryBus, featureId]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const buildStory = useCallback((formValues: Record<string, string>) => {
    return [
      `Problem: ${formValues.problem ?? ""}`,
      `User: ${formValues.audience ?? ""}`,
      `Desired outcome: ${formValues.outcome ?? ""}`,
      formValues.constraints
        ? `Constraints and non-goals: ${formValues.constraints}`
        : "",
    ]
      .filter(Boolean)
      .join("\n\n");
  }, []);

  const saveDraft = useCallback(async () => {
    try {
      const story = buildStory(values);
      await commandBus.dispatch({
        type: "saveDraft",
        featureId,
        story,
        testCases: values.testCases ?? "",
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [commandBus, featureId, values, buildStory]);

  const submitForm = useCallback((formValues: Record<string, string>) => {
    setValues(formValues);
    setView("preview");
  }, []);

  const approvePreview = useCallback(async () => {
    try {
      const story = buildStory(values);
      const result = await commandBus.dispatch({
        type: "approveRefinement",
        featureId,
        story,
        testCases: values.testCases ?? "",
      });

      if (!result.success) {
        setError(result.error.message);
        return;
      }

      if (architectEnabled) {
        setArchitectRunning(true);
        setView("architect");
        void commandBus
          .dispatch({
            type: "startArchitectRefinement",
            featureId,
            story,
          })
          .then((architectResult) => {
            setArchitectRunning(false);
            if (!architectResult.success) {
              setError(architectResult.error.message);
              return;
            }
            const data = architectResult.data as {
              status: "awaiting_clarification" | "ready_for_review";
            };
            void refreshRevision();
            void refreshPacketContent();
            setView(
              data.status === "awaiting_clarification"
                ? "clarifications"
                : "review",
            );
          });
      } else {
        onExit();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [
    commandBus,
    featureId,
    values,
    architectEnabled,
    buildStory,
    onExit,
    refreshPacketContent,
    refreshRevision,
  ]);

  const rejectPreview = useCallback(() => {
    setView("form");
  }, []);

  const quitPreview = useCallback(() => {
    onExit();
  }, [onExit]);

  const toggleArchitect = useCallback(() => {
    setArchitectEnabled((prev) => !prev);
  }, []);
  const editPacketBrief = useCallback(() => setView("form"), []);
  const cancelArchitect = useCallback(async () => {
    await commandBus.dispatch({ type: "cancelArchitectRefinement", featureId });
    setArchitectRunning(false);
    setView("preview");
  }, [commandBus, featureId]);
  const startArchitectPass = useCallback(
    (revisionId?: string) => {
      const story = buildStory(values);
      setArchitectRunning(true);
      setView("architect");
      void commandBus
        .dispatch({
          type: "startArchitectRefinement",
          featureId,
          story,
          revisionId,
        })
        .then((result) => {
          setArchitectRunning(false);
          if (!result.success) {
            setError(result.error.message);
            return;
          }
          const data = result.data as {
            status: "awaiting_clarification" | "ready_for_review";
          };
          void refreshRevision();
          void refreshPacketContent();
          setView(
            data.status === "awaiting_clarification"
              ? "clarifications"
              : "review",
          );
        });
    },
    [
      buildStory,
      commandBus,
      featureId,
      refreshPacketContent,
      refreshRevision,
      values,
    ],
  );
  const submitAnswers = useCallback(
    async (answers: string) => {
      if (!revision) return;
      const result = await commandBus.dispatch({
        type: "submitArchitectAnswers",
        featureId,
        revisionId: revision.id,
        answers,
      });
      if (!result.success) {
        setError(result.error.message);
        return;
      }
      startArchitectPass(revision.id);
    },
    [commandBus, featureId, revision, startArchitectPass],
  );
  const approvePacket = useCallback(async () => {
    if (!revision) return;
    const result = await commandBus.dispatch({
      type: "reviewRefinementPacket",
      featureId,
      revisionId: revision.id,
      decision: "approved",
    });
    if (!result.success) return setError(result.error.message);
    await refreshRevision();
    onExit();
  }, [commandBus, featureId, onExit, refreshRevision, revision]);
  const requestPacketChanges = useCallback(
    async (feedback: string) => {
      if (!revision) return;
      const result = await commandBus.dispatch({
        type: "reviewRefinementPacket",
        featureId,
        revisionId: revision.id,
        decision: "changes_requested",
        feedback,
      });
      if (!result.success) return setError(result.error.message);
      const data = result.data as { nextRevisionId?: string };
      startArchitectPass(data.nextRevisionId);
    },
    [commandBus, featureId, revision, startArchitectPass],
  );
  useKeyboard((event: { name: string }) => {
    if (view === "packet" && event.name === "e") editPacketBrief();
    if (view === "packet" && (event.name === "q" || event.name === "escape"))
      onExit();
  });

  return [
    {
      feature,
      draft,
      view,
      values,
      loading,
      error,
      architectEnabled,
      architectRunning,
      packetContent,
      revision,
      questions,
    },
    {
      setView,
      setValues,
      saveDraft,
      submitForm,
      approvePreview,
      rejectPreview,
      quitPreview,
      toggleArchitect,
      editPacketBrief,
      cancelArchitect,
      submitAnswers,
      approvePacket,
      requestPacketChanges,
    },
  ];
}
