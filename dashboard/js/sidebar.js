class Sidebar {
  constructor(api) {
    this.api = api;
    this.maxFeedItems = 50;
  }

  async refresh() {
    try {
      const stats = await this.api.getLiveStats();
      document.getElementById('stat-active').textContent = stats.active_now.toLocaleString();
      document.getElementById('stat-today').textContent = stats.today_total.toLocaleString();
      document.getElementById('stat-p95').textContent = `${stats.p95_latency_ms.toFixed(1)}ms`;
      document.getElementById('stat-hitrate').textContent = `${(stats.cache_hit_rate * 100).toFixed(1)}%`;
    } catch (e) {}

    try {
      const countries = await this.api.getCountries(24);
      this.renderCountries(countries);
    } catch (e) {}
  }

  renderCountries(countries) {
    const el = document.getElementById('country-list');
    if (!countries || countries.length === 0) {
      el.innerHTML = '<div class="country-row" style="color:var(--text-dim)">No data yet</div>';
      return;
    }
    const max = countries[0].requests;
    el.innerHTML = countries.slice(0, 10).map(c => `
      <div class="country-row">
        <span>${this.countryFlag(c.country)} ${c.country || 'Unknown'}</span>
        <div class="country-bar" style="width:${(c.requests / max * 100).toFixed(0)}%"></div>
        <span class="country-count">${c.requests}</span>
      </div>
    `).join('');
  }

  addFeedEvent(event) {
    const color = event.cache_status === 'HIT' ? 'var(--green)' :
                  event.cache_status === 'STALE' ? 'var(--amber)' : 'var(--red)';

    const el = document.getElementById('live-feed');
    const item = document.createElement('div');
    item.className = 'feed-item';
    item.innerHTML = `
      <span class="dot" style="background:${color}"></span>
      <span>${this.countryFlag(event.client_country)}</span>
      <span class="path">${event.method} ${event.path}</span>
      <span class="latency">${event.total_latency_ms.toFixed(1)}ms</span>`;

    el.insertBefore(item, el.firstChild);

    while (el.children.length > this.maxFeedItems) {
      el.removeChild(el.lastChild);
    }
  }

  countryFlag(code) {
    if (!code || code.length !== 2) return '';
    return String.fromCodePoint(
      ...[...code.toUpperCase()].map(c => 0x1F1E6 + c.charCodeAt(0) - 65)
    );
  }
}
