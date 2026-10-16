package apicontract

type LocalMarkdownConfig struct {
	Enabled bool   `json:"enabled"`
	Box     string `json:"box"`
	Root    string `json:"root"`
	Target  string `json:"target"`
}

type LocalMarkdownRequest struct {
	Action         string               `json:"action" api:"optional"`
	Config         *LocalMarkdownConfig `json:"config" api:"optional"`
	ID             string               `json:"id" api:"optional"`
	Path           string               `json:"path" api:"optional"`
	Revision       *string              `json:"revision" api:"optional,nonnullable"`
	AcceptExternal bool                 `json:"acceptExternal" api:"optional"`
}

type LocalMarkdownFile struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Revision string `json:"revision"`
	Linked   bool   `json:"linked"`
}

type LocalMarkdownEntry struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	State       string `json:"state"`
	Error       string `json:"error"`
	Conflict    string `json:"conflict"`
	LastSuccess string `json:"lastSuccess"`
}

type LocalMarkdownDocument struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Exists bool   `json:"exists"`
}

type LocalMarkdownData struct {
	Changed     bool                    `json:"changed"`
	File        *LocalMarkdownFile      `json:"file,omitempty"`
	Config      LocalMarkdownConfig     `json:"config"`
	State       string                  `json:"state"`
	Error       string                  `json:"error"`
	LastSuccess string                  `json:"lastSuccess"`
	Pending     int                     `json:"pending"`
	Entries     []LocalMarkdownEntry    `json:"entries"`
	Documents   []LocalMarkdownDocument `json:"documents"`
}
