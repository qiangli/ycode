module example/ollm-list

go 1.26.2

require github.com/qiangli/coreutils v0.0.0

require (
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/ollama/ollama v0.21.2 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	golang.org/x/crypto v0.50.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// For local development, point to the local coreutils module.
// External consumers: replace this with the published version, e.g.:
//   require github.com/qiangli/coreutils v0.1.0
replace github.com/qiangli/coreutils => ../../../coreutils

// Required for local development: use the Ollama source owned by coreutils.
// External consumers should follow the coreutils release notes for the
// supported github.com/ollama/ollama version or replace target.
replace github.com/ollama/ollama => ../../../coreutils/external/ollama/src
