import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useErrorToast } from "../components/ErrorToastProvider";
import { useRefreshTicker } from "../components/RefreshTickerProvider";
import { apiRequest } from "../lib/api";
import { SUPPORTED_CURRENCIES } from "../lib/currencies";
import { formatDate } from "../lib/format";

function expenseReportFilename(contentDisposition, fallbackName) {
  const match = /filename="([^"]+)"/i.exec(contentDisposition || "");
  return match?.[1] || fallbackName;
}

export default function DashboardPage() {
  const navigate = useNavigate();
  const { showError } = useErrorToast();
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [newGroupName, setNewGroupName] = useState("");
  const [baseCurrency, setBaseCurrency] = useState("USD");
  const [creatingGroup, setCreatingGroup] = useState(false);
  const [acceptingInvitationID, setAcceptingInvitationID] = useState("");
  const [deletingGroupID, setDeletingGroupID] = useState("");
  const [archivingGroupID, setArchivingGroupID] = useState("");
  const [unarchivingGroupID, setUnarchivingGroupID] = useState("");
  const [reportMenuGroupID, setReportMenuGroupID] = useState("");
  const refreshTick = useRefreshTicker();

  async function loadDashboard({ silent = false } = {}) {
    if (!silent) {
      setLoading(true);
      setLoadFailed(false);
    }

    try {
      const response = await apiRequest("/v1/dashboard");
      setData(response);
      setLoadFailed(false);
    } catch (requestError) {
      if (!silent || !data) {
        setLoadFailed(true);
      }
      showError(requestError.message);
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  }

  useEffect(() => {
    loadDashboard();
  }, []);

  useEffect(() => {
    if (refreshTick === 0) {
      return;
    }
    loadDashboard({ silent: true });
  }, [refreshTick]);

  async function handleCreateGroup(event) {
    event.preventDefault();
    if (!newGroupName.trim()) {
      return;
    }

    setCreatingGroup(true);

    try {
      const response = await apiRequest("/v1/groups", {
        method: "POST",
        body: { name: newGroupName, baseCurrency }
      });
      setNewGroupName("");
      navigate(`/groups/${response.id}`);
    } catch (requestError) {
      showError(requestError.message);
    } finally {
      setCreatingGroup(false);
    }
  }

  async function handleAcceptInvitation(invitationID) {
    setAcceptingInvitationID(invitationID);

    try {
      const response = await apiRequest(`/v1/invitations/${invitationID}/accept`, {
        method: "POST"
      });
      navigate(`/groups/${response.group.id}`);
    } catch (requestError) {
      showError(requestError.message);
    } finally {
      setAcceptingInvitationID("");
    }
  }

  async function handleDeleteGroup(groupID) {
    setDeletingGroupID(groupID);

    try {
      await apiRequest(`/v1/groups/${groupID}`, {
        method: "DELETE"
      });
      await loadDashboard({ silent: true });
    } catch (requestError) {
      showError(requestError.message);
    } finally {
      setDeletingGroupID("");
    }
  }

  async function handleArchiveGroup(groupID) {
    setArchivingGroupID(groupID);

    try {
      await apiRequest(`/v1/groups/${groupID}/archive`, {
        method: "POST"
      });
      await loadDashboard({ silent: true });
    } catch (requestError) {
      showError(requestError.message);
    } finally {
      setArchivingGroupID("");
    }
  }

  async function handleUnarchiveGroup(groupID) {
    setUnarchivingGroupID(groupID);

    try {
      await apiRequest(`/v1/groups/${groupID}/unarchive`, {
        method: "POST"
      });
      await loadDashboard({ silent: true });
    } catch (requestError) {
      showError(requestError.message);
    } finally {
      setUnarchivingGroupID("");
    }
  }

  function handleOpenGroup(groupID) {
    navigate(`/groups/${groupID}`);
  }

  function handleOpenGroupKeyDown(event, groupID) {
    if (event.key !== "Enter" && event.key !== " ") {
      return;
    }
    event.preventDefault();
    handleOpenGroup(groupID);
  }

  function toggleReportMenu(event, groupID) {
    event.stopPropagation();
    setReportMenuGroupID((currentID) => (currentID === groupID ? "" : groupID));
  }

  async function downloadExpenseReport(event, groupID, format) {
    event.stopPropagation();
    setReportMenuGroupID("");

    if (!groupID) {
      showError("Unable to download expense report.");
      return;
    }

    const normalizedFormat = String(format || "").trim().toLowerCase();
    if (normalizedFormat !== "csv" && normalizedFormat !== "json") {
      showError("Invalid expense report format.");
      return;
    }

    const requestPath = `/api/v1/groups/${groupID}/expense-report?format=${encodeURIComponent(normalizedFormat)}`;
    const browserTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;

    try {
      const response = await fetch(requestPath, {
        method: "GET",
        credentials: "include",
        headers: browserTimeZone
          ? {
              "X-Time-Zone": browserTimeZone
            }
          : undefined
      });

      if (!response.ok) {
        const contentType = response.headers.get("content-type") || "";
        let message = `Request failed with status ${response.status}`;

        if (contentType.includes("application/json")) {
          const payload = await response.json();
          message = payload?.detail || payload?.message || payload?.title || message;
        } else {
          const payload = (await response.text()).trim();
          if (payload) {
            message = payload;
          }
        }

        throw new Error(message);
      }

      const blob = await response.blob();
      const objectURL = window.URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = objectURL;
      link.download = expenseReportFilename(
        response.headers.get("content-disposition"),
        `expense-report.${normalizedFormat}`
      );
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      window.setTimeout(() => window.URL.revokeObjectURL(objectURL), 0);
    } catch (requestError) {
      showError(requestError.message);
    }
  }

  if (loading) {
    return <div className="state-panel">Loading dashboard...</div>;
  }

  if (loadFailed && !data) {
    return <div className="state-panel error-state">Unable to load dashboard.</div>;
  }

  const groups = data?.groups || [];
  const invitations = data?.invitations || [];

  return (
    <section className="dashboard-grid">
      <div className="panel">
        <div className="section-heading">
          <div>
            <span className="eyebrow">Workspace</span>
            <h1>Your shared groups</h1>
          </div>
          <p className="muted-text">
            Only groups you belong to are listed here. Pending email invitations are shown
            separately below.
          </p>
        </div>

        <form className="inline-form dashboard-create-form" onSubmit={handleCreateGroup}>
          <input
            type="text"
            name="groupName"
            placeholder="Weekend in Lisbon"
            value={newGroupName}
            onChange={(event) => setNewGroupName(event.target.value)}
            required
          />
          <select value={baseCurrency} onChange={(event) => setBaseCurrency(event.target.value)}>
            {SUPPORTED_CURRENCIES.map((currency) => (
              <option key={currency.code} value={currency.code}>
                Base {currency.label}
              </option>
            ))}
          </select>
          <button className="primary-button" type="submit" disabled={creatingGroup}>
            {creatingGroup ? "Creating..." : "Create group"}
          </button>
        </form>

        <div className="stack-list">
          {groups.length === 0 ? (
            <div className="empty-state">
              <strong>No groups yet.</strong>
              <span>Create your first shared tab to start tracking expenses.</span>
            </div>
          ) : (
            groups.map((group) => (
              <div
                className="list-card dashboard-group-card"
                key={group.id}
                title={`Members: ${group.memberCount}\nExpenses: ${group.expenseCount}\nCreated: ${formatDate(group.createdAt)}`}
                aria-label={`${group.name}. ${group.memberCount} members. ${group.expenseCount} expenses. Created ${formatDate(group.createdAt)}.`}
                role="link"
                tabIndex={0}
                onClick={() => handleOpenGroup(group.id)}
                onKeyDown={(event) => handleOpenGroupKeyDown(event, group.id)}
              >
                <div>
                  <strong className="dashboard-group-link">{group.name}</strong>
                </div>

                <div className="dashboard-group-actions">
                  {group.isOwner ? (
                    <div className="dashboard-report-actions">
                      <button
                        className="secondary-button"
                        type="button"
                        onClick={(event) => toggleReportMenu(event, group.id)}
                      >
                        Expense Report
                      </button>
                      {reportMenuGroupID === group.id ? (
                        <div className="dashboard-report-menu">
                          <button
                            className="secondary-button dashboard-report-format-button"
                            type="button"
                            onClick={(event) => downloadExpenseReport(event, group.id, "csv")}
                          >
                            Download CSV
                          </button>
                          <button
                            className="secondary-button dashboard-report-format-button"
                            type="button"
                            onClick={(event) => downloadExpenseReport(event, group.id, "json")}
                          >
                            Download JSON
                          </button>
                        </div>
                      ) : null}
                    </div>
                  ) : null}
                  {group.canDelete ? (
                    <button
                      className="secondary-button"
                      type="button"
                      onClick={(event) => {
                        event.stopPropagation();
                        handleDeleteGroup(group.id);
                      }}
                      disabled={deletingGroupID === group.id || archivingGroupID === group.id || unarchivingGroupID === group.id}
                    >
                      {deletingGroupID === group.id ? "Deleting..." : "Delete"}
                    </button>
                  ) : null}
                  {group.canArchive ? (
                    <button
                      className="secondary-button"
                      type="button"
                      onClick={(event) => {
                        event.stopPropagation();
                        handleArchiveGroup(group.id);
                      }}
                      disabled={deletingGroupID === group.id || archivingGroupID === group.id || unarchivingGroupID === group.id}
                    >
                      {archivingGroupID === group.id ? "Archiving..." : "Archive"}
                    </button>
                  ) : null}
                  {group.canUnarchive ? (
                    <button
                      className="secondary-button"
                      type="button"
                      onClick={(event) => {
                        event.stopPropagation();
                        handleUnarchiveGroup(group.id);
                      }}
                      disabled={deletingGroupID === group.id || archivingGroupID === group.id || unarchivingGroupID === group.id}
                    >
                      {unarchivingGroupID === group.id ? "Restoring..." : "Unarchive"}
                    </button>
                  ) : null}
                </div>
              </div>
            ))
          )}
        </div>
      </div>

      <div className="panel">
        <div className="section-heading">
          <div>
            <span className="eyebrow">Pending access</span>
            <h2>Invitations for your email</h2>
          </div>
        </div>

        <div className="stack-list">
          {invitations.length === 0 ? (
            <div className="empty-state">
              <strong>No pending invitations.</strong>
              <span>Any group invite sent to your account email will appear here.</span>
            </div>
          ) : (
            invitations.map((invitation) => (
              <div className="list-card invitation-card" key={invitation.id}>
                <div>
                  <strong>{invitation.groupName}</strong>
                  <span>
                    Invited by {invitation.invitedByName} on {formatDate(invitation.createdAt)}
                  </span>
                </div>
                <button
                  className="primary-button"
                  type="button"
                  onClick={() => handleAcceptInvitation(invitation.id)}
                  disabled={acceptingInvitationID === invitation.id}
                >
                  {acceptingInvitationID === invitation.id ? "Joining..." : "Accept"}
                </button>
              </div>
            ))
          )}
        </div>
      </div>
    </section>
  );
}
