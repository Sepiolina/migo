/** Display the modifier used for Studio commands on this platform. */
export function shortcut(key: string, shift = false): string {
  const apple = /Mac|iPhone|iPad|iPod/i.test(navigator.platform);
  if (apple) return `${shift ? "⇧" : ""}⌘${key === "Enter" ? "↵" : key}`;
  return `Ctrl+${shift ? "Shift+" : ""}${key}`;
}
