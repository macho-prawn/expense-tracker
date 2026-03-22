import { useState } from "react";
import { useNavigate } from "react-router-dom";
import AuthLayout from "../components/AuthLayout";
import { useErrorToast } from "../components/ErrorToastProvider";

export default function LoginPage({ onLogin }) {
  const navigate = useNavigate();
  const { showError } = useErrorToast();
  const [form, setForm] = useState({ email: "", password: "" });
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event) {
    event.preventDefault();
    setSubmitting(true);

    try {
      await onLogin(form);
      navigate("/dashboard");
    } catch (requestError) {
      showError(requestError.message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthLayout
      title="Welcome back"
      subtitle="Use your account to return to your groups, pending invites, and live balances."
      footerText="Need an account?"
      footerLink="/register"
      footerLabel="Create one"
    >
      <form className="stack-form" onSubmit={handleSubmit}>
        <label>
          <span>Email</span>
          <input
            autoComplete="email"
            name="email"
            type="email"
            value={form.email}
            onChange={(event) => setForm({ ...form, email: event.target.value })}
            required
          />
        </label>
        <label>
          <span>Password</span>
          <input
            autoComplete="current-password"
            name="password"
            type="password"
            value={form.password}
            onChange={(event) => setForm({ ...form, password: event.target.value })}
            required
            minLength={8}
          />
        </label>

        <button className="primary-button" type="submit" disabled={submitting}>
          {submitting ? "Signing in..." : "Log in"}
        </button>
      </form>
    </AuthLayout>
  );
}
