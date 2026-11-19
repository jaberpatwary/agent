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

  // The window scrolls as on the Star's e-paper: past 120 pixels the tool bar
  // and breaking news hold to the top (the small nameplate in place of Home)
  // and the button back to the top shows; it goes up in 0.8 seconds.
  var totop = $('[data-totop]');
  function onScroll() {
    var y = window.pageYOffset || document.documentElement.scrollTop;
    body.classList.toggle('ep-sticky', y >= 120);
    if (totop) totop.hidden = y <= 120;
  }
  window.addEventListener('scroll', onScroll, { passive: true });
  onScroll();
  function scrollToY(to, ms) {
    var from = window.pageYOffset, t0 = null;
    if (!ms || Math.abs(to - from) < 2) { window.scrollTo(0, to); return; }
    function step(t) {
      if (t0 === null) t0 = t;
      var p = Math.min(1, (t - t0) / ms);
      window.scrollTo(0, from + (to - from) * (0.5 - Math.cos(p * Math.PI) / 2)); // jQuery's "swing"
      if (p < 1) requestAnimationFrame(step);
    }
    requestAnimationFrame(step);
  }
  on(totop, 'click', function () { scrollToY(0, 800); });

  var stage = document.querySelector('[data-stage]');
  if (!stage) return;
  var pages = Array.prototype.slice.call(stage.querySelectorAll('.np-page'));
  var state = { page: Math.max(1, Math.min(pages.length, EP.page || 1)), view: EP.view === 1 ? 1 : 2, story: null, fs: 16, all: false };
  // A page's size in the paper's units: 1008 x 1548 broadsheet, 792 x 1008 tabloid.
  function dims(p) { return { w: +p.dataset.w || 1008, h: +p.dataset.h || 1548 }; }

  // Each page sits in a box of its scaled size, so the stage lays it out.
  pages.forEach(function (p) {
    var box = document.createElement('div');
    box.className = 'ep-scale';
    p.parentNode.insertBefore(box, p);
    box.appendChild(p);
  });

  /* Setting the text as the paper does. Every box is where the Star's own
     page plan puts it; here its headline is sized to its width, its text runs
     down its columns (the paper's 145-unit columns, 14 apart) around its
     pictures, line on line, and the rest of a story that goes on ("SEE PAGE
     2 COL 4") is set in its box on the later page ("FROM PAGE 1"). */
  var LH = 9.8, GUT = 14;
  var jumps = {};
  function textItems(src) {
    var out = [];
    if (!src) return out;
    $$('.np-bitem', src).forEach(function (b) { out.push(b); });
    if (out.length) return out;
    $$('p, h2, h3, h4, li', src).forEach(function (n) {
      if (n.parentNode !== src && n.parentNode.closest('p, li')) return;
      var t = n.textContent.replace(/\s+/g, ' ').trim();
      if (!t) return;
      var p = document.createElement('p');
      p.textContent = t;
      if (/^H[234]$/.test(n.tagName)) p.className = 'np-sub';
      out.push(p);
    });
    if (!out.length) {
      var t = src.textContent.replace(/\s+/g, ' ').trim();
      if (t) { var p = document.createElement('p'); p.textContent = t; out.push(p); }
    }
    return out;
  }
  function rectOf(el) { return { x: el.offsetLeft, y: el.offsetTop, w: el.offsetWidth, h: el.offsetHeight }; }
  // The free stretches of a column, top to bottom, round what stands in it.
  function spans(col, H, obst) {
    var out = [[0, H]];
    obst.forEach(function (o) {
      if (o.x >= col.x + col.w - 1 || o.x + o.w <= col.x + 1) return;
      var next = [];
      out.forEach(function (s) {
        var a = o.y, b = o.y + o.h;
        if (b <= s[0] || a >= s[1]) { next.push(s); return; }
        if (a > s[0]) next.push([s[0], a]);
        if (b < s[1]) next.push([b, s[1]]);
      });
      out = next;
    });
    return out;
  }
  function segments(W, H, cols, obst) {
    var cw = (W - GUT * (cols - 1)) / cols, segs = [];
    for (var c = 0; c < cols; c++) {
      var col = { x: c * (cw + GUT), w: cw };
      spans(col, H, obst).forEach(function (s) {
        var h = Math.floor((s[1] - s[0]) / LH) * LH;
        if (h >= LH * 2 - 0.1) segs.push({ x: col.x, y: s[0], w: cw, h: h });
      });
    }
    return segs;
  }
  // A paragraph that does not fit is cut at the last word that does; the
  // rest starts the next column.
  function splitP(p, col, h) {
    var words = p.textContent.split(' '), lo = 0, hi = words.length;
    while (lo < hi) {
      var mid = Math.ceil((lo + hi) / 2);
      p.textContent = words.slice(0, mid).join(' ');
      if (col.scrollHeight <= h + 0.5) lo = mid; else hi = mid - 1;
    }
    if (lo === 0) { p.textContent = words.join(' '); return null; }
    p.textContent = words.slice(0, lo).join(' ');
    p.classList.add('np-split');
    var rest = document.createElement('p');
    rest.className = 'np-cont' + (p.classList.contains('np-sub') ? ' np-sub' : '');
    rest.textContent = words.slice(lo).join(' ');
    return rest;
  }
  function pour(box, list, segs) {
    var i = 0, last = null;
    for (var s = 0; s < segs.length && i < list.length; s++) {
      var g = segs[s], col = document.createElement('div');
      col.className = 'np-col';
      col.style.cssText = 'left:' + g.x + 'px;top:' + g.y + 'px;width:' + g.w + 'px;height:' + g.h + 'px';
      box.appendChild(col);
      last = col;
      while (i < list.length) {
        var n = list[i];
        col.appendChild(n);
        if (col.scrollHeight <= g.h + 0.5) { i++; continue; }
        if (n.tagName === 'P') {
          var rest = splitP(n, col, g.h);
          if (rest) list[i] = rest; else col.removeChild(n);
        } else if (col.children.length > 1) {
          col.removeChild(n);
        } else {
          i++; // a brief longer than a whole column keeps what fits
        }
        break;
      }
    }
    return { rest: list.slice(i), last: last };
  }
  // A story cut short ends on a whole sentence.
  function endOnSentence(col) {
    var p = col && col.lastElementChild;
    if (!p || p.tagName !== 'P' || !p.classList.contains('np-split')) return;
    var t = p.textContent, at = Math.max(t.lastIndexOf('. '), t.lastIndexOf('." '), t.lastIndexOf('.” '));
    if (at > 0) { p.textContent = t.slice(0, t.indexOf(' ', at + 1) > 0 ? t.indexOf(' ', at + 1) : at + 1); p.classList.remove('np-split'); }
  }
  // The headline takes its size from the box's width, then steps down until
  // it sits in three lines (four in a single column).
  // The headline takes its size from the box's width (the front page's lead
  // biggest), then steps down until it sits in three lines (four in a single
  // column) and, with its deck, clears any picture below it.
  function fitHead(box, head, cols, maxH) {
    var h3 = head.querySelector('h3'), deck = head.querySelector('.np-deck');
    if (!h3) return;
    var c = Math.min(cols, 6) - 1, jump = box.classList.contains('np-k-jump');
    var size = jump ? 17 : !box.classList.contains('np-lead') ? [20, 25, 29, 32, 36, 38][c]
      : box.closest('.np-front') ? [30, 38, 44, 48, 52, 56][c] : [24, 30, 34, 38, 42, 44][c];
    var maxLines = cols === 1 ? 4 : 3;
    box.style.setProperty('--hs', size + 'px');
    for (var k = 0; k < 40 && size > 13; k++) {
      var lines = Math.round(h3.offsetHeight / (size * 1.02));
      if (lines <= maxLines && head.offsetHeight <= maxH) break;
      if (deck && deck.style.display !== 'none' && lines <= maxLines) { deck.style.display = 'none'; continue; }
      size -= 1;
      box.style.setProperty('--hs', size + 'px');
    }
  }
  function placeHead(box, head, W, H, pics) {
    var y = 0, left = 0, right = W;
    pics.forEach(function (p) { if (p.y < 4 && p.w > W * 0.6) y = Math.max(y, p.y + p.h + 6); });
    pics.forEach(function (p) {
      if (p.w > W * 0.6 || p.y > y + 40 || p.y + p.h <= y) return;
      if (p.x + p.w / 2 < W / 2) left = Math.max(left, p.x + p.w + GUT); else right = Math.min(right, p.x - GUT);
    });
    if (right - left < W * 0.4) {
      pics.forEach(function (p) { if (p.y <= y + 40) y = Math.max(y, p.y + p.h + 6); });
      left = 0; right = W;
    }
    // The head ends above the first picture under it.
    var maxH = H - y - LH * 3;
    pics.forEach(function (p) {
      if (p.y > y && p.x < right && p.x + p.w > left) maxH = Math.min(maxH, p.y - y - 8);
    });
    head.style.cssText = 'left:' + left + 'px;top:' + y + 'px;width:' + (right - left) + 'px';
    fitHead(box, head, Math.max(1, Math.floor((right - left + 10) / 150)), Math.max(maxH, 30));
    return { x: left, y: 0, w: right - left, h: y + head.offsetHeight + 7 };
  }
  function layBox(box) {
    if (box.dataset.laid) return;
    box.dataset.laid = '1';
    var W = box.offsetWidth, H = box.offsetHeight, cols = +box.dataset.cols || 1;
    var pics = $$(':scope > .np-pic', box).map(rectOf);
    var obst = pics.map(function (p) { return { x: p.x - 2, y: p.y - 2, w: p.w + 4, h: p.h + 8 }; });
    // A smaller box set over this one (a quote, a fact box): the text runs round it.
    var me = rectOf(box);
    $$('.np-box', box.parentNode).forEach(function (o) {
      if (o === box || o.classList.contains('np-empty')) return;
      var r = rectOf(o);
      if (r.w * r.h >= me.w * me.h) return;
      var x1 = Math.max(r.x, me.x), y1 = Math.max(r.y, me.y), x2 = Math.min(r.x + r.w, me.x + me.w), y2 = Math.min(r.y + r.h, me.y + me.h);
      if (x2 - x1 > 4 && y2 - y1 > 4) obst.push({ x: x1 - me.x - 8, y: y1 - me.y - 6, w: x2 - x1 + 16, h: y2 - y1 + 12 });
    });
    if (box.classList.contains('np-k-photo')) {
      var bottom = 0;
      pics.forEach(function (p) { bottom = Math.max(bottom, p.y + p.h); });
      var cap = box.querySelector('.np-caption'), cr = box.querySelector('.np-credit');
      cap.style.top = (bottom + 4) + 'px';
      cr.style.top = (bottom + 4 + cap.offsetHeight) + 'px';
      return;
    }
    if (box.classList.contains('np-k-quote')) {
      var q = box.querySelector('.np-quote'), size = 18;
      while (size > 9 && q.scrollHeight > H) { size -= 1; q.style.fontSize = size + 'px'; }
      return;
    }
    var head = box.querySelector(':scope > .np-head, :scope > .np-brief-head');
    if (head) obst.push(head.classList.contains('np-head') ? placeHead(box, head, W, H, pics) : { x: 0, y: 0, w: W, h: head.offsetHeight + 4 });
    var list = box.classList.contains('np-k-jump') ? (jumps[box.dataset.jump] || []) : textItems(box.querySelector('.np-src'));
    if (!list.length) return;
    if (!box.classList.contains('np-k-jump') && list[0].tagName === 'P') list[0].classList.add('np-first');
    var segs = segments(W, H, cols, obst);
    var see = box.querySelector('.np-see');
    if (see && segs.length) segs[segs.length - 1].h -= LH;
    var r = pour(box, list, segs);
    if (see) {
      if (r.rest.length && r.last) {
        var g = segs[segs.length - 1];
        see.style.cssText = 'left:' + g.x + 'px;top:' + (g.y + g.h) + 'px;width:' + g.w + 'px';
        jumps[box.dataset.jump] = r.rest;
      } else {
        see.remove();
      }
    } else if (r.rest.length) {
      endOnSentence(r.last);
    }
  }
  function rules(page) {
    $$('.np-box', page).forEach(function (b) {
      var x = b.offsetLeft;
      if (x < 60) return;
      var r = document.createElement('i');
      r.className = 'np-rule';
      r.style.cssText = 'left:' + (x - 7.5) + 'px;top:' + b.offsetTop + 'px;height:' + b.offsetHeight + 'px';
      page.appendChild(r);
    });
  }
  // One box that cannot be set must not stop the others.
  function safeLay(b) {
    try { layBox(b); } catch (e) { (window.EPerrors = window.EPerrors || []).push(String(e && e.stack || e)); }
  }
  function setPages() {
    // A page out of view has no size to measure: all are shown while the
    // text is set, then the view is drawn again.
    pages.forEach(function (p) { p.parentNode.style.display = 'block'; });
    pages.forEach(function (p) {
      rules(p);
      $$('.np-box:not(.np-k-jump)', p).forEach(safeLay);
    });
    pages.forEach(function (p) { $$('.np-box.np-k-jump', p).forEach(safeLay); });
    // A jumped box whose story fitted where it began stays empty: hide it.
    $$('.np-box.np-k-jump').forEach(function (b) { if (!b.querySelector('.np-col')) b.classList.add('np-empty'); });
  }

  // The page list, by supplement: "1 FRONT", "2 NEWS" …, "1 BIZFRONT" …
  var side = $('[data-side]');
  function thumbs() {
    pages.forEach(function (p) {
      var list = side.querySelector('[data-thumbs="' + (p.dataset.group || '') + '"]');
      if (!list) return;
      var d = dims(p);
      var b = document.createElement('button');
      b.type = 'button';
      b.className = 'ep-thumb' + (+p.dataset.page === state.page ? ' on' : '');
      b.dataset.page = p.dataset.page;
      b.title = p.dataset.gno + ': ' + p.dataset.name;
      var box = document.createElement('div');
      box.className = 'ep-thumb-box';
      var copy = p.cloneNode(true);
      copy.removeAttribute('data-page');
      var k = Math.min(86 / d.w, 130 / d.h);
      copy.style.transform = 'scale(' + k + ')';
      box.style.width = Math.round(d.w * k) + 'px';
      box.style.height = Math.round(d.h * k) + 'px';
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
  }
  $$('[data-fold]').forEach(function (b) {
    b.addEventListener('click', function () { b.closest('.ep-group').classList.toggle('open'); });
  });

  function storiesOn(n) {
    var seen = {}, out = [];
    $$('[data-story]', pages[n - 1]).forEach(function (s) {
      var id = s.dataset.story;
      if (id && !seen[id]) { seen[id] = true; out.push(id); }
    });
    return out;
  }
  function tpl(id) { return document.getElementById('st-' + id); }
  function pageEl(n) { return pages[n - 1]; }

  // Fit the page to the screen: the whole page in view, the story beside it.
  function draw() {
    var shown = state.all ? pages.map(function (p) { return +p.dataset.page; }) : [state.page];
    var availH = stage.clientHeight - 28;
    var cur = dims(pageEl(state.page)), W = cur.w;
    var scale;
    stage.style.width = '';
    // The page fits the width of its column and the window scrolls down it,
    // as on the Star's e-paper: beside the story the column is 500 wide.
    if (state.all) {
      scale = 0.24;
    } else if (!wide()) {
      scale = (stage.clientWidth - 64) / W;
    } else if (state.view === 2) {
      var col = Math.round(Math.max(320, Math.min(500, (body.clientWidth - side.offsetWidth) * 0.4)));
      scale = col / W;
      stage.style.width = col + 'px';
    } else {
      scale = Math.min(1, (stage.clientWidth - 80) / W);
    }
    pages.forEach(function (p) {
      var show = shown.indexOf(+p.dataset.page) >= 0, d = dims(p);
      // Among all pages a tabloid supplement shows at its own smaller size.
      var k = scale;
      p.parentNode.style.display = show ? 'block' : 'none';
      p.style.transform = 'scale(' + k + ')';
      p.parentNode.style.width = (d.w * k) + 'px';
      p.parentNode.style.height = (d.h * k) + 'px';
    });
    $$('.ep-thumb', side).forEach(function (t) { t.classList.toggle('on', +t.dataset.page === state.page); });
    var sel = $('[data-page-select]');
    if (sel) sel.value = String(state.page);
    $('[data-prev]').hidden = state.all || state.page <= 1;
    $('[data-next]').hidden = state.all || state.page >= pages.length;
    // The arrows hold their height in the window; across, they sit at the
    // edges of the page's column and of the story beside it.
    var sr = stage.getBoundingClientRect(), cr = $('[data-clip]').getBoundingClientRect();
    $('[data-prev]').style.left = sr.left + 'px';
    $('[data-next]').style.left = (sr.right - 22) + 'px';
    $('[data-clip-prev]').style.left = (cr.left + 4) + 'px';
    $('[data-clip-next]').style.left = (cr.right - 40) + 'px';
  }
  // A page list keeps its page in view by scrolling itself, not the window.
  function showThumb(t) {
    var list = t && t.parentNode;
    if (!list) return;
    if (list.scrollWidth > list.clientWidth + 2) {
      if (t.offsetLeft < list.scrollLeft || t.offsetLeft + t.offsetWidth > list.scrollLeft + list.clientWidth) list.scrollLeft = t.offsetLeft - 8;
    } else if (t.offsetTop < list.scrollTop || t.offsetTop + t.offsetHeight > list.scrollTop + list.clientHeight) {
      list.scrollTop = t.offsetTop - 10;
    }
  }
  // The window comes up to the top of the page (or the story) when it has
  // scrolled past it.
  function bringUp(el) {
    var top = el.getBoundingClientRect().top + window.pageYOffset - 70;
    if (window.pageYOffset > top) window.scrollTo(0, Math.max(0, top));
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
    bringUp(stage);
    showThumb(side.querySelector('.ep-thumb.on'));
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
    $$('.sel', stage).forEach(function (s) { s.classList.remove('sel'); });
    if (!id) { clipBody.innerHTML = ''; return; }
    $$('[data-story="' + id + '"]', stage).forEach(function (s) { s.classList.add('sel'); });
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
    if (state.view === 2 && wide()) { select(s.dataset.story); remember(); bringUp($('[data-clip]')); }
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
  // The text is set once the paper's faces have loaded; then the page list.
  var fontsIn = document.fonts && document.fonts.ready ? document.fonts.ready : Promise.resolve();
  Promise.race([fontsIn, new Promise(function (r) { setTimeout(r, 3000); })]).then(function () {
    var t0 = Date.now();
    setPages();
    EP.setMs = Date.now() - t0;
    draw();
    thumbs();
    showThumb(side.querySelector('.ep-thumb.on'));
    body.classList.add('ep-set');
  });
  // A shared story link opens on its page with that story.
  var start = EP.aid && tpl(EP.aid);
  if (start) go(+start.dataset.page, EP.aid, true);
  else go(state.page);
  if (window.location.hash === '#popular') {
    history.replaceState(null, '', window.location.pathname + window.location.search);
    $('[data-popular]').click();
  }
})();
