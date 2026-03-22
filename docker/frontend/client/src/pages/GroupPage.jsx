import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useErrorToast } from "../components/ErrorToastProvider";
import { useRefreshTicker } from "../components/RefreshTickerProvider";
import { apiRequest } from "../lib/api";
import { formatCurrency, formatDate, parseAmountToCents, todayISODate } from "../lib/format";

const EXPENSE_CATEGORIES = ["food", "transport", "accommodation", "entertainment", "rent", "subscription"];

const CATEGORY_LABELS = {
  food: "Food",
  transport: "Transport",
  accommodation: "Accommodation",
  entertainment: "Entertainment",
  rent: "Rent",
  subscription: "Subscription",
  uncategorized: "Uncategorized"
};

function formatCategoryLabel(category) {
  const normalized = String(category || "").trim().toLowerCase();
  return CATEGORY_LABELS[normalized] || "Uncategorized";
}

function parseOptionalAmountToCents(rawValue) {
  const trimmed = String(rawValue || "").trim();
  if (!trimmed) {
    return 0;
  }
  return parseAmountToCents(trimmed);
}

function buildInitialCustomSplitInputs(members) {
  return Object.fromEntries((members || []).map((member) => [member.id, ""]));
}

function defaultDueDayOfMonth() {
  return String(new Date().getDate());
}

function firstSettleUpExpenseID(expenses) {
  return expenses[0]?.expenseId || "";
}

function firstSettleUpTransferUserID(transfers) {
  return transfers[0] ? `${transfers[0].fromUserId}:${transfers[0].toUserId}` : "";
}

function settlementTransferKey(transfer) {
  return `${transfer.fromUserId}:${transfer.toUserId}`;
}

const GROUP_PANEL_IDS = [
  "expense",
  "invite",
  "settlement",
  "transactions",
  "openExpenses",
  "chat"
];

function defaultGroupPanelState() {
  return GROUP_PANEL_IDS.reduce((state, panelId) => {
    state[panelId] = false;
    return state;
  }, {});
}

function groupPanelStorageKey(userId, groupId) {
  return `group-panels:${userId}:${groupId}`;
}

function loadStoredGroupPanelState(userId, groupId) {
  if (!userId || !groupId || typeof window === "undefined") {
    return defaultGroupPanelState();
  }

  try {
    const rawValue = window.sessionStorage.getItem(groupPanelStorageKey(userId, groupId));
    if (!rawValue) {
      return defaultGroupPanelState();
    }

    const parsedValue = JSON.parse(rawValue);
    return GROUP_PANEL_IDS.reduce((state, panelId) => {
      state[panelId] = Boolean(parsedValue?.[panelId]);
      return state;
    }, {});
  } catch {
    return defaultGroupPanelState();
  }
}

export default function GroupPage() {
  const { groupId } = useParams();
  const { showError } = useErrorToast();
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [savingExpense, setSavingExpense] = useState(false);
  const [savingSettlement, setSavingSettlement] = useState(false);
  const [sendingInvite, setSendingInvite] = useState(false);
  const [sendingMessage, setSendingMessage] = useState(false);
  const [deletingExpenseId, setDeletingExpenseId] = useState("");
  const [deletingMessageId, setDeletingMessageId] = useState("");
  const [inviteEmail, setInviteEmail] = useState("");
  const [messageBody, setMessageBody] = useState("");
  const refreshTick = useRefreshTicker();
  const [expenseForm, setExpenseForm] = useState({
    description: "",
    category: "food",
    expenseType: "one-time",
    dueDayOfMonth: defaultDueDayOfMonth(),
    amount: "",
    currency: "",
    splitMode: "equal",
    paidByUserId: "",
    ownerUserId: "",
    participantUserIds: [],
    customSplitInputs: {},
    incurredOn: todayISODate()
  });
  const [settlementForm, setSettlementForm] = useState({
    expenseId: "",
    simplifiedTransferKey: "",
    toUserId: "",
    amount: "",
    currency: "",
    settledOn: todayISODate()
  });
  const [settlementMode, setSettlementMode] = useState("exact");
  const [panelState, setPanelState] = useState(() => defaultGroupPanelState());
  const [hasLoadedPanelState, setHasLoadedPanelState] = useState(false);
  const [summaryNow, setSummaryNow] = useState(() => new Date());

  async function loadGroup({ silent = false } = {}) {
    if (!silent) {
      setLoading(true);
      setLoadFailed(false);
    }

    try {
      const response = await apiRequest(`/v1/groups/${groupId}`);
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
    loadGroup();
  }, [groupId]);

  useEffect(() => {
    if (refreshTick === 0) {
      return;
    }
    loadGroup({ silent: true });
  }, [refreshTick, groupId]);

  useEffect(() => {
    if (!data?.currentUserId || !groupId) {
      setPanelState(defaultGroupPanelState());
      setHasLoadedPanelState(false);
      return;
    }

    setPanelState(loadStoredGroupPanelState(data.currentUserId, groupId));
    setHasLoadedPanelState(true);
  }, [data?.currentUserId, groupId]);

  useEffect(() => {
    if (!hasLoadedPanelState || !data?.currentUserId || !groupId || typeof window === "undefined") {
      return;
    }

    window.sessionStorage.setItem(
      groupPanelStorageKey(data.currentUserId, groupId),
      JSON.stringify(panelState)
    );
  }, [data?.currentUserId, groupId, hasLoadedPanelState, panelState]);

  useEffect(() => {
    const timerId = window.setInterval(() => {
      setSummaryNow(new Date());
    }, 1000);
    return () => window.clearInterval(timerId);
  }, []);

  useEffect(() => {
    if (!data) {
      return;
    }

    const members = data.members || [];
    const settleUpExpenses = data.balances?.settleUpExpenses || [];
    const settleUpTransfers = data.balances?.simplifiedSettleTo || [];
    const defaultParticipantIDs = members.map((member) => member.id);
    const fallbackPayer = data.currentUserId || members[0]?.id || "";
    const fallbackOwner = data.currentUserId || members[0]?.id || "";
    const fallbackSettlementExpenseID = firstSettleUpExpenseID(settleUpExpenses);
    const fallbackTransferUserID = firstSettleUpTransferUserID(settleUpTransfers);
    const fallbackSettlementOption =
      settleUpExpenses.find((expense) => expense.expenseId === fallbackSettlementExpenseID) || null;
    const fallbackTransferOption =
      settleUpTransfers.find((transfer) => settlementTransferKey(transfer) === fallbackTransferUserID) || null;
    const selectedSettlementOption =
      settleUpExpenses.find((expense) => expense.expenseId === settlementForm.expenseId) || null;
    const selectedSettlementTransfer =
      settleUpTransfers.find(
        (transfer) => settlementTransferKey(transfer) === settlementForm.simplifiedTransferKey
      ) || null;
    const hasExactOptions = settleUpExpenses.length > 0;
    const hasSimplifiedOptions = settleUpTransfers.length > 0;
    const fallbackCurrency = data.group?.baseCurrency || "USD";

    setExpenseForm((currentForm) => ({
      ...currentForm,
      paidByUserId: currentForm.paidByUserId || fallbackPayer,
      ownerUserId: currentForm.ownerUserId || fallbackOwner,
      dueDayOfMonth: currentForm.dueDayOfMonth || defaultDueDayOfMonth(),
      currency: currentForm.currency || fallbackCurrency,
      participantUserIds:
        currentForm.participantUserIds.length > 0
          ? currentForm.participantUserIds
          : defaultParticipantIDs,
      customSplitInputs: {
        ...buildInitialCustomSplitInputs(members),
        ...currentForm.customSplitInputs
      }
    }));

    setSettlementMode((currentMode) => {
      if (currentMode === "exact" && hasExactOptions) {
        return currentMode;
      }
      if (currentMode === "simplified" && hasSimplifiedOptions) {
        return currentMode;
      }
      if (hasExactOptions) {
        return "exact";
      }
      if (hasSimplifiedOptions) {
        return "simplified";
      }
      return currentMode;
    });

    setSettlementForm((currentForm) => ({
      ...currentForm,
      expenseId:
        settlementMode === "exact" &&
        settleUpExpenses.some((expense) => expense.expenseId === currentForm.expenseId)
          ? currentForm.expenseId
          : settlementMode === "exact"
            ? fallbackSettlementExpenseID
            : "",
      simplifiedTransferKey:
        settlementMode === "simplified" &&
        settleUpTransfers.some(
          (transfer) => settlementTransferKey(transfer) === currentForm.simplifiedTransferKey
        )
          ? currentForm.simplifiedTransferKey
          : settlementMode === "simplified"
            ? fallbackTransferUserID
            : "",
      toUserId:
        settlementMode === "simplified"
          ? selectedSettlementTransfer
            ? selectedSettlementTransfer.toUserId
            : fallbackTransferOption?.toUserId || ""
          : selectedSettlementOption
            ? selectedSettlementOption.toUserId
            : fallbackSettlementOption?.toUserId || "",
      amount:
        settlementMode === "simplified"
          ? selectedSettlementTransfer
            ? String((selectedSettlementTransfer.amountCents / 100).toFixed(2))
            : fallbackTransferOption
              ? String((fallbackTransferOption.amountCents / 100).toFixed(2))
              : ""
          : selectedSettlementOption
            ? String((selectedSettlementOption.amountCents / 100).toFixed(2))
            : fallbackSettlementOption
              ? String((fallbackSettlementOption.amountCents / 100).toFixed(2))
              : "",
      currency: currentForm.currency || fallbackCurrency
    }));
  }, [data, settlementForm.expenseId, settlementForm.simplifiedTransferKey, settlementMode]);

  const members = data?.members || [];
  const invitations = data?.invitations || [];
  const messages = data?.messages || [];
  const baseCurrency = data?.group?.baseCurrency || "USD";
  const settleUpExpenses = data?.balances?.settleUpExpenses || [];
  const settleUpTransfers = useMemo(
    () => data?.balances?.simplifiedSettleTo || [],
    [data?.balances?.simplifiedSettleTo]
  );

  const selectedParticipants = useMemo(
    () => new Set(expenseForm.participantUserIds),
    [expenseForm.participantUserIds]
  );
  const currentMember = useMemo(
    () => members.find((member) => member.id === data?.currentUserId) || null,
    [data?.currentUserId, members]
  );
  const paymentRows = data?.balances?.payments || [];
  const settlementRows = data?.balances?.settlements || [];
  const openExpensePaymentRows = data?.balances?.openExpensePayments || [];
  const ownedOpenExpensePaymentRows = useMemo(
    () =>
      openExpensePaymentRows.filter((paymentRow) => paymentRow.ownerUserId === data?.currentUserId),
    [data?.currentUserId, openExpensePaymentRows]
  );
  const aggregatedOwnedOpenExpenseRows = useMemo(() => {
    const rowsByExpenseID = new Map();
    const aggregatedRows = [];

    ownedOpenExpensePaymentRows.forEach((paymentRow) => {
      const existingRow = rowsByExpenseID.get(paymentRow.expenseId);
      if (existingRow) {
        existingRow.amountCents += paymentRow.amountCents;
        existingRow.canDelete = existingRow.canDelete && paymentRow.canDelete;
        return;
      }

      const nextRow = {
        expenseId: paymentRow.expenseId,
        expense: paymentRow.expense,
        amountCents: paymentRow.amountCents,
        canDelete: paymentRow.canDelete
      };
      rowsByExpenseID.set(paymentRow.expenseId, nextRow);
      aggregatedRows.push(nextRow);
    });

    return aggregatedRows;
  }, [ownedOpenExpensePaymentRows]);
  const selectedSettlementExpense =
    settleUpExpenses.find((expense) => expense.expenseId === settlementForm.expenseId) || null;
  const selectedSettlementTransfer =
    settleUpTransfers.find(
      (transfer) => settlementTransferKey(transfer) === settlementForm.simplifiedTransferKey
    ) || null;
  const isLongGroupTitle = (data?.group?.name || "").length > 25;
  const browserDateLabel = useMemo(
    () =>
      new Intl.DateTimeFormat(undefined, {
        dateStyle: "full"
      }).format(summaryNow),
    [summaryNow]
  );
  const browserTimeLabel = useMemo(
    () =>
      new Intl.DateTimeFormat(undefined, {
        timeStyle: "medium"
      }).format(summaryNow),
    [summaryNow]
  );
  const displayTotalOwedCents = useMemo(
    () => paymentRows.reduce((total, row) => total + row.amountCents, 0),
    [paymentRows]
  );
  const displayTotalToOweCents = useMemo(
    () => settlementRows.reduce((total, row) => total + row.amountCents, 0),
    [settlementRows]
  );
  const welcomeLabel = currentMember?.name || currentMember?.email || "member";
  const canUseExactSettlement = settleUpExpenses.length > 0;
  const canUseSimplifiedSettlement = settleUpTransfers.length > 0;
  const isGroupArchived = Boolean(data?.group?.isArchived);

  const customSplitDraftAmountCents = useMemo(() => {
    const trimmed = String(expenseForm.amount || "").trim();
    return trimmed ? parseAmountToCents(trimmed) || 0 : 0;
  }, [expenseForm.amount]);

  const customSplitSummary = useMemo(() => {
    let hasInvalidAmount = false;
    let allocatedCents = 0;

    for (const member of members) {
      const parsedAmount = parseOptionalAmountToCents(expenseForm.customSplitInputs[member.id]);
      if (parsedAmount === null) {
        hasInvalidAmount = true;
        continue;
      }
      allocatedCents += parsedAmount;
    }

    return {
      allocatedCents,
      hasInvalidAmount,
      remainingCents: customSplitDraftAmountCents - allocatedCents
    };
  }, [customSplitDraftAmountCents, expenseForm.customSplitInputs, members]);

  function toggleParticipant(memberId) {
    setExpenseForm((currentForm) => {
      const nextIds = new Set(currentForm.participantUserIds);
      if (nextIds.has(memberId)) {
        nextIds.delete(memberId);
      } else {
        nextIds.add(memberId);
      }
      return { ...currentForm, participantUserIds: Array.from(nextIds) };
    });
  }

  function updateExpenseField(field, value) {
    setExpenseForm((currentForm) => ({ ...currentForm, [field]: value }));
  }

  function updateCustomSplit(memberId, value) {
    setExpenseForm((currentForm) => ({
      ...currentForm,
      customSplitInputs: {
        ...currentForm.customSplitInputs,
        [memberId]: value
      }
    }));
  }

  function togglePanel(panelId) {
    setPanelState((currentState) => ({
      ...currentState,
      [panelId]: !currentState[panelId]
    }));
  }

  function renderPanelToggle(panelId, label) {
    const isExpanded = Boolean(panelState[panelId]);

    return (
      <button
        className={`panel-toggle-button${isExpanded ? " is-expanded" : ""}`}
        type="button"
        aria-expanded={isExpanded}
        aria-label={`${isExpanded ? "Collapse" : "Expand"} ${label}`}
        onClick={() => togglePanel(panelId)}
      >
        {isExpanded ? "Collapse" : "Expand"}
      </button>
    );
  }

  async function handleInviteSubmit(event) {
    event.preventDefault();
    setSendingInvite(true);

    try {
      await apiRequest(`/v1/groups/${groupId}/invitations`, {
        method: "POST",
        body: { email: inviteEmail }
      });
      setInviteEmail("");
      await loadGroup({ silent: true });
    } catch (requestError) {
      showError(requestError.message);
    } finally {
      setSendingInvite(false);
    }
  }

  async function handleExpenseSubmit(event) {
    event.preventDefault();
    const amountCents = parseAmountToCents(expenseForm.amount);
    if (!amountCents) {
      showError("Enter a valid amount.");
      return;
    }

    let body = {
      description: expenseForm.description,
      category: expenseForm.category,
      expenseType: expenseForm.expenseType,
      dueDayOfMonth: expenseForm.expenseType === "monthly" ? Number(expenseForm.dueDayOfMonth || 0) : 0,
      amountCents,
      currency: expenseForm.currency,
      splitMode: expenseForm.splitMode,
      paidByUserId:
        expenseForm.expenseType === "monthly" ? expenseForm.ownerUserId : expenseForm.paidByUserId,
      ownerUserId: expenseForm.ownerUserId,
      incurredOn: expenseForm.incurredOn
    };

    if (expenseForm.expenseType === "monthly") {
      const dueDay = Number(expenseForm.dueDayOfMonth || 0);
      if (!Number.isInteger(dueDay) || dueDay < 1 || dueDay > 31) {
        showError("Enter a valid day of month between 1 and 31.");
        return;
      }
    }

    if (expenseForm.splitMode === "equal") {
      if (expenseForm.participantUserIds.length === 0) {
        showError("Select at least one participant.");
        return;
      }
      body = {
        ...body,
        participantUserIds: expenseForm.participantUserIds
      };
    } else {
      if (customSplitSummary.hasInvalidAmount) {
        showError("Enter valid custom split amounts.");
        return;
      }

      const splitRows = members
        .map((member) => ({
          userId: member.id,
          amountCents: parseOptionalAmountToCents(expenseForm.customSplitInputs[member.id])
        }))
        .filter((split) => split.amountCents > 0);

      if (splitRows.length === 0) {
        showError("Enter at least one custom split amount.");
        return;
      }
      if (customSplitSummary.allocatedCents !== amountCents) {
        showError("Custom split amounts must add up to the full expense amount.");
        return;
      }

      body = {
        ...body,
        participantUserIds: splitRows.map((split) => split.userId),
        splits: splitRows
      };
    }

    setSavingExpense(true);

    try {
      await apiRequest(`/v1/groups/${groupId}/expenses`, {
        method: "POST",
        body
      });

      setExpenseForm({
        description: "",
        category: "food",
        expenseType: "one-time",
        dueDayOfMonth: defaultDueDayOfMonth(),
        amount: "",
        currency: baseCurrency,
        splitMode: "equal",
        paidByUserId: data?.currentUserId || "",
        ownerUserId: data?.currentUserId || "",
        participantUserIds: (data?.members || []).map((member) => member.id),
        customSplitInputs: buildInitialCustomSplitInputs(data?.members || []),
        incurredOn: todayISODate()
      });
      await loadGroup({ silent: true });
    } catch (requestError) {
      showError(requestError.message);
    } finally {
      setSavingExpense(false);
    }
  }

  async function handleSettlementSubmit(event) {
    event.preventDefault();
    if (!data?.currentUserId) {
      showError("Choose an amount to settle.");
      return;
    }
    if (settlementMode === "exact") {
      if (!settlementForm.toUserId || !settlementForm.expenseId || !selectedSettlementExpense) {
        showError("Choose an expense you currently owe.");
        return;
      }
    } else if (!settlementForm.toUserId || !selectedSettlementTransfer) {
      showError("Choose a simplified debt to settle.");
      return;
    }
    const amountCents =
      settlementMode === "simplified"
        ? selectedSettlementTransfer.amountCents
        : selectedSettlementExpense.amountCents;

    setSavingSettlement(true);

    try {
      const requestBody = {
        kind: settlementMode === "simplified" ? "netted" : "direct_expense",
        fromUserId: data.currentUserId,
        toUserId: settlementForm.toUserId,
        amountCents,
        currency: settlementForm.currency,
        settledOn: settlementForm.settledOn
      };
      if (settlementMode === "exact") {
        requestBody.expenseId = settlementForm.expenseId;
      }

      await apiRequest(`/v1/groups/${groupId}/settlements`, {
        method: "POST",
        body: requestBody
      });

      setSettlementForm((currentForm) => ({
        ...currentForm,
        expenseId: "",
        simplifiedTransferKey: "",
        toUserId: "",
        amount: "",
        currency: baseCurrency,
        settledOn: todayISODate()
      }));
      await loadGroup({ silent: true });
    } catch (requestError) {
      showError(requestError.message);
    } finally {
      setSavingSettlement(false);
    }
  }

  async function handleDeleteExpense(expenseId) {
    setDeletingExpenseId(expenseId);

    try {
      await apiRequest(`/v1/groups/${groupId}/expenses/${expenseId}`, {
        method: "DELETE"
      });
      await loadGroup({ silent: true });
    } catch (requestError) {
      showError(requestError.message);
    } finally {
      setDeletingExpenseId("");
    }
  }

  async function handleMessageSubmit(event) {
    event.preventDefault();
    const trimmedBody = messageBody.trim();
    if (!trimmedBody) {
      showError("Enter a message.");
      return;
    }

    setSendingMessage(true);
    try {
      await apiRequest(`/v1/groups/${groupId}/messages`, {
        method: "POST",
        body: { body: trimmedBody }
      });
      setMessageBody("");
      await loadGroup({ silent: true });
    } catch (requestError) {
      showError(requestError.message);
    } finally {
      setSendingMessage(false);
    }
  }

  async function handleDeleteMessage(messageId) {
    setDeletingMessageId(messageId);
    try {
      await apiRequest(`/v1/groups/${groupId}/messages/${messageId}`, {
        method: "DELETE"
      });
      await loadGroup({ silent: true });
    } catch (requestError) {
      showError(requestError.message);
    } finally {
      setDeletingMessageId("");
    }
  }

  if (loading) {
    return <div className="state-panel">Loading group...</div>;
  }

  if (loadFailed && !data) {
    return (
      <div className="state-panel error-state">
        Unable to load this group.
        <Link className="secondary-link" to="/dashboard">
          Return to dashboard
        </Link>
      </div>
    );
  }

  if (isGroupArchived) {
    return (
      <div className="state-panel">
        <div className="archived-state-panel">
          <strong>{data.group.name} is archived.</strong>
          <span>The group detail view is unavailable while this group is archived.</span>
          <Link className="primary-link archived-state-link" to="/dashboard">
            Return to Dashboard
          </Link>
        </div>
      </div>
    );
  }

  return (
    <section className="group-layout">
      <div className="group-panel-grid">
        <div className={`panel group-header${isLongGroupTitle ? " group-header-stacked" : " group-header-summary-below"}`}>
          <div className="group-header-title">
            <div className="panel-header-topline">
              <span className="eyebrow">Group Summary</span>
            </div>
            <h2>{data.group.name}</h2>
            <p className="group-header-greeting">
              <span className="group-header-meta-label">Name:</span> {welcomeLabel}
            </p>
            <p className="group-header-datetime">
              <span className="group-header-meta-label">Date:</span> {browserDateLabel}
              <span className="group-header-live-time">{browserTimeLabel}</span>
            </p>
          </div>
          <div className="summary-strip">
            <div>
              <strong>Members</strong>
              <span>{members.length}</span>
            </div>
            <div>
              <strong>Pending Invites</strong>
              <span>{invitations.filter((invitation) => invitation.status === "pending").length}</span>
            </div>
            <div>
              <strong>Base Currency</strong>
              <span>{baseCurrency}</span>
            </div>
            <div>
              <strong>Total Owed</strong>
              <span>{formatCurrency(displayTotalOwedCents, baseCurrency)}</span>
            </div>
            <div>
              <strong>Total To Owe</strong>
              <span>{formatCurrency(displayTotalToOweCents, baseCurrency)}</span>
            </div>
            <div>
              <strong>Expenses</strong>
              <span>
                Active {data.expenseSummary?.activeCount || 0} · Closed {data.expenseSummary?.closedCount || 0}
              </span>
            </div>
          </div>
        </div>

        <div className="panel expense-panel">
          <div className={`section-heading expense-panel-heading${panelState.expense ? "" : " is-collapsed"}`}>
            <div>
              <div className="panel-header-topline">
                <span className="eyebrow">New expense</span>
                {renderPanelToggle("expense", "New Expense")}
              </div>
              {panelState.expense ? <h2>Add A Shared Charge</h2> : null}
            </div>
          </div>

          {panelState.expense ? (
          <form className="stack-form" onSubmit={handleExpenseSubmit}>
            <label>
              <span>Description</span>
              <input
                type="text"
                value={expenseForm.description}
                onChange={(event) => updateExpenseField("description", event.target.value)}
                placeholder="Dinner at Mercado"
                required
              />
            </label>

            <div className="split-fields">
              <label>
                <span>Category</span>
                <select
                  value={expenseForm.category}
                  onChange={(event) => updateExpenseField("category", event.target.value)}
                  required
                >
                  {EXPENSE_CATEGORIES.map((category) => (
                    <option key={category} value={category}>
                      {formatCategoryLabel(category)}
                    </option>
                  ))}
                </select>
              </label>

              <label>
                <span>Expense type</span>
                <div className="expense-type-radio-group">
                  <label className="radio-chip">
                    <input
                      type="radio"
                      name="expenseType"
                      value="one-time"
                      checked={expenseForm.expenseType === "one-time"}
                      onChange={(event) => updateExpenseField("expenseType", event.target.value)}
                    />
                    <span>One-time</span>
                  </label>
                  <label className="radio-chip">
                    <input
                      type="radio"
                      name="expenseType"
                      value="monthly"
                      checked={expenseForm.expenseType === "monthly"}
                      onChange={(event) => updateExpenseField("expenseType", event.target.value)}
                    />
                    <span>Monthly</span>
                  </label>
                </div>
              </label>

              <label>
                <span>Amount</span>
                <input
                  type="text"
                  inputMode="decimal"
                  value={expenseForm.amount}
                  onChange={(event) => updateExpenseField("amount", event.target.value)}
                  placeholder="128.50"
                  required
                />
              </label>

              <label>
                <span>Currency</span>
                <div className="readonly-field">{baseCurrency}</div>
              </label>

              {expenseForm.expenseType === "monthly" ? (
                <label>
                  <span>Due day each month</span>
                  <input
                    type="number"
                    min="1"
                    max="31"
                    value={expenseForm.dueDayOfMonth}
                    onChange={(event) => updateExpenseField("dueDayOfMonth", event.target.value)}
                    required
                  />
                </label>
              ) : (
                <label>
                  <span>Date</span>
                  <input
                    type="date"
                    value={expenseForm.incurredOn}
                    onChange={(event) => updateExpenseField("incurredOn", event.target.value)}
                    required
                  />
                </label>
              )}
            </div>

            <div className="split-fields">
              {expenseForm.expenseType === "one-time" ? (
                <label>
                  <span>Paid by</span>
                  <select
                    value={expenseForm.paidByUserId}
                    onChange={(event) => updateExpenseField("paidByUserId", event.target.value)}
                    required
                  >
                    {members.map((member) => (
                      <option key={member.id} value={member.id}>
                        {member.name}
                      </option>
                    ))}
                  </select>
                </label>
              ) : null}

              <label>
                <span>Owner</span>
                <select
                  value={expenseForm.ownerUserId}
                  onChange={(event) => updateExpenseField("ownerUserId", event.target.value)}
                  required
                >
                  {members.map((member) => (
                    <option key={member.id} value={member.id}>
                      {member.name}
                    </option>
                  ))}
                </select>
              </label>

              <label>
                <span>Split mode</span>
                <select
                  value={expenseForm.splitMode}
                  onChange={(event) => updateExpenseField("splitMode", event.target.value)}
                  required
                >
                  <option value="equal">Equal</option>
                  <option value="custom">Custom</option>
                </select>
              </label>
            </div>

            {expenseForm.splitMode === "equal" ? (
              <div className="participant-section">
                <span className="participant-section-label">Split between</span>
                <fieldset className="participant-grid">
                  {members.map((member) => (
                    <label className="checkbox-row" key={member.id}>
                      <input
                        type="checkbox"
                        checked={selectedParticipants.has(member.id)}
                        onChange={() => toggleParticipant(member.id)}
                      />
                      <span className="checkbox-copy">
                        <strong className="checkbox-name">{member.name}</strong>
                        <small>{member.email}</small>
                      </span>
                    </label>
                  ))}
                </fieldset>
              </div>
            ) : (
              <div className="participant-section">
                <span className="participant-section-label">Custom split amounts</span>
                <div className="custom-split-grid">
                  {members.map((member) => (
                    <label className="custom-split-row" key={member.id}>
                      <span className="custom-split-name">
                        <strong>{member.name}</strong>
                        <small>{member.email}</small>
                      </span>
                      <input
                        type="text"
                        inputMode="decimal"
                        value={expenseForm.customSplitInputs[member.id] || ""}
                        onChange={(event) => updateCustomSplit(member.id, event.target.value)}
                        placeholder="0.00"
                      />
                    </label>
                  ))}
                </div>
                <div className="split-summary">
                  <span>
                    Allocated {formatCurrency(customSplitSummary.allocatedCents, baseCurrency)}
                  </span>
                  <span className={customSplitSummary.remainingCents === 0 ? "positive" : "negative"}>
                    Remaining {formatCurrency(customSplitSummary.remainingCents, baseCurrency)}
                  </span>
                </div>
              </div>
            )}

            <button className="primary-button" type="submit" disabled={savingExpense}>
              {savingExpense ? "Saving..." : "Add expense"}
            </button>
          </form>
          ) : null}

        </div>

        <div className="panel invite-panel">
          <div className={`section-heading${panelState.invite ? "" : " is-collapsed"}`}>
            <div>
              <div className="panel-header-topline">
                <span className="eyebrow">Invite members</span>
                {renderPanelToggle("invite", "Invite Members")}
              </div>
              {panelState.invite ? <h2>Add People By Email</h2> : null}
            </div>
          </div>

          {panelState.invite ? (
          <>
          <form className="inline-form" onSubmit={handleInviteSubmit}>
            <input
              type="email"
              value={inviteEmail}
              onChange={(event) => setInviteEmail(event.target.value)}
              placeholder="friend@example.com"
              required
            />
            <button className="primary-button" type="submit" disabled={sendingInvite}>
              {sendingInvite ? "Sending..." : "Invite"}
            </button>
          </form>

          <div className="stack-list compact-list invitation-list">
            {invitations.length === 0 ? (
              <div className="empty-state">
                <strong>No invitations yet.</strong>
                <span>Invite by email and the user can accept from their dashboard.</span>
              </div>
            ) : (
              invitations.map((invitation) => (
                <div
                  className="list-card invitation-card"
                  key={invitation.id}
                  title={`Status: ${invitation.status}\nInvited by: ${invitation.invitedByName}\nDate: ${formatDate(invitation.createdAt)}`}
                  aria-label={`Invitation for ${invitation.email}. Status ${invitation.status}. Invited by ${invitation.invitedByName}. Date ${formatDate(invitation.createdAt)}.`}
                >
                  <div className="invitation-card-row">
                    <span className="invitation-primary">{invitation.email}</span>
                    <span className="invitation-status" aria-hidden="true">
                      {invitation.status === "accepted" ? "✅" : "⏳"}
                    </span>
                  </div>
                </div>
              ))
            )}
          </div>

          <div className="member-panel">
            <h3>Members</h3>
            <div className="stack-list member-list">
              {members.map((member) => {
                const isOwner = member.role === "owner";
                const acceptedDateValue = formatDate(member.acceptedAt || member.joinedAt);
                const ownerCreatedDateValue = formatDate(member.joinedAt);
                const invitedDateLine = !isOwner && member.invitedAt
                  ? `\nInvited: ${formatDate(member.invitedAt)}`
                  : "";
                const invitedDateAria = !isOwner && member.invitedAt
                  ? ` Invited ${formatDate(member.invitedAt)}.`
                  : "";
                const dateLabel = isOwner ? "Created Date" : "Accepted";
                const dateValue = isOwner ? ownerCreatedDateValue : acceptedDateValue;

                return (
                  <div
                    className="list-card member-card"
                    key={member.id}
                    title={`Group role: ${isOwner ? "Group owner" : "Group member"}\nEmail: ${member.email}\n${dateLabel}: ${dateValue}${invitedDateLine}`}
                    aria-label={`Member ${member.name}. Group role ${isOwner ? "Group owner" : "Group member"}. Email ${member.email}. ${dateLabel} ${dateValue}.${invitedDateAria}`}
                  >
                    <div>
                      <span className="member-card-name">{member.name}</span>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
          </>
          ) : null}
        </div>

        <div className="panel settlement-panel">
          <div className={`section-heading${panelState.settlement ? "" : " is-collapsed"}`}>
            <div>
              <div className="panel-header-topline">
                <span className="eyebrow">Settle up</span>
                {renderPanelToggle("settlement", "Record Settlement")}
              </div>
              {panelState.settlement ? <h2>Record Settlement</h2> : null}
            </div>
          </div>

          {panelState.settlement ? (
            canUseExactSettlement || canUseSimplifiedSettlement ? (
              <form className="stack-form settlement-form" onSubmit={handleSettlementSubmit}>
                <div className="settlement-mode-toggle" aria-label="Settlement mode">
                  {canUseExactSettlement ? (
                    <button
                      className={`settlement-mode-link${settlementMode === "exact" ? " is-active" : ""}`}
                      type="button"
                      aria-pressed={settlementMode === "exact"}
                      onClick={() => setSettlementMode("exact")}
                    >
                      Exact Expense
                    </button>
                  ) : null}
                  {canUseSimplifiedSettlement ? (
                    <button
                      className={`settlement-mode-link${settlementMode === "simplified" ? " is-active" : ""}`}
                      type="button"
                      aria-pressed={settlementMode === "simplified"}
                      onClick={() => setSettlementMode("simplified")}
                    >
                      Simplified Debt
                    </button>
                  ) : null}
                </div>

                <div className="split-fields">
                  <label>
                    {settlementMode === "simplified" ? (
                      <select
                        aria-label="Simplified debt"
                        value={settlementForm.simplifiedTransferKey}
                        onChange={(event) => {
                          const nextTransfer =
                            settleUpTransfers.find(
                              (transfer) => settlementTransferKey(transfer) === event.target.value
                            ) || null;
                          setSettlementForm((currentForm) => ({
                            ...currentForm,
                            expenseId: "",
                            simplifiedTransferKey: event.target.value,
                            toUserId: nextTransfer?.toUserId || "",
                            amount: nextTransfer ? String((nextTransfer.amountCents / 100).toFixed(2)) : ""
                          }));
                        }}
                        required
                      >
                        <option value="" disabled>
                          Select a simplified debt
                        </option>
                        {settleUpTransfers.map((transfer) => (
                          <option
                            key={settlementTransferKey(transfer)}
                            value={settlementTransferKey(transfer)}
                          >
                            {transfer.fromName} owes {transfer.toName} {formatCurrency(transfer.amountCents, baseCurrency)}
                          </option>
                        ))}
                      </select>
                    ) : (
                      <select
                        aria-label="Exact expense"
                        value={settlementForm.expenseId}
                        onChange={(event) => {
                          const nextExpense =
                            settleUpExpenses.find((expense) => expense.expenseId === event.target.value) || null;
                          setSettlementForm((currentForm) => ({
                            ...currentForm,
                            expenseId: event.target.value,
                            toUserId: nextExpense?.toUserId || "",
                            amount: nextExpense ? String((nextExpense.amountCents / 100).toFixed(2)) : ""
                          }));
                        }}
                        required
                      >
                        <option value="" disabled>
                          Select an expense
                        </option>
                        {settleUpExpenses.map((expense) => (
                          <option key={expense.expenseId} value={expense.expenseId}>
                            {expense.toName} - {expense.expense} - {formatCurrency(expense.amountCents, baseCurrency)}
                          </option>
                        ))}
                      </select>
                    )}
                  </label>
                </div>

                <div className="split-fields">
                  <label>
                    <span>Exact amount</span>
                    <div className="readonly-field">
                      {settlementMode === "simplified"
                        ? selectedSettlementTransfer
                          ? formatCurrency(selectedSettlementTransfer.amountCents, baseCurrency)
                          : "--"
                        : selectedSettlementExpense
                          ? formatCurrency(selectedSettlementExpense.amountCents, baseCurrency)
                          : "--"}
                    </div>
                  </label>

                  <label>
                    <span>Currency</span>
                    <div className="readonly-field">{baseCurrency}</div>
                  </label>

                  <label>
                    <span>Date</span>
                    <input
                      type="date"
                      value={settlementForm.settledOn}
                      onChange={(event) =>
                        setSettlementForm((currentForm) => ({
                          ...currentForm,
                          settledOn: event.target.value
                        }))
                      }
                      required
                    />
                  </label>
                </div>

                <button
                  className="primary-button"
                  type="submit"
                  disabled={savingSettlement}
                >
                  {savingSettlement ? "Saving..." : "Record settlement"}
                </button>
              </form>
            ) : (
              <div className="empty-state">
                <span>Nil</span>
              </div>
            )
          ) : null}
        </div>

        <div className="panel group-balances-panel">
          <div className="section-heading transactions-panel-heading">
            <div>
              <div className="panel-header-topline">
                <span className="eyebrow">Transactions</span>
                {renderPanelToggle("transactions", "Transactions")}
              </div>
              {panelState.transactions ? <h2>Owed / To Owe</h2> : null}
            </div>
          </div>

          {panelState.transactions ? (
          <>
          <div className="settlement-list">
            <h3>Payments From</h3>
            {paymentRows.length === 0 ? (
              <div className="empty-state">
                <span>Nil</span>
              </div>
            ) : (
              <div className="table-card">
                <div className="table-scroll">
                  <table className="data-table suggestion-table transactions-table">
                    <colgroup>
                      <col className="transactions-column-who" />
                      <col className="transactions-column-amount" />
                      <col className="transactions-column-expense" />
                    </colgroup>
                    <thead>
                      <tr>
                        <th scope="col">Who</th>
                        <th className="numeric-cell" scope="col">
                          Amount
                        </th>
                        <th scope="col">Expense</th>
                      </tr>
                    </thead>
                    <tbody>
                      {paymentRows.map((paymentRow) => (
                        <tr key={`${paymentRow.who}-${paymentRow.expense}`}>
                          <td>{paymentRow.who}</td>
                          <td className="numeric-cell">
                            {formatCurrency(paymentRow.amountCents, baseCurrency)}
                          </td>
                          <td>{paymentRow.expense}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}
          </div>

          <div className="settlement-list">
            <h3>Settlements To</h3>
            {settlementRows.length === 0 ? (
              <div className="empty-state">
                <span>Nil</span>
              </div>
            ) : (
              <div className="table-card">
                <div className="table-scroll">
                  <table className="data-table settlement-history-table transactions-table">
                    <colgroup>
                      <col className="transactions-column-who" />
                      <col className="transactions-column-amount" />
                      <col className="transactions-column-expense" />
                    </colgroup>
                    <thead>
                      <tr>
                        <th scope="col">Who</th>
                        <th className="numeric-cell" scope="col">
                          Amount
                        </th>
                        <th scope="col">Expense</th>
                      </tr>
                    </thead>
                    <tbody>
                      {settlementRows.map((settlementRow, index) => (
                        <tr key={`${settlementRow.who}-${settlementRow.expense}-${index}`}>
                          <td>{settlementRow.who}</td>
                          <td className="numeric-cell">
                            {formatCurrency(settlementRow.amountCents, baseCurrency)}
                          </td>
                          <td>{settlementRow.expense}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}
          </div>
          </>
          ) : null}
        </div>

        <div className="panel open-expenses-panel">
          <div className={`section-heading${panelState.openExpenses ? "" : " is-collapsed"}`}>
            <div>
              <div className="panel-header-topline">
                <span className="eyebrow">Expenses</span>
                {renderPanelToggle("openExpenses", "Open Expenses")}
              </div>
              {panelState.openExpenses ? <h2>Open Expenses</h2> : null}
            </div>
          </div>

          {panelState.openExpenses ? (
            aggregatedOwnedOpenExpenseRows.length > 0 ? (
              <div className="settlement-list">
                <div className="table-card">
                  <div className="table-scroll">
                    <table className="data-table settlement-history-table">
                      <thead>
                        <tr>
                          <th scope="col">Expense</th>
                          <th className="numeric-cell" scope="col">
                            Amount
                          </th>
                          <th scope="col">Action</th>
                        </tr>
                      </thead>
                      <tbody>
                        {aggregatedOwnedOpenExpenseRows.map((paymentRow) => {
                          const isDeleting = deletingExpenseId === paymentRow.expenseId;
                          const showDeleteAction = paymentRow.canDelete;

                          return (
                            <tr key={paymentRow.expenseId}>
                              <td>{paymentRow.expense}</td>
                              <td className="numeric-cell">
                                {formatCurrency(paymentRow.amountCents, baseCurrency)}
                              </td>
                              <td>
                                {showDeleteAction ? (
                                  <button
                                    className="settlement-mode-link expense-delete-button"
                                    type="button"
                                    onClick={() => handleDeleteExpense(paymentRow.expenseId)}
                                    disabled={isDeleting}
                                  >
                                    {isDeleting ? "Deleting..." : "Delete"}
                                  </button>
                                ) : null}
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                </div>
              </div>
            ) : (
              <div className="empty-state">
                <span>Nil</span>
              </div>
            )
          ) : null}
        </div>

        <div className="panel group-chat-panel">
          <div className={`section-heading${panelState.chat ? "" : " is-collapsed"}`}>
            <div>
              <div className="panel-header-topline">
                <span className="eyebrow">Group Chat</span>
                {renderPanelToggle("chat", "Group Chat")}
              </div>
              {panelState.chat ? <h2>Message Board</h2> : null}
            </div>
          </div>

          {panelState.chat ? (
            <div className="group-chat-content">
              <form className="stack-form group-chat-form" onSubmit={handleMessageSubmit}>
                <label>
                  <span>Message</span>
                  <textarea
                    value={messageBody}
                    onChange={(event) => setMessageBody(event.target.value)}
                    placeholder="Share an update with the group"
                    rows={4}
                    maxLength={2000}
                    required
                  />
                </label>
                <button className="primary-button" type="submit" disabled={sendingMessage}>
                  {sendingMessage ? "Sending..." : "Post message"}
                </button>
              </form>

              {messages.length === 0 ? (
                <div className="empty-state">
                  <strong>No messages yet.</strong>
                  <span>Start the conversation with the first post.</span>
                </div>
              ) : (
                <div className="stack-list message-list">
                  {messages.map((message) => {
                    const isDeleting = deletingMessageId === message.id;

                    return (
                      <div className="list-card message-card" key={message.id}>
                        <div className="message-card-copy">
                          <div className="message-card-meta">
                            <strong>{message.userName}</strong>
                            <span>{formatDate(message.createdAt)}</span>
                          </div>
                          <p className="message-card-body">{message.body}</p>
                        </div>
                        {message.canDelete ? (
                          <button
                            className="settlement-mode-link message-delete-button"
                            type="button"
                            onClick={() => handleDeleteMessage(message.id)}
                            disabled={isDeleting}
                          >
                            {isDeleting ? "Deleting..." : "Delete"}
                          </button>
                        ) : null}
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          ) : null}
        </div>
      </div>
    </section>
  );
}
