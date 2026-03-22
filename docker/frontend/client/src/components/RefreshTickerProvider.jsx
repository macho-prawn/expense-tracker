import { createContext, useContext, useEffect, useState } from "react";

const RefreshTickerContext = createContext(0);

export function RefreshTickerProvider({ children }) {
  const [tick, setTick] = useState(0);

  useEffect(() => {
    const intervalId = window.setInterval(() => {
      setTick((currentTick) => currentTick + 1);
    }, 60000);

    return () => window.clearInterval(intervalId);
  }, []);

  return <RefreshTickerContext.Provider value={tick}>{children}</RefreshTickerContext.Provider>;
}

export function useRefreshTicker() {
  return useContext(RefreshTickerContext);
}
