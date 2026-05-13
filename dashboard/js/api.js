class MeridianAPI {
  constructor(apiKey) {
    this.apiKey = apiKey;
    this.baseURL = window.location.origin;
    this.ws = null;
    this.onEvent = null;
    this.onStatusChange = null;
  }

  async fetch(path, params = {}) {
    const url = new URL(`${this.baseURL}${path}`);
    Object.entries(params).forEach(([k, v]) => url.searchParams.set(k, v));
    const res = await fetch(url, {
      headers: { 'Authorization': `Bearer ${this.apiKey}` }
    });
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
  }

  async validate() {
    return this.fetch('/api/v1/cache/policies');
  }

  getLiveStats()                 { return this.fetch('/api/v1/stats/live'); }
  getCountries(hours = 24)       { return this.fetch('/api/v1/geo/countries', { hours }); }
  getRecentEvents(limit = 50)    { return this.fetch('/api/v1/events/recent', { limit }); }
  getCacheOverview(hours = 1)    { return this.fetch('/api/v1/cache/overview', { hours }); }
  getCachePolicies()             { return this.fetch('/api/v1/cache/policies'); }
  getMigrationStatus()           { return this.fetch('/api/v1/cache/migration/status'); }
  getTimeseries(hours = 24, bucket = 60) { return this.fetch('/api/v1/stats/timeseries', { hours, bucket }); }
  getRoutingAnalysis(hours = 24) { return this.fetch('/api/v1/routing/analysis', { hours }); }

  startMigration(policy) {
    return fetch(`${this.baseURL}/api/v1/cache/migrate`, {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${this.apiKey}`,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({ policy })
    });
  }

  abortMigration() {
    return fetch(`${this.baseURL}/api/v1/cache/migrate/abort`, {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${this.apiKey}`,
        'Content-Type': 'application/json'
      },
      body: '{}'
    });
  }

  connectWebSocket() {
    const wsProto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsURL = `${wsProto}//${window.location.host}/api/v1/events/stream?key=${this.apiKey}`;

    this.ws = new WebSocket(wsURL);

    this.ws.onopen = () => {
      if (this.onStatusChange) this.onStatusChange('connected');
    };

    this.ws.onmessage = (msg) => {
      try {
        const event = JSON.parse(msg.data);
        if (this.onEvent) this.onEvent(event);
      } catch (e) {}
    };

    this.ws.onclose = () => {
      if (this.onStatusChange) this.onStatusChange('disconnected');
      setTimeout(() => this.connectWebSocket(), 3000);
    };

    this.ws.onerror = () => {
      if (this.onStatusChange) this.onStatusChange('error');
    };
  }
}
