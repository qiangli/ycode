package ycodecli

type configuredTool struct {
	Name         string   `json:"name"`
	Contract     string   `json:"contract"`
	RequestType  string   `json:"requestType"`
	ResultType   string   `json:"resultType"`
	Operations   []string `json:"operations"`
	Effects      []string `json:"effectsCeiling"`
	Permission   string   `json:"permissionCeiling"`
	Preflight    string   `json:"preflight"`
	HostFallback string   `json:"hostFallback"`
}
