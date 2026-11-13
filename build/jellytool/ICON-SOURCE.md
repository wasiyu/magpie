# Jellytool icon

## Desktop application and Dock

The user selected the A2 soft-matte concept on 2026-11-10: a white rounded-square
tile with a pink jelly face. It was generated with the built-in imagegen tool.

- Original concept: `assets/jellytool-icon-concepts/2026-11-10/round-2/A2-soft-matte.png`.
- Original size: 1254 × 1254 PNG with transparency.
- Embedded desktop asset: `internal/gui/assets/icons/jellytool-app.png`, resized
  to 1024 × 1024 with macOS `sips`, preserving transparency.
- Original SHA-256: `3a0c580d97f893ac57ff4da46778ab8571f7478094ba8f93c1674c81db97f839`.
- Embedded SHA-256: `ab439ea40f6e7860fca6a315fedb5e59d67abdcbe38b24a98db061b7ca31d1db`.

`jellytoolAppIcon` supplies this artwork to the native application. The packager
converts the same embedded PNG to ICNS using macOS `sips` and `iconutil`, offline.
This first local preview uses the selected bitmap, not an Icon Composer project.

## Website brand and tray glyph source

The page brand, favicon, Jellytoken supplier tile and tray glyph source retain
the square apple-touch icon linked by the Jellytoken homepage:

- Homepage: https://www.jellytoken.com/
- Source: https://www.jellytoken.com/apple-icon.png?868a7d61201ed975
- Retrieved: 2026-11-04
- Original size: 256 × 256 PNG, transparency preserved.
- Repository asset: `internal/gui/assets/icons/jellytoken.png`.

The monochrome menu bar glyph is derived from this original artwork; the
Windows tray uses its full-colour version.

Source SHA-256: `c42521c669ba7928af9c50954d368549fff210fef3421aa4d66bbc22e18f3ebd`.
