/* The Daily Star E-Paper viewer (Dhaka Edition and its supplements): the
   menu, the page fitted to the screen, the page list by supplement, the story
   beside the page (Text View, Full View, Dload Image, font-inc, Listen),
   search, date and page choice, all pages, PDF, sharing, full screen,
   keyboard and swipe, and the most popular stories. */
(function () {
  var EP = window.EP || {};
  var body = document.body;
  function $(s) { return document.querySelector(s); }
  function $$(s, root) { return Array.prototype.slice.call((root || document).querySelectorAll(s)); }
  function on(el, ev, fn) { if (el) el.addEventListener(ev, fn); }
  function wide() { return window.innerWidth > 860; }
  function toast(msg) {
    var t = $('[data-toast]');
    if (!t) return;
    t.textContent = msg;
    t.classList.add('show');
    clearTimeout(toast.t);
    toast.t = setTimeout(function () { t.classList.remove('show'); }, 2200);
  }

  // The menu (☰) slides in from the left.
  var nav = $('[data-nav]'), shade = $('[data-nav-shade]');
  function openNav(v) { nav.hidden = !v; shade.hidden = !v; }
  $$('[data-menu]').forEach(function (b) { on(b, 'click', function () { openNav(true); }); });
  on($('[data-nav-close]'), 'click', function () { openNav(false); });
  on(shade, 'click', function () { openNav(false); });

  // Most Popular Article: the day's stories readers opened most.
  var pop = $('[data-pop]'), popBody = $('[data-pop-body]');
  on($('[data-popular]'), 'click', function (e) {
    e.preventDefault();
    openNav(false);
    if (!pop) { window.location = '/epaper#popular'; return; } // from the FAQs
    var url = '/epaper/popular' + (EP.date ? '?date=' + EP.date : '');
    fetch(url).then(function (r) { return r.json(); }).then(function (list) {
      var box = document.createElement('div');
      box.className = 'ep-popular';
      var h = document.createElement('h2');
      h.textContent = 'Most Popular Article';
      box.appendChild(h);
      if (!list.length) {
        var p = document.createElement('p');
        p.textContent = 'No edition yet.';
        box.appendChild(p);
      }
      list.forEach(function (s, i) {
        var a = document.createElement('a');
        a.href = '#';
        if (s.Image) { var img = document.createElement('img'); img.src = s.Image; img.alt = ''; img.loading = 'lazy'; a.appendChild(img); }
        var t = document.createElement('span');
        var n = document.createElement('b'); n.textContent = (i + 1) + '. ';
        t.appendChild(n);
        t.appendChild(document.createTextNode(s.Title));
        var small = document.createElement('small'); small.textContent = s.Page;
        t.appendChild(small);
        a.appendChild(t);
        a.addEventListener('click', function (ev) {
          ev.preventDefault();
          pop.hidden = true;
          if (typeof window.EPgo === 'function') window.EPgo(s.No, s.ID, true);
        });
        box.appendChild(a);
      });
      popBody.innerHTML = '';
      popBody.appendChild(box);
      pop.hidden = false;
    }).catch(function () { toast('Could not load the list'); });
  });
  on(pop, 'click', function (e) { if (e.target === pop || e.target.closest('[data-pop-close]')) closePop(); });
  function closePop() { pop.hidden = true; stopListening(); }

  // Read aloud with the browser's own voice.
  var speaking = false;
  function stopListening() {
    if (window.speechSynthesis) window.speechSynthesis.cancel();
    speaking = false;
    var l = $('[data-listen-label]');
    if (l) l.textContent = 'Listen';
  }

  var stage = document.querySelector('[data-stage]');
  if (!stage) return;
  var pages = Array.prototype.slice.call(stage.querySelectorAll('.np-page'));
  var W = 1000, H = 1414;
  var state = { page: Math.max(1, Math.min(pages.length, EP.page || 1)), view: EP.view === 1 ? 1 : 2, story: null, fs: 16, all: false };

  // Each page sits in a box of its scaled size, so the stage lays it out.
  pages.forEach(function (p) {
    var box = document.createElement('div');
    box.className = 'ep-scale';
    p.parentNode.insertBefore(box, p);
    box.appendChild(p);
  });

  // The page list, by supplement: "1 FRONT", "2 NEWS" …, "1 BIZFRONT" …
  var side = $('[data-side]');
  pages.forEach(function (p) {
    var list = side.querySelector('[data-thumbs="' + (p.dataset.group || '') + '"]');
    if (!list) return;
    var b = document.createElement('button');
    b.type = 'button';
    b.className = 'ep-thumb';
    b.dataset.page = p.dataset.page;
    b.title = p.dataset.gno + ': ' + p.dataset.name;
    var box = document.createElement('div');
    box.className = 'ep-thumb-box';
    var copy = p.cloneNode(true);
    copy.removeAttribute('data-page');
    copy.style.transform = '';
    $$('[data-story]', copy).forEach(function (s) { s.removeAttribute('data-story'); s.removeAttribute('tabindex'); });
    $$('img', copy).forEach(function (i) { i.loading = 'lazy'; });
    box.appendChild(copy);
    var label = document.createElement('span');
    label.textContent = p.dataset.gno + ' ' + p.dataset.name;
    b.appendChild(box);
    b.appendChild(label);
    b.addEventListener('click', function () { go(+b.dataset.page); });
    list.appendChild(b);
  });
  $$('[data-fold]').forEach(function (b) {
    b.addEventListener('click', function () { b.closest('.ep-group').classList.toggle('open'); });
  });

  function storiesOn(n) {
    return $$('[data-story]', pages[n - 1]).map(function (s) { return s.dataset.story; });
  }
  function tpl(id) { return document.getElementById('st-' + id); }
  function pageEl(n) { return pages[n - 1]; }

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
      var room = body.clientWidth - side.offsetWidth - 440 - 80;
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
    $$('.ep-thumb', side).forEach(function (t) { t.classList.toggle('on', +t.dataset.page === state.page); });
    var sel = $('[data-page-select]');
    if (sel) sel.value = String(state.page);
    $('[data-prev]').hidden = state.all || state.page <= 1;
    $('[data-next]').hidden = state.all || state.page >= pages.length;
    var mid = Math.round(Math.min(stage.clientHeight, H * scale + 28) * 0.36);
    $('[data-prev]').style.top = mid + 'px';
    $('[data-next]').style.top = mid + 'px';
    $('[data-clip-next]').style.top = mid + 'px';
    $('[data-clip-prev]').style.top = mid + 'px';
  }

  function remember() {
    var u = new URL(window.location.href);
    u.searchParams.set('pgid', state.page);
    u.searchParams.set('view', state.view);
    if (state.story && state.view === 2) u.searchParams.set('aid', state.story);
    else u.searchParams.delete('aid');
    history.replaceState(null, '', u);
  }

  function go(n, story, opened) {
    state.page = Math.max(1, Math.min(pages.length, n));
    if (state.all) setAll(false);
    // The page list opens the Dhaka Edition or the supplement this page is in.
    var g = pageEl(state.page).dataset.group || '';
    $$('.ep-group', side).forEach(function (x) { x.classList.toggle('open', x.dataset.group === g); });
    draw();
    stage.scrollTop = 0;
    stage.scrollLeft = 0;
    var t = side.querySelector('.ep-thumb.on');
    if (t) t.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    select(story || storiesOn(state.page)[0]);
    if (story && opened) {
      read(story);
      if (!(state.view === 2 && wide())) open(story);
    }
    remember();
  }
  window.EPgo = go;

  // A story opened by the reader counts once per visit for Most Popular.
  var counted = {};
  function read(id) {
    if (!id || counted[id] || !EP.date) return;
    counted[id] = true;
    fetch('/epaper/read/' + id + '?date=' + EP.date, { method: 'POST', keepalive: true }).catch(function () {});
  }

  // The story beside the page.
  var clipBody = $('[data-clip-body]');
  function select(id) {
    state.story = id || null;
    stopListening();
    $$('.np-story.sel').forEach(function (s) { s.classList.remove('sel'); });
    if (!id) { clipBody.innerHTML = ''; return; }
    var s = stage.querySelector('[data-story="' + id + '"]');
    if (s) s.classList.add('sel');
    drawClip();
  }
  function storyBox(id) {
    var t = tpl(id);
    var box = document.createElement('div');
    box.className = 'ep-read';
    box.style.setProperty('--fs', state.fs + 'px');
    if (t) box.appendChild(t.content.cloneNode(true));
    return box;
  }
  function drawClip() {
    clipBody.innerHTML = '';
    if (!state.story) return;
    clipBody.appendChild(storyBox(state.story));
    clipBody.scrollTop = 0;
    var list = storiesOn(state.page), i = list.indexOf(state.story);
    $('[data-clip-prev]').hidden = i <= 0 && state.page <= 1;
  }
  on($('[data-fontinc]'), 'click', function () {
    state.fs = state.fs >= 22 ? 16 : state.fs + 2;
    $$('.ep-read').forEach(function (b) { b.style.setProperty('--fs', state.fs + 'px'); });
  });
  on($('[data-fullview]'), 'click', function () { if (state.story) open(state.story); });
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
  on($('[data-listen]'), 'click', function () {
    if (!window.speechSynthesis || !window.SpeechSynthesisUtterance) { toast('Your browser cannot read aloud'); return; }
    if (speaking) { stopListening(); return; }
    var t = state.story && tpl(state.story);
    if (!t) return;
    var c = t.content;
    var text = [c.querySelector('h2'), c.querySelector('.ep-intro'), c.querySelector('.ep-text')]
      .filter(Boolean).map(function (n) { return n.textContent.trim(); }).join('. ');
    var u = new SpeechSynthesisUtterance(text);
    u.lang = 'en-GB';
    u.onend = stopListening;
    window.speechSynthesis.cancel();
    window.speechSynthesis.speak(u);
    speaking = true;
    $('[data-listen-label]').textContent = 'Pause';
  });
  function stepStory(d) {
    var list = storiesOn(state.page), i = list.indexOf(state.story);
    if (d > 0) {
      if (i >= 0 && i < list.length - 1) { select(list[i + 1]); remember(); }
      else if (state.page < pages.length) go(state.page + 1);
    } else {
      if (i > 0) { select(list[i - 1]); remember(); }
      else if (state.page > 1) { var prev = storiesOn(state.page - 1); go(state.page - 1, prev[prev.length - 1]); }
    }
  }
  on($('[data-clip-next]'), 'click', function () { stepStory(1); });
  on($('[data-clip-prev]'), 'click', function () { stepStory(-1); });

  // Full view: the story over the page.
  function open(id) {
    if (!tpl(id)) return;
    popBody.innerHTML = '';
    popBody.appendChild(storyBox(id));
    pop.hidden = false;
    pop.querySelector('.ep-pop-box').scrollTop = 0;
  }

  // Clicking a story: beside the page in Page and Article View, else over it.
  stage.addEventListener('click', function (e) {
    if (state.all) {
      var p = e.target.closest('.np-page');
      if (p) go(+p.dataset.page);
      return;
    }
    var s = e.target.closest('[data-story]');
    if (!s) return;
    read(s.dataset.story);
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

  // Edition: the Dhaka Edition or a supplement.
  var edMenu = $('[data-edition-menu]');
  on($('[data-edition-btn]'), 'click', function (e) { e.stopPropagation(); edMenu.hidden = !edMenu.hidden; });
  $$('[data-goto-group]').forEach(function (a) {
    a.addEventListener('click', function (e) { e.preventDefault(); edMenu.hidden = true; go(+a.dataset.gotoGroup); });
  });

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
      var pe = pageEl(s.page);
      small.textContent = (pe.dataset.group ? pe.dataset.group + ' ' : '') + 'Page ' + pe.dataset.gno + ': ' + pe.dataset.name;
      b.appendChild(small);
      b.addEventListener('click', function () { results.hidden = true; go(s.page, s.id, true); });
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
        whatsapp: 'https://wa.me/?text=' + tt + '%20' + u
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
    if (!pop.hidden) { if (e.key === 'Escape') closePop(); return; }
    if (!nav.hidden) { if (e.key === 'Escape') openNav(false); return; }
    if (e.key === 'Escape') { sharebox.hidden = true; if (state.all) { setAll(false); draw(); } }
    if (e.key === 'ArrowRight' || e.key === 'PageDown') go(state.page + 1);
    if (e.key === 'ArrowLeft' || e.key === 'PageUp') go(state.page - 1);
    if (e.key === 'ArrowDown' && state.view === 2) { e.preventDefault(); stepStory(1); }
    if (e.key === 'ArrowUp' && state.view === 2) { e.preventDefault(); stepStory(-1); }
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
  if (start) go(+start.dataset.page, EP.aid, true);
  else go(state.page);
  if (window.location.hash === '#popular') {
    history.replaceState(null, '', window.location.pathname + window.location.search);
    $('[data-popular]').click();
  }
})();
