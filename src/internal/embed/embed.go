package embed

import "embed"

//go:embed frontend/dist
var FrontendFS embed.FS

//go:embed icon.txt
var IconTxt string
