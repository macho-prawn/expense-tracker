import { Link } from "react-router-dom";

export default function AuthLayout({
  title,
  subtitle,
  footerText,
  footerLink,
  footerLabel,
  children
}) {
  return (
    <section className="auth-page">
      <div className="auth-panel">
        <span className="eyebrow">ShareTab account</span>
        <h1>{title}</h1>
        <p>{subtitle}</p>
        {children}
        <p className="muted-text">
          {footerText} <Link to={footerLink}>{footerLabel}</Link>
        </p>
      </div>
    </section>
  );
}

