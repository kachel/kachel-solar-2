document.querySelectorAll('.accordion').forEach((accordion) => {
  const trigger = accordion.querySelector('.accordion-title');

  trigger.addEventListener('click', () => {
    accordion.classList.toggle('is-open');
  });
});

// Live battery widget — polls kacheld's /api/battery and reflects charge
// level, charging state, and a "stale/unreachable" state that's visually
// distinct from an error page (the data is just old, not missing).
(function initBatteryWidget() {
  const widget = document.querySelector('.battery');
  if (!widget) return;

  const cells = Array.from(widget.querySelectorAll('.cell'));
  const POLL_MS = 20000;

  function render(percent, charging, stale, unreachable) {
    const filled = percent == null
      ? 0
      : Math.min(cells.length, Math.max(0, Math.round((percent / 100) * cells.length)));
    cells.forEach((cell, i) => cell.classList.toggle('cell-filled', i < filled));

    widget.classList.toggle('is-charging', !!charging && !unreachable);
    widget.classList.toggle('is-stale', !!stale || !!unreachable);

    let label = 'Battery status: ';
    label += percent == null ? 'unknown' : Math.round(percent) + '% charged';
    if (charging && !unreachable) label += ', charging';
    if (unreachable) {
      label += ' (sensor unreachable, showing last known reading)';
    } else if (stale) {
      label += ' (data may be out of date)';
    }
    label += '. Link: read the project write-up.';
    widget.setAttribute('aria-label', label);
    widget.title = label;
  }

  async function poll() {
    try {
      const res = await fetch('/api/battery', { cache: 'no-store' });
      if (res.status === 503) {
        render(null, false, true, true);
        return;
      }
      if (!res.ok) throw new Error('unexpected status ' + res.status);
      const data = await res.json();
      render(data.percent, data.charging, data.stale, false);
    } catch (err) {
      // Network error, Pi offline, battery dead, etc. — same "stale/
      // unreachable" treatment, never a broken-looking widget.
      render(null, false, true, true);
    }
  }

  poll();
  setInterval(poll, POLL_MS);
})();
