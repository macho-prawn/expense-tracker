import { Navigate, Outlet } from "react-router-dom";
import { RefreshTickerProvider } from "./RefreshTickerProvider";

export function ProtectedRoute({ isLoading, user }) {
  if (isLoading) {
    return <div className="state-panel">Loading session...</div>;
  }

  if (!user) {
    return <Navigate to="/login" replace />;
  }

  return (
    <RefreshTickerProvider>
      <Outlet />
    </RefreshTickerProvider>
  );
}

export function PublicOnlyRoute({ isLoading, user }) {
  if (isLoading) {
    return <div className="state-panel">Loading session...</div>;
  }

  if (user) {
    return <Navigate to="/dashboard" replace />;
  }

  return <Outlet />;
}
