/* The Daily Star E-Paper viewer: pages fitted or zoomed, one or two at a
   time, thumbnails and page numbers, text view, full screen, print/PDF,
   sharing, keyboard and swipe, and story pop-ups. */
(function () {
  var stage = document.querySelector('[data-stage]');
  if (!stage) return;
  var EP = window.EP || {};
  var pages = Array.prototype.slice.call(stage.querySelectorAll('.np-page'));
  var W = 1000, H = 1414;
  var state = { page: EP.page || 1, view: EP.view || 1, zoom: 0, fit: 'page', text: false };
  // zoom 0 means "fit"; otherwise a scale factor.

  function $(s) { return document.querySelector(s); }
  function toast(msg) {
    var t = $('[data-toast]');
    t.textContent = msg;
    t.classList.add('show');
    clearTimeout(toast.t);
    toast.t = setTimeout(function () { t.classList.remove('show'); }, 2200);
  }

  // Each page sits in a box of its scaled size, so the stage lays them out.
  pages.forEach(function (p) {
    var box = document.createElement('div');
    box.className = 'ep-scale';
    p.parentNode.insertBefore(box, p);
    box.appendChild(p);
  });

  // Thumbnails: a small copy of every page.
  var thumbs = $('[data-thumbs]');
  pages.forEach(function (p) {
    var b = document.createElement('button');
    b.type = 'button';
    b.className = 'ep-thumb';
    b.dataset.page = p.dataset.page;
    var box = document.createElement('div');
    box.className = 'ep-thumb-box';
    var copy = p.cloneNode(true);
    copy.removeAttribute('data-page');
    copy.style.transform = '';
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

  function fitScale(shown) {
    var availW = stage.clientWidth - 36 - (shown.length - 1) * 18;
    var availH = stage.clientHeight - 36;
    if (window.innerWidth <= 760) return (stage.clientWidth - 20) / W;
    if (state.fit === 'width') return availW / shown.length / W;
    return Math.min(availW / shown.length / W, availH / H);
  }

  function draw() {
    var shown = visible();
    var scale = state.zoom || fitScale(shown);
    pages.forEach(function (p) {
      var on = shown.indexOf(+p.dataset.page) >= 0;
      p.parentNode.style.display = on ? 'block' : 'none';
      p.style.transform = 'scale(' + scale + ')';
      p.parentNode.style.width = (W * scale) + 'px';
      p.parentNode.style.height = (H * scale) + 'px';
    });
    stage.style.justifyContent = (W * scale * shown.length > stage.clientWidth) ? 'flex-start' : 'center';
    thumbs.querySelectorAll('.ep-thumb').forEach(function (t) { t.classList.toggle('on', shown.indexOf(+t.dataset.page) >= 0); });
    document.querySelectorAll('[data-goto]').forEach(function (b) { b.classList.toggle('on', shown.indexOf(+b.dataset.goto) >= 0); });
    $('[data-page-label]').textContent = 'Page ' + shown.join('–') + ' of ' + pages.length;
    $('[data-zoom-label]').textContent = state.zoom ? Math.round(state.zoom * 100) + '%' : (state.fit === 'width' ? 'Fit width' : 'Fit page');
    var sel = $('[data-page-select]');
    if (sel) sel.value = String(state.page);
    if (state.text) drawText();
  }

  function remember() {
    var u = new URL(window.location.href);
    u.searchParams.set('pgid', state.page);
    u.searchParams.set('view', state.view);
    u.searchParams.delete('aid');
    history.replaceState(null, '', u);
  }

  function go(n) {
    state.page = Math.max(1, Math.min(pages.length, n));
    remember();
    draw();
    stage.scrollTop = 0;
    stage.scrollLeft = 0;
    var on = thumbs.querySelector('.ep-thumb.on');
    if (on) on.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }

  function step(d) {
    var shown = visible();
    if (d > 0) go(shown[shown.length - 1] + 1);
    else go(state.view === 2 && shown[0] > 2 ? shown[0] - 2 : shown[0] - 1);
  }

  function zoomBy(d) {
    var cur = state.zoom || fitScale(visible());
    var steps = [0.25, 0.35, 0.45, 0.55, 0.7, 0.85, 1, 1.25, 1.5, 2];
    var next = cur;
    if (d > 0) { next = steps.find(function (s) { return s > cur + 0.001; }) || 2; }
    else { next = steps.slice().reverse().find(function (s) { return s < cur - 0.001; }) || 0.25; }
    state.zoom = next;
    draw();
  }

  // Text view: the current page's stories as cards.
  var textView = $('[data-textview]');
  function drawText() {
    textView.innerHTML = '';
    visible().forEach(function (n) {
      pages[n - 1].querySelectorAll('[data-story]').forEach(function (s) {
        var t = document.getElementById('st-' + s.dataset.story);
        if (!t) return;
        var c = t.content;
        var card = document.createElement('article');
        card.dataset.story = s.dataset.story;
        var k = document.createElement('div'); k.className = 'k'; k.textContent = c.querySelector('.ep-pop-kicker').textContent;
        var h = document.createElement('h3'); h.textContent = c.querySelector('h2').textContent;
        card.appendChild(k);
        var img = c.querySelector('img');
        if (img) { var i = document.createElement('img'); i.src = img.getAttribute('src'); i.alt = ''; i.loading = 'lazy'; card.appendChild(i); }
        card.appendChild(h);
        var p = document.createElement('p');
        var first = c.querySelector('.ep-pop-intro, .ep-pop-text p');
        p.textContent = first ? first.textContent : '';
        card.appendChild(p);
        textView.appendChild(card);
      });
    });
  }
  textView.addEventListener('click', function (e) {
    var a = e.target.closest('[data-story]');
    if (a) open(a.dataset.story);
  });

  // Controls
  $('[data-first]').addEventListener('click', function () { go(1); });
  $('[data-last]').addEventListener('click', function () { go(pages.length); });
  $('[data-prev]').addEventListener('click', function () { step(-1); });
  $('[data-next]').addEventListener('click', function () { step(1); });
  $('[data-page-select]').addEventListener('change', function (e) { go(+e.target.value); });
  document.querySelectorAll('[data-goto]').forEach(function (b) { b.addEventListener('click', function () { go(+b.dataset.goto); }); });
  document.querySelectorAll('[data-zoom]').forEach(function (b) { b.addEventListener('click', function () { zoomBy(+b.dataset.zoom); }); });
  document.querySelectorAll('[data-fit]').forEach(function (b) {
    b.addEventListener('click', function () { state.zoom = 0; state.fit = b.dataset.fit; draw(); });
  });
  document.querySelectorAll('[data-view]').forEach(function (b) {
    b.addEventListener('click', function () {
      state.view = +b.dataset.view;
      document.body.classList.toggle('ep-double', state.view === 2);
      document.querySelectorAll('[data-view]').forEach(function (x) { x.classList.toggle('on', x === b); });
      go(state.page);
    });
  });
  var textBtn = $('[data-text]');
  textBtn.addEventListener('click', function () {
    state.text = !state.text;
    textBtn.classList.toggle('on', state.text);
    textView.hidden = !state.text;
    stage.hidden = state.text;
    draw();
  });
  $('[data-full]').addEventListener('click', function () {
    if (document.fullscreenElement) document.exitFullscreen();
    else if (document.documentElement.requestFullscreen) document.documentElement.requestFullscreen().catch(function () {});
  });
  document.addEventListener('fullscreenchange', function () { setTimeout(draw, 100); });

  // Date: the calendar opens the chosen day's edition.
  var dateInput = $('[data-date]');
  if (dateInput) dateInput.addEventListener('change', function () {
    var d = dateInput.value.split('-');
    if (d.length !== 3) return;
    window.location = '/epaper/DhakaEdition?eid=1&edate=' + encodeURIComponent(d[2] + '/' + d[1] + '/' + d[0]) + '&pgid=1&view=' + state.view;
  });

  // Print and PDF: the browser's print dialog ("Save as PDF").
  function printWith(cls, prep) {
    document.body.classList.add(cls);
    pages.forEach(function (p) { p.parentNode.style.display = 'block'; });
    if (prep) prep();
    window.print();
    document.body.classList.remove(cls);
    document.querySelectorAll('.print-me').forEach(function (x) { x.classList.remove('print-me'); });
    draw();
  }
  $('[data-print]').addEventListener('click', function () { printWith('print-all'); });
  $('[data-print-page]').addEventListener('click', function () {
    printWith('print-one', function () { visible().forEach(function (n) { pages[n - 1].parentNode.classList.add('print-me'); }); });
  });

  // Share the page.
  var shareMenu = $('[data-share-menu]');
  function pageLink() { return window.location.href.split('#')[0]; }
  $('[data-share-btn]').addEventListener('click', function () {
    var u = encodeURIComponent(pageLink()), t = encodeURIComponent(document.title);
    var to = {
      facebook: 'https://www.facebook.com/sharer/sharer.php?u=' + u,
      x: 'https://x.com/intent/tweet?text=' + t + '&url=' + u,
      whatsapp: 'https://wa.me/?text=' + t + '%20' + u,
      email: 'mailto:?subject=' + t + '&body=' + u
    };
    shareMenu.querySelectorAll('[data-share-to]').forEach(function (a) { a.href = to[a.dataset.shareTo]; });
    shareMenu.hidden = !shareMenu.hidden;
  });
  function copy(text) {
    (navigator.clipboard ? navigator.clipboard.writeText(text) : Promise.reject())
      .then(function () { toast('Link copied'); })
      .catch(function () { window.prompt('Copy this link', text); });
  }
  shareMenu.querySelector('[data-copy]').addEventListener('click', function () { copy(pageLink()); shareMenu.hidden = true; });
  document.addEventListener('click', function (e) { if (!e.target.closest('.ep-share')) shareMenu.hidden = true; });

  // Story pop-up, with the page's other stories one click away.
  var pop = $('[data-pop]'), popBody = $('[data-pop-body]');
  var current = null, fontSize = 17;
  function storiesOnPage(id) {
    var t = document.getElementById('st-' + id);
    var n = t ? +t.dataset.page : state.page;
    return Array.prototype.map.call(pages[n - 1].querySelectorAll('[data-story]'), function (s) { return s.dataset.story; });
  }
  function open(id) {
    var t = document.getElementById('st-' + id);
    if (!t) return;
    current = id;
    popBody.innerHTML = '';
    popBody.appendChild(t.content.cloneNode(true));
    popBody.style.setProperty('--fs', fontSize + 'px');
    pop.hidden = false;
    popBody.scrollTop = 0;
    var list = storiesOnPage(id), i = list.indexOf(id);
    $('[data-pop-prev]').disabled = i <= 0;
    $('[data-pop-next]').disabled = i < 0 || i >= list.length - 1;
    document.querySelectorAll('.np-story.hl').forEach(function (s) { s.classList.remove('hl'); });
  }
  function close() { pop.hidden = true; current = null; }
  function storyLink(id) {
    var u = new URL(window.location.href);
    u.searchParams.set('aid', id);
    return u.toString();
  }
  $('[data-pop-prev]').addEventListener('click', function () { var l = storiesOnPage(current), i = l.indexOf(current); if (i > 0) open(l[i - 1]); });
  $('[data-pop-next]').addEventListener('click', function () { var l = storiesOnPage(current), i = l.indexOf(current); if (i >= 0 && i < l.length - 1) open(l[i + 1]); });
  document.querySelectorAll('[data-font]').forEach(function (b) {
    b.addEventListener('click', function () {
      fontSize = Math.max(14, Math.min(24, fontSize + 2 * +b.dataset.font));
      popBody.style.setProperty('--fs', fontSize + 'px');
    });
  });
  $('[data-pop-share]').addEventListener('click', function () { if (current) copy(storyLink(current)); });
  $('[data-pop-print]').addEventListener('click', function () {
    document.body.classList.add('print-story');
    window.print();
    document.body.classList.remove('print-story');
  });
  stage.addEventListener('click', function (e) {
    var s = e.target.closest('[data-story]');
    if (s) open(s.dataset.story);
  });
  stage.addEventListener('keydown', function (e) {
    var s = e.target.closest('[data-story]');
    if (s && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); open(s.dataset.story); }
  });
  pop.addEventListener('click', function (e) { if (e.target === pop || e.target.closest('[data-pop-close]')) close(); });

  // Keyboard
  document.addEventListener('keydown', function (e) {
    if (e.target.tagName === 'SELECT' || e.target.tagName === 'INPUT') return;
    if (!pop.hidden) {
      if (e.key === 'Escape') close();
      if (e.key === 'ArrowRight') $('[data-pop-next]').click();
      if (e.key === 'ArrowLeft') $('[data-pop-prev]').click();
      return;
    }
    if (e.key === 'ArrowRight' || e.key === 'PageDown') step(1);
    if (e.key === 'ArrowLeft' || e.key === 'PageUp') step(-1);
    if (e.key === 'Home') go(1);
    if (e.key === 'End') go(pages.length);
    if (e.key === '+' || e.key === '=') zoomBy(1);
    if (e.key === '-') zoomBy(-1);
    if (e.key === '0') { state.zoom = 0; draw(); }
  });

  // Swipe on phones.
  var x0 = null;
  stage.addEventListener('touchstart', function (e) { if (e.touches.length === 1) x0 = e.touches[0].clientX; }, { passive: true });
  stage.addEventListener('touchend', function (e) {
    if (x0 === null || state.zoom) return;
    var dx = e.changedTouches[0].clientX - x0;
    if (Math.abs(dx) > 60) step(dx < 0 ? 1 : -1);
    x0 = null;
  });

  window.addEventListener('resize', draw);
  draw();
  // A shared story link opens on its page with the story.
  if (EP.aid) {
    var t = document.getElementById('st-' + EP.aid);
    if (t) { go(+t.dataset.page); open(EP.aid); }
  }
})();
