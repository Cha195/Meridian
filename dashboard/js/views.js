class Views {
  constructor(api) {
    this.api = api;
    this.activeView = 'globe';

    document.querySelectorAll('.nav-tab').forEach(tab => {
      tab.addEventListener('click', () => this.switchTo(tab.dataset.view));
    });
  }

  switchTo(view) {
    this.activeView = view;
    document.querySelectorAll('.nav-tab').forEach(t => t.classList.toggle('active', t.dataset.view === view));
    document.querySelectorAll('.view').forEach(v => v.classList.toggle('active', v.id === `view-${view}`));

    if (view === 'cache-lab') this.refreshCacheLab();
    if (view === 'performance') this.refreshPerformance();
  }

  async refreshCacheLab() {
    try {
      const policies = await this.api.getCachePolicies();
      document.getElementById('cache-policies').innerHTML = this.renderPolicies(policies.policies || []);

      const migration = await this.api.getMigrationStatus();
      document.getElementById('migration-status').innerHTML = this.renderMigration(migration);

      const overview = await this.api.getCacheOverview(1);
      document.getElementById('cache-overview').innerHTML = this.renderCacheOverview(overview);
    } catch (e) {
      document.getElementById('cache-policies').innerHTML = `<p style="color:var(--text-dim)">Failed to load: ${e.message}</p>`;
    }
  }

  renderPolicies(policies) {
    if (!policies.length) return '<p style="color:var(--text-dim)">No policies configured</p>';
    return policies.map(p => `
      <div style="background:var(--surface-2);padding:12px;border-radius:8px;margin-bottom:8px">
        <div style="display:flex;justify-content:space-between;margin-bottom:8px">
          <strong>${p.project_id}</strong>
          <span style="color:var(--accent)">${p.active_policy}</span>
        </div>
        <div style="display:grid;grid-template-columns:repeat(4,1fr);gap:8px;font-size:13px">
          <div><span style="color:var(--text-dim)">Items</span><br>${p.items.toLocaleString()}</div>
          <div><span style="color:var(--text-dim)">Size</span><br>${(p.size_bytes / 1024 / 1024).toFixed(1)}MB</div>
          <div><span style="color:var(--text-dim)">Hit Rate</span><br>${(p.hit_rate * 100).toFixed(1)}%</div>
          <div><span style="color:var(--text-dim)">Evictions</span><br>${p.evictions.toLocaleString()}</div>
        </div>
      </div>
    `).join('');
  }

  renderMigration(statuses) {
    const entries = Object.entries(statuses);
    if (!entries.length) return '<p style="color:var(--text-dim)">No migration data</p>';
    return entries.map(([id, s]) => {
      if (!s.IsWarming) return `<p style="color:var(--text-dim)">${id}: No active migration</p>`;
      return `
        <div style="background:var(--surface-2);padding:12px;border-radius:8px">
          <div><strong>${id}</strong>: ${s.ActiveName} → ${s.WarmingName}</div>
          <div style="margin-top:8px;display:flex;gap:16px;font-size:13px">
            <span>Active HR: ${(s.ActiveHitRate * 100).toFixed(1)}%</span>
            <span>Warming HR: ${(s.WarmingHitRate * 100).toFixed(1)}%</span>
            <span>Reqs: ${s.WarmingReqs}</span>
            <span>Elapsed: ${s.ElapsedSeconds.toFixed(0)}s</span>
          </div>
        </div>`;
    }).join('');
  }

  renderCacheOverview(o) {
    const total = o.hits + o.misses + o.stale;
    if (total === 0) return '<p style="color:var(--text-dim)">No cache data</p>';
    return `
      <div style="display:flex;gap:16px;margin-bottom:12px">
        <div style="background:var(--surface-2);padding:12px;border-radius:8px;flex:1;text-align:center">
          <div style="color:var(--green);font-size:24px;font-weight:700">${o.hits}</div>
          <div style="font-size:11px;color:var(--text-dim)">HITS</div>
        </div>
        <div style="background:var(--surface-2);padding:12px;border-radius:8px;flex:1;text-align:center">
          <div style="color:var(--red);font-size:24px;font-weight:700">${o.misses}</div>
          <div style="font-size:11px;color:var(--text-dim)">MISSES</div>
        </div>
        <div style="background:var(--surface-2);padding:12px;border-radius:8px;flex:1;text-align:center">
          <div style="color:var(--amber);font-size:24px;font-weight:700">${o.stale}</div>
          <div style="font-size:11px;color:var(--text-dim)">STALE</div>
        </div>
      </div>`;
  }

  async refreshPerformance() {
    try {
      const series = await this.api.getTimeseries(24, 60);
      this.drawTimeseries(series);
    } catch (e) {}

    try {
      const routing = await this.api.getRoutingAnalysis(24);
      document.getElementById('routing-analysis').innerHTML = this.renderRouting(routing);
    } catch (e) {}
  }

  drawTimeseries(data) {
    const canvas = document.getElementById('timeseries-chart');
    const ctx = canvas.getContext('2d');
    const w = canvas.width;
    const h = canvas.height;
    const pad = { top: 20, right: 20, bottom: 30, left: 50 };

    ctx.clearRect(0, 0, w, h);
    if (!data || data.length < 2) {
      ctx.fillStyle = '#64748b';
      ctx.font = '14px system-ui';
      ctx.fillText('Not enough data for chart', w / 2 - 80, h / 2);
      return;
    }

    const chartW = w - pad.left - pad.right;
    const chartH = h - pad.top - pad.bottom;
    const maxReqs = Math.max(...data.map(d => d.requests), 1);

    // Grid
    ctx.strokeStyle = '#1e2230';
    ctx.lineWidth = 1;
    for (let i = 0; i <= 4; i++) {
      const y = pad.top + chartH * (1 - i / 4);
      ctx.beginPath();
      ctx.moveTo(pad.left, y);
      ctx.lineTo(w - pad.right, y);
      ctx.stroke();

      ctx.fillStyle = '#64748b';
      ctx.font = '10px monospace';
      ctx.textAlign = 'right';
      ctx.fillText(Math.round(maxReqs * i / 4), pad.left - 6, y + 3);
    }

    // Line
    ctx.strokeStyle = '#38bdf8';
    ctx.lineWidth = 2;
    ctx.beginPath();
    data.forEach((d, i) => {
      const x = pad.left + (i / (data.length - 1)) * chartW;
      const y = pad.top + chartH * (1 - d.requests / maxReqs);
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.stroke();

    // Fill
    ctx.lineTo(pad.left + chartW, pad.top + chartH);
    ctx.lineTo(pad.left, pad.top + chartH);
    ctx.closePath();
    ctx.fillStyle = 'rgba(56, 189, 248, 0.08)';
    ctx.fill();
  }

  renderRouting(r) {
    if (!r.total) return '<p style="color:var(--text-dim)">No routing data</p>';
    return `
      <div style="background:var(--surface-2);padding:12px;border-radius:8px">
        <div style="display:flex;gap:24px;margin-bottom:8px">
          <span>Total: <strong>${r.total}</strong></span>
          <span>Correct: <strong style="color:var(--green)">${r.correctly_routed}</strong></span>
          <span>Accuracy: <strong>${(r.accuracy * 100).toFixed(1)}%</strong></span>
        </div>
        ${r.misroutes && r.misroutes.length ? `
          <div style="font-size:12px;color:var(--text-dim);margin-top:8px">
            <strong>Top misroutes:</strong>
            ${r.misroutes.map(m => `<div>${m.country}: sent to ${m.edge_node}, should be ${m.closest_node} (${m.count}x)</div>`).join('')}
          </div>` : ''}
      </div>`;
  }

  setupCacheLabControls() {
    document.getElementById('btn-migrate-start').addEventListener('click', async () => {
      const policy = document.getElementById('migrate-policy').value;
      await this.api.startMigration(policy);
      setTimeout(() => this.refreshCacheLab(), 500);
    });

    document.getElementById('btn-migrate-abort').addEventListener('click', async () => {
      await this.api.abortMigration();
      setTimeout(() => this.refreshCacheLab(), 500);
    });
  }
}
