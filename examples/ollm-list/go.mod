module example/ollm-list

go 1.26.2

require github.com/qiangli/ycode/pkg/ollm v0.0.0

require (
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/ollama/ollama v0.21.2 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	golang.org/x/crypto v0.43.0 // indirect
	golang.org/x/sys v0.37.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// For local development, point to the local pkg/ollm module.
// External consumers: replace this with the published version, e.g.:
//   require github.com/qiangli/ycode/pkg/ollm v0.1.0
replace github.com/qiangli/ycode/pkg/ollm => ../../pkg/ollm

// Required: the ollama fork replace directive.
// Copy this into your own go.mod when importing pkg/ollm.
replace github.com/ollama/ollama => github.com/qiangli/ollama v0.0.0-20260426003157-db9cd0a2004b
