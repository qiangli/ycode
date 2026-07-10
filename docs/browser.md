# Browser modes — moved

Browser automation has been **removed from ycode**. The pure-Go browser
stack (navigate / click / type / screenshot / extract / eval / …) now
lives in **bashy** as the `bashy browser` command (backed by
`coreutils/pkg/browser`).

ycode no longer registers `browser_*` tools, no longer hosts a browser
hub in `ycode serve`, and no longer ships the live Chrome extension.

To drive a web page, use `bashy browser` instead. See the bashy
documentation for the current tool surface and setup.
