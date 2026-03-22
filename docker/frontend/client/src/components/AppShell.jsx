import { Link, NavLink } from "react-router-dom";

export default function AppShell({ user, onLogout, children }) {
  return (
    <div className="app-shell">
      <header className="topbar">
        <Link className="brand" to={user ? "/dashboard" : "/"}>
          <span className="brand-mark">ST</span>
          <span>
            <strong>ShareTab</strong>
            <small>Shared expense tracker</small>
          </span>
        </Link>

        <nav className="topnav">
          {user ? (
            <>
              <NavLink className="primary-link" to="/dashboard">
                Dashboard
              </NavLink>
              <button className="secondary-button" type="button" onClick={onLogout}>
                Log out
              </button>
            </>
          ) : (
            <>
              <NavLink className="primary-link" to="/login">
                Log in
              </NavLink>
              <NavLink className="primary-link" to="/register">
                Create account
              </NavLink>
            </>
          )}
        </nav>
      </header>

      <main className="page-frame">{children}</main>
    </div>
  );
}
