# Brand

| File | Use |
|---|---|
| `icon.svg` | The icon: favicons, avatars, app and tab icons. Readable down to 16 px. |
| `logo.svg` | Icon and wordmark, for light backgrounds. |
| `social-preview.svg`, `social-preview.png` | 1280 × 640 card for the repository's GitHub social preview and for link previews elsewhere. The SVG is the source; the PNG is its render, under 1 MB. |

The icon follows the uBix family tile (uBixSys blue `#0f62fe`, white shapes at full and
55% opacity). A white crook stands over a 2 × 2 flock: the one voice, and the agents it
keeps in their lanes. The crook is also the family's name for the policy layer that pulls
a stray agent back (see [naming.md](../naming.md)).

The wordmark is IBM Plex Sans, `uBix` light and `Shepherd` semibold. In SVG it is live
text that falls back to Helvetica or Arial where Plex is not installed.

## GitHub social preview

GitHub has no API for this setting, so a person uploads `social-preview.png` by hand:
Settings → General → Social preview → Upload an image.

Regenerate the PNG after editing the SVG:

```sh
rsvg-convert -w 1280 -h 640 docs/brand/social-preview.svg -o docs/brand/social-preview.png
```

The SVG names IBM Plex Sans and falls back to Helvetica Neue or Arial. Install IBM Plex
Sans (static TTF, weights 300 and 600) before rendering if you want the exact face.

## Use of the brand

These files are in use by the project and are not covered by the code license. See
[TRADEMARKS.md](../../TRADEMARKS.md).
