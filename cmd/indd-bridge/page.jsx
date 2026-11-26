// Lays out one e-paper page in Adobe InDesign from L (put above this script
// by indd-bridge) and saves it as L.out. Runs inside InDesign (ExtendScript,
// so ES3: plain loops, no JSON). One A3 page with the e-paper's margins and
// its six columns, then the frames in the order the browser drew them:
// fills, photos, rules, text. Each text frame gets the paragraphs' own type;
// a face the computer lacks is stood in for (see fontsFor in main.go).
// It runs as one undo step (FAST_ENTIRE_SCRIPT), which makes InDesign set
// the page several times faster (see the end).
function makePage() {
  var old = app.scriptPreferences.userInteractionLevel;
  app.scriptPreferences.userInteractionLevel = UserInteractionLevels.NEVER_INTERACT;
  var doc = null, standIns = {}, result;
  try {
    var k = L.mm, pt = L.mm * 72 / 25.4; // page pixel -> mm, -> points
    doc = app.documents.add(false);
    doc.viewPreferences.horizontalMeasurementUnits = MeasurementUnits.MILLIMETERS;
    doc.viewPreferences.verticalMeasurementUnits = MeasurementUnits.MILLIMETERS;
    doc.viewPreferences.rulerOrigin = RulerOrigin.PAGE_ORIGIN;
    doc.zeroPoint = [0, 0];
    var dp = doc.documentPreferences;
    dp.facingPages = false;
    dp.pagesPerDocument = 1;
    dp.pageWidth = L.width * k;
    dp.pageHeight = L.height * k;
    doc.textPreferences.typographersQuotes = false;
    var page = doc.pages[0];
    var mg = L.margins;
    var setMargins = function (mp) {
      mp.top = mg[0] * k; mp.right = mg[1] * k; mp.bottom = mg[2] * k; mp.left = mg[3] * k;
      mp.columnCount = 6; mp.columnGutter = 0;
    };
    try { setMargins(doc.masterSpreads[0].pages[0].marginPreferences); } catch (e) {}
    setMargins(page.marginPreferences);

    // Swatches are kept by name and found again each time: in one undo step
    // (FAST_ENTIRE_SCRIPT) a reference kept to a new colour goes stale.
    var swatches = {};
    var colour = function (hex) {
      if (!hex) hex = "#111111";
      var name = swatches[hex];
      if (!name) {
        var r = parseInt(hex.substr(1, 2), 16), g = parseInt(hex.substr(3, 2), 16), b = parseInt(hex.substr(5, 2), 16);
        if (r < 40 && g < 40 && b < 40) {
          name = "Black"; // near-black prints as black ink
        } else if (r > 250 && g > 250 && b > 250) {
          name = "Paper";
        } else {
          name = "RGB " + r + " " + g + " " + b;
          if (!doc.colors.itemByName(name).isValid) doc.colors.add({ name: name, model: ColorModel.PROCESS, space: ColorSpace.RGB, colorValue: [r, g, b] });
        }
        swatches[hex] = name;
      }
      return doc.swatches.itemByName(name);
    };
    var bounds = function (it) { return [it.y * k, it.x * k, (it.y + it.h) * k, (it.x + it.w) * k]; };
    var JUST = { left: Justification.LEFT_ALIGN, right: Justification.RIGHT_ALIGN, center: Justification.CENTER_ALIGN, justify: Justification.LEFT_JUSTIFIED };

    // The first of the paragraph's fonts this computer has (each looked up
    // once).
    var fontCache = {};
    var fontOf = function (name) {
      if (!fontCache.hasOwnProperty(name)) {
        var f = app.fonts.itemByName(name);
        fontCache[name] = f.isValid ? f : null;
      }
      return fontCache[name];
    };
    var applyFont = function (p, s) {
      for (var i = 0; i < s.fonts.length; i++) {
        var f = fontOf(s.fonts[i]);
        if (f) {
          try { p.appliedFont = f; } catch (e) { continue; }
          if (i > 0) standIns[s.fonts[0].replace("\t", " ") + " -> " + s.fonts[i].replace("\t", " ")] = 1;
          return i;
        }
      }
      standIns[s.fonts[0].replace("\t", " ") + " -> InDesign's default"] = 1;
      return -1;
    };
    var style = function (p, s, hscale) {
      var used = applyFont(p, s);
      var hs = hscale || 100;
      if (s.cond > 0 && used >= s.cond) hs = hs * (s.narrow || 1);
      p.horizontalScale = hs;
      p.pointSize = Math.max(1, s.size * pt);
      p.leading = Math.max(1, s.lead * pt);
      p.justification = JUST[s.align] || Justification.LEFT_ALIGN;
      p.fillColor = colour(s.color);
      p.firstLineIndent = (s.indent || 0) * k;
      p.tracking = s.track || 0;
      p.spaceBefore = (s.before || 0) * k;
      p.spaceAfter = (s.after || 0) * k;
      p.hyphenation = s.align === "justify";
      if (s.rules) { // the pull quote, between rules
        p.ruleAbove = true; p.ruleAboveLineWeight = 0.75; p.ruleAboveColor = colour("#111111"); p.ruleAboveOffset = s.lead * k;
        p.ruleBelow = true; p.ruleBelowLineWeight = 0.75; p.ruleBelowColor = colour("#111111"); p.ruleBelowOffset = s.lead * 0.45 * k;
      }
    };

    var textFrame = function (it) {
      var tf = page.textFrames.add({ geometricBounds: bounds(it) });
      var tp = tf.textFramePreferences;
      tp.insetSpacing = [0, 0, 0, 0];
      tp.ignoreWrap = true;
      if (it.cols > 1) { tp.textColumnCount = it.cols; tp.textColumnGutter = it.gap * k; }
      var parts = [];
      for (var i = 0; i < it.paras.length; i++) parts.push(it.paras[i].t);
      tf.contents = parts.join("\r");
      var ps = tf.parentStory.paragraphs;
      for (var j = 0; j < it.paras.length && j < ps.length; j++) style(ps[j], it.paras[j], it.hscale);
      // A headline or label grows down rather than hide a line; one that
      // never wraps grows sideways too, from the side it is set against.
      // Then, where a stand-in face runs wider than the e-paper's, the type
      // is narrowed until the frame is the e-paper's width again (a label)
      // or takes no more lines than there (a headline).
      if (it.fit) {
        try {
          var al = it.paras[0].align;
          tp.autoSizingReferencePoint = !it.one || al === "left" || al === "justify" ? AutoSizingReferenceEnum.TOP_LEFT_POINT :
            al === "right" ? AutoSizingReferenceEnum.TOP_RIGHT_POINT : AutoSizingReferenceEnum.TOP_CENTER_POINT;
          if (it.one) tp.useNoLineBreaksForAutoSizing = true;
          tp.autoSizingType = it.one ? AutoSizingTypeEnum.HEIGHT_AND_WIDTH : AutoSizingTypeEnum.HEIGHT_ONLY;
          var want = it.one ? it.w * k : it.h * k, size = function () {
            var g = tf.geometricBounds;
            return it.one ? g[3] - g[1] : g[2] - g[0];
          };
          for (var tries = 0; tries < 12 && size() > want * 1.04; tries++) {
            var f = it.one ? Math.max(0.5, want / size()) : 0.95;
            for (var q = 0; q < ps.length; q++) {
              if (ps[q].horizontalScale * f < 55) { tries = 99; break; }
              ps[q].horizontalScale = ps[q].horizontalScale * f;
            }
          }
        } catch (e) {}
      }
    };
    var image = function (it) {
      var r = page.rectangles.add({ geometricBounds: bounds(it) });
      r.strokeWeight = 0;
      var f = it.file ? File(it.file) : null;
      if (f && f.exists) {
        try {
          r.place(f);
          r.fit(FitOptions.FILL_PROPORTIONALLY);
          r.fit(FitOptions.CENTER_CONTENT);
          return;
        } catch (e) {}
      }
      r.fillColor = colour("#e6e6e6"); // the photo could not be had
    };
    var rule = function (it) {
      var ln = page.graphicLines.add();
      ln.paths[0].entirePath = [[it.x * k, it.y * k], [it.x2 * k, it.y2 * k]];
      ln.strokeWeight = Math.max(0.1, (it.sw || 1) * pt);
      ln.strokeColor = colour(it.color);
      if (it.double) { try { ln.strokeType = "Thin - Thin"; } catch (e) {} }
    };
    var fill = function (it) {
      var r = page.rectangles.add({ geometricBounds: bounds(it) });
      r.strokeWeight = 0;
      r.fillColor = colour(it.fill);
    };

    for (var n = 0; n < L.items.length; n++) {
      var it = L.items[n];
      if (it.k === "rect") fill(it);
      else if (it.k === "img") image(it);
      else if (it.k === "line") rule(it);
      else if (it.k === "text") textFrame(it);
    }
    doc.save(File(L.out));
    doc.close(SaveOptions.NO);
    doc = null;
    var notes = [];
    for (var s in standIns) notes.push(s);
    result = (notes.length ? "fonts stood in: " + notes.join("; ") + "\n" : "") + "OK";
  } catch (e) {
    result = "ERR " + e.message + " (script line " + e.line + ")";
    try { if (doc && doc.isValid) doc.close(SaveOptions.NO); } catch (e2) {}
  }
  app.scriptPreferences.userInteractionLevel = old;
  return result;
}

var R;
app.doScript(function () { R = makePage(); }, ScriptLanguage.JAVASCRIPT, undefined, UndoModes.ENTIRE_SCRIPT, "Daily Star page");
R;
