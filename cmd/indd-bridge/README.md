# InDesign bridge: e-paper pages as `.indd`

The e-paper's **Id** button (in the tool bar) downloads the page on screen as
an Adobe InDesign document (`.indd`). The button is there only when the
e-paper is opened on the computer the portal runs on
(`http://localhost:8081/epaper` or `127.0.0.1:8081`); the hosted site
(bangla-news-portal.onrender.com) does not show it. Only InDesign itself can
write a `.indd`, so the file is made on that Windows computer, by the
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
4. Open the e-paper on this computer (`http://localhost:8081/epaper`), go to
   a page, press **Id**.

Options: `-addr` (default `127.0.0.1:8095`), `-origins` (more e-paper sites
allowed besides `http://localhost:8081` and `http://127.0.0.1:8081`),
`-save <folder>` (keep a copy of every file made).

## How long it takes

- InDesign already open: about 4–6 seconds a page.
- InDesign closed: it has to open first (about 10 seconds on this computer),
  so the first page takes about 13–16 seconds. To save that wait the
  e-paper asks the bridge to open InDesign as soon as the pointer comes to the
  **Id** button, and, once the button has been used in that browser, as soon
  as the e-paper opens. InDesign is left open afterwards.
- Photos are fetched from the outlets' sites once and kept for a week
  (`%LOCALAPPDATA%\DailyStar\indd-bridge\photos`); a site that takes more
  than 10 seconds leaves its photo frame grey rather than hold up the page.
- While the page is made, the e-paper shows what is happening and the
  seconds gone; the bridge logs each page's timings to
  `%LOCALAPPDATA%\DailyStar\indd-bridge\bridge.log`.

## Fonts

The pages are set in The Daily Star's faces: **Denton Condensed** (headlines),
**Tinos** (text), **Fira Sans Extra Condensed** (teasers). Install them on the
computer for the exact look. Where one is missing the bridge uses Times New
Roman / Arial Narrow and narrows it so lines break about where they do on the
page; InDesign lists those under *Type › Find Font*, and installing the face
afterwards brings the real one back.

## Notes

- The bridge listens on 127.0.0.1 only and answers only the e-paper opened
  on this computer; it runs nothing but the page script it carries.
- Photos come from the outlets' sites ("Photo: Collected"); check the rights
  before printing them.
- A story cut short on the e-paper is cut short in the frame too, so InDesign
  may show the red overset mark on long stories: the rest of the text is in
  the frame, ready to be flowed on.
