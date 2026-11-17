/* The Daily Star E-Paper viewer (Dhaka Edition): the page fitted to the
   screen, the page list, the story beside the page (image or text view),
   full view, favourites, photo download, search, date and page choice, all
   pages, PDF, text of the page, sharing, full screen, keyboard and swipe. */
(function () {
  var stage = document.querySelector('[data-stage]');
  if (!stage) return;
  var EP = window.EP || {};
  var body = document.body;
  var pages = Array.prototype.slice.call(stage.querySelectorAll('.np-page'));
  var W = 1000, H = 1414;
  var state = { page: Math.max(1, Math.min(pages.length, EP.page || 1)), view: EP.view === 1 ? 1 : 2, story: null, mode: 'image', fs: 17, all: false };

  function $(s) { return document.querySelector(s); }
  function $$(s, root) { return Array.prototype.slice.call((root || document).querySelectorAll(s)); }
  function on(el, ev, fn) { if (el) el.addEventListener(ev, fn); }
  function wide() { return window.innerWidth > 860; }
  function toast(msg) {
    var t = $('[data-toast]');
    t.textContent = msg;
    t.classList.add('show');
    clearTimeout(toast.t);
    toast.t = setTimeout(function () { t.classList.remove('show'); }, 2200);
  }
  function store(key, val) {
    try {
      if (val === undefined) return JSON.parse(localStorage.getItem(key) || 'null');
      localStorage.setItem(key, JSON.stringify(val));
    } catch (e) { return null; }
  }

  // Each page sits in a box of its scaled size, so the stage lays it out.
  pages.forEach(function (p) {
    var box = document.createElement('div');
    box.className = 'ep-scale';
    p.parentNode.insertBefore(box, p);
    box.appendChild(p);
  });

  // The page list: a small copy of every page, "1 FRONT", "2 NEWS" …
  var thumbs = $('[data-thumbs]');
  pages.forEach(function (p) {
    var b = document.createElement('button');
    b.type = 'button';
    b.className = 'ep-thumb';
    b.dataset.page = p.dataset.page;
    b.setAttribute('aria-label', 'Page ' + p.dataset.page + ': ' + p.dataset.name);
    var box = document.createElement('div');
    box.className = 'ep-thumb-box';
    var copy = p.cloneNode(true);
    copy.removeAttribute('data-page');
    copy.style.transform = '';
    $$('[data-story]', copy).forEach(function (s) { s.removeAttribute('data-story'); s.removeAttribute('tabindex'); });
    $$('img', copy).forEach(function (i) { i.loading = 'lazy'; });
    box.appendChild(copy);
    var label = document.createElement('span');
    label.textContent = p.dataset.page + ' ' + p.dataset.name;
    b.appendChild(box);
    b.appendChild(label);
    b.addEventListener('click', function () { go(+b.dataset.page); });
    thumbs.appendChild(b);
  });

  function storiesOn(n) {
    return $$('[data-story]', pages[n - 1]).map(function (s) { return s.dataset.story; });
  }
  function tpl(id) { return document.getElementById('st-' + id); }

  // Fit the page to the screen: the whole page in view, the story beside it.
  function draw() {
    var shown = state.all ? pages.map(function (p) { return +p.dataset.page; }) : [state.page];
    var availH = stage.clientHeight - 28;
    var scale;
    stage.style.width = '';
    if (state.all) {
      scale = 0.24;
    } else if (!wide()) {
      scale = (stage.clientWidth - 64) / W;
    } else if (state.view === 2) {
      var room = body.clientWidth - (side.classList.contains('closed') ? 0 : side.offsetWidth) - 420 - 80;
      scale = Math.max(0.2, Math.min(availH / H, room / W));
      stage.style.width = Math.ceil(W * scale + 80) + 'px';
    } else {
      scale = Math.max(0.2, Math.min(availH / H, (stage.clientWidth - 80) / W));
    }
    pages.forEach(function (p) {
      var show = shown.indexOf(+p.dataset.page) >= 0;
      p.parentNode.style.display = show ? 'block' : 'none';
      p.style.transform = 'scale(' + scale + ')';
      p.parentNode.style.width = (W * scale) + 'px';
      p.parentNode.style.height = (H * scale) + 'px';
    });
    $$('.ep-thumb', thumbs).forEach(function (t) { t.classList.toggle('on', +t.dataset.page === state.page); });
    var sel = $('[data-page-select]');
    if (sel) sel.value = String(state.page);
    $('[data-prev]').hidden = state.all || state.page <= 1;
    $('[data-next]').hidden = state.all || state.page >= pages.length;
    // The arrows sit a little above the middle of the screen, as on the paper's site.
    var mid = Math.round(Math.min(stage.clientHeight, H * scale + 28) * 0.36);
    $('[data-prev]').style.top = mid + 'px';
    $('[data-next]').style.top = mid + 'px';
    $('[data-clip-next]').style.top = mid + 'px';
  }

  function remember() {
    var u = new URL(window.location.href);
    u.searchParams.set('pgid', state.page);
    u.searchParams.set('view', state.view);
    if (state.story && state.view === 2) u.searchParams.set('aid', state.story);
    else u.searchParams.delete('aid');
    history.replaceState(null, '', u);
  }

  function go(n, story) {
    state.page = Math.max(1, Math.min(pages.length, n));
    if (state.all) setAll(false);
    draw();
    stage.scrollTop = 0;
    stage.scrollLeft = 0;
    var t = thumbs.querySelector('.ep-thumb.on');
    if (t) t.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    select(story || storiesOn(state.page)[0]);
    remember();
  }

  // The story beside the page.
  var clipBody = $('[data-clip-body]');
  function select(id) {
    state.story = id || null;
    $$('.np-story.sel').forEach(function (s) { s.classList.remove('sel'); });
    if (!id) { clipBody.innerHTML = ''; return; }
    var s = stage.querySelector('[data-story="' + id + '"]');
    if (s) s.classList.add('sel');
    drawClip();
  }
  function drawClip() {
    var t = state.story && tpl(state.story);
    clipBody.innerHTML = '';
    if (!t) return;
    var box = document.createElement('div');
    box.className = state.mode === 'text' ? 'ep-read' : 'ep-cut';
    box.style.setProperty('--fs', state.fs + 'px');
    box.appendChild(t.content.cloneNode(true));
    clipBody.appendChild(box);
    clipBody.scrollTop = 0;
    $$('[data-mode]').forEach(function (b) { b.classList.toggle('on', b.dataset.mode === state.mode); });
    $('[data-fontinc]').disabled = state.mode !== 'text';
  }
  $$('[data-mode]').forEach(function (b) {
    b.addEventListener('click', function () { state.mode = b.dataset.mode; drawClip(); });
  });
  on($('[data-fontinc]'), 'click', function () {
    state.fs = state.fs >= 23 ? 17 : state.fs + 2;
    drawClip();
  });
  on($('[data-fullview]'), 'click', function () { if (state.story) open(state.story); });
  on($('[data-favourite]'), 'click', function () {
    var t = state.story && tpl(state.story);
    if (!t) return;
    var favs = store('ep-favourites') || [];
    if (favs.some(function (f) { return f.id === state.story; })) { toast('Already in your favourites'); return; }
    favs.unshift({ id: state.story, title: t.dataset.title, date: EP.date, page: +t.dataset.page });
    store('ep-favourites', favs.slice(0, 100));
    toast(store('ep-favourites') ? 'Saved as favourite' : 'Favourites cannot be saved in this browser');
  });
  on($('[data-download]'), 'click', function () {
    var t = state.story && tpl(state.story);
    var src = t && t.dataset.img;
    if (!src) { toast('This story has no photo'); return; }
    var a = document.createElement('a');
    a.href = src;
    a.download = '';
    a.target = '_blank';
    a.rel = 'noopener';
    document.body.appendChild(a);
    a.click();
    a.remove();
  });
  function nextStory() {
    var list = storiesOn(state.page), i = list.indexOf(state.story);
    if (i >= 0 && i < list.length - 1) { select(list[i + 1]); remember(); }
    else if (state.page < pages.length) go(state.page + 1);
  }
  on($('[data-clip-next]'), 'click', nextStory);

  // Full view: the story (or the whole page's text) in a box over the page.
  var pop = $('[data-pop]'), popBody = $('[data-pop-body]');
  function showPop(nodes) {
    popBody.innerHTML = '';
    var box = document.createElement('div');
    box.className = 'ep-read';
    box.style.setProperty('--fs', state.fs + 'px');
    nodes.forEach(function (n) { box.appendChild(n); });
    popBody.appendChild(box);
    pop.hidden = false;
    pop.querySelector('.ep-pop-box').scrollTop = 0;
  }
  function open(id) {
    var t = tpl(id);
    if (t) showPop([t.content.cloneNode(true)]);
  }
  function close() { pop.hidden = true; }
  on(pop, 'click', function (e) { if (e.target === pop || e.target.closest('[data-pop-close]')) close(); });
  on($('[data-textpage]'), 'click', function () {
    var nodes = [];
    var h = document.createElement('p');
    h.className = 'ep-kicker';
    h.textContent = 'PAGE ' + state.page + ': ' + pages[state.page - 1].dataset.name;
    nodes.push(h);
    storiesOn(state.page).forEach(function (id, i) {
      var t = tpl(id);
      if (!t) return;
      if (i > 0) nodes.push(document.createElement('hr'));
      var c = t.content.cloneNode(true);
      var k = c.querySelector('.ep-kicker');
      if (k) k.remove();
      nodes.push(c);
    });
    showPop(nodes);
  });

  // Clicking a story: beside the page in the two-part view, else full view.
  stage.addEventListener('click', function (e) {
    if (state.all) {
      var p = e.target.closest('.np-page');
      if (p) go(+p.dataset.page);
      return;
    }
    var s = e.target.closest('[data-story]');
    if (!s) return;
    if (state.view === 2 && wide()) { select(s.dataset.story); remember(); }
    else open(s.dataset.story);
  });
  stage.addEventListener('keydown', function (e) {
    var s = e.target.closest('[data-story]');
    if (s && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); s.click(); }
  });

  // Tool bar
  on($('[data-prev]'), 'click', function () { go(state.page - 1); });
  on($('[data-next]'), 'click', function () { go(state.page + 1); });
  on($('[data-page-select]'), 'change', function (e) { go(+e.target.value); });
  var side = $('[data-side]');
  $$('[data-toggle-pages]').forEach(function (b) {
    b.addEventListener('click', function () { side.classList.toggle('closed'); draw(); });
  });
  if (!wide()) side.classList.remove('closed');
  $$('[data-view]').forEach(function (b) {
    b.addEventListener('click', function () {
      state.view = +b.dataset.view;
      if (state.all) setAll(false);
      body.classList.toggle('ep-clipview', state.view === 2);
      $$('[data-view]').forEach(function (x) { x.classList.toggle('on', x === b); });
      draw();
      remember();
    });
  });
  function setAll(v) {
    state.all = v;
    body.classList.toggle('ep-allpages', v);
    body.classList.toggle('ep-clipview', !v && state.view === 2);
    $('[data-allpages]').classList.toggle('on', v);
  }
  on($('[data-allpages]'), 'click', function () { setAll(!state.all); draw(); stage.scrollTop = 0; });
  on($('[data-full]'), 'click', function () {
    if (document.fullscreenElement) document.exitFullscreen();
    else if (document.documentElement.requestFullscreen) document.documentElement.requestFullscreen().catch(function () { toast('Full screen is not available here'); });
  });
  document.addEventListener('fullscreenchange', function () { setTimeout(draw, 100); });

  // Date: the calendar opens that day's edition.
  var dateInput = $('[data-date]');
  on(dateInput, 'click', function () { try { dateInput.showPicker(); } catch (e) {} });
  on(dateInput, 'change', function () {
    var d = dateInput.value.split('-');
    if (d.length !== 3) return;
    window.location = '/epaper/DhakaEdition?eid=1&edate=' + encodeURIComponent(d[2] + '/' + d[1] + '/' + d[0]) + '&pgid=1&view=' + state.view;
  });

  // Edition: Dhaka is the only one.
  var edMenu = $('[data-edition-menu]');
  on($('[data-edition-btn]'), 'click', function (e) { e.stopPropagation(); edMenu.hidden = !edMenu.hidden; });

  // PDF: every page through the browser's print dialog ("Save as PDF").
  on($('[data-print]'), 'click', function () {
    var imgs = $$('.ep-pages img');
    imgs.forEach(function (i) { i.loading = 'eager'; });
    var wait = imgs.filter(function (i) { return !i.complete; }).map(function (i) {
      return new Promise(function (r) { i.addEventListener('load', r); i.addEventListener('error', r); });
    });
    toast('Preparing the edition …');
    Promise.race([Promise.all(wait), new Promise(function (r) { setTimeout(r, 4000); })]).then(function () { window.print(); });
  });

  // Search the edition's stories.
  var index = $$('template[id^="st-"]').map(function (t) {
    var text = t.content.querySelector('.ep-text');
    return {
      id: t.id.slice(3), page: +t.dataset.page, title: t.dataset.title,
      hay: (t.dataset.title + ' ' + (text ? text.textContent : '')).toLowerCase()
    };
  });
  var search = $('[data-search]'), results = $('[data-results]');
  function runSearch() {
    var q = search.value.trim().toLowerCase();
    results.innerHTML = '';
    if (q.length < 2) { results.hidden = true; return; }
    var hits = index.filter(function (s) { return s.hay.indexOf(q) >= 0; }).slice(0, 20);
    if (!hits.length) {
      var p = document.createElement('p');
      p.textContent = 'Nothing found in this edition';
      results.appendChild(p);
    }
    hits.forEach(function (s) {
      var b = document.createElement('button');
      b.type = 'button';
      b.textContent = s.title;
      var small = document.createElement('small');
      small.textContent = 'Page ' + s.page + ': ' + pages[s.page - 1].dataset.name;
      b.appendChild(small);
      b.addEventListener('click', function () {
        results.hidden = true;
        go(s.page, s.id);
        if (!(state.view === 2 && wide())) open(s.id);
      });
      results.appendChild(b);
    });
    results.hidden = false;
  }
  on(search, 'input', runSearch);
  on(search, 'keydown', function (e) {
    if (e.key === 'Enter') { var b = results.querySelector('button'); if (b) b.click(); }
    if (e.key === 'Escape') { results.hidden = true; search.blur(); }
  });

  // Share the page or the story.
  var sharebox = $('[data-sharebox]'), shareLink = '';
  function link(withStory) {
    var u = new URL(window.location.href);
    u.hash = '';
    if (withStory && state.story) u.searchParams.set('aid', state.story);
    else u.searchParams.delete('aid');
    return u.toString();
  }
  $$('[data-share]').forEach(function (b) {
    b.addEventListener('click', function (e) {
      e.stopPropagation();
      var story = b.dataset.share === 'story';
      shareLink = link(story);
      var t = story && state.story && tpl(state.story);
      var title = t ? t.dataset.title : document.title;
      var u = encodeURIComponent(shareLink), tt = encodeURIComponent(title);
      var to = {
        facebook: 'https://www.facebook.com/sharer/sharer.php?u=' + u,
        x: 'https://x.com/intent/tweet?text=' + tt + '&url=' + u,
        whatsapp: 'https://wa.me/?text=' + tt + '%20' + u,
        email: 'mailto:?subject=' + tt + '&body=' + u
      };
      $$('[data-share-to]', sharebox).forEach(function (a) { a.href = to[a.dataset.shareTo]; });
      var r = b.getBoundingClientRect();
      sharebox.hidden = false;
      sharebox.style.top = Math.min(window.innerHeight - sharebox.offsetHeight - 8, r.bottom + 6) + 'px';
      sharebox.style.left = Math.max(8, r.right - sharebox.offsetWidth) + 'px';
    });
  });
  on(sharebox.querySelector('[data-copy]'), 'click', function () {
    sharebox.hidden = true;
    (navigator.clipboard ? navigator.clipboard.writeText(shareLink) : Promise.reject())
      .then(function () { toast('Link copied'); })
      .catch(function () { window.prompt('Copy this link', shareLink); });
  });
  document.addEventListener('click', function (e) {
    if (!e.target.closest('[data-sharebox]')) sharebox.hidden = true;
    if (!e.target.closest('.ep-edition')) edMenu.hidden = true;
    if (!e.target.closest('.ep-search')) results.hidden = true;
  });

  // Breaking news runs at a steady speed whatever its length.
  var run = $('.ep-ticker-run');
  if (run) run.style.animationDuration = Math.max(30, run.scrollWidth / 2 / 70) + 's';

  // Keyboard
  document.addEventListener('keydown', function (e) {
    if (e.target.tagName === 'SELECT' || e.target.tagName === 'INPUT') return;
    if (!pop.hidden) { if (e.key === 'Escape') close(); return; }
    if (e.key === 'Escape') { sharebox.hidden = true; if (state.all) { setAll(false); draw(); } }
    if (e.key === 'ArrowRight' || e.key === 'PageDown') go(state.page + 1);
    if (e.key === 'ArrowLeft' || e.key === 'PageUp') go(state.page - 1);
    if (e.key === 'ArrowDown' && state.view === 2) { e.preventDefault(); nextStory(); }
    if (e.key === 'Home') go(1);
    if (e.key === 'End') go(pages.length);
  });

  // Swipe on phones.
  var x0 = null;
  stage.addEventListener('touchstart', function (e) { if (e.touches.length === 1) x0 = e.touches[0].clientX; }, { passive: true });
  stage.addEventListener('touchend', function (e) {
    if (x0 === null) return;
    var dx = e.changedTouches[0].clientX - x0;
    if (Math.abs(dx) > 60) go(state.page + (dx < 0 ? 1 : -1));
    x0 = null;
  });

  window.addEventListener('resize', draw);
  // A shared story link opens on its page with that story.
  var start = EP.aid && tpl(EP.aid);
  if (start) {
    go(+start.dataset.page, EP.aid);
    if (!(state.view === 2 && wide())) open(EP.aid);
  } else {
    go(state.page);
  }
})();
