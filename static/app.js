document.addEventListener('DOMContentLoaded', function () {
  const refreshBtn = document.getElementById('refreshBtn');
  const banner = document.getElementById('statusBanner');
  const activityField = document.getElementById('activityField');
  const viewMode = document.getElementById('viewMode');

  const bindActivityFilters = function () {
    document.querySelectorAll('.activity-pill').forEach(function (button) {
      button.addEventListener('click', function () {
        const form = button.closest('form');
        if (activityField) {
          activityField.value = button.dataset.activity || '';
        }
        if (form) {
          form.submit();
          return;
        }
        const url = new URL(window.location.href);
        url.searchParams.set('activity', button.dataset.activity || '');
        window.location.href = url.toString();
      });
    });
  };

  const bindViewButtons = function () {
    document.querySelectorAll('.view-btn').forEach(function (button) {
      button.addEventListener('click', function () {
        const nextValue = button.dataset.view || 'cards';
        if (viewMode) {
          viewMode.value = nextValue;
        }
        const form = button.closest('form');
        if (form) {
          form.submit();
          return;
        }
        const url = new URL(window.location.href);
        url.searchParams.set('view', nextValue);
        window.location.href = url.toString();
      });
    });
  };

  bindActivityFilters();
  bindViewButtons();

  if (refreshBtn) {
    refreshBtn.addEventListener('click', function () {
      if (banner) {
        banner.classList.add('visible');
        banner.textContent = 'Refreshing artist data...';
      }

      fetch('/api/refresh', { method: 'POST' })
        .then(function (response) {
          if (!response.ok) {
            throw new Error('Refresh failed');
          }
          return response.json();
        })
        .then(function (payload) {
          if (banner) {
            banner.textContent = payload.message || 'Data refreshed successfully';
            banner.classList.add('visible');
          }
          window.location.reload();
        })
        .catch(function () {
          if (banner) {
            banner.textContent = 'Refresh failed. The app stayed online with cached fallback data.';
            banner.classList.add('visible');
          }
        });
    });
  }
});
