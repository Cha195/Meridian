let api, globe, sidebar, views;

document.addEventListener('DOMContentLoaded', () => {
  const saved = sessionStorage.getItem('meridian_api_key');
  if (saved) {
    tryAuth(saved);
  }

  document.getElementById('auth-submit').addEventListener('click', () => {
    const key = document.getElementById('api-key-input').value.trim();
    if (key) tryAuth(key);
  });

  document.getElementById('api-key-input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      const key = e.target.value.trim();
      if (key) tryAuth(key);
    }
  });
});

async function tryAuth(key) {
  api = new MeridianAPI(key);
  const errEl = document.getElementById('auth-error');

  try {
    await api.validate();
    sessionStorage.setItem('meridian_api_key', key);
    errEl.hidden = true;
    initDashboard();
  } catch (e) {
    errEl.textContent = 'Invalid API key or server unreachable';
    errEl.hidden = false;
    sessionStorage.removeItem('meridian_api_key');
  }
}

function initDashboard() {
  document.getElementById('auth-screen').hidden = true;
  document.getElementById('dashboard').hidden = false;

  globe = new Globe(document.getElementById('globe-container'));
  sidebar = new Sidebar(api);
  views = new Views(api);
  views.setupCacheLabControls();

  api.onEvent = (event) => {
    globe.addEvent(event);
    sidebar.addFeedEvent(event);
  };

  api.onStatusChange = (status) => {
    const el = document.getElementById('ws-status');
    el.textContent = status;
    el.style.color = status === 'connected' ? 'var(--green)' : 'var(--text-dim)';
  };

  api.connectWebSocket();

  sidebar.refresh();
  setInterval(() => sidebar.refresh(), 5000);
}
