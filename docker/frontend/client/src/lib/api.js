export async function apiRequest(path, options = {}) {
  const headers = new Headers(options.headers || {});
  const isJsonBody = options.body && !(options.body instanceof FormData);
  const browserTimeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;

  if (isJsonBody && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  if (browserTimeZone && !headers.has("X-Time-Zone")) {
    headers.set("X-Time-Zone", browserTimeZone);
  }

  const response = await fetch(`/api${path}`, {
    method: options.method || "GET",
    credentials: "include",
    headers,
    body: isJsonBody ? JSON.stringify(options.body) : options.body
  });

  const contentType = response.headers.get("content-type") || "";
  const payload = contentType.includes("application/json")
    ? await response.json()
    : null;

  if (!response.ok) {
    const message =
      payload?.detail ||
      payload?.message ||
      payload?.title ||
      `Request failed with status ${response.status}`;
    const error = new Error(message);
    error.status = response.status;
    error.payload = payload;
    throw error;
  }

  return payload;
}
