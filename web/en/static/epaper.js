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
    $$('.np-bitem', src).forEach(function (b) { out.push(b.cloneNode(true)); });
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
  function segments(W, H, cols, obst, lh) {
    lh = lh || LH;
    var cw = (W - GUT * (cols - 1)) / cols, segs = [];
    for (var c = 0; c < cols; c++) {
      var col = { x: c * (cw + GUT), w: cw };
      spans(col, H, obst).forEach(function (s) {
        var h = Math.floor((s[1] - s[0]) / lh) * lh;
        if (h >= lh * 2 - 0.1) segs.push({ x: col.x, y: s[0], w: cw, h: h });
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
    if (box.dataset.hs) { size = +box.dataset.hs; maxLines += 2; } // set bigger by fillBox
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
      // The photos take the whole box above their caption, so a box grown
      // over the paper's advertisements is not left with empty room.
      var right = 0, bottom = 0;
      pics.forEach(function (p) { right = Math.max(right, p.x + p.w); bottom = Math.max(bottom, p.y + p.h); });
      var cap = box.querySelector('.np-caption'), cr = box.querySelector('.np-credit');
      var room = H - cap.offsetHeight - cr.offsetHeight - 8;
      // (Not when smaller boxes are set over this one: they keep their place.)
      if (right > 0 && bottom > 0 && room > 40 && obst.length === pics.length) {
        var one = pics.length === 1, sx = one ? 1 : W / right, sy = one ? 1 : room / bottom;
        $$(':scope > .np-pic', box).forEach(function (el, i) {
          var p = one ? { x: 0, y: 0, w: W, h: room } : { x: pics[i].x * sx, y: pics[i].y * sy, w: pics[i].w * sx, h: pics[i].h * sy };
          el.style.left = p.x + 'px'; el.style.width = p.w + 'px';
          el.style.top = p.y + 'px'; el.style.height = p.h + 'px';
        });
        bottom = room;
      }
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
    var list = box.classList.contains('np-k-jump') ? (jumps[box.dataset.jump] || []).map(function (n) { return n.cloneNode(true); }) : textItems(box.querySelector('.np-src'));
    if (!list.length) return;
    // Stories set under this one (fillBox): each under its own headline.
    // In the IN BRIEF column each is a brief: its headline and first lines.
    // Elsewhere a story set under another has its headline, its photo (the
    // column's width) and its text.
    var brief = box.classList.contains('np-k-brief');
    (box._stack || []).forEach(function (t) {
      var h = t.content.querySelector('.np-stackhead'), text = textItems(t.content.querySelector('.np-src'));
      if (brief) {
        var item = document.createElement('div');
        item.className = 'np-bitem';
        item.dataset.story = t.dataset.story;
        var h4 = document.createElement('h4');
        h4.textContent = h ? h.textContent.trim() : '';
        var p = document.createElement('p');
        var first = text.length ? text[0].textContent : '';
        p.textContent = first.length > 320 ? first.slice(0, first.lastIndexOf(' ', 320)) + ' …' : first;
        item.appendChild(h4);
        item.appendChild(p);
        list.push(item);
        return;
      }
      if (h) list.push(h.cloneNode(true));
      if (t.dataset.img) {
        var fig = document.createElement('figure');
        fig.className = 'np-stackfig';
        var img = document.createElement('img');
        img.alt = '';
        img.src = t.dataset.img;
        img.onerror = function () { fig.remove(); };
        fig.appendChild(img);
        list.push(fig);
      }
      list = list.concat(text);
    });
    if (!box.classList.contains('np-k-jump') && list[0].tagName === 'P') list[0].classList.add('np-first');
    var lh = LH * (+box.dataset.ts || 1);
    var segs = segments(W, H, cols, obst, lh);
    var see = box.querySelector('.np-see');
    if (see && segs.length) segs[segs.length - 1].h -= lh;
    var r = pour(box, list, segs);
    // How much of the room for text the text takes, for fillBox.
    var room = 0, used = 0;
    segs.forEach(function (g) { room += g.w * g.h; });
    $$(':scope > .np-col', box).forEach(function (c) {
      var last = c.lastElementChild;
      used += c.offsetWidth * Math.min(c.offsetHeight, last ? last.offsetTop + last.offsetHeight : 0);
    });
    box._fill = { rest: r.rest.length, room: room, used: used };
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
    try { layBox(b); fillBox(b); } catch (e) { (window.EPerrors = window.EPerrors || []).push(String(e && e.stack || e)); }
  }
  function relay(box) {
    $$(':scope > .np-col', box).forEach(function (c) { c.remove(); });
    delete box.dataset.laid;
    layBox(box);
  }
  function emptyShare(box) {
    var f = box._fill;
    return !f || f.rest || !f.room ? 0 : 1 - f.used / f.room;
  }
  function fits(box) { return box._fill && !box._fill.rest; }
  // The next story to set under a box's own: a box kept for a story's rest
  // takes its own story first, then the page's spares.
  function nextSpare(box, anyPage) {
    var alt = box.querySelector(':scope > template.np-alt');
    if (alt && !alt.dataset.used) return alt;
    var pg = box.closest('.np-page');
    var from = anyPage ? $$('.np-page').filter(function (p) { return p.dataset.group === pg.dataset.group; }) : [pg];
    for (var i = 0; i < from.length; i++) {
      var t = $$('template.np-spare', from[i]).filter(function (t) { return !t.dataset.used; })[0];
      if (t) return t;
    }
    return null;
  }
  // No room in a box is left empty. A story shorter than its box first gets
  // its photo (at the top of the box, as the paper sets one), and the photo
  // grows, no deeper than it is wide, until the text just reaches the foot of
  // the box; while a fifth of the box is still empty, another story of the
  // page is set under it, under its own headline, as the paper runs two
  // stories down a column; what is left is taken up by larger type, up to
  // 1.45 times, the most that still fits (the IN BRIEF column too).
  function fillBox(box, anyPage) {
    var story = box.classList.contains('np-k-story'), jump = box.classList.contains('np-k-jump');
    if (!(story || jump || box.classList.contains('np-k-brief')) || !fits(box) || emptyShare(box) < 0.04) return;
    if (anyPage && box.dataset.ts) { // set again from plain type
      delete box.dataset.ts;
      box.style.removeProperty('--ts');
      relay(box);
    }
    var W = box.offsetWidth, H = box.offsetHeight;
    var pics = $$(':scope > .np-pic', box);
    var added = null;
    if (story && !pics.length && box.dataset.img) {
      added = document.createElement('figure');
      added.className = 'np-pic';
      var img = document.createElement('img');
      img.alt = '';
      img.src = box.dataset.img;
      img.onerror = function () { added.remove(); relay(box); };
      var cap = document.createElement('figcaption');
      cap.textContent = 'PHOTO: COLLECTED';
      added.appendChild(img);
      added.appendChild(cap);
      added.style.cssText = 'left:0;top:0;width:' + W + 'px;height:0';
      box.insertBefore(added, box.firstChild);
      pics = [added];
    }
    if (pics.length) {
      var pic = pics.reduce(function (a, b) { return a.offsetWidth * a.offsetHeight >= b.offsetWidth * b.offsetHeight ? a : b; });
      var lo = pic.offsetHeight, hi = Math.min(H * 0.75, pic.offsetWidth + 10);
      if (hi > lo + 4) {
        pic.style.height = hi + 'px';
        relay(box);
        if (!fits(box)) {
          for (var i = 0; i < 7; i++) {
            var mid = (lo + hi) / 2;
            pic.style.height = mid + 'px';
            relay(box);
            if (fits(box)) lo = mid; else hi = mid;
          }
          pic.style.height = lo + 'px';
          relay(box);
        }
      }
      if (added && pic.offsetHeight < 50) { added.remove(); relay(box); }
      if (emptyShare(box) < 0.04) return;
    }
    if (story || jump || box.classList.contains('np-k-brief')) {
      for (var n = 0; n < 10 && fits(box) && emptyShare(box) >= (story || jump ? 0.2 : 0.12); n++) {
        var t = nextSpare(box, anyPage);
        if (!t) break;
        t.dataset.used = '1';
        box._stack = (box._stack || []).concat([t]);
        relay(box);
      }
      if (!fits(box)) { endOnSentence($$(':scope > .np-col', box).pop()); return; }
      if (emptyShare(box) < 0.04) return;
    }
    var a = 1, b = 1.45;
    box.dataset.ts = b;
    box.style.setProperty('--ts', b);
    relay(box);
    if (!fits(box)) {
      for (var k = 0; k < 6; k++) {
        var m = (a + b) / 2;
        box.dataset.ts = m;
        box.style.setProperty('--ts', m);
        relay(box);
        if (fits(box)) a = m; else b = m;
      }
      box.dataset.ts = a;
      box.style.setProperty('--ts', a);
      relay(box);
    }
    // Last, a story still short of its box takes a bigger headline, as the
    // paper gives a short story a big head.
    var h3 = story && box.querySelector(':scope > .np-head h3');
    if (h3 && fits(box) && emptyShare(box) >= 0.1) {
      var hs = parseFloat(getComputedStyle(h3).fontSize), top = Math.min(hs * 2, 64);
      while (hs + 2 <= top) {
        box.dataset.hs = hs + 2;
        relay(box);
        if (!fits(box)) { box.dataset.hs = hs; relay(box); break; }
        hs += 2;
        if (emptyShare(box) < 0.06) break;
      }
    }
  }
  // A box kept for a story's rest that none came to (it fitted where it
  // began) sets its own story instead.
  function useAlt(box) {
    var alt = box.querySelector(':scope > template.np-alt');
    if (!alt) { box.classList.add('np-empty'); return; }
    $$(':scope > *', box).forEach(function (n) { if (n !== alt) n.remove(); });
    var c = alt.content.cloneNode(true);
    $$('.np-stackhead', c).forEach(function (n) { n.remove(); });
    box.appendChild(c);
    alt.dataset.used = '1';
    box.classList.remove('np-k-jump');
    box.classList.add('np-k-story');
    box.dataset.story = alt.dataset.story;
    if (alt.dataset.img) box.dataset.img = alt.dataset.img;
    delete box.dataset.jump;
    relay(box);
    fillBox(box);
  }
  function setPages() {
    // A page out of view has no size to measure: all are shown while the
    // text is set, then the view is drawn again.
    pages.forEach(function (p) { p.parentNode.style.display = 'block'; });
    pages.forEach(function (p) {
      // A smaller box set over a bigger one shows above it.
      $$('.np-box', p).forEach(function (b) { b.style.zIndex = Math.max(1, 2000 - Math.round(b.offsetWidth * b.offsetHeight / 500)); });
      rules(p);
      $$('.np-box:not(.np-k-jump)', p).forEach(safeLay);
    });
    pages.forEach(function (p) { $$('.np-box.np-k-jump', p).forEach(safeLay); });
    $$('.np-box.np-k-jump').forEach(function (b) {
      if (!b.querySelector('.np-col')) {
        try { useAlt(b); } catch (e) { b.classList.add('np-empty'); }
      }
    });
    // A box a fifth empty still takes a spare of another page of its edition.
    $$('.np-box').forEach(function (b) {
      if (fits(b) && emptyShare(b) >= 0.2) {
        try { fillBox(b, true); } catch (e) { (window.EPerrors = window.EPerrors || []).push(String(e && e.stack || e)); }
      }
    });
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
    // A teaser turns to its page.
    var t = e.target.closest('[data-goto-page]');
    if (t) { e.preventDefault(); go(+t.dataset.gotoPage); return; }
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

  // InDesign: the page as drawn here (every frame's place and size, its text
  // and type, the photos and rules, in page pixels) goes to the InDesign
  // bridge on this computer (cmd/indd-bridge, 127.0.0.1:8095), which has
  // Adobe InDesign build the page and sends the .indd back.
  var BRIDGE = 'http://127.0.0.1:8095';
  function hexOf(c) {
    var m = /rgba?\(([\d.]+),\s*([\d.]+),\s*([\d.]+)(?:,\s*([\d.]+))?\)/.exec(c || '');
    if (!m || (m[4] !== undefined && +m[4] < 0.05)) return '';
    return '#' + [m[1], m[2], m[3]].map(function (v) { return ('0' + Math.round(+v).toString(16)).slice(-2); }).join('');
  }
  function paraOf(cs, t) {
    var size = parseFloat(cs.fontSize);
    var lead = cs.lineHeight === 'normal' ? size * 1.2 : parseFloat(cs.lineHeight);
    var al = cs.textAlign;
    al = al === 'justify' ? 'justify' : /center/.test(al) ? 'center' : (al === 'right' || al === 'end') ? 'right' : 'left';
    var ls = parseFloat(cs.letterSpacing);
    return {
      t: t, font: cs.fontFamily.split(',')[0].replace(/["']/g, '').trim(), weight: parseInt(cs.fontWeight, 10) || 400,
      italic: cs.fontStyle === 'italic', size: size, lead: lead, align: al, color: hexOf(cs.color) || '#111111',
      indent: parseFloat(cs.textIndent) || 0, track: isNaN(ls) ? 0 : Math.round(ls / size * 1000)
    };
  }
  // The part of an element's box left showing by the boxes around it that
  // hide what overflows them.
  function shown(el, pg, r) {
    r = { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
    for (var a = el; a; a = a.parentElement) {
      if (a === pg || getComputedStyle(a).overflow !== 'visible') {
        var c = a.getBoundingClientRect();
        r.left = Math.max(r.left, c.left); r.top = Math.max(r.top, c.top);
        r.right = Math.min(r.right, c.right); r.bottom = Math.min(r.bottom, c.bottom);
      }
      if (a === pg) break;
    }
    return r.right - r.left > 0.5 && r.bottom - r.top > 0.5 ? r : null;
  }
  // Where an element's words are (not its pictures).
  function wordsBox(el) {
    var tw = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, null), n, rg = document.createRange();
    var l = Infinity, t = Infinity, r = -Infinity, b = -Infinity;
    while ((n = tw.nextNode())) {
      if (!n.nodeValue.trim()) continue;
      rg.selectNodeContents(n);
      var rs = rg.getClientRects();
      for (var i = 0; i < rs.length; i++) {
        if (!rs[i].width) continue;
        l = Math.min(l, rs[i].left); t = Math.min(t, rs[i].top); r = Math.max(r, rs[i].right); b = Math.max(b, rs[i].bottom);
      }
    }
    return l < r ? { left: l, top: t, right: r, bottom: b } : null;
  }
  function drawing(im, r, filter) {
    var c = document.createElement('canvas');
    c.width = Math.max(1, Math.min(4000, Math.round(r.width * 4)));
    c.height = Math.max(1, Math.min(4000, Math.round(r.height * 4)));
    var ctx = c.getContext('2d');
    ctx.drawImage(im, 0, 0, c.width, c.height);
    if (/brightness\(0\)/.test(filter)) { // the nameplate prints black
      ctx.globalCompositeOperation = 'source-in';
      ctx.fillStyle = '#000';
      ctx.fillRect(0, 0, c.width, c.height);
    }
    return c.toDataURL('image/png');
  }
  var TEXT = '.np-topline, .np-rtk, .np-edition, .np-dateline > span, .np-teaser b, .np-teaser span, .np-biz-name, .np-biz-date, .np-biz-no, ' +
    '.np-bt-head, .np-bt-img b, .np-sport-date, .np-sport-name, .np-tab-no, .np-tab-name, .np-tab-date, .np-folio-name, .np-folio-date, ' +
    '.np-folio-no, .np-edmast span, .np-brief-head span, .np-kicker, .np-head h3, .np-deck, .np-byline, .np-from, .np-see, ' +
    '.np-pic figcaption, .np-caption, .np-credit, .np-quote, .np-imprint > span';
  function pageLayout(pg) {
    var dm = dims(pg), pr = pg.getBoundingClientRect(), k = dm.w / pr.width;
    var box = function (r) { return { x: (r.left - pr.left) * k, y: (r.top - pr.top) * k, w: (r.right - r.left) * k, h: (r.bottom - r.top) * k }; };
    var fills = [], pics = [], rules = [], texts = [];
    var sel = $$('.np-box.sel, .np-bitem.sel', pg);
    sel.forEach(function (s) { s.classList.remove('sel'); });
    if (document.activeElement && pg.contains(document.activeElement)) document.activeElement.blur();
    // What a smaller box set over a bigger one hides is left out.
    var zs = $$('.np-box', pg).map(function (bx) { return { el: bx, z: +bx.style.zIndex || 0, r: box(bx.getBoundingClientRect()) }; });
    var hidden = function (el, it) {
      var own = el.closest('.np-box'), z = own ? (+own.style.zIndex || 0) : -1, cx = it.x + it.w / 2, cy = it.y + it.h / 2;
      return zs.some(function (o) { return o.el !== own && o.z > z && cx > o.r.x && cx < o.r.x + o.r.w && cy > o.r.y && cy < o.r.y + o.r.h; });
    };
    var rule = function (x, y, x2, y2, w, colour, dbl) {
      rules.push({ k: 'line', x: x, y: y, x2: x2, y2: y2, sw: w, color: colour || '#111111', double: !!dbl });
    };
    $$('*', pg).forEach(function (el) {
      var cs = getComputedStyle(el);
      if (cs.display === 'none' || cs.visibility === 'hidden' || el.tagName === 'IMG') return;
      var r = shown(el, pg, el.getBoundingClientRect());
      if (!r) return;
      var b = box(r);
      var bg = hexOf(cs.backgroundColor);
      if (bg && bg !== '#ffffff' && !el.classList.contains('np-box')) fills.push({ k: 'rect', x: b.x, y: b.y, w: b.w, h: b.h, fill: bg });
      [['Top', 0, 0, 1, 0], ['Bottom', 0, 1, 1, 1], ['Left', 0, 0, 0, 1], ['Right', 1, 0, 1, 1]].forEach(function (s) {
        var w = parseFloat(cs['border' + s[0] + 'Width']), st = cs['border' + s[0] + 'Style'];
        if (!(w > 0) || st === 'none' || st === 'hidden') return;
        var dx = s[0] === 'Left' ? w / 2 : s[0] === 'Right' ? -w / 2 : 0, dy = s[0] === 'Top' ? w / 2 : s[0] === 'Bottom' ? -w / 2 : 0;
        rule(b.x + s[1] * b.w + dx, b.y + s[2] * b.h + dy, b.x + s[3] * b.w + dx, b.y + s[4] * b.h + dy, w, hexOf(cs['border' + s[0] + 'Color']), st === 'double');
      });
      // Rules drawn by ::before and ::after (the IN BRIEF ribbon's line).
      ['::before', '::after'].forEach(function (ps) {
        var p = getComputedStyle(el, ps);
        if (!p || p.content === 'none' || p.content === 'normal' || p.position !== 'absolute') return;
        var left = parseFloat(p.left), width = parseFloat(p.width), top = parseFloat(p.top), bottom = parseFloat(p.bottom);
        var bt = parseFloat(p.borderTopWidth) || 0, hgt = (parseFloat(p.height) || 0) + bt;
        var x = b.x + (isNaN(left) ? b.w - parseFloat(p.right) - width : left);
        var y = isNaN(top) ? b.y + b.h - (bottom || 0) - hgt : b.y + top;
        if (bt > 0) rule(x, y + bt / 2, x + width, y + bt / 2, bt, hexOf(p.borderTopColor), p.borderTopStyle === 'double');
        else if (hexOf(p.backgroundColor) && hgt <= 2) rule(x, y + hgt / 2, x + width, y + hgt / 2, Math.max(hgt, 1), hexOf(p.backgroundColor));
      });
    });
    $$('img', pg).forEach(function (im) {
      var r0 = im.getBoundingClientRect(), cs = getComputedStyle(im);
      if (!r0.width || !r0.height || cs.display === 'none') return;
      var r = shown(im, pg, r0);
      if (!r) return;
      var b = box(r), it = { k: 'img', x: b.x, y: b.y, w: b.w, h: b.h };
      var src = im.currentSrc || im.src;
      if (hidden(im, it)) return;
      if (/\.svg([?#]|$)/i.test(src)) {
        try { it.data = drawing(im, { width: r0.width * k, height: r0.height * k }, cs.filter); } catch (e) { return; }
      } else {
        it.src = new URL(src, location.href).href;
      }
      pics.push(it);
    });
    $$(TEXT, pg).forEach(function (el) {
      var t = el.innerText.replace(/\s+$/, '').replace(/^\s+/, '');
      var wb = t && wordsBox(el);
      var r = wb && shown(el, pg, wb);
      if (!r) return;
      var b = box(r), cs = getComputedStyle(el);
      var m = /matrix\(([-\d.]+)/.exec(cs.transform);
      var pa = paraOf(cs, t);
      // A frame grows to hold its text in another face, except where the
      // e-paper itself cuts the text short (a clamped intro or teaser);
      // text that never wraps here grows sideways too.
      var cut = el.matches('.np-intro') || (cs.webkitLineClamp && cs.webkitLineClamp !== 'none');
      var it = { k: 'text', x: b.x - 1, y: b.y, w: b.w + 3, h: b.h + 2, fit: !cut, paras: [pa] };
      it.one = !cut && Math.round(b.h / pa.lead) <= t.split('\n').length;
      if (m) it.hscale = +m[1] * 100;
      if (!hidden(el, it)) texts.push(it);
    });
    // The text, column by column as it is set here, its paragraphs (a brief's
    // headline and text) in their own type. A column holding more than one
    // story (a story set under another, with its headline and photo) gives
    // a frame to each run of text between them, where the browser set it,
    // so InDesign's lines keep clear of the photo and the rule.
    $$('.np-col', pg).forEach(function (el) {
      var r = shown(el, pg, el.getBoundingClientRect());
      if (!r) return;
      var cb = box(r), runs = [], cur = null;
      var flush = function () { if (cur && cur.paras.length) runs.push(cur); cur = null; };
      $$(':scope > *', el).forEach(function (n) {
        if (n.tagName === 'FIGURE') { flush(); return; } // a photo: its own frame (above)
        var head = n.classList.contains('np-stackhead');
        if (head) flush();
        var nr = n.getBoundingClientRect();
        if (!cur) cur = { top: nr.top, bottom: nr.bottom, paras: [] };
        cur.bottom = Math.max(cur.bottom, nr.bottom);
        var parts = n.classList.contains('np-bitem') || head ? $$(':scope > *', n) : [n];
        if (head && !parts.length) parts = [n];
        parts.forEach(function (q) {
          var t = q.innerText.replace(/\s+/g, ' ').trim();
          if (t) cur.paras.push(paraOf(getComputedStyle(q), t));
        });
        if (head) flush();
      });
      flush();
      var yOf = function (clientY) { return (clientY - pr.top) * k; };
      runs.forEach(function (run, i) {
        // Each run starts where the browser set it (under a photo at the
        // column's top too); the last ends at the column's foot.
        var top = Math.max(cb.y, yOf(run.top));
        var bottom = i === runs.length - 1 ? cb.y + cb.h : Math.min(cb.y + cb.h, yOf(run.bottom));
        var it = { k: 'text', x: cb.x, y: top, w: cb.w + 1, h: Math.max(4, bottom - top) + 1, cols: 1, paras: run.paras };
        if (!hidden(el, it)) texts.push(it);
      });
    });
    sel.forEach(function (s) { s.classList.add('sel'); });
    return {
      name: 'DailyStar_' + (EP.date || '') + '_' + (pg.dataset.group ? pg.dataset.group + '_' : '') + 'P' + pg.dataset.gno + '_' + (pg.dataset.name || '').replace(/&/g, 'and'),
      width: dm.w, height: dm.h,
      margins: dm.w < 900 ? [24, 24, 24, 24] : [30, 36, 36, 36],
      items: fills.concat(pics, rules, texts)
    };
  }
  function inddHelp() {
    var box = document.createElement('div');
    box.className = 'ep-help';
    box.innerHTML = '<h2>Download as an InDesign file (.indd)</h2>' +
      '<p>An InDesign file can only be made by Adobe InDesign itself. To download e-paper pages as <code>.indd</code> on this computer:</p>' +
      '<ol><li>Adobe InDesign must be installed here.</li>' +
      '<li>Start The Daily Star InDesign bridge, <code>indd-bridge.exe</code>, and keep it running.</li>' +
      '<li>Press the <b>Id</b> button again.</li></ol>' +
      '<p>Ask IT to set it up if it is not on your computer.</p>';
    popBody.innerHTML = '';
    popBody.appendChild(box);
    pop.hidden = false;
  }
  // InDesign takes about ten seconds to open: the bridge is asked to open it
  // as soon as the pointer comes to the Id button (and when the page opens,
  // on a computer where the button has been used), so it is ready by the
  // click. inddState is what the bridge last said: "ready" or "starting".
  // The Id button is only on the copy opened on the computer it runs on (the
  // hosted site leaves it out); without it the page never calls the bridge.
  var inddState = '', warmed = 0;
  function warmInDesign() {
    if (Date.now() - warmed < 20000) return;
    warmed = Date.now();
    fetch(BRIDGE + '/warm').then(function (r) { return r.json(); })
      .then(function (j) { inddState = j.indesign || ''; }).catch(function () {});
  }
  var inddBtn = $('[data-indd]');
  on(inddBtn, 'mouseenter', warmInDesign);
  on(inddBtn, 'focus', warmInDesign);
  if (inddBtn) {
    try { if (localStorage.getItem('ep-indd') === '1') warmInDesign(); } catch (e) {}
  }
  // While the page is made, the note says what is happening and for how long.
  function progress(msg) {
    var t = $('[data-toast]'), t0 = Date.now();
    if (!t) return function () {};
    clearTimeout(toast.t);
    var tick = function () { t.textContent = msg() + ' ' + Math.round((Date.now() - t0) / 1000) + ' s'; };
    tick();
    t.classList.add('show');
    var iv = setInterval(tick, 1000);
    return function () { clearInterval(iv); t.classList.remove('show'); };
  }
  on(inddBtn, 'click', function () {
    var btn = inddBtn;
    if (btn.classList.contains('busy')) return;
    var lay;
    try { lay = pageLayout(pages[state.page - 1]); } catch (e) { toast('This page could not be read'); return; }
    btn.classList.add('busy');
    var t0 = Date.now();
    var stop = progress(function () {
      // Opening InDesign is the long part; the bridge's answer to the warm-up
      // says whether it was open.
      return inddState === 'starting' && Date.now() - t0 < 40000 ? 'Opening InDesign, then making this page …' : 'InDesign is making this page …';
    });
    fetch(BRIDGE + '/indd', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(lay) })
      .then(function (r) {
        if (!r.ok) return r.text().then(function (t) { throw new Error(t || ('error ' + r.status)); });
        return r.blob();
      })
      .then(function (blob) {
        var a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = lay.name + '.indd';
        document.body.appendChild(a);
        a.click();
        a.remove();
        setTimeout(function () { URL.revokeObjectURL(a.href); }, 60000);
        inddState = 'ready';
        try { localStorage.setItem('ep-indd', '1'); } catch (e) {}
        stop();
        toast('Downloaded ' + lay.name + '.indd (' + Math.round((Date.now() - t0) / 1000) + ' s)');
      })
      .catch(function (e) {
        stop();
        if (e instanceof TypeError) inddHelp(); // the bridge is not running here
        else toast('InDesign: ' + e.message);
      })
      .then(function () { btn.classList.remove('busy'); });
  });
  window.EPlayout = function (n) { return pageLayout(pages[(n || state.page) - 1]); };

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
