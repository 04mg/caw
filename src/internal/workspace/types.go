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