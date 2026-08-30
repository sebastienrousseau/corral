# Vendored theme: Lucid

These layouts are a copy of the **Lucid** documentation theme from
<https://github.com/sebastienrousseau/ssg-themes.github.io> (`themes/lucid`),
licensed MIT or Apache-2.0.

They are vendored rather than fetched so the documentation build has no
network dependency on another repository and cannot break when that theme
changes. The cost is that fixes upstream do not arrive on their own: to
resync, copy `themes/lucid/_layouts/` over this directory and rebuild.

The theme holds every WCAG 2.2 AAA criterion a theme can determine on its
own, verified in that repository by `make check-aaa`. Editing these files
without re-running those gates can silently break that.
