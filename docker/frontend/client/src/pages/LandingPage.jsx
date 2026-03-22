import { Link } from "react-router-dom";

const features = [
  "Track trip, household, or event expenses with real membership boundaries.",
  "Split each purchase across selected participants instead of forcing everyone into every charge.",
  "See who owes whom with simplified settlement suggestions that stay accurate as the group grows."
];

export default function LandingPage() {
  return (
    <section className="landing-page">
      <div className="hero-card">
        <div className="hero-copy">
          <span className="eyebrow">Production-focused group tabs</span>
          <h1>Clean shared-expense tracking that people will actually use.</h1>
          <p>
            ShareTab keeps group visibility strict, sessions durable, and balances clear for
            trips, roommates, and friend groups.
          </p>

          <div className="hero-actions">
            <Link className="primary-link large-link" to="/register">
              Start a shared tab
            </Link>
            <Link className="secondary-link large-link" to="/login">
              Log in
            </Link>
          </div>
        </div>

        <div className="hero-metrics">
          <div className="metric-card">
            <strong>Member-scoped</strong>
            <span>Only group members can read or mutate that group.</span>
          </div>
          <div className="metric-card">
            <strong>Split-aware</strong>
            <span>Every expense can target only the participants who shared it.</span>
          </div>
          <div className="metric-card">
            <strong>Session-persistent</strong>
            <span>Email/password auth stays active across reloads with secure cookies.</span>
          </div>
        </div>
      </div>

      <section className="feature-grid">
        {features.map((feature) => (
          <article className="panel" key={feature}>
            <h2>{feature.split(".")[0]}</h2>
            <p>{feature}</p>
          </article>
        ))}
      </section>
    </section>
  );
}
