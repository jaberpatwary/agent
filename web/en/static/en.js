/* The English portal's page behaviour, as on www.thedailystar.net: sticky
   menu, mega menu and search, sub-menus on phones, "Load More", share and
   the photo viewer. */

/* A photo that fails to load leaves the theme's grey box. */
function dsImgFail(img) {
  var box = img.closest('.card-image, .featured-image, .img');
  if (box) box.classList.add('no-img');
  img.remove();
}

(function () {
  var body = document.body;

  /* Sticky menu bar. */
  var nav = document.getElementById('site-nav-header');
  var navTop = nav ? nav.getBoundingClientRect().top + window.scrollY : 0;
  window.addEventListener('scroll', function () { body.classList.toggle('sticky_nav', !!nav && window.scrollY >= navTop); }, { passive: true });

  /* Mega menu and search. */
  var searchBtns = document.querySelectorAll('.search-icon');
  var menuBtns = document.querySelectorAll('.hamburger-icon');
  function setAll(list, on) { list.forEach(function (b) { b.classList.toggle('active', on); }); }
  menuBtns.forEach(function (b) {
    b.addEventListener('click', function (e) {
      e.preventDefault();
      var open = !body.classList.contains('main-nav-open');
      body.classList.toggle('main-nav-open', open);
      setAll(menuBtns, open);
      body.classList.remove('search-open');
      setAll(searchBtns, false);
    });
  });
  searchBtns.forEach(function (b) {
    b.addEventListener('click', function (e) {
      e.preventDefault();
      var open = !body.classList.contains('search-open');
      body.classList.toggle('search-open', open);
      setAll(searchBtns, open);
      body.classList.remove('main-nav-open');
      setAll(menuBtns, false);
      if (open) {
        var input = Array.prototype.find.call(document.querySelectorAll('.search-form input'), function (i) { return i.offsetParent !== null; });
        if (input) input.focus();
      }
    });
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') { body.classList.remove('search-open', 'main-nav-open'); setAll(searchBtns, false); setAll(menuBtns, false); }
  });

  /* Sub-menus open with their arrow on phones. */
  document.querySelectorAll('.sub_nav_show').forEach(function (arrow) {
    arrow.addEventListener('click', function () { arrow.parentNode.classList.toggle('active'); });
  });

  /* "Load More" brings the next page's stories into the list. */
  document.querySelectorAll('[data-pager]').forEach(function (pager) {
    pager.addEventListener('click', function (e) {
      var a = e.target.closest('[data-next]');
      if (!a) return;
      e.preventDefault();
      a.textContent = 'Loading…';
      fetch(a.href).then(function (r) { return r.text(); }).then(function (html) {
        var doc = new DOMParser().parseFromString(html, 'text/html');
        var list = pager.parentNode.querySelector('[data-listing]');
        doc.querySelectorAll('[data-listing] > [data-item]').forEach(function (item) { list.appendChild(item); });
        var next = doc.querySelector('[data-pager] [data-next]');
        if (next) { a.href = next.getAttribute('href'); a.textContent = 'Load More'; } else { a.parentNode.remove(); }
      }).catch(function () { window.location = a.href; });
    });
  });

  /* Share: the menu opens on click; links point at this page. */
  var link = window.location.href.split('#')[0];
  document.querySelectorAll('.share-dropdown').forEach(function (dd) {
    var btn = dd.querySelector('button, .share-btn');
    var menu = dd.querySelector('.share-menu');
    if (btn && menu) btn.addEventListener('click', function (e) { e.preventDefault(); menu.classList.toggle('hidden'); });
    dd.querySelectorAll('[data-share-to]').forEach(function (a) {
      var u = encodeURIComponent(link), t = encodeURIComponent(document.title);
      a.href = { x: 'https://x.com/intent/tweet?text=' + t + '&url=' + u, facebook: 'https://www.facebook.com/sharer/sharer.php?u=' + u, linkedin: 'https://www.linkedin.com/shareArticle?mini=true&url=' + u }[a.getAttribute('data-share-to')];
    });
    var copy = dd.querySelector('[data-copy-link]');
    if (copy) copy.addEventListener('click', function (e) {
      e.preventDefault();
      (navigator.clipboard ? navigator.clipboard.writeText(link) : Promise.reject()).catch(function () { window.prompt('Copy this link', link); });
    });
  });

  /* The featured photo opens full size. */
  var g = document.querySelector('[data-ds-gallery]');
  if (g) g.addEventListener('click', function () {
    var box = document.createElement('div');
    box.className = 'ds-lightbox';
    var img = document.createElement('img');
    img.src = g.getAttribute('data-src');
    img.alt = '';
    box.appendChild(img);
    function close() { box.remove(); document.removeEventListener('keydown', onKey); }
    function onKey(e) { if (e.key === 'Escape') close(); }
    box.addEventListener('click', close);
    document.addEventListener('keydown', onKey);
    body.appendChild(box);
  });
})();

/* The menu marks the section the page belongs to, as the site does. */
(function () {
  var parts = window.location.pathname.split('/');
  if (parts[1] !== 'en' || !parts[2]) return;
  var here = '/en/' + parts[2];
  document.querySelectorAll('#site-nav-header a[href="' + here + '"]').forEach(function (a) {
    a.classList.add('is-active');
    a.setAttribute('aria-current', 'page');
  });
  var sub = parts[3] ? here + '/' + parts[3] : '';
  if (sub) document.querySelectorAll('.block-breadcrumb-block a[href="' + sub + '"]').forEach(function (a) { a.classList.add('is-active'); });
})();
