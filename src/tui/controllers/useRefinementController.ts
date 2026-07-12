import { useState, useEffect, useCallback, useReducer } from "react";
import { useKeyboard } from "@opentui/react";
import type { CommandBus } from "@system/bus/command-bus.js";
import type { QueryBus } from "@system/bus/query-bus.js";
import type { FeatureReadModel } from "@domains/features/types/feature.js";
import type {
  RefinementDraft,
  DraftField,
} from "@domains/refinement/types/draft.js";
import {
  formatRefinementBrief,
  parseRefinementBrief,
} from "@helpers/formatting/refinement-brief.js";
import {
  ARCHITECT_DETAIL_LEVELS,
  ARCHITECT_EFFORTS,
  DEFAULT_ARCHITECT_PREFERENCES,
  type ArchitectPreferences,
} from "@domains/refinement/types/architect-preferences.js";
import type {
  ClarificationQuestion,
  RefinementRevision,
} from "@domains/refinement/types/revision.js";
import type {
  RefinementControllerActions,
  RefinementLifecycleAction,
  RefinementLifecycleState,
  RefinementControllerState,
  RefinementView,
} from "@tui/types/refinement.js";

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
  {
    name: "guidelines",
    label: "Implementation and design guidance (optional)",
    guidance:
      "Add architecture, coding, design-system, accessibility, or delivery guidance the architect must preserve.",
    required: false,
    multiline: true,
  },
] as const;

function lifecycleReducer(
  state: RefinementLifecycleState,
  action: RefinementLifecycleAction,
): RefinementLifecycleState {
  switch (action.type) {
    case "view":
      return { ...state, view: action.view };
    case "loaded":
      return { ...state, view: action.view, loading: false, error: null };
    case "error":
      return {
        ...state,
        view: "error",
        loading: false,
        error: action.error,
        architectLifecycle: "failed",
      };
    case "startArchitect":
      return {
        ...state,
        previousView:
          state.view === "architect"
            ? state.previousView
            : (state.view as RefinementLifecycleState["previousView"]),
        view: "architect",
        architectLifecycle: "running",
        error: null,
      };
    case "architectComplete":
      return { ...state, view: action.view, architectLifecycle: "idle" };
    case "architectCancelled":
      return {
        ...state,
        view: state.previousView,
        architectLifecycle: "cancelled",
      };
  }
}

export function useRefinementController(
  commandBus: CommandBus,
  queryBus: QueryBus,
  featureId: string,
  onExit: () => void,
): [RefinementControllerState, RefinementControllerActions] {
  const [feature, setFeature] = useState<FeatureReadModel | null>(null);
  const [draft, setDraft] = useState<RefinementDraft | null>(null);
  const [lifecycle, dispatchLifecycle] = useReducer(lifecycleReducer, {
    view: "loading",
    loading: true,
    error: null,
    architectLifecycle: "idle",
    previousView: "form",
  });
  const { view, loading, error, architectLifecycle } = lifecycle;
  const architectRunning = architectLifecycle === "running";
  const setView = useCallback(
    (next: Exclude<RefinementView, "loading" | "error">) =>
      dispatchLifecycle({ type: "view", view: next }),
    [],
  );
  const setError = useCallback((next: string | null) => {
    if (next) dispatchLifecycle({ type: "error", error: next });
  }, []);
  const [values, setValues] = useState<Record<string, string>>({});
  const [architectEnabled, setArchitectEnabled] = useState(false);
  const [researchEnabled, setResearchEnabled] = useState(false);
  const [architectPreferences, setArchitectPreferences] =
    useState<ArchitectPreferences>(DEFAULT_ARCHITECT_PREFERENCES);
  const [packetContent, setPacketContent] =
    useState<RefinementControllerState["packetContent"]>(null);
  const [revision, setRevision] = useState<RefinementRevision | null>(null);
  const [questions, setQuestions] = useState<readonly ClarificationQuestion[]>(
    [],
  );
  const [researchReport, setResearchReport] = useState<string | null>(null);

  const loadData = useCallback(async () => {
    let hasPacket = false;
    try {
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
        hasPacket = Boolean(
          content.spec.trim() || content.plan.trim() || content.tasks.trim(),
        );
        setValues((current) =>
          current.problem
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
            ...parseRefinementBrief(data.draft.story),
            testCases: data.draft.testCases,
          });
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      dispatchLifecycle({
        type: "loaded",
        view: hasPacket ? "packet" : "form",
      });
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
    return formatRefinementBrief({
      problem: formValues.problem ?? "",
      audience: formValues.audience ?? "",
      outcome: formValues.outcome ?? "",
      constraints: formValues.constraints ?? "",
      guidelines: formValues.guidelines ?? "",
    });
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

  const startResearch = useCallback(() => {
    const story = buildStory(values);
    setResearchReport(null);
    setView("research");
    void commandBus
      .dispatch({ type: "startResearchRefinement", featureId, story })
      .then((result) => {
        if (!result.success) {
          setError(result.error.message);
          return;
        }
        const data = result.data as { report: string };
        setResearchReport(data.report);
        setView("researchReview");
      });
  }, [buildStory, commandBus, featureId, setError, setView, values]);

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

      if (architectEnabled && researchEnabled) {
        startResearch();
      } else if (architectEnabled) {
        dispatchLifecycle({ type: "startArchitect" });
        void commandBus
          .dispatch({
            type: "startArchitectRefinement",
            featureId,
            story,
            preferences: architectPreferences,
          })
          .then((architectResult) => {
            if (!architectResult.success) {
              setError(architectResult.error.message);
              return;
            }
            const data = architectResult.data as {
              status: "awaiting_clarification" | "ready_for_review";
            };
            void refreshRevision();
            void refreshPacketContent();
            dispatchLifecycle({
              type: "architectComplete",
              view:
                data.status === "awaiting_clarification"
                  ? "clarifications"
                  : "review",
            });
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
    researchEnabled,
    architectPreferences,
    buildStory,
    onExit,
    refreshPacketContent,
    refreshRevision,
    startResearch,
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
  const toggleResearch = useCallback(() => {
    setResearchEnabled((prev) => !prev);
  }, []);
  const cycleArchitectPreference = useCallback(
    (kind: "effort" | "detailLevel") => {
      setArchitectPreferences((current) => {
        const values =
          kind === "effort" ? ARCHITECT_EFFORTS : ARCHITECT_DETAIL_LEVELS;
        const currentValue = current[kind];
        const next =
          values[(values.indexOf(currentValue as never) + 1) % values.length]!;
        return kind === "effort"
          ? { ...current, effort: next as ArchitectPreferences["effort"] }
          : {
              ...current,
              detailLevel: next as ArchitectPreferences["detailLevel"],
            };
      });
    },
    [],
  );
  const editPacketBrief = useCallback(() => setView("form"), []);
  const cancelArchitect = useCallback(async () => {
    await commandBus.dispatch({ type: "cancelArchitectRefinement", featureId });
    dispatchLifecycle({ type: "architectCancelled" });
  }, [commandBus, featureId]);
  const startArchitectPass = useCallback(
    (revisionId?: string) => {
      const story = buildStory(values);
      dispatchLifecycle({ type: "startArchitect" });
      void commandBus
        .dispatch({
          type: "startArchitectRefinement",
          featureId,
          story,
          revisionId,
          preferences: architectPreferences,
        })
        .then((result) => {
          if (!result.success) {
            setError(result.error.message);
            return;
          }
          const data = result.data as {
            status: "awaiting_clarification" | "ready_for_review";
          };
          void refreshRevision();
          void refreshPacketContent();
          dispatchLifecycle({
            type: "architectComplete",
            view:
              data.status === "awaiting_clarification"
                ? "clarifications"
                : "review",
          });
        });
    },
    [
      buildStory,
      commandBus,
      featureId,
      refreshPacketContent,
      refreshRevision,
      values,
      architectPreferences,
    ],
  );
  const acceptResearch = useCallback(() => {
    startArchitectPass();
  }, [startArchitectPass]);
  const cancelResearch = useCallback(async () => {
    await commandBus.dispatch({ type: "cancelResearchRefinement", featureId });
  }, [commandBus, featureId]);
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
      researchEnabled,
      architectPreferences,
      architectLifecycle,
      architectRunning,
      packetContent,
      revision,
      questions,
      researchReport,
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
      toggleResearch,
      cycleArchitectPreference,
      startResearch,
      acceptResearch,
      cancelResearch,
      editPacketBrief,
      cancelArchitect,
      submitAnswers,
      approvePacket,
      requestPacketChanges,
    },
  ];
}
