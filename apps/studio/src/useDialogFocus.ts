import { useLayoutEffect, type RefObject } from "react";

export function useDialogFocus(
  dialogRef: RefObject<HTMLElement | null>,
  initialRef: RefObject<HTMLElement | null> = dialogRef,
  returnFocusTo?: HTMLElement | null,
) {
  useLayoutEffect(() => {
    const dialog = dialogRef.current;
    const opener = returnFocusTo ?? document.activeElement;
    initialRef.current?.focus();
    return () => {
      const active = document.activeElement;
      if (opener instanceof HTMLElement && opener.isConnected && (active === document.body || dialog?.contains(active))) {
        opener.focus({ preventScroll: true });
      }
    };
  }, [dialogRef, initialRef, returnFocusTo]);
}
