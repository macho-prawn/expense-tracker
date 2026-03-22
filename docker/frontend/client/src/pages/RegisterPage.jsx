import { useState } from "react";
import { useNavigate } from "react-router-dom";
import AuthLayout from "../components/AuthLayout";
import { useErrorToast } from "../components/ErrorToastProvider";

export default function RegisterPage({ onRegister }) {
  const navigate = useNavigate();
  const { showError } = useErrorToast();
  const [form, setForm] = useState({ name: "", email: "", password: "" });
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event) {
    event.preventDefault();
    setSubmitting(true);

    try {
      await onRegister(form);
      navigate("/dashboard");
    } catch (requestError) {
      showError(requestError.message);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthLayout
      title="Create your account"
      subtitle="Start a private workspace for real shared trips, weekends, and group tabs."
      footerText="Already have an account?"
      footerLink="/login"
      footerLabel="Log in"
    >
      <form className="stack-form" onSubmit={handleSubmit}>
        <label>
          <span>Name</span>
          <input
            autoComplete="name"
            name="name"
            type="text"
            value={form.name}
            onChange={(event) => setForm({ ...form, name: event.target.value })}
            required
          />
        </label>
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
            autoComplete="new-password"
            name="password"
            type="password"
            value={form.password}
            onChange={(event) => setForm({ ...form, password: event.target.value })}
            required
            minLength={8}
          />
        </label>

        <button className="primary-button" type="submit" disabled={submitting}>
          {submitting ? "Creating account..." : "Create account"}
        </button>
      </form>
    </AuthLayout>
  );
}
