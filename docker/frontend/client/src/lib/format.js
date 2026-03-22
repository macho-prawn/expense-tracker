const currencyFormatterCache = new Map();

const dateFormatter = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  year: "numeric"
});

function currencyFormatter(currencyCode) {
  const normalizedCurrency = String(currencyCode || "USD").toUpperCase();
  if (!currencyFormatterCache.has(normalizedCurrency)) {
    currencyFormatterCache.set(
      normalizedCurrency,
      new Intl.NumberFormat("en-US", {
        style: "currency",
        currency: normalizedCurrency
      })
    );
  }
  return currencyFormatterCache.get(normalizedCurrency);
}

export function formatCurrency(cents, currencyCode = "USD") {
  return currencyFormatter(currencyCode).format((cents || 0) / 100);
}

export function formatDate(dateLike) {
  if (!dateLike) {
    return "Unknown";
  }

  const date = new Date(dateLike);
  if (Number.isNaN(date.getTime())) {
    return dateLike;
  }

  return dateFormatter.format(date);
}

export function parseAmountToCents(rawValue) {
  const normalized = String(rawValue || "")
    .trim()
    .replace(/[$,\s]/g, "");
  const amount = Number(normalized);
  if (!Number.isFinite(amount) || amount <= 0) {
    return null;
  }
  return Math.round(amount * 100);
}

export function todayISODate() {
  return new Date().toISOString().slice(0, 10);
}
