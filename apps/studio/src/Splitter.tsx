import { useRef } from "react";

/**
 * A drag handle that resizes a panel. `value` is the panel's size in pixels;
 * `invert` is for panels that grow as the handle moves left or up (the
 * inspector, the console). Arrow keys move it by 16px, double-click resets it.
 */
export function Splitter({
  label,
  axis,
  value,
  min,
  max,
  fallback,
  invert,
  className,
  onChange,
}: {
  label: string;
  axis: "x" | "y";
  value: number;
  min: number;
  max: number;
  fallback: number;
  invert?: boolean;
  className: string;
  onChange: (value: number) => void;
}) {
  const drag = useRef<{ start: number; from: number } | null>(null);
  const clamp = (v: number) => Math.round(Math.min(Math.max(v, min), Math.max(min, max)));
  const pos = (e: React.PointerEvent) => (axis === "x" ? e.clientX : e.clientY);
  const sign = invert ? -1 : 1;
  const grow = axis === "x" ? (invert ? "ArrowLeft" : "ArrowRight") : invert ? "ArrowUp" : "ArrowDown";
  const shrink = axis === "x" ? (invert ? "ArrowRight" : "ArrowLeft") : invert ? "ArrowDown" : "ArrowUp";
  return (
    <div
      className={`split ${axis} ${className}`}
      role="separator"
      aria-label={label}
      aria-orientation={axis === "x" ? "vertical" : "horizontal"}
      aria-valuenow={value}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      title={`${label} (double-click to reset)`}
      onPointerDown={(e) => {
        if (e.button !== 0) return;
        e.preventDefault();
        e.currentTarget.setPointerCapture?.(e.pointerId);
        drag.current = { start: pos(e), from: value };
        document.body.classList.add(axis === "x" ? "resizing-x" : "resizing-y");
      }}
      onPointerMove={(e) => {
        if (drag.current) onChange(clamp(drag.current.from + sign * (pos(e) - drag.current.start)));
      }}
      onPointerUp={(e) => {
        drag.current = null;
        e.currentTarget.releasePointerCapture?.(e.pointerId);
        document.body.classList.remove("resizing-x", "resizing-y");
      }}
      onPointerCancel={() => {
        drag.current = null;
        document.body.classList.remove("resizing-x", "resizing-y");
      }}
      onDoubleClick={() => onChange(clamp(fallback))}
      onKeyDown={(e) => {
        if (e.key === grow) onChange(clamp(value + 16));
        else if (e.key === shrink) onChange(clamp(value - 16));
        else return;
        e.preventDefault();
      }}
    />
  );
}
