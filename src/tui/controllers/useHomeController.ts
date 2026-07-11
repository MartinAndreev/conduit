import { useState, useEffect, useCallback } from "react";
import { useKeyboard } from "@opentui/react";
import type { FeatureReadModel } from "@domains/features/types/feature.js";
import type { RolePortrait } from "@domains/roles/interfaces/role-portrait.js";
import type { CommandBus } from "@system/bus/command-bus.js";
import type { QueryBus } from "@system/bus/query-bus.js";
import tips from "@tui/assets/tips.json" with { type: "json" };

export const FEATURE_ACTIONS = ["View", "Refine", "Run", "Status"] as const;

export type FeatureAction = (typeof FEATURE_ACTIONS)[number];

export interface HomeControllerState {
  features: readonly FeatureReadModel[];
  portraits: readonly RolePortrait[];
  selectedIndex: number;
  searchQuery: string;
  searching: boolean;
  creating: boolean;
  featureTitle: string;
  actionModalOpen: boolean;
  selectedAction: number;
  tip: string;
  filteredFeatures: readonly FeatureReadModel[];
}

export interface HomeControllerActions {
  handleKeyDown: (event: {
    name: string;
    ctrl: boolean;
    shift: boolean;
    meta: boolean;
  }) => void;
}

function randomTip(): string {
  return tips[Math.floor(Math.random() * tips.length)]!;
}

export function useHomeController(
  commandBus: CommandBus,
  queryBus: QueryBus,
  onExit: () => void,
  onRefine: (featureId: string) => void,
  onView: (featureId: string) => void,
): [HomeControllerState, HomeControllerActions] {
  const [features, setFeatures] = useState<readonly FeatureReadModel[]>([]);
  const [portraits, setPortraits] = useState<readonly RolePortrait[]>([]);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [searchQuery, setSearchQuery] = useState("");
  const [searching, setSearching] = useState(false);
  const [creating, setCreating] = useState(false);
  const [featureTitle, setFeatureTitle] = useState("");
  const [actionModalOpen, setActionModalOpen] = useState(false);
  const [selectedAction, setSelectedAction] = useState(0);
  const [tip] = useState(randomTip);

  const loadData = useCallback(async () => {
    const featuresResult = await queryBus.execute({ type: "listFeatures" });
    if (featuresResult.success) {
      setFeatures(
        (featuresResult.data as { features: readonly FeatureReadModel[] })
          .features,
      );
    }

    const portraitsResult = await queryBus.execute({ type: "listPortraits" });
    if (portraitsResult.success) {
      setPortraits(
        (portraitsResult.data as { portraits: readonly RolePortrait[] })
          .portraits,
      );
    }
  }, [queryBus]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const filteredFeatures = searchQuery
    ? features.filter((f) =>
        f.title.toLowerCase().includes(searchQuery.toLowerCase()),
      )
    : features;

  const handleKeyDown = useCallback(
    (event: { name: string; ctrl: boolean; shift: boolean; meta: boolean }) => {
      const key = event.name ?? "";

      if (searching) {
        if (key === "escape") {
          setSearching(false);
          setSearchQuery("");
          return;
        }
        if (key === "return") {
          setSearching(false);
          return;
        }
        if (key === "backspace") {
          setSearchQuery((prev) => prev.slice(0, -1));
          return;
        }
        if (key.length === 1) {
          setSearchQuery((prev) => prev + key);
        }
        return;
      }
      if (creating) {
        if (key === "escape") {
          setCreating(false);
          setFeatureTitle("");
          return;
        }
        if (key === "backspace") {
          setFeatureTitle((value) => value.slice(0, -1));
          return;
        }
        if (key === "return" && featureTitle.trim()) {
          void commandBus
            .dispatch({ type: "createFeature", title: featureTitle })
            .then((result) => {
              if (result.success) {
                setCreating(false);
                setFeatureTitle("");
                onRefine((result.data as { id: string }).id);
              }
            });
          return;
        }
        if (key.length === 1) setFeatureTitle((value) => value + key);
        return;
      }

      if (actionModalOpen) {
        if (key === "escape" || key === "q") {
          setActionModalOpen(false);
          return;
        }
        if (key === "up") {
          setSelectedAction((prev) => Math.max(0, prev - 1));
          return;
        }
        if (key === "down") {
          setSelectedAction((prev) =>
            Math.min(FEATURE_ACTIONS.length - 1, prev + 1),
          );
          return;
        }
        if (key === "return") {
          const action = FEATURE_ACTIONS[selectedAction];
          if (action === "View" && filteredFeatures[selectedIndex]) {
            onView(filteredFeatures[selectedIndex]!.id);
            setActionModalOpen(false);
            return;
          }
          if (action === "Refine" && filteredFeatures[selectedIndex]) {
            const feature = filteredFeatures[selectedIndex]!;
            void commandBus
              .dispatch({
                type: "updateFeatureMetadata",
                featureId: feature.id,
                lifecycle: "in_progress",
              })
              .finally(() => onRefine(feature.id));
          }
          setActionModalOpen(false);
          return;
        }
        return;
      }

      if (key === "/") {
        setSearching(true);
        setSearchQuery("");
        return;
      }
      if (key === "n") {
        setCreating(true);
        return;
      }
      if (key === "q") {
        onExit();
        return;
      }
      if (key === "up") {
        setSelectedIndex((prev) => Math.max(0, prev - 1));
        return;
      }
      if (key === "down") {
        setSelectedIndex((prev) =>
          Math.min(filteredFeatures.length - 1, prev + 1),
        );
        return;
      }
      if (key === "return") {
        if (filteredFeatures.length > 0) {
          setActionModalOpen(true);
          setSelectedAction(0);
        }
        return;
      }
    },
    [
      searching,
      creating,
      featureTitle,
      actionModalOpen,
      selectedAction,
      selectedIndex,
      filteredFeatures,
      commandBus,
      onExit,
      onRefine,
      onView,
    ],
  );

  useKeyboard(handleKeyDown);

  return [
    {
      features,
      portraits,
      selectedIndex,
      searchQuery,
      searching,
      creating,
      featureTitle,
      actionModalOpen,
      selectedAction,
      tip,
      filteredFeatures,
    },
    { handleKeyDown },
  ];
}
