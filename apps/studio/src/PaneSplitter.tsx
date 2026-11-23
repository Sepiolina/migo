import { useRef } from "react";

export const splitterWidth = 8;

const minNavigator = 220;
const minInspector = 280;
const minTable = 360;

export type SidePane = "navigator" | "inspector";
export type PanePreferences = Record<SidePane, number> & { priority: SidePane };

function clampPaneWidth(width: number, limits: { min: number; max: number }) {
  return Number.isFinite(width) ? Math.min(limits.max, Math.max(limits.min, Math.round(width))) : limits.min;
}

/** Fit preferred widths into the viewport without replacing saved preferences. */
export function paneSizes(viewport: number, showNavigator: boolean, showInspector: boolean, preferred: PanePreferences) {
  const minimum = { navigator: showNavigator ? minNavigator : 0, inspector: showInspector ? minInspector : 0 };
  const dividers = splitterWidth * (Number(showNavigator) + Number(showInspector));
  const space = Math.max(minimum.navigator + minimum.inspector, viewport - minTable - dividers);
  const first: SidePane = preferred.priority === "inspector" ? "inspector" : "navigator";
  const second = first === "navigator" ? "inspector" : "navigator";
  const fit = (pane: SidePane, reserved: number) => minimum[pane]
    ? clampPaneWidth(preferred[pane], { min: minimum[pane], max: space - reserved })
    : 0;
  const widths = { navigator: 0, inspector: 0 };
  widths[first] = fit(first, minimum[second]);
  widths[second] = fit(second, widths[first]);
  return {
    navigator: { width: widths.navigator, min: minNavigator, max: space - widths.inspector },
    inspector: { width: widths.inspector, min: minInspector, max: space - widths.navigator },
  };
}

interface Props {
  label: string;
  controls: string;
  width: number;
  min: number;
  max: number;
  side: "left" | "right";
  onResize: (width: number) => void;
}

export function PaneSplitter({ label, controls, width, min, max, side, onResize }: Props) {
  const drag = useRef<{ pointer: number; x: number; width: number; max: number } | null>(null);
  const direction = side === "left" ? 1 : -1;
  const resize = (value: number, limit = max) => {
    const next = clampPaneWidth(value, { min, max: limit });
    if (next !== width) onResize(next);
  };
  return (
    <div
      className="pane-splitter"
      role="separator"
      tabIndex={0}
      aria-label={label}
      aria-controls={controls}
      aria-orientation="vertical"
      aria-valuemin={min}
      aria-valuenow={width}
      aria-valuemax={max}
      aria-valuetext={`${width} pixels`}
      onKeyDown={(event) => {
        let next: number;
        switch (event.key) {
          case "Home": next = min; break;
          case "End": next = max; break;
          case "ArrowLeft": next = width - 16 * direction; break;
          case "ArrowRight": next = width + 16 * direction; break;
          default: return;
        }
        event.preventDefault();
        resize(next);
      }}
      onPointerDown={(event) => {
        if (event.button !== 0 || drag.current) return;
        event.preventDefault();
        event.currentTarget.focus({ preventScroll: true });
        event.currentTarget.setPointerCapture(event.pointerId);
        drag.current = { pointer: event.pointerId, x: event.clientX, width, max };
      }}
      onPointerMove={(event) => {
        const start = drag.current;
        if (start?.pointer !== event.pointerId) return;
        resize(start.width + (event.clientX - start.x) * direction, start.max);
      }}
      onLostPointerCapture={() => { drag.current = null; }}
    />
  );
}
