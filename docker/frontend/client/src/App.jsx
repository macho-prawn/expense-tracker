import { createContext, useContext, useEffect, useMemo, useState } from "react";
import { Navigate, Route, Routes, useNavigate } from "react-router-dom";
import AppShell from "./components/AppShell";
import { ErrorToastProvider, useErrorToast } from "./components/ErrorToastProvider";
import { ProtectedRoute, PublicOnlyRoute } from "./components/RouteGuards";
import { apiRequest } from "./lib/api";
import DashboardPage from "./pages/DashboardPage";
import GroupPage from "./pages/GroupPage";
import LandingPage from "./pages/LandingPage";
import LoginPage from "./pages/LoginPage";
import NotFoundPage from "./pages/NotFoundPage";
import RegisterPage from "./pages/RegisterPage";

const AuthContext = createContext(null);

function AuthProvider({ children }) {
  const { showError } = useErrorToast();
  const [user, setUser] = useState(null);
  const [loading, setLoading] = useState(true);

  async function refreshSession() {
    setLoading(true);
    try {
      const response = await apiRequest("/v1/auth/me");
      setUser(response.user);
    } catch (error) {
      if (error.status !== 401) {
        console.error(error);
        showError(error.message);
      }
      setUser(null);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    refreshSession();
  }, [showError]);

  const value = useMemo(
    () => ({
      user,
      loading,
      refreshSession,
      async login(payload) {
        const response = await apiRequest("/v1/auth/login", {
          method: "POST",
          body: payload
        });
        setUser(response.user);
        return response.user;
      },
      async register(payload) {
        const response = await apiRequest("/v1/auth/register", {
          method: "POST",
          body: payload
        });
        setUser(response.user);
        return response.user;
      },
      async logout() {
        await apiRequest("/v1/auth/logout", { method: "POST" });
        setUser(null);
      }
    }),
    [loading, user, showError]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

function AppRoutes() {
  const auth = useContext(AuthContext);
  const navigate = useNavigate();

  async function handleLogout() {
    try {
      await auth.logout();
    } finally {
      navigate("/");
    }
  }

  return (
    <AppShell user={auth.user} onLogout={handleLogout}>
      <Routes>
        <Route index element={<LandingPage />} />

        <Route element={<PublicOnlyRoute isLoading={auth.loading} user={auth.user} />}>
          <Route path="/login" element={<LoginPage onLogin={auth.login} />} />
          <Route path="/register" element={<RegisterPage onRegister={auth.register} />} />
        </Route>

        <Route element={<ProtectedRoute isLoading={auth.loading} user={auth.user} />}>
          <Route path="/dashboard" element={<DashboardPage />} />
          <Route path="/groups/:groupId" element={<GroupPage />} />
        </Route>

        <Route path="/home" element={<Navigate replace to="/" />} />
        <Route path="*" element={<NotFoundPage />} />
      </Routes>
    </AppShell>
  );
}

export default function App() {
  return (
    <ErrorToastProvider>
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </ErrorToastProvider>
  );
}
