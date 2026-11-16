/* The Daily Star E-Paper viewer: pages fitted to the screen, one or two at
   a time, thumbnails, keyboard and story pop-ups. */
(function () {
  var stage = document.querySelector('[data-stage]');
  if (!stage) return;
  var pages = Array.prototype.slice.call(stage.querySelectorAll('.np-page'));
  var state = { page: window.EP.page || 1, view: window.EP.view || 1 };
  var W = 1000, H = 1414;

  // Each page sits in a box of its scaled size, so the stage lays them out.
  pages.forEach(function (p) {
    var box = document.createElement('div');
    box.className = 'ep-scale';
    p.parentNode.insertBefore(box, p);
    box.appendChild(p);
  });

  // Thumbnails: a small copy of every page.
  var thumbs = document.querySelector('[data-thumbs]');
  pages.forEach(function (p) {
    var b = document.createElement('button');
    b.type = 'button';
    b.className = 'ep-thumb';
    b.dataset.page = p.dataset.page;
    var box = document.createElement('div');
    box.className = 'ep-thumb-box';
    var copy = p.cloneNode(true);
    copy.classList.remove('show');
    copy.style.display = 'block';
    copy.removeAttribute('data-page');
    copy.querySelectorAll('[data-story]').forEach(function (s) { s.removeAttribute('data-story'); s.removeAttribute('tabindex'); });
    box.appendChild(copy);
    var label = document.createElement('span');
    label.textContent = p.dataset.page + ' · ' + p.dataset.name;
    b.appendChild(box);
    b.appendChild(label);
    b.addEventListener('click', function () { go(+b.dataset.page); });
    thumbs.appendChild(b);
  });

  function visible() {
    var n = state.page;
    if (state.view === 2 && window.innerWidth > 760) {
      // Spreads as in print: the front page alone, then 2-3, 4-5 …
      if (n === 1) return [1];
      var left = n % 2 === 0 ? n : n - 1;
      return [left, left + 1].filter(function (x) { return x <= pages.length; });
    }
    return [n];
  }

  function fit() {
    var shown = visible();
    var availW = stage.clientWidth - 36 - (shown.length - 1) * 18;
    var availH = stage.clientHeight - 36;
    var scale = Math.min(availW / shown.length / W, availH / H);
    if (window.innerWidth <= 760) scale = (stage.clientWidth - 20) / W;
    pages.forEach(function (p) {
      var on = shown.indexOf(+p.dataset.page) >= 0;
      p.classList.toggle('show', on);
      p.parentNode.style.display = on ? 'block' : 'none';
      p.style.transform = 'scale(' + scale + ')';
      p.parentNode.style.width = (W * scale) + 'px';
      p.parentNode.style.height = (H * scale) + 'px';
    });
    thumbs.querySelectorAll('.ep-thumb').forEach(function (t) { t.classList.toggle('on', shown.indexOf(+t.dataset.page) >= 0); });
    var label = document.querySelector('[data-page-label]');
    if (label) label.textContent = shown.join('–') + ' / ' + pages.length;
    var sel = document.querySelector('[data-page-select]');
    if (sel) sel.value = String(state.page);
  }

  function go(n) {
    state.page = Math.max(1, Math.min(pages.length, n));
    var u = new URL(window.location.href);
    u.searchParams.set('pgid', state.page);
    u.searchParams.set('view', state.view);
    history.replaceState(null, '', u);
    fit();
    stage.scrollTop = 0;
    var on = thumbs.querySelector('.ep-thumb.on');
    if (on) on.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }

  function step(d) {
    var shown = visible();
    go(d > 0 ? shown[shown.length - 1] + 1 : shown[0] - (state.view === 2 && shown[0] > 2 ? 2 : 1));
  }

  document.querySelector('[data-prev]').addEventListener('click', function () { step(-1); });
  document.querySelector('[data-next]').addEventListener('click', function () { step(1); });
  document.querySelector('[data-page-select]').addEventListener('change', function (e) { go(+e.target.value); });
  document.querySelectorAll('[data-view]').forEach(function (b) {
    b.addEventListener('click', function () {
      state.view = +b.dataset.view;
      document.body.classList.toggle('ep-double', state.view === 2);
      document.querySelectorAll('[data-view]').forEach(function (x) { x.classList.toggle('on', x === b); });
      go(state.page);
    });
  });
  document.querySelector('[data-print]').addEventListener('click', function () {
    pages.forEach(function (p) { p.parentNode.style.display = 'block'; });
    window.print();
    fit();
  });
  window.addEventListener('resize', fit);

  // Story pop-up.
  var pop = document.querySelector('[data-pop]');
  var popBody = document.querySelector('[data-pop-body]');
  function open(id) {
    var t = document.getElementById('st-' + id);
    if (!t) return;
    popBody.innerHTML = '';
    popBody.appendChild(t.content.cloneNode(true));
    pop.hidden = false;
    pop.querySelector('.ep-pop-box').scrollTop = 0;
  }
  function close() { pop.hidden = true; }
  stage.addEventListener('click', function (e) {
    var s = e.target.closest('[data-story]');
    if (s) open(s.dataset.story);
  });
  stage.addEventListener('keydown', function (e) {
    var s = e.target.closest('[data-story]');
    if (s && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); open(s.dataset.story); }
  });
  pop.addEventListener('click', function (e) { if (e.target === pop || e.target.closest('[data-pop-close]')) close(); });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') close();
    if (!pop.hidden || e.target.tagName === 'SELECT') return;
    if (e.key === 'ArrowRight') step(1);
    if (e.key === 'ArrowLeft') step(-1);
  });

  fit();
})();
