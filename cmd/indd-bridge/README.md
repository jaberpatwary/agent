# InDesign bridge: e-paper pages as `.indd`

The e-paper's **Id** button (in the tool bar) downloads the page on screen as
an Adobe InDesign document (`.indd`). Only InDesign itself can write a
`.indd`, so the file is made on the reader's own Windows computer, by the
InDesign installed there:

```
e-paper page in the browser ──(page layout, JSON)──▶ indd-bridge.exe on 127.0.0.1:8095
                                                         │ downloads the photos
                                                         │ runs page.jsx in InDesign (COM)
browser saves  DailyStar_<date>_P<n>_<PAGE>.indd ◀───────┘ InDesign saves the .indd
```

The browser sends each frame's place and size, its text, type faces and
sizes, the photos and the rules, exactly as the page is drawn. InDesign
builds one A3 page (297 × 420 mm, six columns) with real, editable text
frames (columns, justified text, pull quotes between rules), placed photos
and drawn rules.

## Setting up a computer

1. Adobe InDesign must be installed (tested with InDesign CC 2018, 13.1).
2. Build the bridge once (or copy `indd-bridge.exe` from IT):

   ```
   go build -o indd-bridge.exe ./cmd/indd-bridge
   ```

3. Start it and keep it running while using the e-paper (double-click
   `indd-bridge.exe`, or start it from `start-indd-bridge.bat` in the Windows
   Startup folder to run it at sign-in).
4. Open the e-paper, go to a page, press **Id**. The first time, Chrome may
   ask to let the site reach this computer: allow it.

Options: `-addr` (default `127.0.0.1:8095`), `-origins` (more e-paper sites
allowed besides the hosted portal and `localhost:8081`), `-save <folder>`
(keep a copy of every file made).

## Fonts

The pages are set in The Daily Star's faces: **Denton Condensed** (headlines),
**Tinos** (text), **Fira Sans Extra Condensed** (teasers). Install them on the
computer for the exact look. Where one is missing the bridge uses Times New
Roman / Arial Narrow and narrows it so lines break about where they do on the
page; InDesign lists those under *Type › Find Font*, and installing the face
afterwards brings the real one back.

## Notes

- The bridge listens on 127.0.0.1 only and answers only the e-paper's own
  sites; it runs nothing but the page script it carries.
- Photos come from the outlets' sites ("Photo: Collected"); check the rights
  before printing them.
- A story cut short on the e-paper is cut short in the frame too, so InDesign
  may show the red overset mark on long stories: the rest of the text is in
  the frame, ready to be flowed on.
