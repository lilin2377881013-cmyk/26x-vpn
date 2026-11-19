# 26x app icons

- `26x-icon-original.jpg` is the supplied original artwork.
- `26x-icon-transparent.png` is the macOS icon master with transparent outer corners. Keep its alpha channel when resizing the macOS app icon assets; an opaque background produces a black square in the Dock.

The transparent master was edited using the built-in imagegen tool with this prompt:

> Remove only the opaque black background outside the luminous blue rounded-square icon boundary, especially all four outer corners. Make those outside pixels genuinely transparent. Preserve the rounded blue square, shield, globe, cyan orbit, lock, glow, and the text “26X” and “VPN”. Do not extract the shield alone or add a border, shadow, padding, or backdrop.

The macOS PNG sizes are generated from the transparent master with `sips -z SIZE SIZE`. The original JPEG is retained for platforms requiring an opaque icon.
