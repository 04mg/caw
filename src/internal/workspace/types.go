package workspace

type FileNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	IsDir    bool       `json:"isDir"`
	Children []FileNode `json:"children,omitempty"`
}

type WriteRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type RenameRequest struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

type CopyRequest struct {
	SourcePath string `json:"sourcePath"`
	DestPath   string `json:"destPath"`
}

type DeleteRequest struct {
	Path string `json:"path"`
}

type CreateRequest struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

type PasteRequest struct {
	SourcePath string `json:"sourcePath"`
	TargetDir  string `json:"targetDir"`
}

type MultiDownloadRequest struct {
	Paths []string `json:"paths"`
}

type SearchHit struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`   // 1-based
	Column  int    `json:"column"` // 0-based byte offset within the line
	Preview string `json:"preview"`
}

type SearchContentResponse struct {
	Results   []SearchHit `json:"results"`
	Truncated bool        `json:"truncated"`
}

type SearchContentRequest struct {
	Root          string `json:"root"`
	Query         string `json:"query"`
	Regex         bool   `json:"regex"`
	CaseSensitive bool   `json:"caseSensitive"`
}

type ReplaceRequest struct {
	Root          string `json:"root"`
	Query         string `json:"query"`
	Replace       string `json:"replace"`
	Regex         bool   `json:"regex"`
	CaseSensitive bool   `json:"caseSensitive"`
}

type ReplaceResponse struct {
	Files        []string `json:"files"`
	Replacements int      `json:"replacements"`
}

type ReplaceBackup struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}