package tools

import "os"

// statFile is the file stat function used by redirect rules.
// Replaceable in tests via the statFileFunc variable.
var statFileFunc = os.Stat

func statFile(path string) (os.FileInfo, error) {
	return statFileFunc(path)
}
