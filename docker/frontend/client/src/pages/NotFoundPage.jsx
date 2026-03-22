import { Link } from "react-router-dom";

export default function NotFoundPage() {
  return (
    <div className="state-panel">
      <strong>Page not found.</strong>
      <Link className="secondary-link" to="/">
        Return home
      </Link>
    </div>
  );
}

