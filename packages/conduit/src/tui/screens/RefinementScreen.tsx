import type { CommandBus } from "@system/bus/command-bus.js";
import type { QueryBus } from "@system/bus/query-bus.js";
import { useKeyboard } from "@opentui/react";
import { useTheme } from "@tui/components/ThemeProvider.js";
import {
  useRefinementController,
  REFINEMENT_FIELDS,
} from "@tui/controllers/useRefinementController.js";
import { RefinementForm } from "@tui/components/RefinementForm.js";
import { RefinementPreview } from "@tui/components/RefinementPreview.js";
import { ArchitectActivity } from "@tui/components/ArchitectActivity.js";
import { useRefinementFormController } from "@tui/controllers/useRefinementFormController.js";
import { useRefinementPreviewController } from "@tui/controllers/useRefinementPreviewController.js";
import { useArchitectActivityController } from "@tui/controllers/useArchitectActivityController.js";
import { RefinementSidebar } from "@tui/sections/RefinementSidebar.js";
import { RefinementPacketSummary } from "@tui/components/RefinementPacketSummary.js";
import { ArchitectClarifications } from "@tui/components/ArchitectClarifications.js";
import { RefinementPacketReview } from "@tui/components/RefinementPacketReview.js";
import { ResearchReport } from "@tui/components/ResearchReport.js";
import { WorkerMonitorScreen } from "@tui/components/WorkerMonitorScreen.js";
import { useWorkerMonitorController } from "@tui/controllers/useWorkerMonitorController.js";

interface RefinementScreenProps {
  commandBus: CommandBus;
  queryBus: QueryBus;
  featureId: string;
  onExit: () => void;
}

export function RefinementScreen({
  commandBus,
  queryBus,
  featureId,
  onExit,
}: RefinementScreenProps) {
  const theme = useTheme();
  const [state, actions] = useRefinementController(
    commandBus,
    queryBus,
    featureId,
    onExit,
  );
  const form = useRefinementFormController(
    REFINEMENT_FIELDS,
    state.values,
    actions.submitForm,
    onExit,
    state.view === "form",
  );
  useRefinementPreviewController(
    {
      approve: () => void actions.approvePreview(),
      reject: actions.rejectPreview,
      quit: actions.quitPreview,
      toggleArchitect: actions.toggleArchitect,
      toggleResearch: actions.toggleResearch,
      cycleArchitectPreference: actions.cycleArchitectPreference,
    },
    state.view === "preview",
  );
  const architect = useArchitectActivityController(
    queryBus,
    featureId,
    () => {
      void actions.cancelArchitect().finally(onExit);
    },
    state.view === "architect",
  );
  const researchMonitor = useWorkerMonitorController(
    queryBus,
    commandBus,
    state.researchRunId ?? "",
    "",
    onExit,
    state.view === "research" && Boolean(state.researchRunId),
    true,
  );

  useKeyboard((_event: { name: string }) => {
    if (state.view === "error") onExit();
  });

  if (state.loading) {
    return (
      <box
        width="100%"
        height="100%"
        flexDirection="column"
        backgroundColor={theme.surface.base}
        justifyContent="center"
        alignItems="center"
      >
        <text content="Loading refinement..." fg={theme.text.muted} />
      </box>
    );
  }

  if (state.error) {
    return (
      <box
        width="100%"
        height="100%"
        flexDirection="column"
        backgroundColor={theme.surface.base}
        justifyContent="center"
        alignItems="center"
      >
        <text content={`Error: ${state.error}`} fg={theme.status.error} />
        <text content="Press any key to exit" fg={theme.text.muted} />
      </box>
    );
  }

  switch (state.view) {
    case "form":
      return (
        <box
          width="100%"
          height="100%"
          flexDirection="row"
          backgroundColor={theme.surface.base}
        >
          <RefinementSidebar
            theme={theme}
            fields={REFINEMENT_FIELDS}
            values={form.values}
            activeFieldIndex={form.activeFieldIndex}
          />
          <box width="70%" height="100%" paddingLeft={1}>
            <RefinementForm
              theme={theme}
              fields={REFINEMENT_FIELDS}
              {...form}
            />
          </box>
        </box>
      );

    case "packet":
      return (
        <RefinementPacketSummary
          theme={theme}
          content={
            state.packetContent ?? {
              spec: "",
              plan: "",
              tasks: "",
              testCases: "",
            }
          }
        />
      );

    case "preview":
      return (
        <box
          width="100%"
          height="100%"
          flexDirection="row"
          backgroundColor={theme.surface.base}
        >
          <RefinementSidebar
            theme={theme}
            fields={REFINEMENT_FIELDS}
            values={state.values}
            activeFieldIndex={form.activeFieldIndex}
          />
          <box width="70%" height="100%">
            <RefinementPreview
              theme={theme}
              values={state.values}
              architectEnabled={state.architectEnabled}
              researchEnabled={state.researchEnabled}
              architectPreferences={state.architectPreferences}
            />
          </box>
        </box>
      );

    case "architect":
      return (
        <ArchitectActivity
          theme={theme}
          {...architect}
          featureId={featureId}
          running={state.architectRunning}
        />
      );

    case "research":
      return (
        <WorkerMonitorScreen
          runId={state.researchRunId ?? "preparing"}
          cancelOnExit
          {...researchMonitor}
        />
      );

    case "researchReview":
      return (
        <ResearchReport
          theme={theme}
          report={state.researchReport}
          onAccept={actions.acceptResearch}
          onRerun={actions.startResearch}
          onEdit={() => actions.setView("form")}
          onExit={onExit}
        />
      );

    case "clarifications":
      return (
        <ArchitectClarifications
          theme={theme}
          questions={state.questions}
          onSubmit={(answers) => void actions.submitAnswers(answers)}
          onExit={() => actions.setView("architect")}
        />
      );

    case "review":
      return (
        <RefinementPacketReview
          theme={theme}
          content={
            state.packetContent ?? {
              spec: "",
              plan: "",
              tasks: "",
              testCases: "",
            }
          }
          onApprove={() => void actions.approvePacket()}
          onRequestChanges={(feedback) =>
            void actions.requestPacketChanges(feedback)
          }
          onExit={onExit}
        />
      );

    default:
      return null;
  }
}
