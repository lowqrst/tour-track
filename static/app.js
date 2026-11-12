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
  refreshButton.addEventListener("click", async () => {
    const originalLabel = refreshButton.innerHTML;
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 15000);
    refreshButton.disabled = true;
    refreshButton.textContent = "Refreshing…";
    statusBanner.hidden = false;
    statusBanner.classList.remove("is-error");
    statusBanner.textContent = "Getting the latest artist data…";
    try {
      const response = await fetch("/api/refresh", {
        method: "POST",
        headers: { Accept: "application/json" },
        signal: controller.signal,
      });
      const payload = await response.json();
      if (!response.ok || payload.status === "error") {
        throw new Error(payload.message || "The API could not be refreshed. Please try again shortly.");
      }
      statusBanner.textContent = payload.message || "The collection has been refreshed.";
      window.location.reload();
    } catch (error) {
      statusBanner.classList.add("is-error");
      statusBanner.textContent = error.name === "AbortError"
        ? "The refresh took too long. You can still explore the collection below."
        : error.message || "The refresh could not be completed. Please try again shortly.";
    } finally {
      window.clearTimeout(timeout);
      refreshButton.disabled = false;
      refreshButton.innerHTML = originalLabel;
    }
  });
}
