import { createContext, useContext, useEffect, useMemo, useRef, useState } from "react";

const ErrorToastContext = createContext(null);
const TOAST_DURATION_MS = 10000;
const TOAST_EXIT_MS = 350;

export function ErrorToastProvider({ children }) {
  const [message, setMessage] = useState("");
  const [isVisible, setIsVisible] = useState(false);
  const [isLeaving, setIsLeaving] = useState(false);
  const dismissTimerRef = useRef(null);
  const exitTimerRef = useRef(null);

  function clearTimers() {
    if (dismissTimerRef.current) {
      window.clearTimeout(dismissTimerRef.current);
      dismissTimerRef.current = null;
    }
    if (exitTimerRef.current) {
      window.clearTimeout(exitTimerRef.current);
      exitTimerRef.current = null;
    }
  }

  function dismiss() {
    setIsLeaving(true);
    exitTimerRef.current = window.setTimeout(() => {
      setIsVisible(false);
      setIsLeaving(false);
      setMessage("");
      exitTimerRef.current = null;
    }, TOAST_EXIT_MS);
  }

  function showError(nextMessage) {
    const normalizedMessage = String(nextMessage || "").trim();
    if (!normalizedMessage) {
      return;
    }

    clearTimers();
    setMessage(normalizedMessage);
    setIsVisible(true);
    setIsLeaving(false);
    dismissTimerRef.current = window.setTimeout(() => {
      dismiss();
      dismissTimerRef.current = null;
    }, TOAST_DURATION_MS);
  }

  useEffect(() => clearTimers, []);

  const value = useMemo(
    () => ({
      showError,
      dismiss
    }),
    []
  );

  return (
    <ErrorToastContext.Provider value={value}>
      {children}
      {isVisible ? (
        <div
          className={`global-error-toast${isLeaving ? " global-error-toast-leaving" : ""}`}
          role="alert"
          aria-live="assertive"
        >
          <span>{message}</span>
          <button type="button" className="toast-dismiss-button" onClick={dismiss} aria-label="Dismiss error">
            Dismiss
          </button>
        </div>
      ) : null}
    </ErrorToastContext.Provider>
  );
}

export function useErrorToast() {
  const context = useContext(ErrorToastContext);
  if (!context) {
    throw new Error("useErrorToast must be used within ErrorToastProvider");
  }
  return context;
}
