package imgs

//go:generate go run ../cmd/gen-icons/main.go

import _ "embed"

//go:embed boabot-icon.png
var BoabotIcon []byte

//go:embed boabot-icon-processed.png
var ProcessedIcon []byte

//go:embed boabot-icon-favicon.png
var FaviconIcon []byte
