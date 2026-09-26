package spec

import "runtime"

// unixModeBits reports whether file mode bits carry privacy on this OS. On
// Windows Go reports 0777 for every directory (there are no Unix permission
// bits; the user profile's ACL makes a control root private), so a
// mode-bit check could never pass there.
var unixModeBits = runtime.GOOS != "windows"
