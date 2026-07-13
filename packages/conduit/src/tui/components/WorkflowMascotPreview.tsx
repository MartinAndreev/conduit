import { useEffect, useState } from "react";

const CELL_WIDTH = 4;
const CELL_HEIGHT = 3;
const DOT_WIDTH = CELL_WIDTH * 2;
const DOT_HEIGHT = CELL_HEIGHT * 4;

export const WORKFLOW_ROLES = [
  "architect",
  "researcher",
  "frontend",
  "backend",
  "qa",
  "documentation",
  "reviewer",
] as const;

export type WorkflowRole = (typeof WORKFLOW_ROLES)[number];

const ROLE_COLORS: Record<WorkflowRole, string> = {
  architect: "#8FB6A0",
  researcher: "#D8C28B",
  frontend: "#A7C4B4",
  backend: "#6F9478",
  qa: "#E5E1D4",
  documentation: "#C8B77A",
  reviewer: "#A8B5AC",
};

const BRAILLE_BITS = [
  [0x1, 0x2, 0x4, 0x40],
  [0x8, 0x10, 0x20, 0x80],
] as const;

function drawBrailleRobot(role: WorkflowRole, frame: number): string {
  const dots = new Uint8Array(CELL_WIDTH * CELL_HEIGHT);
  const dot = (x: number, y: number) => {
    if (x < 0 || y < 0 || x >= DOT_WIDTH || y >= DOT_HEIGHT) return;
    const cellX = Math.floor(x / 2);
    const cellY = Math.floor(y / 4);
    dots[cellY * CELL_WIDTH + cellX]! |= BRAILLE_BITS[x % 2]![y % 4]!;
  };
  const line = (x1: number, y1: number, x2: number, y2: number) => {
    for (let x = x1; x <= x2; x++) dot(x, y1);
    for (let y = y1; y <= y2; y++) dot(x1, y);
  };
  // Minimal monitor head: three cells wide and two cells high.
  line(0, 4, 5, 4);
  line(0, 4, 0, 10);
  line(5, 4, 5, 10);
  line(0, 10, 5, 10);

  switch (role) {
    case "architect": {
      // A thinking antenna sway and an occasional one-eye wink.
      dot(frame % 4 < 2 ? 2 : 3, 2);
      dot(2, 6);
      dot(2, 7);
      if (frame % 6 === 5) dot(4, 7);
      else {
        dot(4, 6);
        dot(4, 7);
      }
      dot(3, 9);
      break;
    }
    case "researcher": {
      // Eyes scan between two columns while a note peeks out at the side.
      const shift = frame % 4 < 2 ? 0 : 1;
      dot(2 + shift, 6);
      dot(2 + shift, 7);
      dot(4 + shift, 6);
      dot(4 + shift, 7);
      line(6, 5, 7, 9);
      if (frame % 3 === 0) dot(7, 6);
      break;
    }
    case "frontend": {
      // The monitor alternates between a single canvas and two UI panels.
      dot(2, 6);
      dot(2, 7);
      dot(4, 6);
      dot(4, 7);
      if (frame % 4 < 2) line(3, 5, 3, 9);
      else {
        dot(1, 5);
        dot(5, 5);
      }
      break;
    }
    case "backend": {
      // A terminal prompt eye and a blinking cursor on the right edge.
      dot(2, 6);
      dot(3, 7);
      dot(2, 8);
      dot(4, 6);
      dot(4, 7);
      if (frame % 2 === 0) dot(6, 8);
      break;
    }
    case "qa": {
      // Focused eyes, then a tiny check confirms the assertion.
      dot(2, 7);
      dot(4, 7);
      if (frame % 4 >= 2) {
        dot(6, 9);
        dot(7, 10);
      }
      break;
    }
    case "documentation": {
      // Robot hides then peeks out from a page on the right.
      dot(2, 6);
      dot(2, 7);
      dot(4, 6);
      dot(4, 7);
      line(6, 4, 7, 10);
      if (frame % 4 < 2) dot(5, 8);
      break;
    }
    case "reviewer": {
      // A magnifier moves over the right eye, then returns to neutral.
      dot(2, 6);
      dot(2, 7);
      dot(4, 6);
      dot(4, 7);
      if (frame % 4 < 2) {
        dot(5, 5);
        dot(6, 6);
        dot(6, 7);
        dot(7, 8);
      }
      break;
    }
  }

  return Array.from({ length: CELL_HEIGHT }, (_, row) =>
    Array.from({ length: CELL_WIDTH }, (_, column) =>
      String.fromCodePoint(0x2800 + dots[row * CELL_WIDTH + column]!),
    ).join(""),
  ).join("\n");
}

interface WorkflowMascotPreviewProps {
  readonly role: WorkflowRole;
}

/** 4×3 Braille mascot: a compact 8×12-dot animated role marker. */
export function WorkflowMascotPreview({ role }: WorkflowMascotPreviewProps) {
  const [frame, setFrame] = useState(0);

  useEffect(() => {
    const timer = setInterval(() => setFrame((current) => current + 1), 280);
    return () => clearInterval(timer);
  }, []);

  return (
    <box width={CELL_WIDTH} height={CELL_HEIGHT} flexShrink={0}>
      <text content={drawBrailleRobot(role, frame)} fg={ROLE_COLORS[role]} />
    </box>
  );
}
