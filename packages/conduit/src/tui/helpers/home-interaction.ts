import { UpdateStatus } from "@domains/updates/enums/update-status.js";
import type { UpdateStatusReadModel } from "@domains/updates/types/update-status-read-model.js";
import type {
  HomeInteraction,
  HomeInteractionAction,
  HomeUpdateKeyDecision,
} from "@tui/types/home.js";

export const FEATURE_ACTIONS = ["View", "Refine", "Run", "Status"] as const;

export function canOfferUpdate(status: UpdateStatusReadModel): boolean {
  return (
    status.status === UpdateStatus.Available && Boolean(status.installation)
  );
}

export function canOpenUpdateConfirmation(
  interaction: HomeInteraction,
  status: UpdateStatusReadModel,
): boolean {
  return interaction.kind === "idle" && canOfferUpdate(status);
}

export function decideUpdateConfirmationKey(
  interaction: HomeInteraction,
  key: string,
): HomeUpdateKeyDecision | undefined {
  if (interaction.kind !== "updateConfirmation") return undefined;
  if (key === "escape" || key === "q")
    return { kind: "interaction", action: { type: "idle" } };
  if (key === "left" || key === "up")
    return {
      kind: "interaction",
      action: { type: "selectUpdateAction", value: 0 },
    };
  if (key === "right" || key === "down")
    return {
      kind: "interaction",
      action: { type: "selectUpdateAction", value: 1 },
    };
  if (key === "return")
    return interaction.actionIndex === 1
      ? { kind: "startUpdate" }
      : { kind: "interaction", action: { type: "idle" } };
  return { kind: "consume" };
}

export function homeInteractionReducer(
  state: HomeInteraction,
  action: HomeInteractionAction,
): HomeInteraction {
  switch (action.type) {
    case "search":
      return { kind: "search", query: "" };
    case "create":
      return { kind: "create", title: "" };
    case "actions":
      return { kind: "featureActions", actionIndex: 0 };
    case "idle":
      return { kind: "idle" };
    case "append":
      return state.kind === "search"
        ? { ...state, query: state.query + action.value }
        : state.kind === "create"
          ? { ...state, title: state.title + action.value }
          : state;
    case "setTitle":
      return state.kind === "create"
        ? { ...state, title: action.value }
        : state;
    case "backspace":
      return state.kind === "search"
        ? { ...state, query: state.query.slice(0, -1) }
        : state.kind === "create"
          ? { ...state, title: state.title.slice(0, -1) }
          : state;
    case "nextAction":
      return state.kind === "featureActions"
        ? {
            ...state,
            actionIndex: Math.min(
              FEATURE_ACTIONS.length - 1,
              state.actionIndex + 1,
            ),
          }
        : state;
    case "previousAction":
      return state.kind === "featureActions"
        ? { ...state, actionIndex: Math.max(0, state.actionIndex - 1) }
        : state;
    case "openUpdateConfirmation":
      return { kind: "updateConfirmation", actionIndex: 0 };
    case "selectUpdateAction":
      return state.kind === "updateConfirmation"
        ? { ...state, actionIndex: action.value }
        : state;
  }
}
