"use strict";

// The catalog is server-rendered; search, filters, and views work without JavaScript.
const imageFallback = "/static/artist-placeholder.svg";
for (const artistImage of document.querySelectorAll("img")) {
  const useFallback = () => {
    if (artistImage.getAttribute("src") !== imageFallback) {
      artistImage.src = imageFallback;
      artistImage.classList.add("image-unavailable");
    }
  };
  artistImage.addEventListener("error", useFallback);
  if (artistImage.complete && artistImage.naturalWidth === 0) useFallback();
}

const refreshButton = document.getElementById("refreshBtn");
const statusBanner = document.getElementById("statusBanner");
if (refreshButton && statusBanner) {
  const pollInterval = 600;
  const refreshTimeout = 30000;
  const requestTimeout = 5000;

  refreshButton.addEventListener("click", async () => {
    if (refreshButton.disabled) return;

    const originalLabel = refreshButton.innerHTML;
    const deadline = Date.now() + refreshTimeout;
    let activeRequest;
    let leavingPage = false;

    const stopPolling = () => {
      leavingPage = true;
      if (activeRequest) activeRequest.abort();
    };
    window.addEventListener("pagehide", stopPolling, { once: true });

    const requestStatus = async (url, method) => {
      const remaining = deadline - Date.now();
      if (remaining <= 0) {
        throw new Error("The update is taking longer than expected. It may still finish in the background; please check again shortly.");
      }

      activeRequest = new AbortController();
      const timeout = window.setTimeout(
        () => activeRequest.abort(),
        Math.min(requestTimeout, remaining),
      );
      try {
        const response = await fetch(url, {
          method,
          cache: "no-store",
          headers: { Accept: "application/json" },
          signal: activeRequest.signal,
        });
        const payload = await response.json().catch(() => null);
        if (!response.ok || payload?.status === "error") {
          throw new Error(payload?.message || "The tour data could not be updated. Please try again shortly.");
        }
        if (!payload || !["running", "ok"].includes(payload.status)) {
          throw new Error("The update status could not be read. Please try again shortly.");
        }
        return payload;
      } finally {
        window.clearTimeout(timeout);
        activeRequest = undefined;
      }
    };

    refreshButton.disabled = true;
    refreshButton.setAttribute("aria-busy", "true");
    refreshButton.textContent = "Refreshing…";
    statusBanner.hidden = false;
    statusBanner.classList.remove("is-error");
    statusBanner.textContent = "Starting a background update of the artist collection…";

    try {
      let payload = await requestStatus("/api/refresh", "POST");
      while (payload.status === "running") {
        statusBanner.textContent = payload.message || "Updating tour data in the background…";
        await new Promise((resolve) => window.setTimeout(resolve, pollInterval));
        if (leavingPage) return;
        payload = await requestStatus("/api/refresh/status", "GET");
      }
      statusBanner.textContent = payload.message || "The collection has been refreshed.";
      window.location.reload();
    } catch (error) {
      if (leavingPage) return;
      statusBanner.classList.add("is-error");
      statusBanner.textContent = error.name === "AbortError"
        ? "The update status took too long to arrive. The background update may still finish; please check again shortly."
        : error.message || "The tour data could not be updated. Please try again shortly.";
    } finally {
      window.removeEventListener("pagehide", stopPolling);
      refreshButton.disabled = false;
      refreshButton.removeAttribute("aria-busy");
      refreshButton.innerHTML = originalLabel;
    }
  });
}
