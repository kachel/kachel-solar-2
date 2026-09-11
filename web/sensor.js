// Sensor stats page — polls kacheld's /api/sensor and fills in the three
// time-window cards with plain-language numbers. On any failure (Pi offline,
// battery dead, network hiccup) it shows a friendly "can't reach it right
// now" banner instead of a broken page or stale-looking zeros.
(function () {
  const POLL_MS = 60000; // matches the server's own /api/sensor cache TTL

  function fmtWh(v) {
    return v == null ? '--' : v.toFixed(1) + ' Wh';
  }

  function fmtPercent(v) {
    return v == null ? '--' : Math.round(v) + '%';
  }

  function fmtDuration(seconds) {
    if (seconds == null) return '--';
    seconds = Math.round(seconds);
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    if (d > 0) return d + 'd ' + h + 'h';
    if (h > 0) return h + 'h ' + m + 'm';
    return m + 'm';
  }

  function fmtChargingFraction(f) {
    return f == null ? '' : ' (' + Math.round(f * 100) + '% of the time)';
  }

  function fmtDate(iso) {
    if (!iso) return '--';
    const d = new Date(iso);
    if (isNaN(d.getTime())) return '--';
    try {
      return d.toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' });
    } catch (err) {
      return iso;
    }
  }

  function fillWindow(name, w) {
    const card = document.querySelector('.card[data-window="' + name + '"]');
    if (!card || !w) return;
    const set = (field, text) => {
      const el = card.querySelector('[data-field="' + field + '"]');
      if (el) el.textContent = text;
    };
    set('energy', fmtWh(w.energy_generated_wh));
    set('avgPercent', fmtPercent(w.avg_percent));
    set('charging', fmtDuration(w.charging_seconds) + fmtChargingFraction(w.charging_fraction));
    set('samples', w.samples != null ? w.samples : '--');
  }

  function showUnreachable(visible) {
    const el = document.getElementById('sensor-status');
    if (el) el.hidden = !visible;
  }

  async function poll() {
    try {
      const res = await fetch('/api/sensor', { cache: 'no-store' });
      if (!res.ok) throw new Error('unexpected status ' + res.status);
      const data = await res.json();
      fillWindow('day', data.day);
      fillWindow('week', data.week);
      fillWindow('all', data.all);
      const sinceEl = document.getElementById('since-date');
      if (sinceEl) sinceEl.textContent = fmtDate(data.since);
      showUnreachable(false);
    } catch (err) {
      showUnreachable(true);
    }
  }

  poll();
  setInterval(poll, POLL_MS);
})();
